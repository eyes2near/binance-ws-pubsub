package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"binance-ws-pubsub/internal/binance"
	"binance-ws-pubsub/internal/broker"
	"binance-ws-pubsub/internal/orderbook"

	"github.com/gorilla/websocket"
)

// ==================== Data Structures ====================

type PublishedEvent struct {
	Snapshot    orderbook.BandSnapshot `json:"snapshot"`
	PublishedAt int64                  `json:"publishedAt"`
	Source      string                 `json:"source"` // "binance" or "binance-coinm"
}

// ExchangeInfo holds the response from Binance's exchangeInfo API.
type ExchangeInfo struct {
	Symbols []SymbolInfo `json:"symbols"`
}

// SymbolInfo holds information about a specific symbol from the exchange.
type SymbolInfo struct {
	Symbol         string `json:"symbol"`
	Pair           string `json:"pair"`
	ContractType   string `json:"contractType"` // e.g., "CURRENT_QUARTER", "NEXT_QUARTER"
	DeliveryDate   int64  `json:"deliveryDate"`
	ContractStatus string `json:"contractStatus"` // e.g., "TRADING"
}

// ==================== Helper Functions ====================

func dialWS(wsURL string) (*websocket.Conn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	d := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
	}
	c, _, err := d.Dial(u.String(), nil)
	return c, err
}

// getContractSymbol fetches quarterly contract information for a given base currency.
func getContractSymbol(baseCurrency, contract, coinmRestBase string) (string, error) {
	if contract != "current" && contract != "next" {
		return "", errors.New("contract flag must be 'current' or 'next'")
	}

	url := strings.TrimRight(coinmRestBase, "/") + "/dapi/v1/exchangeInfo"
	log.Printf("fetching exchange info for COIN-M from %s", url)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request to exchangeInfo failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exchangeInfo returned non-200 status: %d", resp.StatusCode)
	}

	var info ExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to decode exchangeInfo JSON: %w", err)
	}

	var quarterlyContracts []SymbolInfo
	targetPair := strings.ToUpper(baseCurrency) + "USD"
	for _, s := range info.Symbols {
		if s.Pair == targetPair && s.ContractStatus == "TRADING" {
			if s.ContractType == "CURRENT_QUARTER" || s.ContractType == "NEXT_QUARTER" {
				quarterlyContracts = append(quarterlyContracts, s)
			}
		}
	}

	if len(quarterlyContracts) == 0 {
		return "", fmt.Errorf("no trading quarterly contracts found for %s", baseCurrency)
	}

	// Sort by delivery date to ensure "current" is first.
	sort.Slice(quarterlyContracts, func(i, j int) bool {
		return quarterlyContracts[i].DeliveryDate < quarterlyContracts[j].DeliveryDate
	})

	targetContractType := "CURRENT_QUARTER"
	if contract == "next" {
		targetContractType = "NEXT_QUARTER"
	}

	for _, s := range quarterlyContracts {
		if s.ContractType == targetContractType {
			log.Printf("found COIN-M contract: %s (type: %s, delivery: %s)", s.Symbol, s.ContractType, time.UnixMilli(s.DeliveryDate).Format("2006-01-02"))
			return s.Symbol, nil
		}
	}

	return "", fmt.Errorf("could not find specific '%s' contract for %s", contract, baseCurrency)
}

// ==================== Main Application ====================

