package localhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/service"
)

// hostTimeout bounds how long an execution waits for Desktop; questions
// to the user use ask_user_question, not host requests.
const hostTimeout = 10 * time.Minute

// hostGrace keeps a host request of a client that disconnected, so a
// Desktop reconnecting after a network blip still answers it.
const hostGrace = 60 * time.Second

// hosts routes daemon requests to the Desktop connections of a scope.
type hosts struct {
	mu      sync.Mutex
	byScope map[int64][]*wsConn
	pending map[string]*hostPending
	// orphans are pending requests of clients that disconnected, by client.
	orphans map[string][]*hostPending
	// origin names the client that last acted on a conversation.
	origin func(conversationID string) string
	grace  time.Duration
}

type hostPending struct {
	reply   chan v2HostReply
	message map[string]any
	// conn is the connection that owes the reply, nil while its client is
	// reconnecting; guarded by hosts.mu.
	conn *wsConn
}

type v2HostReply struct {
	result json.RawMessage
	err    error
}

// attach makes c the scope's preferred Desktop, hands it the requests its
// client left when it disconnected, and returns its detach.
func (h *hosts) attach(scopeID int64, c *wsConn) func() {
	h.mu.Lock()
	h.byScope[scopeID] = append(h.byScope[scopeID], c)
	var resend []*hostPending
	if c.caller.clientKey != "" {
		resend = h.orphans[c.caller.clientKey]
		delete(h.orphans, c.caller.clientKey)
		for _, p := range resend {
			p.conn = c
		}
	}
	h.mu.Unlock()
	for _, p := range resend {
		_ = c.send(p.message)
	}
	return func() { h.detach(scopeID, c) }
}

func (h *hosts) detach(scopeID int64, c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.byScope[scopeID]
	for i, x := range conns {
		if x == c {
			h.byScope[scopeID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	var left []*hostPending
	for _, p := range h.pending {
		if p.conn == c {
			p.conn = nil
			left = append(left, p)
		}
	}
	if len(left) == 0 {
		return
	}
	if c.caller.clientKey == "" {
		for _, p := range left {
			h.fail(p)
		}
		return
	}
	key := c.caller.clientKey
	h.orphans[key] = append(h.orphans[key], left...)
	time.AfterFunc(h.grace, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, p := range h.orphans[key] {
			if p.conn == nil {
				h.fail(p)
			}
		}
		delete(h.orphans, key)
	})
}

// fail ends p with ErrHostUnavailable; h.mu is held.
func (h *hosts) fail(p *hostPending) {
	select {
	case p.reply <- v2HostReply{err: service.ErrHostUnavailable}:
	default:
	}
}

// touch prefers c, the connection the user acted through last.
func (h *hosts) touch(scopeID int64, c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.byScope[scopeID]
	for i, x := range conns {
		if x == c && i != len(conns)-1 {
			h.byScope[scopeID] = append(append(conns[:i:i], conns[i+1:]...), c)
			return
		}
	}
}

// target picks the connection of the client that last acted on the
// conversation, or else the scope's preferred one; h.mu is held.
func (h *hosts) target(scopeID int64, conversationID string) *wsConn {
	conns := h.byScope[scopeID]
	if len(conns) == 0 {
		return nil
	}
	if h.origin != nil {
		if key := h.origin(conversationID); key != "" {
			for i := len(conns) - 1; i >= 0; i-- {
				if conns[i].caller.clientKey == key {
					return conns[i]
				}
			}
		}
	}
	return conns[len(conns)-1]
}

// localTarget is target among this device's own connections; a remote
// device's Desktop attaches to the same scope but is another machine.
func (h *hosts) localTarget(scopeID int64, conversationID string) *wsConn {
	var fallback *wsConn
	origin := ""
	if h.origin != nil {
		origin = h.origin(conversationID)
	}
	conns := h.byScope[scopeID]
	for i := len(conns) - 1; i >= 0; i-- {
		c := conns[i]
		if c.caller.kind == callerRemote {
			continue
		}
		if origin != "" && c.caller.clientKey == origin {
			return c
		}
		if fallback == nil {
			fallback = c
		}
	}
	return fallback
}

// request sends kind to a Desktop of the scope and waits for its reply.
func (h *hosts) request(ctx context.Context, scopeID int64, conversationID, kind string, payload any) (json.RawMessage, error) {
	h.mu.Lock()
	c := h.target(scopeID, conversationID)
	if c == nil {
		h.mu.Unlock()
		return nil, service.ErrHostUnavailable
	}
	return h.send(ctx, c, kind, payload)
}

// requestLocal sends kind to the Desktop running on this device only.
func (h *hosts) requestLocal(ctx context.Context, scopeID int64, conversationID, kind string, payload any) (json.RawMessage, error) {
	h.mu.Lock()
	c := h.localTarget(scopeID, conversationID)
	if c == nil {
		h.mu.Unlock()
		return nil, service.ErrLocalHostUnavailable
	}
	return h.send(ctx, c, kind, payload)
}

// send delivers a request to c and waits for its reply; h.mu is held and
// released here so the pending entry is visible before the request is.
func (h *hosts) send(ctx context.Context, c *wsConn, kind string, payload any) (json.RawMessage, error) {
	id := "host-" + newRequestID()
	p := &hostPending{reply: make(chan v2HostReply, 1), conn: c,
		message: map[string]any{"version": V2Version, "type": "host.request", "request_id": id, "kind": kind, "payload": payload}}
	h.pending[id] = p
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()
	if err := c.send(p.message); err != nil {
		return nil, service.ErrHostUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, hostTimeout)
	defer cancel()
	select {
	case r := <-p.reply:
		return r.result, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// reply delivers Desktop's answer to the pending request it names.
func (h *hosts) reply(c *wsConn, msg wsClientMessage) {
	h.mu.Lock()
	p := h.pending[msg.RequestID]
	answerable := p != nil && p.conn == c
	h.mu.Unlock()
	if !answerable {
		return
	}
	r := v2HostReply{result: msg.Result}
	if msg.Error != nil {
		r.err = &service.HostError{Kind: msg.Error.Kind, Message: msg.Error.Message}
	}
	select {
	case p.reply <- r:
	default:
	}
}

func newRequestID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
