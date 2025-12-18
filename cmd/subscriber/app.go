package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"binance-ws-pubsub/internal/broker"
)

func Run(ctx context.Context, cfg Config) error {
	if cfg.BrokerURL == "" {
		return fmt.Errorf("missing -broker")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Second
	}
	if cfg.Rows <= 0 {
		cfg.Rows = 20
	}
	if cfg.OrderbookTopic == "" && cfg.TradesTopic == "" {
		return fmt.Errorf("no topics specified: set -topic and/or -trades-topic")
	}

	// UI init
	var ui *InPlaceUI = &InPlaceUI{}
	if err := ui.Init(); err != nil {
		log.Printf("ui init failed: %v, falling back to plain output", err)
		ui = nil
	}
	if ui != nil {
		defer ui.Close()
	}

	state := NewAppState(cfg)

	// Broker client with auto-reconnect
	topics := []string{cfg.OrderbookTopic, cfg.TradesTopic}
	bc := NewBrokerClient(cfg.BrokerURL, topics, func(st ConnStatus) {
		state.SetConnStatus(st)
		// Uncomment for debugging:
		// debugStatusLog(st)
	})
	msgCh := bc.Run(ctx)

	// Render loop: fixed interval (1s default)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				vm := state.SnapshotAndResetWindow()
				Render(ui, vm, cfg.Rows)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Message processing loop
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil

		case msg, ok := <-msgCh:
			if !ok {
				wg.Wait()
				return nil
			}

			switch msg.Type {
			case "ack":
				log.Printf("[ack] %s", msg.Message)
			case "error":
				log.Printf("[error] %s", msg.Message)
			case "event":
				if cfg.OrderbookTopic != "" && msg.Topic == cfg.OrderbookTopic {
					if ob, err := ParseOrderbookEvent(msg); err == nil {
						state.UpdateOrderbook(ob)
					} else {
						log.Printf("[warn] parse orderbook event failed: %v", err)
					}
					continue
				}
				if cfg.TradesTopic != "" && msg.Topic == cfg.TradesTopic {
					if te, err := ParseTradesEvent(msg); err == nil {
						state.UpdateTrade(te)
					} else {
						log.Printf("[warn] parse trades event failed: %v", err)
					}
					continue
				}
			}
		}
	}
}

// compile-time check: we still reference broker.ServerMessage in this file
var _ broker.ServerMessage
