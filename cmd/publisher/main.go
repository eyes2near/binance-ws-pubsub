package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"binance-ws-pubsub/internal/publisher"
)

func main() {
	var (
		brokerURL = flag.String("broker", "ws://localhost:8080/ws", "broker ws url")

		// Market selection
		market   = flag.String("market", "spot", "market type: 'spot' or 'coin-m'")
		symbol   = flag.String("symbol", "BNBBTC", "spot symbol (e.g., BNBBTC) or coin-m base currency (e.g., BTC)")
		contract = flag.String("contract", "current", "for coin-m, which contract: 'current' or 'next'")

		// Publish toggles
		publishOrderbook = flag.Bool("orderbook", true, "publish orderbook band snapshots")
		publishTrades    = flag.Bool("trades", false, "publish trades stream")

		// Orderbook options
		band          = flag.Float64("band", 0.005, "band percent as fraction, 0.005 = 0.5%")
		maxLevels     = flag.Int("max-levels", 200, "max levels per side in published snapshot")
		snapshotLimit = flag.Int("snapshot-limit", 5000, "depth snapshot limit")

		// Endpoint overrides (optional)
		spotRestBase   = flag.String("spot-rest-base", "https://api.binance.com", "spot REST base url")
		spotStreamBase = flag.String("spot-stream-base", "wss://stream.binance.com:9443/ws", "spot WS base url")

		coinmRestBase   = flag.String("coinm-rest-base", "https://dapi.binance.com", "coin-m REST base url")
		coinmStreamBase = flag.String("coinm-stream-base", "wss://dstream.binance.com/ws", "coin-m WS base url")
	)

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	endpoints := publisher.Endpoints{
		SpotRestBase:    *spotRestBase,
		SpotStreamBase:  *spotStreamBase,
		CoinMRestBase:   *coinmRestBase,
		CoinMStreamBase: *coinmStreamBase,
	}

	rm, err := publisher.ResolveMarket(ctx, *market, *symbol, *contract, endpoints)
	if err != nil {
		log.Fatalf("resolve market failed: %v", err)
	}

	cfg := publisher.SupervisorConfig{
		BrokerURL:        *brokerURL,
		Market:           rm,
		PublishOrderbook: *publishOrderbook,
		PublishTrades:    *publishTrades,

		Orderbook: publisher.OrderbookConfig{
			Topic:         "orderbook.band." + rm.Symbol,
			BandPct:       *band,
			MaxLevels:     *maxLevels,
			SnapshotLimit: *snapshotLimit,
		},
		Trades: publisher.TradesConfig{
			Topic: "trades." + rm.Symbol,
		},
	}

	// Startup summary (bring back the "final topic" hints for subscribers)
	log.Printf("[publisher] broker=%s market=%s symbol=%s source=%s",
		cfg.BrokerURL, rm.MarketType, rm.Symbol, rm.SourceName)

	if cfg.PublishOrderbook {
		log.Printf("--> Publishing orderbook band to topic: %s", cfg.Orderbook.Topic)
	}
	if cfg.PublishTrades {
		log.Printf("--> Publishing trades to topic: %s", cfg.Trades.Topic)
	}

	if err := publisher.RunSupervisor(ctx, cfg); err != nil && ctx.Err() == nil {
		log.Fatalf("publisher exited with error: %v", err)
	}
}
