package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	twclient "github.com/cconger/pulse/pkg/twitch"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gempir/go-twitch-irc/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func clickhouseClient(ctx context.Context, addr string, auth clickhouse.Auth) (driver.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: auth,
		ClientInfo: clickhouse.ClientInfo{
			Products: []struct {
				Name    string
				Version string
			}{
				{"pulse", "0.1"},
			},
		},
		Debugf: func(format string, v ...interface{}) {
			slog.Debug(format, "params", v)
		},
		/*
			TLS: &tls.Config{
				InsecureSkipVerify: true,
			},
		*/
	})
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(ctx); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			fmt.Printf("Exception [%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		}
		return nil, err
	}
	return conn, nil
}

type UserResolver struct {
	TwitchClient *twclient.Client
}

func (c *UserResolver) lookupUserByDisplayName(ctx context.Context, displayName string) (*User, error) {
	slog.Info("Looking up user by display name", "displayName", displayName)

	users, err := c.TwitchClient.GetUsersByLogin(ctx, strings.ToLower(displayName))
	if err != nil {
		return nil, err
	}

	slog.Info("getting user response", "users", users)
	if users == nil || len(users) < 1 {
		slog.Error("no users found", "displayName", displayName)
		return nil, fmt.Errorf("no user found")
	}

	return &User{
		ID:          users[0].ID,
		DisplayName: users[0].DisplayName,
		Login:       users[0].Login,
	}, nil
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	registerChatMetrics(reg)

	clientID := os.Getenv("TWITCH_CLIENT_ID")
	clientSecret := os.Getenv("TWITCH_SECRET")

	client, err := twclient.NewClient(clientID, clientSecret, &http.Client{})
	if err != nil {
		panic(err)
	}

	clickhouseAddr := os.Getenv("CH_ADDR")
	if clickhouseAddr == "" {
		clickhouseAddr = "localhost:9000"
	}

	chconn, err := clickhouseClient(ctx, clickhouseAddr, clickhouse.Auth{
		Database: "pulse",
		Username: os.Getenv("CH_USER"),
		Password: os.Getenv("CH_PASSWORD"),
	})
	if err != nil {
		panic(err)
	}

	tSink := &ClickhouseSink{CHConn: chconn}
	psMiddleware := NewPubSubMiddleware(tSink)

	oauth := os.Getenv("TWITCH_OAUTH")
	c := twitch.NewClient("shindaggers", "oauth:"+oauth)
	c.Join("shindaggers", "shindigs", "northernlion")

	userResolver := &UserResolver{
		TwitchClient: client,
	}

	handler := ChatHandler{
		RootContext: context.Background(),
		UserCache: NewUserCache(
			10000,
			userResolver.lookupUserByDisplayName,
		),
		TSink: psMiddleware,
	}
	c.OnConnect(func() {
		slog.Info("connected to twitch irc")
	})
	c.OnPrivateMessage(handler.HandleMessage)

	mux := http.NewServeMux()

	// Use id=39214310
	mux.HandleFunc("/balance/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		slog.Info("got request", "request", r)

		res := chconn.QueryRow(r.Context(), "SELECT SUM(value) FROM pulse.checkin WHERE channel = ? and target_user = ?", id, id)

		if res.Err() != nil {
			json.NewEncoder(w).Encode(res.Err())
		}

		var balance int64 = 0
		err := res.Scan(&balance)
		if err != nil {
			json.NewEncoder(w).Encode(err)
		}

		json.NewEncoder(w).Encode(balance)
	})

	mux.HandleFunc("GET /stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.NotFound(w, r)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.NotFound(w, r)
			return
		}
		// Register with pubsub to get live events
		c, unsub := psMiddleware.Subscribe(r.Context(), id)
		defer unsub()

		// Send the initial headers saying we're gonna stream the response.
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		enc := json.NewEncoder(w)

		for {
			select {
			case <-r.Context().Done():
				return
			case trans := <-c:
				err := enc.Encode(trans)
				if err != nil {
					slog.Error("encoding transaction", "err", err)
				}
				flusher.Flush()
			}
		}
	})

	port := os.Getenv("PORT")
	port = ":" + port
	s := &http.Server{
		Addr:           port,
		Handler:        mux,
		ErrorLog:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   1 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}

	promMux := http.NewServeMux()

	promMux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	promPort := ":" + os.Getenv("PROM_PORT")
	proms := &http.Server{
		Addr:           promPort,
		Handler:        promMux,
		ErrorLog:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		if err := s.ListenAndServe(); err != nil {
			panic(err)
		}
	}()

	go func() {
		if err := proms.ListenAndServe(); err != nil {
			panic(err)
		}
	}()

	err = c.Connect()
	if err != nil {
		slog.Error("twitch irc", "err", err)
		return
	}
}
