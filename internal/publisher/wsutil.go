package publisher

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func dialWS(ctx context.Context, wsURL string, handshakeTimeout time.Duration) (*websocket.Conn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	d := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: handshakeTimeout,
	}
	// DialContext is available in gorilla/websocket.
	c, _, err := d.DialContext(ctx, u.String(), nil)
	return c, err
}

func enableAutoPong(conn *websocket.Conn) {
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Time{})
	})
}
