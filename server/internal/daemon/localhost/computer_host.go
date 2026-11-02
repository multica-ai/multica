package localhost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
)

// computerHost is device-local and main-only. It never follows the viewer
// connection, forwards to another device, or replays requests on reconnect.
type computerHost struct {
	mu         sync.Mutex
	connection *computerConnection
}
type computerConnection struct {
	ws      *websocket.Conn
	write   sync.Mutex
	mu      sync.Mutex
	pending map[string]chan v2HostReply
	closed  bool
}

func (c *computerConnection) send(value any) error {
	c.write.Lock()
	defer c.write.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.ws.WriteJSON(value)
}
func (c *computerConnection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	_ = c.ws.Close()
	for _, ch := range c.pending {
		select {
		case ch <- v2HostReply{err: errors.New("outcome_unknown: Computer Use host disconnected; inspect the app before retrying")}:
		default:
		}
	}
}
func (h *computerHost) request(ctx context.Context, _ int64, _ string, _ string, payload any) (json.RawMessage, error) {
	h.mu.Lock()
	c := h.connection
	h.mu.Unlock()
	if c == nil {
		return nil, service.ErrHostUnavailable
	}
	id := newRequestID()
	ch := make(chan v2HostReply, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, service.ErrHostUnavailable
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.send(map[string]any{"type": "request", "id": id, "payload": payload}); err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		return reply.result, reply.err
	case <-ctx.Done():
		_ = c.send(map[string]any{"type": "cancel", "id": id})
		return nil, ctx.Err()
	}
}
func (v *V2) serveComputerHost(w http.ResponseWriter, r *http.Request) {
	if callerOf(r.Context()).kind != callerMain {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	v.computer.mu.Lock()
	if v.computer.connection != nil {
		v.computer.mu.Unlock()
		http.Error(w, "Computer Use host already connected", http.StatusConflict)
		return
	}
	ws, err := v.upgrader.Upgrade(w, r, nil)
	if err != nil {
		v.computer.mu.Unlock()
		return
	}
	c := &computerConnection{ws: ws, pending: map[string]chan v2HostReply{}}
	v.computer.connection = c
	v.computer.mu.Unlock()
	defer func() {
		c.close()
		v.svc.StopComputerHost()
		v.computer.mu.Lock()
		if v.computer.connection == c {
			v.computer.connection = nil
		}
		v.computer.mu.Unlock()
	}()
	ws.SetReadLimit(1 << 20)
	for {
		var msg struct {
			Type           string          `json:"type"`
			ConversationID string          `json:"conversationId"`
			TurnID         string          `json:"turnId"`
			ID             string          `json:"id"`
			Result         json.RawMessage `json:"result"`
			Error          string          `json:"error"`
		}
		if ws.ReadJSON(&msg) != nil {
			return
		}
		if msg.Type == "stop" {
			v.svc.StopComputer(msg.ConversationID, msg.TurnID)
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		reply := v2HostReply{result: msg.Result}
		if msg.Error != "" {
			reply.err = errors.New(msg.Error)
		}
		select {
		case ch <- reply:
		default:
		}
	}
}
