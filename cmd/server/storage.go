package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Transaction struct {
	Channel     string
	Source      string
	TargetUser  string
	TargetTopic string
	Value       int
	Timestamp   time.Time
}

type TransactionSink interface {
	Insert(ctx context.Context, t Transaction) error
}

type ClickhouseSink struct {
	CHConn driver.Conn
}

func (c *ClickhouseSink) Insert(ctx context.Context, t Transaction) error {
	slog.Info("inserting transaction", "transaction", t)
	err := c.CHConn.Exec(ctx, `
    INSERT INTO pulse.checkin (channel, source, target_user, target_topic, value, timestamp)
    VALUES (?, ?, ?, ?, ?, ?)
  `, t.Channel, t.Source, t.TargetUser, t.TargetTopic, t.Value, t.Timestamp)
	if err != nil {
		return err
	}
	return nil
}

type PrintSink struct{}

func (p *PrintSink) Insert(ctx context.Context, t Transaction) error {
	slog.Info("transaction", "transaction", t)
	return nil
}
