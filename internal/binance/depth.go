package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DepthEvent represents a depth update event from Binance WebSocket.
// Note: The `pu` field (PrevUpdateID) is only present for Futures markets.
type DepthEvent struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	TransactTime  int64      `json:"T,omitempty"` // 仅期货
	Symbol        string     `json:"s"`
	PairSymbol    string     `json:"ps,omitempty"` // 仅期货，例如 "BTCUSD"
	FirstUpdateID int64      `json:"U"`
	FinalUpdateID int64      `json:"u"`
	PrevUpdateID  int64      `json:"pu,omitempty"` // 仅期货：上一个事件的 FinalUpdateID
	Bids          [][]string `json:"b"`            // [price, qty]
	Asks          [][]string `json:"a"`            // [price, qty]
}

type DepthSnapshot struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	MessageTime  int64      `json:"T,omitempty"` // 仅期货
	EventTime    int64      `json:"E,omitempty"` // 仅期货
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

func FetchDepthSnapshot(ctx context.Context, restBase string, symbol string, limit int, market string) (DepthSnapshot, error) {
	var snap DepthSnapshot

	u, err := url.Parse(restBase)
	if err != nil {
		return snap, err
	}

	// Select the correct API path based on the market type.
	if market == "binance-coinm" {
		u.Path = "/dapi/v1/depth"
	} else {
		u.Path = "/api/v3/depth"
	}

	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return snap, err
	}

	log.Printf("[DEBUG] Fetching snapshot from URL: %s", u.String())
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return snap, err
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] Snapshot response status: %s", resp.Status)
	if resp.StatusCode/100 != 2 {
		// Try to parse Retry-After header if present
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			// Retry-After can be seconds or HTTP-date
			if s, err := strconv.Atoi(ra); err == nil {
				retryAfter = time.Duration(s) * time.Second
			} else if tm, err := http.ParseTime(ra); err == nil {
				retryAfter = time.Until(tm)
			}
			log.Printf("[DEBUG] Snapshot retry-after header found: %s", retryAfter)
		}
		return snap, HTTPError{StatusCode: resp.StatusCode, RetryAfter: retryAfter, Msg: resp.Status}
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&snap); err != nil {
		return snap, err
	}
	log.Printf("[DEBUG] Snapshot decoded successfully. LastUpdateID: %d", snap.LastUpdateID)
	return snap, nil
}

// HTTPError represents a non-2xx HTTP response, with optional Retry-After duration.
type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
	Msg        string
}

func (e HTTPError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("snapshot http status: %s (retry-after=%s)", e.Msg, e.RetryAfter)
	}
	return fmt.Sprintf("snapshot http status: %s", e.Msg)
}
