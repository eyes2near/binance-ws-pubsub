package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"binance-ws-pubsub/internal/broker"

	"github.com/gorilla/websocket"
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

	c, err := dialWS(cfg.BrokerURL)
	if err != nil {
		return fmt.Errorf("dial broker failed: %w", err)
	}
	defer c.Close()

	// Subscribe topics
	if cfg.OrderbookTopic != "" {
		if err := sendSubscribe(c, cfg.OrderbookTopic); err != nil {
			return fmt.Errorf("subscribe orderbook failed: %w", err)
		}
	}
	if cfg.TradesTopic != "" {
		if err := sendSubscribe(c, cfg.TradesTopic); err != nil {
			return fmt.Errorf("subscribe trades failed: %w", err)
		}
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

	// Render loop: at most once per interval; trades are aggregated per-interval.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				view, ok := state.SnapshotAndResetWindow()
				if !ok {
					continue
				}
				Render(ui, view, cfg.Rows)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read loop
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		default:
		}

		_, data, err := c.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			wg.Wait()
			return fmt.Errorf("read failed: %w", err)
		}

		var msg broker.ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "ack":
			log.Printf("[ack] %s", msg.Message)
		case "error":
			log.Printf("[error] %s", msg.Message)
		case "event":
			handled := false

			if cfg.OrderbookTopic != "" && msg.Topic == cfg.OrderbookTopic {
				if ob, err := ParseOrderbookEvent(msg); err == nil {
					state.UpdateOrderbook(ob)
					handled = true
				} else {
					log.Printf("[warn] parse orderbook event failed: %v", err)
				}
			}

			if !handled && cfg.TradesTopic != "" && msg.Topic == cfg.TradesTopic {
				if te, err := ParseTradesEvent(msg); err == nil {
					state.UpdateTrade(te)
					handled = true
				} else {
					log.Printf("[warn] parse trades event failed: %v", err)
				}
			}

			if !handled {
				// Unknown topic/event type for this subscriber config; ignore silently.
			}
		}
	}
}

func sendSubscribe(c *websocket.Conn, topic string) error {
	sub := broker.ClientMessage{
		Type:  "subscribe",
		Topic: topic,
	}
	wire, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	return c.WriteMessage(websocket.TextMessage, wire)
}
