package orderbook

import (
	"errors"
	"log"
	"sort"
	"strconv"
)

var ErrGap = errors.New("orderbook gap detected")

// MarketType represents the type of market for the order book.
type MarketType string

const (
	MarketSpot  MarketType = "spot"
	MarketCoinM MarketType = "coinm"
)

type Side map[string]string // priceStr -> qtyStr

type OrderBook struct {
	Symbol            string
	UpdateID          int64
	Bids              Side
	Asks              Side
	MarketType        MarketType
	firstEventApplied bool // 用于 COIN-M：标记第一个事件是否已应用
}

func NewFromSnapshot(symbol string, updateID int64, bids [][]string, asks [][]string, marketType MarketType) *OrderBook {
	ob := &OrderBook{
		Symbol:            symbol,
		UpdateID:          updateID,
		Bids:              make(Side, len(bids)),
		Asks:              make(Side, len(asks)),
		MarketType:        marketType,
		firstEventApplied: false, // 初始化：第一个事件尚未应用
	}
	for _, lv := range bids {
		if len(lv) < 2 {
			continue
		}
		ob.Bids[lv[0]] = lv[1]
	}
	for _, lv := range asks {
		if len(lv) < 2 {
			continue
		}
		ob.Asks[lv[0]] = lv[1]
	}
	log.Printf("[DEBUG] OrderBook created: symbol=%s, updateID=%d, marketType=%s, bids=%d, asks=%d",
		symbol, updateID, marketType, len(ob.Bids), len(ob.Asks))
	return ob
}

// DepthEvent 依赖字段：U/u/pu/b/a
// pu (PrevUpdateID) 仅用于期货市场
type DepthEvent struct {
	FirstUpdateID int64
	FinalUpdateID int64
	PrevUpdateID  int64 // pu field, only for futures markets
	Bids          [][]string
	Asks          [][]string
}

func parseFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func qtyIsZero(q string) bool {
	f, ok := parseFloat(q)
	if !ok {
		return false
	}
	return f == 0
}

// Apply 按 Binance 官方流程的"应用增量更新"逻辑：
//
// Spot (现货):
//   - u < localUpdateID: ignore
//   - U > localUpdateID+1: gap -> ErrGap
//
// COIN-M (币本位期货):
//   - u < localUpdateID: ignore
//   - 第一个事件: 需满足 U <= lastUpdateId AND u >= lastUpdateId
//   - 后续事件: pu != localUpdateID -> gap -> ErrGap
func (ob *OrderBook) Apply(ev DepthEvent) (bool, error) {
	// 忽略过旧的事件
	if ev.FinalUpdateID < ob.UpdateID {
		return false, nil
	}

	// 根据市场类型进行连续性检查
	if ob.MarketType == MarketCoinM {
		if !ob.firstEventApplied {
			// COIN-M 第一个事件：验证 U <= lastUpdateId AND u >= lastUpdateId
			// 这个事件"跨越"了快照点，所以它的 pu 会小于 lastUpdateId，这是正常的
			if !(ev.FirstUpdateID <= ob.UpdateID && ev.FinalUpdateID >= ob.UpdateID) {
				return false, ErrGap
			}
			ob.firstEventApplied = true
		} else {
			// COIN-M 后续事件：验证 pu == lastUpdateId (前一个事件的 u)
			if ev.PrevUpdateID != ob.UpdateID {
				return false, ErrGap
			}
		}
	} else {
		// Spot：验证 U <= lastUpdateId+1
		if ev.FirstUpdateID > ob.UpdateID+1 {
			return false, ErrGap
		}
	}

	// 应用 bid 更新
	for _, lv := range ev.Bids {
		if len(lv) < 2 {
			continue
		}
		price, qty := lv[0], lv[1]
		if qtyIsZero(qty) {
			delete(ob.Bids, price)
		} else {
			ob.Bids[price] = qty
		}
	}

	// 应用 ask 更新
	for _, lv := range ev.Asks {
		if len(lv) < 2 {
			continue
		}
		price, qty := lv[0], lv[1]
		if qtyIsZero(qty) {
			delete(ob.Asks, price)
		} else {
			ob.Asks[price] = qty
		}
	}

	ob.UpdateID = ev.FinalUpdateID
	return true, nil
}

func (ob *OrderBook) BestBidAsk() (bestBid float64, bestAsk float64, ok bool) {
	// bestBid = max(bids), bestAsk = min(asks)
	bestBid = -1
	bestAsk = 0

	for p := range ob.Bids {
		f, okp := parseFloat(p)
		if !okp {
			continue
		}
		if f > bestBid {
			bestBid = f
		}
	}
	for p := range ob.Asks {
		f, okp := parseFloat(p)
		if !okp {
			continue
		}
		if bestAsk == 0 || f < bestAsk {
			bestAsk = f
		}
	}

	if bestBid <= 0 || bestAsk <= 0 {
		return 0, 0, false
	}
	return bestBid, bestAsk, true
}

type BandSnapshot struct {
	Symbol   string     `json:"symbol"`
	UpdateID int64      `json:"updateId"`
	MidPrice float64    `json:"midPrice"`
	BandPct  float64    `json:"bandPct"`
	Low      float64    `json:"low"`
	High     float64    `json:"high"`
	Bids     [][]string `json:"bids"` // within band, sorted desc
	Asks     [][]string `json:"asks"` // within band, sorted asc
}

type level struct {
	priceF float64
	priceS string
	qtyS   string
}

func (ob *OrderBook) BuildBandSnapshot(bandPct float64, maxLevels int) (BandSnapshot, bool) {
	bestBid, bestAsk, ok := ob.BestBidAsk()
	if !ok {
		return BandSnapshot{}, false
	}
	mid := (bestBid + bestAsk) / 2
	low := mid * (1 - bandPct)
	high := mid * (1 + bandPct)

	bids := make([]level, 0, 256)
	asks := make([]level, 0, 256)

	for p, q := range ob.Bids {
		pf, okp := parseFloat(p)
		if !okp {
			continue
		}
		if pf >= low && pf <= mid {
			bids = append(bids, level{priceF: pf, priceS: p, qtyS: q})
		}
	}
	for p, q := range ob.Asks {
		pf, okp := parseFloat(p)
		if !okp {
			continue
		}
		if pf <= high && pf >= mid {
			asks = append(asks, level{priceF: pf, priceS: p, qtyS: q})
		}
	}

	sort.Slice(bids, func(i, j int) bool { return bids[i].priceF > bids[j].priceF })
	sort.Slice(asks, func(i, j int) bool { return asks[i].priceF < asks[j].priceF })

	if maxLevels > 0 {
		if len(bids) > maxLevels {
			bids = bids[:maxLevels]
		}
		if len(asks) > maxLevels {
			asks = asks[:maxLevels]
		}
	}

	outBids := make([][]string, 0, len(bids))
	outAsks := make([][]string, 0, len(asks))
	for _, lv := range bids {
		outBids = append(outBids, []string{lv.priceS, lv.qtyS})
	}
	for _, lv := range asks {
		outAsks = append(outAsks, []string{lv.priceS, lv.qtyS})
	}

	return BandSnapshot{
		Symbol:   ob.Symbol,
		UpdateID: ob.UpdateID,
		MidPrice: mid,
		BandPct:  bandPct,
		Low:      low,
		High:     high,
		Bids:     outBids,
		Asks:     outAsks,
	}, true
}