func main() {
	// --- Command Line Flags ---
	var (
		brokerURL = flag.String("broker", "ws://localhost:8080/ws", "broker ws url")
		// Generic symbol flag; meaning depends on market
		symbol = flag.String("symbol", "BNBBTC", "spot symbol (e.g., BNBBTC) or coin-m base currency (e.g., BTC)")
		band   = flag.Float64("band", 0.005, "band percent as fraction, 0.005 = 0.5%")
		// Market selection
		market   = flag.String("market", "spot", "market type: 'spot' or 'coin-m'")
		contract = flag.String("contract", "current", "for coin-m, which contract: 'current' or 'next'")
		// Advanced / Endpoint overrides
		maxLevels     = flag.Int("max-levels", 200, "max levels per side in published snapshot")
		snapshotLimit = flag.Int("snapshot-limit", 5000, "depth snapshot limit")
		// Spot Endpoints
		spotRestBase   = "https://api.binance.com"
		spotStreamBase = "wss://stream.binance.com:9443/ws"
		// COIN-M Endpoints
		coinmRestBase   = "https://dapi.binance.com"
		coinmStreamBase = "wss://dstream.binance.com/ws"
	)
	flag.Parse()

	// --- Resolve Symbol and Endpoints based on Market ---
	var finalSymbol, finalRestBase, finalStreamBase, sourceName string
	var marketType orderbook.MarketType

	switch *market {
	case "spot":
		finalSymbol = strings.ToUpper(*symbol)
		finalRestBase = spotRestBase
		finalStreamBase = spotStreamBase
		sourceName = "binance"
		marketType = orderbook.MarketSpot
	case "coin-m":
		// For coin-m, the -symbol flag is the base currency (e.g. BTC)
		// We fetch the actual contract symbol (e.g. BTCUSD_231229)
		contractSymbol, err := getContractSymbol(*symbol, *contract, coinmRestBase)
		if err != nil {
			log.Fatalf("could not determine COIN-M contract symbol: %v", err)
		}
		finalSymbol = contractSymbol
		finalRestBase = coinmRestBase
		finalStreamBase = coinmStreamBase
		sourceName = "binance-coinm"
		marketType = orderbook.MarketCoinM
	default:
		log.Fatalf("invalid market type: '%s'. Must be 'spot' or 'coin-m'", *market)
	}
	log.Printf("[DEBUG] Market resolution: finalSymbol=%s, finalRestBase=%s, finalStreamBase=%s, marketType=%s",
		finalSymbol, finalRestBase, finalStreamBase, marketType)

	topic := "orderbook.band." + finalSymbol

	// --- Connect to Broker ---
	bc, err := dialWS(*brokerURL)
	if err != nil {
		log.Fatalf("dial broker failed: %v", err)
	}
	defer func() {
		if bc != nil {
			_ = bc.Close()
		}
	}()

	startReadLoop := func(c *websocket.Conn) {
		go func() {
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}
	startReadLoop(bc)
	log.Printf("publisher: market=%s broker=%s topic=%s symbol=%s band=%.6f", *market, *brokerURL, topic, finalSymbol, *band)
	log.Printf("--> Publishing to topic: %s", topic)

	// --- Setup Graceful Shutdown & Main Loop ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	backoff := 500 * time.Millisecond
	maxBackoff := 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		runErr := runOnce(ctx, bc, topic, finalSymbol, sourceName, marketType, *band, *maxLevels, finalRestBase, finalStreamBase, *snapshotLimit)
		if runErr != nil {
			log.Printf("[ERROR] publisher run failed, restarting... error: %v", runErr)
			if bc != nil {
				_ = bc.Close()
				bc = nil
			}

			// Re-dial broker with backoff
			redialBackoff := 500 * time.Millisecond
			for {
				if ctx.Err() != nil {
					return
				}
				nb, derr := dialWS(*brokerURL)
				if derr == nil {
					bc = nb
					startReadLoop(bc)
					break
				}
				log.Printf("publisher: broker re-dial failed: %v", derr)
				select {
				case <-time.After(redialBackoff):
				case <-ctx.Done():
					return
				}
				redialBackoff *= 2
				if redialBackoff > maxBackoff {
					redialBackoff = maxBackoff
				}
			}

			// Wait before retrying runOnce
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = 500 * time.Millisecond
	}
}

// runOnce connects to Binance for a single symbol and processes its order book data.
func runOnce(
	ctx context.Context,
	bc *websocket.Conn,
	topic string,
	symbol string,
	sourceName string,
	marketType orderbook.MarketType,
	bandPct float64,
	maxLevels int,
	restBase string,
	streamBase string,
	snapshotLimit int,
) error {
	// For COIN-M, the max snapshot limit is 1000. Adjust if necessary.
	finalSnapshotLimit := snapshotLimit
	if marketType == orderbook.MarketCoinM && finalSnapshotLimit > 1000 {
		log.Printf("[WARN] Snapshot limit of %d is too high for COIN-M market. Adjusting to 1000.", snapshotLimit)
		finalSnapshotLimit = 1000
	}

	stream := strings.ToLower(symbol) + "@depth@100ms"
	binanceWS := strings.TrimRight(streamBase, "/") + "/" + stream

	log.Printf("[DEBUG] Connecting to Binance WebSocket: %s", binanceWS)
	bws, _, err := websocket.DefaultDialer.Dial(binanceWS, nil)
	if err != nil {
		return err
	}
	defer bws.Close()
	log.Printf("[DEBUG] Successfully connected to Binance WebSocket.")

	bws.SetReadLimit(4 << 20)
	bws.SetPingHandler(func(appData string) error {
		// log.Printf("[DEBUG] Received ping from Binance, sending pong.")
		return bws.WriteControl(websocket.PongMessage, []byte(appData), time.Time{})
	})

	evCh := make(chan binance.DepthEvent, 20000)
	errCh := make(chan error, 1)

	go func() {
		defer close(evCh)
		for {
			_, data, err := bws.ReadMessage()
			if err != nil {
				log.Printf("[ERROR] Binance WebSocket read failed: %v", err)
				errCh <- err
				return
			}
			var ev binance.DepthEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				log.Printf("[ERROR] Failed to unmarshal event from Binance: %v. Data: %s", err, string(data))
				continue
			}

			select {
			case evCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	var buffer []binance.DepthEvent
	var firstU int64 = -1

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	case ev, ok := <-evCh:
		if !ok {
			return nil
		}
		buffer = append(buffer, ev)
		firstU = ev.FirstUpdateID
	}

	type snapRes struct {
		snap binance.DepthSnapshot
		err  error
	}
	snapCh := make(chan snapRes, 1)
	go func() {
		backoff := 1 * time.Second
		maxBackoff := 30 * time.Second
		for {
			if ctx.Err() != nil {
				snapCh <- snapRes{err: ctx.Err()}
				return
			}

			snap, err := binance.FetchDepthSnapshot(ctx, restBase, symbol, finalSnapshotLimit, sourceName)
			if err != nil {
				log.Printf("[ERROR] FetchDepthSnapshot failed: %v. Retrying in %s", err, backoff)
				if he, ok := err.(binance.HTTPError); ok && he.RetryAfter > 0 {
					select {
					case <-time.After(he.RetryAfter):
						continue
					case <-ctx.Done():
						snapCh <- snapRes{err: ctx.Err()}
						return
					}
				}
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					snapCh <- snapRes{err: ctx.Err()}
					return
				}
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			if snap.LastUpdateID < firstU {
				select {
				case <-time.After(500 * time.Millisecond):
					continue
				case <-ctx.Done():
					snapCh <- snapRes{err: ctx.Err()}
					return
				}
			}

			snapCh <- snapRes{snap: snap, err: nil}
			return
		}
	}()

	var snap binance.DepthSnapshot
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case ev, ok := <-evCh:
			if ok {
				buffer = append(buffer, ev)
			}
		case res := <-snapCh:
			if res.err != nil {
				return res.err
			}
			snap = res.snap
			goto SNAP_READY
		}
	}
SNAP_READY:

	lastID := snap.LastUpdateID

	// 根据市场类型过滤事件
	// Spot: 丢弃 u <= lastUpdateId 的事件
	// COIN-M: 丢弃 u < lastUpdateId 的事件
	filtered := buffer[:0]
	for _, ev := range buffer {
		if marketType == orderbook.MarketCoinM {
			// COIN-M: 保留 u >= lastUpdateId 的事件
			if ev.FinalUpdateID < lastID {
				continue
			}
		} else {
			// Spot: 保留 u > lastUpdateId 的事件
			if ev.FinalUpdateID <= lastID {
				continue
			}
		}
		filtered = append(filtered, ev)
	}
	buffer = filtered

	if len(buffer) == 0 {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				return err
			case ev, ok := <-evCh:
				if !ok {
					return nil
				}
				// 应用相同的过滤逻辑
				shouldDrop := false
				if marketType == orderbook.MarketCoinM {
					if ev.FinalUpdateID < lastID {
						shouldDrop = true
					}
				} else {
					if ev.FinalUpdateID <= lastID {
						shouldDrop = true
					}
				}

				if shouldDrop {
					continue
				}

				buffer = append(buffer, ev)
				goto HAVE_FIRST_AFTER_SNAP
			}
		}
	}
