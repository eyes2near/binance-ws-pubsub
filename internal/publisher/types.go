package publisher

import "binance-ws-pubsub/internal/orderbook"

type Endpoints struct {
	SpotRestBase    string
	SpotStreamBase  string
	CoinMRestBase   string
	CoinMStreamBase string
}

type ResolvedMarket struct {
	// Final symbol:
	// - spot: uppercased symbol (e.g. BNBBTC)
	// - coin-m: resolved contract symbol (e.g. BTCUSD_231229)
	Symbol string

	RestBase   string
	StreamBase string

	// "binance" | "binance-coinm"
	SourceName string

	MarketType orderbook.MarketType
}

type OrderbookConfig struct {
	Topic         string
	BandPct       float64
	MaxLevels     int
	SnapshotLimit int
}

type TradesConfig struct {
	Topic string
}

type SupervisorConfig struct {
	BrokerURL string
	Market    ResolvedMarket

	PublishOrderbook bool
	PublishTrades    bool

	Orderbook OrderbookConfig
	Trades    TradesConfig
}
