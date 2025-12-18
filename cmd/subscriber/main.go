package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := Config{}

	flag.StringVar(&cfg.BrokerURL, "broker", "ws://localhost:8080/ws", "broker ws url")

	// Keep backward compatibility: -topic is the orderbook topic.
	flag.StringVar(&cfg.OrderbookTopic, "topic", "orderbook.band.BNBBTC", "orderbook topic to subscribe (e.g. orderbook.band.BNBBTC)")
	flag.StringVar(&cfg.TradesTopic, "trades-topic", "", "trades topic to subscribe (e.g. trades.BNBBTC). empty disables trades")

	flag.IntVar(&cfg.Rows, "rows", 20, "number of levels per side to display")
	flag.DurationVar(&cfg.Interval, "interval", 1*time.Second, "ui refresh interval (default 1s)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// shutdown signals
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	if err := Run(ctx, cfg); err != nil && ctx.Err() == nil {
		log.Fatalf("subscriber error: %v", err)
	}
}
