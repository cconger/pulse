package main

import (
	"context"
	"fmt"
	"sync"
)

type PubSubMiddleware struct {
	Sink TransactionSink

	subscriberMutex sync.RWMutex
	subscribers     map[string][]chan Transaction
}

func NewPubSubMiddleware(sink TransactionSink) *PubSubMiddleware {
	// TODO: Instrument this so we can see how many subs we have
	return &PubSubMiddleware{
		Sink:        sink,
		subscribers: make(map[string][]chan Transaction),
	}
}

func (p *PubSubMiddleware) Subscribe(ctx context.Context, channel string) (chan Transaction, func() error) {
	ch := make(chan Transaction)

	p.subscriberMutex.Lock()
	defer p.subscriberMutex.Unlock()
	p.subscribers[channel] = append(p.subscribers[channel], ch)

	return ch, func() error {
		p.subscriberMutex.Lock()
		defer p.subscriberMutex.Unlock()

		for i, sub := range p.subscribers[channel] {
			if sub == ch {
				// Swap this chanel to the end and then truncate
				p.subscribers[channel][i] = p.subscribers[channel][len(p.subscribers[channel])-1]
				p.subscribers[channel] = p.subscribers[channel][:len(p.subscribers[channel])-1]
				close(ch)
				return nil
			}
		}
		return fmt.Errorf("channel not found")
	}
}

func (p *PubSubMiddleware) Insert(ctx context.Context, t Transaction) error {
	// First pass along
	err := p.Sink.Insert(ctx, t)
	if err != nil {
		return err
	}

	// Then notify anyone who is subbed
	p.subscriberMutex.RLock()
	defer p.subscriberMutex.RUnlock()
	channels := p.subscribers[t.Channel]
	for _, c := range channels {
		c <- t
	}

	return nil
}
