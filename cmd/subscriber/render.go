package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// clearScreen clears the terminal and moves cursor to top-left (ANSI)
func clearScreen() {
	fmt.Print("\x1b[2J\x1b[H")
}

func Render(ui *InPlaceUI, vm ViewModel, maxRows int) {
	var b bytes.Buffer

	// fmt.Fprintf(&b, "Broker: %s   Now: %s\n", vm.BrokerURL, vm.Now.Format(time.RFC3339))
	b.WriteString(strings.Repeat("=", 80) + "\n")

	if vm.OrderbookTopic != "" {
		if vm.Orderbook == nil {
			fmt.Fprintf(&b, "[Orderbook] topic=%s (waiting...)\n", vm.OrderbookTopic)
		} else {
			renderOrderbookBlock(&b, *vm.Orderbook, maxRows)
		}
		b.WriteString(strings.Repeat("-", 80) + "\n")
	}

	if vm.TradesTopic != "" {
		if vm.Trades == nil {
			fmt.Fprintf(&b, "[Trades] topic=%s (waiting...)\n", vm.TradesTopic)
		} else {
			renderTradesBlock(&b, *vm.Trades)
		}
		b.WriteString(strings.Repeat("-", 80) + "\n")
	}

	out := b.String()
	if ui != nil {
		_ = ui.Draw(out)
	} else {
		clearScreen()
		fmt.Print(out)
	}
}

func renderOrderbookBlock(b *bytes.Buffer, ob OrderbookView, maxRows int) {
	t := time.UnixMilli(ob.PublishedAt)
	snap := ob.Snapshot

	var rows [][]string
	rcount := len(snap.Bids)
	if len(snap.Asks) > rcount {
		rcount = len(snap.Asks)
	}
	if rcount > maxRows {
		rcount = maxRows
	}

	for i := 0; i < rcount; i++ {
		var bp, bq, ap, aq string
		if i < len(snap.Bids) {
			bp = snap.Bids[i][0]
			bq = snap.Bids[i][1]
		}
		if i < len(snap.Asks) {
			ap = snap.Asks[i][0]
			aq = snap.Asks[i][1]
		}
		rows = append(rows, []string{bp, bq, ap, aq})
	}

	header := []string{"BID PRICE", "QTY", "ASK PRICE", "QTY"}
	tbl := renderTable(header, rows)

	fmt.Fprintf(b, "[Orderbook] topic=%s source=%s\n", ob.Topic, ob.Source)
	fmt.Fprintf(b, "Symbol: %s   Mid: %.8f   UpdateID: %d   Published: %s\n",
		snap.Symbol, snap.MidPrice, snap.UpdateID, t.Format(time.RFC3339))
	b.WriteString(tbl)
	fmt.Fprintf(b, "Mid: %.8f  Band: %.4f  Low: %.8f  High: %.8f\n", snap.MidPrice, snap.BandPct, snap.Low, snap.High)
	fmt.Fprintf(b, "Counts: bids=%d asks=%d   Updated: %s ago\n",
		len(snap.Bids), len(snap.Asks), time.Since(t).Truncate(time.Second))
}

func renderTradesBlock(b *bytes.Buffer, tr TradesView) {
	tPub := time.UnixMilli(tr.PublishedAt)
	tTrade := time.UnixMilli(tr.TradeTime)

	fmt.Fprintf(b, "[Trades] topic=%s source=%s\n", tr.Topic, tr.Source)
	fmt.Fprintf(b, "Symbol: %s   Published: %s   Updated: %s ago\n",
		tr.Symbol, tPub.Format(time.RFC3339), time.Since(tPub).Truncate(time.Second))

	fmt.Fprintf(b, "Last: %s  price=%s  qty=%s  tradeId=%d  tradeTime=%s\n",
		tr.Side, tr.Price, tr.Qty, tr.TradeID, tTrade.Format(time.RFC3339))

	fmt.Fprintf(b, "Last 1s: trades=%d  buy=%d  sell=%d  baseQty=%.6f  quoteQty=%.6f\n",
		tr.Count, tr.BuyCount, tr.SellCount, tr.BaseQtySum, tr.QuoteQtySum)
}

// renderTable builds a simple ASCII table using runewidth-aware padding
func renderTable(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = runewidth.StringWidth(h)
	}
	for _, r := range rows {
		for i := range headers {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			if w := runewidth.StringWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	pad := func(s string, w int) string {
		sw := runewidth.StringWidth(s)
		if sw >= w {
			return s
		}
		return s + strings.Repeat(" ", w-sw)
	}

	var b bytes.Buffer
	sep := func() {
		b.WriteString("+")
		for _, w := range widths {
			b.WriteString(strings.Repeat("-", w+2))
			b.WriteString("+")
		}
		b.WriteString("\n")
	}

	sep()
	b.WriteString("|")
	for i, h := range headers {
		b.WriteString(" ")
		b.WriteString(pad(h, widths[i]))
		b.WriteString(" |")
	}
	b.WriteString("\n")
	sep()

	for _, r := range rows {
		b.WriteString("|")
		for i := range headers {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			b.WriteString(" ")
			b.WriteString(pad(cell, widths[i]))
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	sep()
	return b.String()
}
