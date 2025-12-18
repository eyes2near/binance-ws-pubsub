package broker

import "encoding/json"

// Client -> Broker
type ClientMessage struct {
	Type  string          `json:"type"`            // "subscribe" | "unsubscribe" | "publish"
	Topic string          `json:"topic,omitempty"` // required for subscribe/unsubscribe/publish
	Event json.RawMessage `json:"event,omitempty"` // required for publish
}

// Broker -> Client
type ServerMessage struct {
	Type    string          `json:"type"`              // "event" | "ack" | "error"
	Topic   string          `json:"topic,omitempty"`   // for event
	Event   json.RawMessage `json:"event,omitempty"`   // for event
	Message string          `json:"message,omitempty"` // for ack/error
}
