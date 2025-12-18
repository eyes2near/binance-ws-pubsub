package publisher

import (
	"context"
	"fmt"
	"strings"

	"binance-ws-pubsub/internal/binance"
	"binance-ws-pubsub/internal/orderbook"
)

func ResolveMarket(ctx context.Context, market, symbol, contract string, ep Endpoints) (ResolvedMarket, error) {
	switch market {
	case "spot":
		finalSymbol := strings.ToUpper(strings.TrimSpace(symbol))
		if finalSymbol == "" {
			return ResolvedMarket{}, fmt.Errorf("spot symbol is empty")
		}
		return ResolvedMarket{
			Symbol:     finalSymbol,
			RestBase:   ep.SpotRestBase,
			StreamBase: ep.SpotStreamBase,
			SourceName: "binance",
			MarketType: orderbook.MarketSpot,
		}, nil

	case "coin-m":
		base := strings.ToUpper(strings.TrimSpace(symbol))
		if base == "" {
			return ResolvedMarket{}, fmt.Errorf("coin-m base currency is empty")
		}
		contractSymbol, err := binance.ResolveCoinMQuarterlySymbol(ctx, base, contract, ep.CoinMRestBase)
		if err != nil {
			return ResolvedMarket{}, err
		}
		return ResolvedMarket{
			Symbol:     contractSymbol,
			RestBase:   ep.CoinMRestBase,
			StreamBase: ep.CoinMStreamBase,
			SourceName: "binance-coinm",
			MarketType: orderbook.MarketCoinM,
		}, nil

	default:
		return ResolvedMarket{}, fmt.Errorf("invalid market type: %q (must be 'spot' or 'coin-m')", market)
	}
}
