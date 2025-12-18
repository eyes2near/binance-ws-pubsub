package main

import "time"

type Config struct {
	BrokerURL string

	// Optional: if empty, don't subscribe/render this section.
	OrderbookTopic string
	TradesTopic    string

	Rows     int
	Interval time.Duration
}
