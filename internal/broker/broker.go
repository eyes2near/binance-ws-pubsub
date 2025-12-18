package broker

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Broker struct {
	mu     sync.RWMutex
	topics map[string]map[*Client]struct{}
}

func NewBroker() *Broker {
	return &Broker{
		topics: make(map[string]map[*Client]struct{}),
	}
}

type Client struct {
	conn *websocket.Conn
	send chan []byte

	mu   sync.Mutex
	subs map[string]struct{}
}

func (b *Broker) ServeWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		// 本地服务为主，直接放开；如需生产使用请严格校验 Origin
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade ws failed: %v", err)
		return
	}

	c := &Client{
		conn: conn,
		send: make(chan []byte, 256),
		subs: make(map[string]struct{}),
	}

	// No read deadlines or pong handlers per user request.
	conn.SetReadLimit(4 << 20) // 4MB

	go b.writeLoop(c)
	b.readLoop(c)
}

func (b *Broker) readLoop(c *Client) {
	defer func() {
		b.removeClientFromAllTopics(c)
		_ = c.conn.Close()
		close(c.send)
	}()

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			b.sendError(c, "invalid json")
			continue
		}

		switch msg.Type {
		case "subscribe":
			if msg.Topic == "" {
				b.sendError(c, "missing topic")
				continue
			}
			b.subscribe(c, msg.Topic)
			b.sendAck(c, "subscribed "+msg.Topic)

		case "unsubscribe":
			if msg.Topic == "" {
				b.sendError(c, "missing topic")
				continue
			}
			b.unsubscribe(c, msg.Topic)
			b.sendAck(c, "unsubscribed "+msg.Topic)

		case "publish":
			if msg.Topic == "" {
				b.sendError(c, "missing topic")
				continue
			}
			if len(msg.Event) == 0 {
				b.sendError(c, "missing event")
				continue
			}
			b.broadcast(msg.Topic, msg.Event)

		default:
			b.sendError(c, "unknown type")
		}
	}
}

func (b *Broker) writeLoop(c *Client) {
	for {
		select {
		case data, ok := <-c.send:
			if !ok {
				return
			}
			// write without write deadlines or ping heartbeats
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

func (b *Broker) subscribe(c *Client, topic string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.topics[topic]; !ok {
		b.topics[topic] = make(map[*Client]struct{})
	}
	b.topics[topic][c] = struct{}{}

	c.mu.Lock()
	c.subs[topic] = struct{}{}
	c.mu.Unlock()
}

func (b *Broker) unsubscribe(c *Client, topic string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.topics[topic]; ok {
		delete(subs, c)
		if len(subs) == 0 {
			delete(b.topics, topic)
		}
	}

	c.mu.Lock()
	delete(c.subs, topic)
	c.mu.Unlock()
}

func (b *Broker) removeClientFromAllTopics(c *Client) {
	c.mu.Lock()
	topics := make([]string, 0, len(c.subs))
	for t := range c.subs {
		topics = append(topics, t)
	}
	c.mu.Unlock()

	for _, t := range topics {
		b.unsubscribe(c, t)
	}
}

func (b *Broker) broadcast(topic string, raw json.RawMessage) {
	out := ServerMessage{
		Type:  "event",
		Topic: topic,
		Event: raw,
	}
	data, _ := json.Marshal(out)

	b.mu.RLock()
	subs := b.topics[topic]
	// 注意：这里只复制指针集合，避免持锁发送
	targets := make([]*Client, 0, len(subs))
	for c := range subs {
		targets = append(targets, c)
	}
	b.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			// 慢消费者：直接断开，避免 broker 被拖死
			_ = c.conn.Close()
		}
	}
}

func (b *Broker) sendAck(c *Client, message string) {
	out := ServerMessage{Type: "ack", Message: message}
	data, _ := json.Marshal(out)
	select {
	case c.send <- data:
	default:
		_ = c.conn.Close()
	}
}

func (b *Broker) sendError(c *Client, message string) {
	out := ServerMessage{Type: "error", Message: message}
	data, _ := json.Marshal(out)
	select {
	case c.send <- data:
	default:
		_ = c.conn.Close()
	}
}
