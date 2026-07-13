package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// errWSRPCUnavailable is returned by wsRPCClient.Call when there is no live WS
// connection to carry the request. Callers treat it as the signal to fall back
// to HTTP.
var errWSRPCUnavailable = errors.New("ws rpc: no active connection")

// errWSRPCWriteBufferFull is returned when the connection's write buffer is
// saturated; the caller falls back to HTTP rather than blocking the socket.
var errWSRPCWriteBufferFull = errors.New("ws rpc: write buffer full")

// wsRPCClient is the daemon-side half of the generic WS request/response
// transport (MUL-4257). It correlates responses to requests by request_id over
// the shared, multiplexed WS control connection so multiple RPCs can be in
// flight concurrently. Sending is delegated to an injected sendFrame func
// (which pushes onto the active connection's write channel); when no connection
// is attached, Call fails fast with errWSRPCUnavailable and the caller uses
// HTTP.
type wsRPCClient struct {
	mu        sync.Mutex
	pending   map[string]chan protocol.RPCResponsePayload
	sendFrame func([]byte) error
	timeout   time.Duration
}

func newWSRPCClient(timeout time.Duration) *wsRPCClient {
	return &wsRPCClient{
		pending: make(map[string]chan protocol.RPCResponsePayload),
		timeout: timeout,
	}
}

// attach binds a live connection's frame writer. Passing nil detaches (on
// disconnect), after which Call fails fast until the next attach. Any pending
// requests are failed so their callers fall back to HTTP immediately.
func (c *wsRPCClient) attach(sendFrame func([]byte) error) {
	c.mu.Lock()
	c.sendFrame = sendFrame
	if sendFrame == nil {
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
	}
	c.mu.Unlock()
}

// connected reports whether a live connection is attached.
func (c *wsRPCClient) connected() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendFrame != nil
}

// Call issues an RPC and blocks until the response, the per-request timeout, or
// ctx cancellation. reqBody is marshaled into the request envelope; on a 2xx
// response respBody (if non-nil) is unmarshaled from the response body. It
// returns the response status (0 when the call never reached the server) so the
// caller can distinguish transport failure (→ HTTP fallback) from a server-side
// error.
func (c *wsRPCClient) Call(ctx context.Context, method string, reqBody, respBody any) (int, error) {
	if c == nil {
		return 0, errWSRPCUnavailable
	}
	var rawReq json.RawMessage
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("ws rpc: marshal request: %w", err)
		}
		rawReq = b
	}
	id := uuid.NewString()
	frame, err := json.Marshal(protocol.Message{
		Type: protocol.EventDaemonRPCRequest,
		Payload: marshalRaw(protocol.RPCRequestPayload{
			RequestID: id,
			Method:    method,
			Body:      rawReq,
		}),
	})
	if err != nil {
		return 0, fmt.Errorf("ws rpc: marshal frame: %w", err)
	}

	ch := make(chan protocol.RPCResponsePayload, 1)
	c.mu.Lock()
	if c.sendFrame == nil {
		c.mu.Unlock()
		return 0, errWSRPCUnavailable
	}
	send := c.sendFrame
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := send(frame); err != nil {
		return 0, fmt.Errorf("ws rpc: send: %w", err)
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-ch:
		if !ok {
			// Connection detached mid-flight.
			return 0, errWSRPCUnavailable
		}
		if resp.Status >= 200 && resp.Status < 300 {
			if respBody != nil && len(resp.Body) > 0 {
				if err := json.Unmarshal(resp.Body, respBody); err != nil {
					return resp.Status, fmt.Errorf("ws rpc: decode response: %w", err)
				}
			}
			return resp.Status, nil
		}
		msg := resp.Error
		if msg == "" {
			msg = fmt.Sprintf("ws rpc status %d", resp.Status)
		}
		return resp.Status, errors.New(msg)
	case <-timer.C:
		return 0, fmt.Errorf("ws rpc: timeout after %s", timeout)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// deliver routes an inbound rpc_response frame to the waiting Call. The send
// happens under the mutex so it is serialized with attach(nil)'s close+delete:
// a channel present in pending is guaranteed not yet closed, so this never
// sends on a closed channel. Unknown request ids (already timed out / detached)
// are dropped.
func (c *wsRPCClient) deliver(resp protocol.RPCResponsePayload) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.pending[resp.RequestID]
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

// ClaimTasksWSFirst is the WS-first claim policy (MUL-4257): it issues the
// tasks.claim RPC over the WS control connection when one is attached, and
// falls back to the HTTP claim endpoint on any transport failure (no
// connection, write-buffer full, timeout) or server error. The request/response
// bodies are identical to the HTTP endpoint so both transports are
// interchangeable. Wired into the claim poller as part of the poller cutover.
func (d *Daemon) ClaimTasksWSFirst(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	if d.wsRPC.connected() {
		var resp struct {
			Tasks []*Task `json:"tasks"`
		}
		reqCtx, cancel := context.WithTimeout(ctx, batchClaimRequestTimeout)
		_, err := d.wsRPC.Call(reqCtx, "tasks.claim", map[string]any{
			"daemon_id":   daemonID,
			"runtime_ids": runtimeIDs,
			"max_tasks":   maxTasks,
		}, &resp)
		cancel()
		if err == nil {
			return resp.Tasks, nil
		}
		d.logger.Debug("ws claim failed; falling back to http", "error", err)
	}
	return d.client.ClaimTasks(ctx, daemonID, runtimeIDs, maxTasks)
}
