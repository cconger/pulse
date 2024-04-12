package main

import (
	"container/list"
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"

	twitchirc "github.com/gempir/go-twitch-irc/v4"
)

type User struct {
	ID          string
	Login       string
	DisplayName string
}

// ChatHandler is a Transaction source, that
type ChatHandler struct {
	RootContext context.Context
	UserCache   *UserCache
	TSink       TransactionSink
}

var (
	numberMatcher = regexp.MustCompile(`[+-][12]`)
	topicMatcher  = regexp.MustCompile(`[@#][a-zA-Z]+`)
)

type match struct {
	Value int
	User  string
	Topic string
}

func matchMessage(m string) *match {
	v := numberMatcher.FindString(m)
	if v == "" {
		return nil
	}

	val, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}

	ma := &match{
		Value: val,
	}

	tmatch := topicMatcher.FindString(m)
	if strings.HasPrefix(tmatch, "@") {
		ma.User = tmatch[1:]
	}
	if strings.HasPrefix(tmatch, "#") {
		ma.Topic = tmatch[1:]
	}

	return ma
}

func (c *ChatHandler) HandleMessage(m twitchirc.PrivateMessage) {
	chatMessages.WithLabelValues(m.Channel).Inc()
	ctx := c.RootContext
	// All Messages should hydrate the usercache
	c.UserCache.Insert(&User{
		ID:          m.User.ID,
		DisplayName: m.User.DisplayName,
		Login:       m.User.Name,
	})

	// Parse message
	match := matchMessage(m.Message)
	if match == nil {
		slog.Debug("Dropping message", "message", m.Message)
		return
	}

	if match.Value == 0 {
		slog.Warn("parsed a vote but value was 0", "message", m.Message)
		return
	}

	tt := "anon"
	targetUserID := ""
	targetTopic := match.Topic
	if match.Topic != "" {
		tt = "topic"
	} else {
		targetUserID = m.RoomID
		if match.User != "" {
			u, err := c.UserCache.GetByDisplayName(ctx, match.User)
			if err != nil {
				slog.Error("loading user", "DisplayName", match.User, "err", err)
				return
			}
			targetUserID = u.ID
		}
	}

	votesProcessed.WithLabelValues(m.Channel, tt).Inc()

	t := Transaction{
		Channel:     m.RoomID,
		Source:      m.User.ID,
		TargetUser:  targetUserID,
		TargetTopic: targetTopic,
		Value:       match.Value,
		Timestamp:   m.Time,
	}

	err := c.TSink.Insert(ctx, t)
	if err != nil {
		slog.Error("inserting transaction", "err", err, "transaction", t)
		return
	}
}

type UserCache struct {
	ByDisplayName map[string]*list.Element
	Users         *list.List
	Limit         int
	BackfillFn    UserLoadingFunction

	cacheLock sync.Mutex
}

func NewUserCache(limit int, backfillFn UserLoadingFunction) *UserCache {
	// TODO: Instrument this so we can see how big the cache is
	return &UserCache{
		ByDisplayName: make(map[string]*list.Element),
		Users:         list.New(),
		Limit:         limit,
		BackfillFn:    backfillFn,
	}
}

type UserLoadingFunction func(ctx context.Context, username string) (*User, error)

func (c *UserCache) Insert(user *User) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	if c.Users.Len() >= c.Limit {
		last := c.Users.Back()
		c.Users.Remove(last)
		delete(c.ByDisplayName, last.Value.(*User).DisplayName)
	}

	f := c.Users.PushFront(user)
	c.ByDisplayName[user.DisplayName] = f
}

func (c *UserCache) GetByDisplayName(ctx context.Context, id string) (*User, error) {
	c.cacheLock.Lock()
	if userEl, ok := c.ByDisplayName[id]; ok {
		c.Users.MoveToFront(userEl)
		user := userEl.Value.(*User)
		c.cacheLock.Unlock()
		return user, nil
	}
	c.cacheLock.Unlock()

	// Backfill
	user, err := c.BackfillFn(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Insert(user)

	return user, nil
}
