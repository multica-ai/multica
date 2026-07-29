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

// errWSRPCUncertain is returned when a request's frame WAS sent but the
// connection dropped before a definitive response. The outcome is unknown (the
// server may have committed), so the caller must NOT fall back to another
// transport for the same work — that risks a double claim (MUL-4257).
var errWSRPCUncertain = errors.New("ws rpc: sent but outcome unknown (connection lost)")

// wsRPCResponseGrace is how much longer the daemon waits for an RPC response
// beyond the server-side execution budget it requested, so a claim that
// committed just before the server deadline still reports back before the
// daemon gives up (MUL-4257).
const wsRPCResponseGrace = 2 * time.Second

var wsClaimUncertainFallbackDelay = batchClaimRequestTimeout + wsRPCResponseGrace

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
// wsOutbound is a frame queued for the WS writer. It is cancelable so an RPC
// caller that gives up (timeout/detach) before the frame has hit the socket can
// prevent it from being delivered later — otherwise a backpressured writer
// could deliver a stale tasks.claim after the daemon already HTTP-fell-back,
// double-claiming (MUL-4257, Sol-Boy review). sent/cancel race under mu so the
// decision is atomic: whoever wins determines whether the frame is delivered.
type wsOutbound struct {
	data     []byte
	mu       sync.Mutex
	sent     bool
	canceled bool
}

// beginWrite is called by the writer immediately before WriteMessage. It
// returns false when the frame was already cancelled (skip it); otherwise it
// marks the frame sent so a concurrent cancel() can no longer un-send it.
func (o *wsOutbound) beginWrite() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.canceled {
		return false
	}
	o.sent = true
	return true
}

// cancel is called by an RPC caller giving up. Returns true if the frame was
// still pending (now cancelled — the writer will skip it, so it is guaranteed
// NOT delivered); false if the writer already began sending it.
func (o *wsOutbound) cancel() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sent {
		return false
	}
	o.canceled = true
	return true
}

type wsRPCClient struct {
	mu        sync.Mutex
	pending   map[string]chan protocol.RPCResponsePayload
	sendFrame func([]byte) (*wsOutbound, error)
	// rpcV1Supported belongs to the currently attached connection. attach
	// clears it before exposing a replacement sender so a claim that races a
	// reconnect can never carry negotiation state across connections.
	rpcV1Supported bool
	generation     uint64
	// grace is added to a call's server-side timeout budget to get how long the
	// daemon waits for the response, so a claim that committed just before the
	// server deadline still reports back before the daemon gives up (MUL-4257).
	grace time.Duration
}

func newWSRPCClient(grace time.Duration) *wsRPCClient {
	return &wsRPCClient{
		pending: make(map[string]chan protocol.RPCResponsePayload),
		grace:   grace,
	}
}

// attach binds a live connection's frame writer and clears the previous
// connection's negotiated capability. Passing nil detaches (on disconnect),
// after which Call fails fast until the next attach and rpc-v1 heartbeat ack.
// Any pending requests are failed so their callers fall back to HTTP
// immediately.
func (c *wsRPCClient) attach(sendFrame func([]byte) (*wsOutbound, error)) uint64 {
	c.mu.Lock()
	c.generation++
	c.rpcV1Supported = false
	c.sendFrame = sendFrame
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	generation := c.generation
	c.mu.Unlock()
	return generation
}

// markRPCV1Supported records explicit server support for the currently
// attached connection. Heartbeat acks received without a live sender cannot
// enable a future connection.
func (c *wsRPCClient) markRPCV1Supported(generation uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.sendFrame != nil && c.generation == generation {
		c.rpcV1Supported = true
	}
	c.mu.Unlock()
}

func (c *wsRPCClient) currentGeneration() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

// supportsRPCV1 reports whether the live connection explicitly negotiated
// rpc-v1. Call repeats this check while capturing the sender under the same
// mutex, so this method is only a fast-path hint and cannot authorize a send by
// itself.
func (c *wsRPCClient) supportsRPCV1() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendFrame != nil && c.rpcV1Supported
}

// Call issues an RPC on any attached connection. Transport-level tests and
// callers that have their own negotiation contract use this directly.
func (c *wsRPCClient) Call(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any) (int, error) {
	return c.call(ctx, method, serverTimeout, reqBody, respBody, false)
}

// CallIfRPCV1Supported issues an RPC only when the currently attached
// connection explicitly negotiated rpc-v1. The capability check and sender
// capture happen under the same mutex, so a reconnect cannot redirect a call
// authorized by the previous connection onto its replacement.
func (c *wsRPCClient) CallIfRPCV1Supported(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any) (int, error) {
	return c.call(ctx, method, serverTimeout, reqBody, respBody, true)
}