HAVE_FIRST_AFTER_SNAP:

	first := buffer[0]

	// 初始 gap 检查：根据市场类型使用不同的规则
	if marketType == orderbook.MarketCoinM {
		// COIN-M: 第一个事件应满足 U <= lastUpdateId AND u >= lastUpdateId
		if !(first.FirstUpdateID <= lastID && first.FinalUpdateID >= lastID) {
			log.Printf("[ERROR] Initial gap detected (COIN-M). first.U=%d, lastID=%d, first.u=%d. "+
				"Required: U <= lastID <= u", first.FirstUpdateID, lastID, first.FinalUpdateID)
			return orderbook.ErrGap
		}
	} else {
		// Spot: 第一个事件应满足 U <= lastUpdateId+1 AND u >= lastUpdateId+1
		if !(first.FirstUpdateID <= lastID+1 && first.FinalUpdateID >= lastID+1) {
			log.Printf("[ERROR] Initial gap detected (Spot). first.U=%d, lastID+1=%d, first.u=%d. "+
				"Required: U <= lastID+1 <= u", first.FirstUpdateID, lastID+1, first.FinalUpdateID)
			return orderbook.ErrGap
		}
	}

	// 创建订单簿
	ob := orderbook.NewFromSnapshot(symbol, snap.LastUpdateID, snap.Bids, snap.Asks, marketType)
	var lastSnapshotBytes []byte

	publishIfChanged := func() error {
		snapResult, ok := ob.BuildBandSnapshot(bandPct, maxLevels)
		if !ok {
			return nil
		}

		sb, _ := json.Marshal(snapResult)
		if string(sb) == string(lastSnapshotBytes) {
			return nil
		}
		lastSnapshotBytes = sb

		out := PublishedEvent{
			Snapshot:    snapResult,
			PublishedAt: time.Now().UnixMilli(),
			Source:      sourceName,
		}
		raw, _ := json.Marshal(out)

		msg := broker.ClientMessage{
			Type:  "publish",
			Topic: topic,
			Event: raw,
		}
		wire, _ := json.Marshal(msg)

		if bc == nil {
			return errors.New("broker connection is nil")
		}
		return bc.WriteMessage(websocket.TextMessage, wire)
	}

	apply := func(ev binance.DepthEvent) error {
		if _, err := ob.Apply(orderbook.DepthEvent{
			FirstUpdateID: ev.FirstUpdateID,
			FinalUpdateID: ev.FinalUpdateID,
			PrevUpdateID:  ev.PrevUpdateID,
			Bids:          ev.Bids,
			Asks:          ev.Asks,
		}); err != nil {
			log.Printf("[ERROR] Failed to apply event to order book: %v", err)
			return err
		}
		return publishIfChanged()
	}

	for _, ev := range buffer {
		if err := apply(ev); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case ev, ok := <-evCh:
			if !ok {
				return nil
			}
			if err := apply(ev); err != nil {
				return err
			}
		}
	}
}
