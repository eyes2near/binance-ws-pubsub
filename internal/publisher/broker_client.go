package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"binance-ws-pubsub/internal/broker"

	"github.com/gorilla/websocket"
)

type Publisher interface {
	Publish(topic string, eventJSON []byte) error
}

type BrokerClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func DialBroker(ctx context.Context, brokerURL string) (*BrokerClient, error) {
	c, err := dialWS(ctx, brokerURL, 10*time.Second)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(4 << 20)
	return &BrokerClient{conn: c}, nil
}

func (bc *BrokerClient) Close() error {
	if bc == nil || bc.conn == nil {
		return nil
	}
	return bc.conn.Close()
}

// StartReadPump drains broker messages (ack/error/event) so the TCP buffers don't fill.
// It returns a channel that will yield the terminal read error.
func (bc *BrokerClient) StartReadPump(ctx context.Context) <-chan error {
	ch := make(chan error, 1)
	go func() {
		defer close(ch)
		if bc == nil || bc.conn == nil {
			ch <- errors.New("broker conn is nil")
			return
		}

		for {
			select {
			case <-ctx.Done():
				ch <- ctx.Err()
				return
			default:
			}

			if _, _, err := bc.conn.ReadMessage(); err != nil {
				ch <- err
				return
			}
		}
	}()
	return ch
}

func (bc *BrokerClient) Publish(topic string, eventJSON []byte) error {
	if bc == nil || bc.conn == nil {
		return errors.New("broker client is nil")
	}

	msg := broker.ClientMessage{
		Type:  "publish",
		Topic: topic,
		Event: json.RawMessage(eventJSON),
	}
	wire, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	bc.writeMu.Lock()
	defer bc.writeMu.Unlock()
	return bc.conn.WriteMessage(websocket.TextMessage, wire)
}
