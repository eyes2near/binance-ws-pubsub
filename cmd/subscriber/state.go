package main

import (
	"sync"
	"time"

	"binance-ws-pubsub/internal/orderbook"
)

type TradesWindow struct {
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

	mu sync.Mutex

	conn struct {
		Connected bool
		At        time.Time
		Err       string
	}

	orderbook struct {
		ev  OrderbookEvent
		has bool
	}

	trades TradesWindow
}

type ViewModel struct {
	Now       time.Time
	BrokerURL string

	ConnConnected bool
	ConnAt        time.Time
	ConnErr       string

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
	PublishedAt int64

	Symbol string

	Side    string
	Price   string
	Qty     string
	TradeID int64

	TradeTime int64

	Count     int
	BuyCount  int
	SellCount int

	BaseQtySum  float64
	QuoteQtySum float64
}

func NewAppState(cfg Config) *AppState {
	return &AppState{cfg: cfg}
}

func (s *AppState) SetConnStatus(st ConnStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.conn.Connected = st.Connected
	s.conn.At = st.At
	if st.Err != nil {
		s.conn.Err = st.Err.Error()
	} else {
		s.conn.Err = ""
	}
}

func (s *AppState) UpdateOrderbook(ev OrderbookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.orderbook.ev = ev
	s.orderbook.has = true
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

	s.trades.BaseQtySum += ev.QtyF
	s.trades.QuoteQtySum += ev.PriceF * ev.QtyF
}

func (s *AppState) SnapshotAndResetWindow() ViewModel {
	s.mu.Lock()
	defer s.mu.Unlock()

	vm := ViewModel{
		Now:           time.Now(),
		BrokerURL:     s.cfg.BrokerURL,
		ConnConnected: s.conn.Connected,
		ConnAt:        s.conn.At,
		ConnErr:       s.conn.Err,

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
	} else if s.cfg.TradesTopic != "" {
		// trades enabled but no trade yet: still show window stats (likely zeros)
		vm.Trades = &TradesView{
			Topic:       s.cfg.TradesTopic,
			Source:      "",
			PublishedAt: 0,
			Symbol:      "",
			Count:       s.trades.Count,
			BuyCount:    s.trades.BuyCount,
			SellCount:   s.trades.SellCount,
			BaseQtySum:  s.trades.BaseQtySum,
			QuoteQtySum: s.trades.QuoteQtySum,
		}
	}

	// Reset window stats each tick (keep last trade)
	s.trades.Count = 0
	s.trades.BuyCount = 0
	s.trades.SellCount = 0
	s.trades.BaseQtySum = 0
	s.trades.QuoteQtySum = 0

	return vm
}
