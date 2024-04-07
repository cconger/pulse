package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var chatMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "chat_messages_total",
	Help: "Total number of chat messages processed",
}, []string{"channel"})

var votesProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "chat_votes_total",
	Help: "Total number of chats that yielded in a vote",
}, []string{"channel", "type"})

func registerChatMetrics(reg *prometheus.Registry) {
	reg.MustRegister(
		chatMessages,
		votesProcessed,
	)
}
