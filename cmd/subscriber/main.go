package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"binance-ws-pubsub/internal/broker"
	"binance-ws-pubsub/internal/orderbook"

	"github.com/gorilla/websocket"
	"github.com/mattn/go-runewidth"
)

// clearScreen clears the terminal and moves cursor to top-left (ANSI)
func clearScreen() {
	fmt.Print("\x1b[2J\x1b[H")
}

// renderSnapshot prints a compact table (top-like) and updates in-place
func renderSnapshot(ui *InPlaceUI, topic string, snap orderbook.BandSnapshot, publishedAt int64, maxRows int) {
	// Build a block string and draw it using the in-place UI if available.
	t := time.UnixMilli(publishedAt)
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

	// header + table
	header := []string{"BID PRICE", "QTY", "ASK PRICE", "QTY"}
	tbl := renderTable(header, rows)

	var b bytes.Buffer
	fmt.Fprintf(&b, "Topic: %s   Symbol: %s   Mid: %.8f   UpdateID: %d   Published: %s\n",
		topic, snap.Symbol, snap.MidPrice, snap.UpdateID, t.Format(time.RFC3339))
	b.WriteString(strings.Repeat("-", 80) + "\n")
	b.WriteString(tbl)
	b.WriteString(strings.Repeat("-", 80) + "\n")
	fmt.Fprintf(&b, "Mid: %.8f  Band: %.4f  Low: %.8f  High: %.8f\n", snap.MidPrice, snap.BandPct, snap.Low, snap.High)
	fmt.Fprintf(&b, "Counts: bids=%d asks=%d   Updated: %s ago\n", len(snap.Bids), len(snap.Asks), time.Since(t).Truncate(time.Second))

	if ui != nil {
		_ = ui.Draw(b.String())
	} else {
		// fallback: clear the screen before printing so repeated prints don't push the table down
		clearScreen()
		fmt.Print(b.String())
	}
}

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

func main() {
	brokerURL := flag.String("broker", "ws://localhost:8080/ws", "broker ws url")
	topic := flag.String("topic", "orderbook.band.BNBBTC", "topic to subscribe")
	rows := flag.Int("rows", 20, "number of levels per side to display")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the subscriber logic and handle exit
	if err := run(ctx, cancel, *brokerURL, *topic, *rows); err != nil {
		log.Fatalf("subscriber error: %v", err)
	}
}

func run(ctx context.Context, cancel context.CancelFunc, brokerURL, topic string, rows int) error {
	c, err := dialWS(brokerURL)
	if err != nil {
		return fmt.Errorf("dial broker failed: %w", err)
	}
	defer c.Close()

	sub := broker.ClientMessage{
		Type:  "subscribe",
		Topic: topic,
	}
	wire, _ := json.Marshal(sub)
	if err := c.WriteMessage(websocket.TextMessage, wire); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	// Initialize in-place UI for low-flicker updates
	var ui *InPlaceUI = &InPlaceUI{}
	if err := ui.Init(); err != nil {
		log.Printf("ui init failed: %v, falling back to plain output", err)
		ui = nil
	}

	// Coalesce incoming snapshots and render at most once per second.
	type evSnap struct {
		topic       string
		snapshot    orderbook.BandSnapshot
		publishedAt int64
	}
	var mu sync.Mutex
	var last evSnap
	var hasLast bool

	var wg sync.WaitGroup
	wg.Add(1)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				if hasLast {
					s := last
					hasLast = false
					mu.Unlock()
					renderSnapshot(ui, s.topic, s.snapshot, s.publishedAt, rows)
				} else {
					mu.Unlock()
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Goroutine to wait for shutdown signal or websocket read error
	go func() {
		select {
		case <-sig:
			// Signal received, cancel context
			cancel()
		case <-ctx.Done():
			// Context cancelled elsewhere
		}
	}()

	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			// If context is done, this is an expected error during shutdown
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read failed: %w", err)
		}

		// Check if context was cancelled (e.g., by shutdown signal)
		if ctx.Err() != nil {
			break
		}

		var msg broker.ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "event":
			// Expect payload: { "snapshot": { ... }, "publishedAt": <ms>, "source": "binance" }
			var payload struct {
				Snapshot    orderbook.BandSnapshot `json:"snapshot"`
				PublishedAt int64                  `json:"publishedAt"`
				Source      string                 `json:"source"`
			}
			if err := json.Unmarshal(msg.Event, &payload); err != nil {
				// fallback to raw pretty print -> send to stderr (log) so it doesn't interfere with the in-place UI
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, msg.Event, "", "  "); err != nil {
					log.Printf("[event topic=%s] %s", msg.Topic, string(msg.Event))
				} else {
					log.Printf("[event topic=%s]\n%s", msg.Topic, pretty.String())
				}
				continue
			}

			// Coalesce snapshot: store latest and let ticker render at most once/sec
			mu.Lock()
			last = evSnap{topic: msg.Topic, snapshot: payload.Snapshot, publishedAt: payload.PublishedAt}
			hasLast = true
			mu.Unlock()
		case "ack":
			log.Printf("[ack] %s", msg.Message)
		case "error":
			log.Printf("[error] %s", msg.Message)
		}
	}

	// Clean shutdown
	log.Println("\nShutting down...")
	if ui != nil {
		ui.Close()
	}
	// c.Close() is deferred
	wg.Wait() // Wait for rendering goroutine to exit
	return nil
}
