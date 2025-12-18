package main

import (
	"sync"
	"time"

	"binance-ws-pubsub/internal/orderbook"
)

type TradesWindow struct {
	// Aggregated over the last UI interval
	Count     int
	BuyCount  int
	SellCount int

	BaseQtySum  float64
	QuoteQtySum float64

	Last    TradeEvent
	HasLast bool
}

type AppState struct {
	cfg Config

	mu    sync.Mutex
	dirty bool

	orderbook struct {
		ev  OrderbookEvent
		has bool
	}

	trades TradesWindow
}

type ViewModel struct {
	Now time.Time

	BrokerURL string

	OrderbookTopic string
	TradesTopic    string

	Orderbook *OrderbookView
	Trades    *TradesView
}

type OrderbookView struct {
	Topic       string
	Source      string
	PublishedAt int64
	Snapshot    orderbook.BandSnapshot
}

type TradesView struct {
	Topic       string
	Source      string
	PublishedAt int64 // last trade publishedAt

	Symbol string

	// last trade
	Side    string
	Price   string
	Qty     string
	TradeID int64

	TradeTime int64 // from exchange message

	// window stats (last interval)
	Count     int
	BuyCount  int
	SellCount int

	BaseQtySum  float64
	QuoteQtySum float64
}

func NewAppState(cfg Config) *AppState {
	return &AppState{cfg: cfg}
}

func (s *AppState) UpdateOrderbook(ev OrderbookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.orderbook.ev = ev
	s.orderbook.has = true
	s.dirty = true
}

func (s *AppState) UpdateTrade(ev TradeEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.trades.Last = ev
	s.trades.HasLast = true

	s.trades.Count++
	if ev.Side == "BUY" {
		s.trades.BuyCount++
	} else {
		s.trades.SellCount++
	}

	// Best-effort sums; if parse failed we stored 0, so it just won't contribute.
	s.trades.BaseQtySum += ev.QtyF
	s.trades.QuoteQtySum += ev.PriceF * ev.QtyF

	s.dirty = true
}

// SnapshotAndResetWindow returns (view, true) if there is anything new to render.
// It also resets the trades window stats (but keeps "last trade" shown until replaced).
func (s *AppState) SnapshotAndResetWindow() (ViewModel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.dirty {
		return ViewModel{}, false
	}

	vm := ViewModel{
		Now:            time.Now(),
		BrokerURL:      s.cfg.BrokerURL,
		OrderbookTopic: s.cfg.OrderbookTopic,
		TradesTopic:    s.cfg.TradesTopic,
	}

	if s.cfg.OrderbookTopic != "" && s.orderbook.has {
		vm.Orderbook = &OrderbookView{
			Topic:       s.orderbook.ev.Topic,
			Source:      s.orderbook.ev.Source,
			PublishedAt: s.orderbook.ev.PublishedAt,
			Snapshot:    s.orderbook.ev.Snapshot,
		}
	}

	if s.cfg.TradesTopic != "" && s.trades.HasLast {
		last := s.trades.Last
		vm.Trades = &TradesView{
			Topic:       last.Topic,
			Source:      last.Source,
			PublishedAt: last.PublishedAt,
			Symbol:      last.Trade.Symbol,
			Side:        last.Side,
			Price:       last.Trade.Price,
			Qty:         last.Trade.Qty,
			TradeID:     last.Trade.TradeID,
			TradeTime:   last.Trade.TradeTime,
			Count:       s.trades.Count,
			BuyCount:    s.trades.BuyCount,
			SellCount:   s.trades.SellCount,
			BaseQtySum:  s.trades.BaseQtySum,
			QuoteQtySum: s.trades.QuoteQtySum,
		}
	}

	// Reset window stats for next interval, but keep last trade.
	s.trades.Count = 0
	s.trades.BuyCount = 0
	s.trades.SellCount = 0
	s.trades.BaseQtySum = 0
	s.trades.QuoteQtySum = 0

	s.dirty = false
	return vm, true
}
