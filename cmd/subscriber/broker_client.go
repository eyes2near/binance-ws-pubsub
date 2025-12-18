package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"binance-ws-pubsub/internal/broker"

	"github.com/gorilla/websocket"
)

type ConnStatus struct {
	Connected bool
	At        time.Time
	Err       error
}

type BrokerClient struct {
	url    string
	topics []string

	onStatus func(ConnStatus)
}

func NewBrokerClient(url string, topics []string, onStatus func(ConnStatus)) *BrokerClient {
	// filter empty topics
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		if t != "" {
			out = append(out, t)
		}
	}
	return &BrokerClient{
		url:      url,
		topics:   out,
		onStatus: onStatus,
	}
}

// Run starts a reconnecting subscription loop.
// It returns a channel of parsed broker.ServerMessage.
// The channel is closed when ctx is done.
func (bc *BrokerClient) Run(ctx context.Context) <-chan broker.ServerMessage {
	msgCh := make(chan broker.ServerMessage, 1024)

	go func() {
		defer close(msgCh)

		backoff := 500 * time.Millisecond
		maxBackoff := 30 * time.Second

		for {
			if ctx.Err() != nil {
				return
			}

			// connect
			conn, err := dialWS(bc.url)
			if err != nil {
				bc.emitStatus(false, err)
				sleep(ctx, backoff)
				backoff = incBackoff(backoff, maxBackoff)
				continue
			}

			// successful connect resets backoff
			backoff = 500 * time.Millisecond
			bc.emitStatus(true, nil)

			// run one session
			sessionErr := bc.runSession(ctx, conn, msgCh)
			_ = conn.Close()

			if ctx.Err() != nil {
				return
			}

			bc.emitStatus(false, sessionErr)
			sleep(ctx, backoff)
			backoff = incBackoff(backoff, maxBackoff)
		}
	}()

	return msgCh
}

func (bc *BrokerClient) emitStatus(connected bool, err error) {
	if bc.onStatus == nil {
		return
	}
	bc.onStatus(ConnStatus{
		Connected: connected,
		At:        time.Now(),
		Err:       err,
	})
}

func (bc *BrokerClient) runSession(ctx context.Context, conn *websocket.Conn, out chan<- broker.ServerMessage) error {
	// Keepalive parameters
	const (
		writeWait  = 5 * time.Second
		pongWait   = 25 * time.Second
		pingPeriod = 10 * time.Second // should be < pongWait
	)

	conn.SetReadLimit(4 << 20)

	// If broker is gone but TCP doesn't notify promptly, read deadline makes it fail.
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Ensure ReadMessage unblocks on ctx cancellation
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var writeMu sync.Mutex

	// subscribe all topics after connect
	for _, topic := range bc.topics {
		sub := broker.ClientMessage{Type: "subscribe", Topic: topic}
		wire, err := json.Marshal(sub)
		if err != nil {
			return err
		}

		writeMu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		werr := conn.WriteMessage(websocket.TextMessage, wire)
		writeMu.Unlock()

		if werr != nil {
			return fmt.Errorf("subscribe %s failed: %w", topic, werr)
		}
	}

	// ping loop
	pingDone := make(chan struct{})
	defer close(pingDone)

	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait))
				writeMu.Unlock()

				if err != nil {
					// Read loop will exit soon (either due to conn close or deadline).
					_ = conn.Close()
					return
				}
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// read loop
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var msg broker.ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			// ignore malformed messages
			continue
		}

		// forward
		select {
		case out <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// local sleep/backoff helpers (avoid import cycle with other files)
func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

func incBackoff(cur, max time.Duration) time.Duration {
	n := cur * 2
	if n > max {
		return max
	}
	return n
}

// Optional: useful for debugging connection flaps
func debugStatusLog(st ConnStatus) {
	if st.Connected {
		log.Printf("[broker] connected at %s", st.At.Format(time.RFC3339))
		return
	}
	if st.Err != nil {
		log.Printf("[broker] disconnected at %s: %v", st.At.Format(time.RFC3339), st.Err)
	} else {
		log.Printf("[broker] disconnected at %s", st.At.Format(time.RFC3339))
	}
}
