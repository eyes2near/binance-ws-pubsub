package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"binance-ws-pubsub/internal/binance"
	"binance-ws-pubsub/internal/orderbook"

	"github.com/gorilla/websocket"
)

type OrderbookWorker struct {
	Market ResolvedMarket
	Cfg    OrderbookConfig
}

type PublishedBandEvent struct {
	Snapshot    orderbook.BandSnapshot `json:"snapshot"`
	PublishedAt int64                  `json:"publishedAt"`
	Source      string                 `json:"source"` // "binance" | "binance-coinm"
}

func (w OrderbookWorker) Run(ctx context.Context, pub Publisher) error {
	if w.Cfg.Topic == "" {
		return errors.New("orderbook topic is empty")
	}
	if w.Market.Symbol == "" {
		return errors.New("resolved symbol is empty")
	}
	// Let supervisor handle restart/backoff; worker returns on any error.
	return w.runOnce(ctx, pub)
}

func (w OrderbookWorker) runOnce(ctx context.Context, pub Publisher) error {
	finalSnapshotLimit := w.Cfg.SnapshotLimit
	if w.Market.MarketType == orderbook.MarketCoinM && finalSnapshotLimit > 1000 {
		log.Printf("[orderbook] snapshot limit %d too high for COIN-M, adjusting to 1000", finalSnapshotLimit)
		finalSnapshotLimit = 1000
	}

	stream := strings.ToLower(w.Market.Symbol) + "@depth@100ms"
	binanceWS := strings.TrimRight(w.Market.StreamBase, "/") + "/" + stream

	log.Printf("[orderbook] connecting binance ws: %s", binanceWS)
	bws, _, err := websocket.DefaultDialer.Dial(binanceWS, nil)
	if err != nil {
		return err
	}
	defer func() { _ = bws.Close() }()

	bws.SetReadLimit(4 << 20)
	enableAutoPong(bws)

	// Ensure ReadMessage unblocks on ctx cancellation.
	go func() {
		<-ctx.Done()
		_ = bws.Close()
	}()

	evCh := make(chan binance.DepthEvent, 20000)
	errCh := make(chan error, 1)

	go func() {
		defer close(evCh)
		for {
			_, data, err := bws.ReadMessage()
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			var ev binance.DepthEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				log.Printf("[orderbook] unmarshal depth event failed: %v, data=%s", err, string(data))
				continue
			}

			select {
			case evCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Buffer until first event arrives to get firstU.
	var buffer []binance.DepthEvent
	var firstU int64 = -1

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	case ev, ok := <-evCh:
		if !ok {
			return errors.New("binance depth stream ended before first event")
		}
		buffer = append(buffer, ev)
		firstU = ev.FirstUpdateID
	}

	// Fetch snapshot with retry (snapshot retry stays inside runOnce).
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

			snap, err := binance.FetchDepthSnapshot(ctx, w.Market.RestBase, w.Market.Symbol, finalSnapshotLimit, w.Market.SourceName)
			if err != nil {
				var he binance.HTTPError
				if errors.As(err, &he) && he.RetryAfter > 0 {
					select {
					case <-time.After(he.RetryAfter):
						continue
					case <-ctx.Done():
						snapCh <- snapRes{err: ctx.Err()}
						return
					}
				}

				log.Printf("[orderbook] FetchDepthSnapshot failed: %v", err)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					snapCh <- snapRes{err: ctx.Err()}
					return
				}
				backoff = incBackoff(backoff, maxBackoff)
				continue
			}

			// Wait snapshot catch up to first buffered event.
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

	// Filter buffered events based on market type.
	filtered := buffer[:0]
	for _, ev := range buffer {
		if shouldDropDepthEvent(w.Market.MarketType, ev.FinalUpdateID, lastID) {
			continue
		}
		filtered = append(filtered, ev)
	}
	buffer = filtered

	// If buffer empty, wait for first acceptable event.
	if len(buffer) == 0 {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				return err
			case ev, ok := <-evCh:
				if !ok {
					return errors.New("binance depth stream ended while waiting for first post-snapshot event")
				}
				if shouldDropDepthEvent(w.Market.MarketType, ev.FinalUpdateID, lastID) {
					continue
				}
				buffer = append(buffer, ev)
				goto HAVE_FIRST_AFTER_SNAP
			}
		}
	}
HAVE_FIRST_AFTER_SNAP:

	first := buffer[0]
	if err := initialGapCheck(w.Market.MarketType, first.FirstUpdateID, first.FinalUpdateID, lastID); err != nil {
		log.Printf("[orderbook] initial gap detected: %v", err)
		return orderbook.ErrGap
	}

	ob := orderbook.NewFromSnapshot(w.Market.Symbol, snap.LastUpdateID, snap.Bids, snap.Asks, w.Market.MarketType)
	var lastSnapshotBytes []byte

	publishIfChanged := func() error {
		snapResult, ok := ob.BuildBandSnapshot(w.Cfg.BandPct, w.Cfg.MaxLevels)
		if !ok {
			return nil
		}

		sb, err := json.Marshal(snapResult)
		if err != nil {
			return fmt.Errorf("marshal band snapshot: %w", err)
		}
		if bytes.Equal(sb, lastSnapshotBytes) {
			return nil
		}
		lastSnapshotBytes = sb

		out := PublishedBandEvent{
			Snapshot:    snapResult,
			PublishedAt: time.Now().UnixMilli(),
			Source:      w.Market.SourceName,
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return fmt.Errorf("marshal PublishedBandEvent: %w", err)
		}

		// IMPORTANT: if broker publish fails, bubble up to supervisor for broker reconnect.
		return pub.Publish(w.Cfg.Topic, raw)
	}

	apply := func(ev binance.DepthEvent) error {
		if _, err := ob.Apply(orderbook.DepthEvent{
			FirstUpdateID: ev.FirstUpdateID,
			FinalUpdateID: ev.FinalUpdateID,
			PrevUpdateID:  ev.PrevUpdateID,
			Bids:          ev.Bids,
			Asks:          ev.Asks,
		}); err != nil {
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
				return errors.New("binance depth stream ended")
			}
			if err := apply(ev); err != nil {
				return err
			}
		}
	}
}

// Spot: drop u <= lastUpdateId
// COIN-M: drop u < lastUpdateId
func shouldDropDepthEvent(marketType orderbook.MarketType, finalUpdateID, lastUpdateID int64) bool {
	if marketType == orderbook.MarketCoinM {
		return finalUpdateID < lastUpdateID
	}
	return finalUpdateID <= lastUpdateID
}

// Spot: require U <= lastID+1 <= u
// COIN-M: require U <= lastID <= u
func initialGapCheck(marketType orderbook.MarketType, firstU, firstu, lastID int64) error {
	if marketType == orderbook.MarketCoinM {
		if !(firstU <= lastID && firstu >= lastID) {
			return fmt.Errorf("COIN-M gap: require U<=lastID<=u, got U=%d lastID=%d u=%d", firstU, lastID, firstu)
		}
		return nil
	}

	if !(firstU <= lastID+1 && firstu >= lastID+1) {
		return fmt.Errorf("Spot gap: require U<=lastID+1<=u, got U=%d lastID+1=%d u=%d", firstU, lastID+1, firstu)
	}
	return nil
}
