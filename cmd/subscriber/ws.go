package main

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

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
