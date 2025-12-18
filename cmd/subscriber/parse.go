package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"binance-ws-pubsub/internal/binance"
	"binance-ws-pubsub/internal/broker"
	"binance-ws-pubsub/internal/orderbook"
)

type OrderbookEvent struct {
	Topic       string
	Snapshot    orderbook.BandSnapshot
	PublishedAt int64
	Source      string
}

func ParseOrderbookEvent(msg broker.ServerMessage) (OrderbookEvent, error) {
	// payload: { "snapshot": { ... }, "publishedAt": <ms>, "source": "binance" }
	var payload struct {
		Snapshot    orderbook.BandSnapshot `json:"snapshot"`
		PublishedAt int64                  `json:"publishedAt"`
		Source      string                 `json:"source"`
	}
	if err := json.Unmarshal(msg.Event, &payload); err != nil {
		return OrderbookEvent{}, err
	}
	return OrderbookEvent{
		Topic:       msg.Topic,
		Snapshot:    payload.Snapshot,
		PublishedAt: payload.PublishedAt,
		Source:      payload.Source,
	}, nil
}

type TradeEvent struct {
	Topic       string
	PublishedAt int64
	Source      string

	Trade binance.TradeEvent
	Side  string // "BUY" or "SELL"

	PriceF float64 // best-effort parsed
	QtyF   float64 // best-effort parsed
}

func ParseTradesEvent(msg broker.ServerMessage) (TradeEvent, error) {
	// payload: { "trade": { ...binance trade json... }, "publishedAt": <ms>, "source": "binance" }
	var payload struct {
		Trade       json.RawMessage `json:"trade"`
		PublishedAt int64           `json:"publishedAt"`
		Source      string          `json:"source"`
	}
	if err := json.Unmarshal(msg.Event, &payload); err != nil {
		return TradeEvent{}, fmt.Errorf("unmarshal outer trade payload: %w", err)
	}

	var te binance.TradeEvent
	if err := json.Unmarshal(payload.Trade, &te); err != nil {
		return TradeEvent{}, fmt.Errorf("unmarshal inner binance trade: %w", err)
	}

	// IsBuyerMaker==true => buyer is maker => taker is seller => treat as SELL aggressor
	side := "BUY"
	if te.IsBuyerMaker {
		side = "SELL"
	}

	priceF := parseFloatOrNaN(te.Price)
	qtyF := parseFloatOrNaN(te.Qty)

	return TradeEvent{
		Topic:       msg.Topic,
		PublishedAt: payload.PublishedAt,
		Source:      payload.Source,
		Trade:       te,
		Side:        side,
		PriceF:      priceF,
		QtyF:        qtyF,
	}, nil
}

func parseFloatOrNaN(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
