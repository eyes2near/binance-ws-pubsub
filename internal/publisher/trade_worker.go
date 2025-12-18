package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"binance-ws-pubsub/internal/binance"

	"github.com/gorilla/websocket"
)

type TradesWorker struct {
	Market ResolvedMarket
	Cfg    TradesConfig
}

type PublishedTradeEvent struct {
	Trade       json.RawMessage `json:"trade"` // raw trade JSON from Binance
	PublishedAt int64           `json:"publishedAt"`
	Source      string          `json:"source"` // "binance" | "binance-coinm"
}

func (w TradesWorker) Run(ctx context.Context, pub Publisher) error {
	if w.Cfg.Topic == "" {
		return errors.New("trades topic is empty")
	}
	if w.Market.Symbol == "" {
		return errors.New("resolved symbol is empty")
	}
	return w.runOnce(ctx, pub)
}

func (w TradesWorker) runOnce(ctx context.Context, pub Publisher) error {
	stream := strings.ToLower(w.Market.Symbol) + "@trade"
	binanceWS := strings.TrimRight(w.Market.StreamBase, "/") + "/" + stream

	log.Printf("[trades] connecting binance ws: %s", binanceWS)
	bws, _, err := websocket.DefaultDialer.Dial(binanceWS, nil)
	if err != nil {
		return err
	}
	defer func() { _ = bws.Close() }()

	bws.SetReadLimit(4 << 20)
	enableAutoPong(bws)

	go func() {
		<-ctx.Done()
		_ = bws.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, data, err := bws.ReadMessage()
		if err != nil {
			return err
		}

		// Validate message shape using typed event (and guard symbol)
		var te binance.TradeEvent
		if err := json.Unmarshal(data, &te); err != nil {
			log.Printf("[trades] unmarshal trade event failed: %v, data=%s", err, string(data))
			continue
		}
		if te.EventType != "" && te.EventType != "trade" {
			continue
		}
		if te.Symbol != "" && strings.ToUpper(te.Symbol) != w.Market.Symbol {
			continue
		}

		out := PublishedTradeEvent{
			Trade:       json.RawMessage(data),
			PublishedAt: time.Now().UnixMilli(),
			Source:      w.Market.SourceName,
		}
		raw, err := json.Marshal(out)
		if err != nil {
			log.Printf("[trades] marshal PublishedTradeEvent failed: %v", err)
			continue
		}

		// IMPORTANT: bubble publish error to supervisor so broker can be re-dialed.
		if err := pub.Publish(w.Cfg.Topic, raw); err != nil {
			return err
		}
	}
}
