// Package envtunnel carries one TCP byte stream over a dedicated WebSocket.
package envtunnel

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const IdleTimeout = 5 * time.Minute

// Conn implements net.Conn without buffering entire WebSocket messages.
type Conn struct {
	ws            *websocket.Conn
	reader        io.Reader
	mu            sync.Mutex
	writeMu       sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

// NewConn wraps a dedicated WebSocket as a byte stream.
func NewConn(ws *websocket.Conn) *Conn {
	return &Conn{ws: ws}
}

func (c *Conn) deadline(explicit time.Time) time.Time {
	idle := time.Now().Add(IdleTimeout)
	if !explicit.IsZero() && explicit.Before(idle) {
		return explicit
	}
	return idle
}

func (c *Conn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		_ = c.ws.SetReadDeadline(c.deadline(c.readDeadline))
		c.mu.Unlock()
		if c.reader == nil {
			kind, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				return 0, io.ErrUnexpectedEOF
			}
			c.reader = r
		}
		n, err := c.reader.Read(p)
		if err == io.EOF {
			c.reader = nil
			if n == 0 {
				continue
			}
			err = nil
		}
		return n, err
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	_ = c.ws.SetWriteDeadline(c.deadline(c.writeDeadline))
	c.mu.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) Close() error {
	return c.ws.Close()
}

func (c *Conn) LocalAddr() net.Addr {
	return c.ws.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *Conn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	// The underlying conn interrupts an already blocked read as net.Conn requires.
	return c.ws.UnderlyingConn().SetReadDeadline(c.deadline(t))
}
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	return c.ws.UnderlyingConn().SetWriteDeadline(c.deadline(t))
}
