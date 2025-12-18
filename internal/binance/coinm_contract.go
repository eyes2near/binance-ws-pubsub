package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type coinmExchangeInfo struct {
	Symbols []coinmSymbolInfo `json:"symbols"`
}

type coinmSymbolInfo struct {
	Symbol         string `json:"symbol"`
	Pair           string `json:"pair"`
	ContractType   string `json:"contractType"`   // "CURRENT_QUARTER", "NEXT_QUARTER", ...
	DeliveryDate   int64  `json:"deliveryDate"`   // millis
	ContractStatus string `json:"contractStatus"` // "TRADING"
}

// ResolveCoinMQuarterlySymbol resolves a COIN-M quarterly contract symbol from a base currency.
// baseCurrency: e.g. "BTC"
// contract: "current" | "next"
func ResolveCoinMQuarterlySymbol(ctx context.Context, baseCurrency, contract, coinmRestBase string) (string, error) {
	if contract != "current" && contract != "next" {
		return "", errors.New("contract flag must be 'current' or 'next'")
	}

	url := strings.TrimRight(coinmRestBase, "/") + "/dapi/v1/exchangeInfo"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request to exchangeInfo failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exchangeInfo returned non-200 status: %d", resp.StatusCode)
	}

	var info coinmExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to decode exchangeInfo JSON: %w", err)
	}

	targetPair := strings.ToUpper(baseCurrency) + "USD"

	var quarterly []coinmSymbolInfo
	for _, s := range info.Symbols {
		if s.Pair != targetPair {
			continue
		}
		if s.ContractStatus != "TRADING" {
			continue
		}
		if s.ContractType == "CURRENT_QUARTER" || s.ContractType == "NEXT_QUARTER" {
			quarterly = append(quarterly, s)
		}
	}

	if len(quarterly) == 0 {
		return "", fmt.Errorf("no trading quarterly contracts found for %s", baseCurrency)
	}

	// Sort by delivery date: earliest is "current".
	sort.Slice(quarterly, func(i, j int) bool { return quarterly[i].DeliveryDate < quarterly[j].DeliveryDate })

	want := "CURRENT_QUARTER"
	if contract == "next" {
		want = "NEXT_QUARTER"
	}

	for _, s := range quarterly {
		if s.ContractType == want {
			_ = time.UnixMilli(s.DeliveryDate) // keep parity with previous logging usage if you want to log
			return s.Symbol, nil
		}
	}

	return "", fmt.Errorf("could not find specific '%s' contract for %s", contract, baseCurrency)
}
