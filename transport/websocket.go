package transport

import (
	"errors"
	"net"
)

func DialWebSocket(endpoint string) (net.Conn, error) {
	return nil, errors.New("websocket transport not implemented")
}

func ListenWebSocket(endpoint string) (Listener, error) {
	return nil, errors.New("websocket transport not implemented")
}