// call blocks until the response, the per-request timeout, or ctx
// cancellation. reqBody is marshaled into the request envelope; on a 2xx
// response respBody (if non-nil) is unmarshaled from the response body. It
// returns the response status (0 when the call never reached the server) so the
// caller can distinguish transport failure (→ HTTP fallback) from a
// server-side error.
func (c *wsRPCClient) call(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any, requireRPCV1 bool) (int, error) {
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
			TimeoutMs: serverTimeout.Milliseconds(),
		}),
	})
	if err != nil {
		return 0, fmt.Errorf("ws rpc: marshal frame: %w", err)
	}

	ch := make(chan protocol.RPCResponsePayload, 1)
	c.mu.Lock()
	if c.sendFrame == nil || (requireRPCV1 && !c.rpcV1Supported) {
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

	item, err := send(frame)
	if err != nil {
		return 0, fmt.Errorf("ws rpc: send: %w", err)
	}

	// giveUp resolves an abandoned request. If the frame is still queued we
	// cancel it so the writer never delivers it — a definitively-not-sent
	// outcome that is safe to HTTP-fall-back. If the writer already began
	// sending it, it may reach the server, so the outcome is uncertain and the
	// caller must NOT fall back (that would double-claim, MUL-4257).
	giveUp := func() error {
		if item.cancel() {
			return errWSRPCUnavailable
		}
		return errWSRPCUncertain
	}

	// Wait the server-side budget PLUS a grace margin: a claim that committed
	// just before the server deadline must still report back before the daemon
	// gives up and falls back to HTTP, or we would double-claim (MUL-4257).
	timeout := serverTimeout + c.grace
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-ch:
		if !ok {
			// The connection detached. Whether the server saw this request
			// depends on whether the frame had already left the writer, so let
			// giveUp() decide (not-sent → safe fallback; sent → uncertain).
			return 0, giveUp()
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
		// The budget elapsed. If the frame is still queued behind a
		// backpressured writer, cancel it so it is never delivered after we
		// fall back (giveUp → not-sent). If it already left the writer, the
		// outcome is uncertain and we must not fall back.
		if err := giveUp(); errors.Is(err, errWSRPCUncertain) {
			return 0, err
		}
		return 0, fmt.Errorf("ws rpc: timeout after %s: %w", timeout, errWSRPCUnavailable)
	case <-ctx.Done():
		item.cancel()
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
// falls back to the HTTP claim endpoint on transport failures that are known not
// to have reached the server (no connection, write-buffer full, unsent timeout)
// or server error. A sent-frame disconnect/timeout is uncertain, so it is
// retried over HTTP only after a short safety window. The request/response bodies
// are identical to the HTTP endpoint so both transports are interchangeable.
// Wired into the claim poller as part of the poller cutover.
func (d *Daemon) ClaimTasksWSFirst(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	// Un-upgraded server without the batch route: a prior poll already learned
	// this (via a 404), so go straight to the legacy per-runtime claim and skip
	// the WS + batch attempts each cycle.
	if d.batchClaimUnsupported.Load() {
		return d.client.claimTasksLegacy(ctx, runtimeIDs, maxTasks)
	}
	bypassWSOnce := false
	if retryAfterNanos := d.wsClaimHTTPFallbackAfter.Load(); retryAfterNanos > 0 {
		retryAfter := time.Unix(0, retryAfterNanos)
		now := time.Now()
		if now.Before(retryAfter) {
			d.logger.Debug("ws claim outcome uncertain; delaying http fallback until safety window elapses",
				"retry_after", retryAfter.Sub(now).Round(time.Millisecond))
			return nil, nil
		}
		if d.wsClaimHTTPFallbackAfter.CompareAndSwap(retryAfterNanos, 0) {
			bypassWSOnce = true
			d.logger.Debug("previous ws claim outcome uncertain; using http fallback for this claim cycle")
		}
	}
	if !bypassWSOnce && d.wsRPC.supportsRPCV1() {
		var resp struct {
			Tasks []*Task `json:"tasks"`
		}
		// batchClaimRequestTimeout is the server-side execution budget; the
		// daemon waits that plus the client's grace margin for the response.
		_, err := d.wsRPC.CallIfRPCV1Supported(ctx, "tasks.claim", batchClaimRequestTimeout, map[string]any{
			"daemon_id":   daemonID,
			"runtime_ids": runtimeIDs,
			"max_tasks":   maxTasks,
		}, &resp)
		if err == nil {
			return resp.Tasks, nil
		}
		if errors.Is(err, errWSRPCUncertain) {
			// The WS claim may have committed server-side; claiming the same
			// free slots again over HTTP immediately would double-claim. Skip
			// this cycle, then force one HTTP batch claim after the server-side
			// execution budget plus response grace has elapsed. If the WS claim
			// committed, the task is already dispatched and stale reclaim owns
			// recovery; if it did not, HTTP regains liveness for the queued task.
			delay := wsClaimUncertainFallbackDelay
			if delay < 0 {
				delay = 0
			}
			d.wsClaimHTTPFallbackAfter.Store(time.Now().Add(delay).UnixNano())
			d.logger.Debug("ws claim outcome uncertain after disconnect; delaying http fallback", "retry_after", delay)
			return nil, nil
		}
		d.logger.Debug("ws claim failed; falling back to http", "error", err)
	}
	tasks, err := d.client.ClaimTasks(ctx, daemonID, runtimeIDs, maxTasks)
	if err == nil {
		return tasks, nil
	}
	// Server has no batch route (404): freeze the old API contract by falling
	// back to the legacy per-runtime claim loop, and remember it so we don't
	// re-probe every cycle.
	if isBatchClaimUnsupported(err) {
		d.batchClaimUnsupported.Store(true)
		d.logger.Info("batch claim route unsupported by server; using legacy per-runtime claim")
		return d.client.claimTasksLegacy(ctx, runtimeIDs, maxTasks)
	}
	return nil, err
}
