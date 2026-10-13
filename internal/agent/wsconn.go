package agent

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSConn wraps a *websocket.Conn as a net.Conn with thread-safe writes,
// allowing yamux (or other byte-stream multiplexers) to run over WebSocket.
type WSConn struct {
	ws *websocket.Conn
	r  io.Reader
	mu sync.Mutex // protects concurrent Write calls to websocket
}

// NewWSConn wraps ws.
func NewWSConn(ws *websocket.Conn) *WSConn {
	return &WSConn{ws: ws}
}

func (c *WSConn) Read(p []byte) (int, error) {
	for {
		if c.r != nil {
			n, err := c.r.Read(p)
			if err == io.EOF {
				c.r = nil
				if n > 0 {
					return n, nil
				}
				continue
			}
			return n, err
		}
		typ, r, err := c.ws.NextReader()
		if err != nil {
			return 0, err
		}
		if typ == websocket.BinaryMessage || typ == websocket.TextMessage {
			c.r = r
		}
	}
}

func (c *WSConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, err := c.ws.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, err := w.Write(p)
	if err != nil {
		_ = w.Close()
		return n, err
	}
	return n, w.Close()
}

func (c *WSConn) Close() error {
	return c.ws.Close()
}

func (c *WSConn) LocalAddr() net.Addr {
	return c.ws.LocalAddr()
}

func (c *WSConn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *WSConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *WSConn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}

func (c *WSConn) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}
