package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
// server may have committed), so the caller must not issue a fresh mutation.
// An idempotent API may safely replay the same operation ID over HTTP.
var errWSRPCUncertain = errors.New("ws rpc: sent but outcome unknown (connection lost)")

// wsRPCResponseGrace is how much longer the daemon waits for an RPC response
// beyond the server-side execution budget it requested, so a claim that
// committed just before the server deadline still reports back before the
// daemon gives up (MUL-4257).
const wsRPCResponseGrace = 2 * time.Second

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

// attach binds a live connection's frame writer. Passing nil detaches (on
// disconnect), after which Call fails fast until the next attach. Any pending
// requests are failed so their callers can either fall back or replay,
// according to the called method's idempotency contract.
func (c *wsRPCClient) attach(sendFrame func([]byte) (*wsOutbound, error)) {
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
func (c *wsRPCClient) Call(ctx context.Context, method string, serverTimeout time.Duration, reqBody, respBody any) (int, error) {
	return c.CallWithID(ctx, uuid.NewString(), method, serverTimeout, reqBody, respBody)
}

// CallWithID is Call with a caller-supplied correlation id. Idempotent claim
// v2 uses claim_attempt_id here so daemon and server logs share one identifier
// across WS delivery and HTTP replay.
func (c *wsRPCClient) CallWithID(ctx context.Context, id, method string, serverTimeout time.Duration, reqBody, respBody any) (int, error) {
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

	item, err := send(frame)
	if err != nil {
		return 0, fmt.Errorf("ws rpc: send: %w", err)
	}

	// giveUp resolves an abandoned request. If the frame is still queued we
	// cancel it so the writer never delivers it — a definitively-not-sent
	// outcome that is safe to HTTP-fall-back. If the writer already began
	// sending it, it may reach the server, so the outcome is uncertain and the
	// caller must either stop or replay the same idempotent operation ID.
	giveUp := func() error {
		if item.cancel() {
			return errWSRPCUnavailable
		}
		return errWSRPCUncertain
	}

	// Wait the server-side budget PLUS a grace margin: a claim that committed
	// just before the server deadline must still report back before the daemon
	// gives up, avoiding an unnecessary replay in the normal path.
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
		// outcome is uncertain; only an idempotent same-ID replay is safe.
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

type pendingClaimAttempt struct {
	ID                   string
	DaemonID             string
	RuntimeIDs           []string
	MaxTasks             int
	MayHaveReachedServer bool
}

// ClaimTasksWSFirst uses tasks.claim.v2 when supported. A WS timeout or detach
// replays the same attempt over HTTP; it never turns an uncertain mutation into
// a fresh claim. The pending attempt remains in memory across poll cycles until
// the server returns its durable task set or a terminal expiry.
func (d *Daemon) ClaimTasksWSFirst(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	d.claimAttemptMu.Lock()
	defer d.claimAttemptMu.Unlock()

	if d.claimReplayUnsupported.Load() && d.pendingClaimAttempt == nil {
		return d.claimTasksV1(ctx, daemonID, runtimeIDs, maxTasks)
	}

	canonicalRuntimeIDs := canonicalClaimRuntimeIDs(runtimeIDs)
	attempt := d.pendingClaimAttempt
	if attempt == nil {
		attempt = &pendingClaimAttempt{
			ID:         uuid.NewString(),
			DaemonID:   daemonID,
			RuntimeIDs: canonicalRuntimeIDs,
			MaxTasks:   maxTasks,
		}
		d.pendingClaimAttempt = attempt
	} else {
		if attempt.DaemonID != daemonID {
			return nil, fmt.Errorf("pending claim attempt daemon changed from %q to %q", attempt.DaemonID, daemonID)
		}
		if maxTasks < attempt.MaxTasks {
			return nil, fmt.Errorf("pending claim attempt requires %d slots, only %d available", attempt.MaxTasks, maxTasks)
		}
	}

	body := map[string]any{
		"claim_attempt_id": attempt.ID,
		"daemon_id":        attempt.DaemonID,
		"runtime_ids":      attempt.RuntimeIDs,
		"max_tasks":        attempt.MaxTasks,
	}
	if d.wsRPC.connected() {
		var resp claimAttemptResponse
		status, err := d.wsRPC.CallWithID(ctx, attempt.ID, "tasks.claim.v2", batchClaimRequestTimeout, body, &resp)
		if err == nil {
			return d.finishClaimAttempt(ctx, attempt, resp)
		}
		if errors.Is(err, errWSRPCUncertain) {
			attempt.MayHaveReachedServer = true
			d.logger.Debug("ws claim outcome uncertain; replaying the same attempt over http",
				"claim_attempt_id", attempt.ID)
		} else if status == http.StatusNotFound && !attempt.MayHaveReachedServer {
			// A definitive unknown-method response proves this WS request did
			// not mutate state. It is safe to retain mixed-version v1 behavior.
			d.claimReplayUnsupported.Store(true)
			d.pendingClaimAttempt = nil
			return d.claimTasksV1(ctx, daemonID, runtimeIDs, maxTasks)
		} else {
			d.logger.Debug("ws claim v2 failed; replaying the same attempt over http",
				"claim_attempt_id", attempt.ID, "error", err)
		}
	}

	resp, err := d.client.ClaimTasksAttempt(ctx, attempt.ID, attempt.DaemonID, attempt.RuntimeIDs, attempt.MaxTasks)
	if err == nil {
		return d.finishClaimAttempt(ctx, attempt, resp)
	}
	if isClaimReplayUnsupported(err) && !attempt.MayHaveReachedServer {
		d.claimReplayUnsupported.Store(true)
		d.pendingClaimAttempt = nil
		return d.claimTasksV1(ctx, daemonID, runtimeIDs, maxTasks)
	}
	attempt.MayHaveReachedServer = true
	if isClaimAttemptExpired(err) {
		d.pendingClaimAttempt = nil
	}
	return nil, fmt.Errorf("replay claim attempt %s: %w", attempt.ID, err)
}

func (d *Daemon) finishClaimAttempt(ctx context.Context, attempt *pendingClaimAttempt, resp claimAttemptResponse) ([]*Task, error) {
	if resp.ClaimAttemptID != attempt.ID {
		return nil, fmt.Errorf("claim replay returned attempt %q, want %q", resp.ClaimAttemptID, attempt.ID)
	}
	if resp.State != "ready" {
		return nil, fmt.Errorf("claim replay returned state %q, want ready", resp.State)
	}
	if len(resp.Tasks) > attempt.MaxTasks {
		return nil, fmt.Errorf("claim replay returned %d tasks for %d slots", len(resp.Tasks), attempt.MaxTasks)
	}
	allowedRuntimes := make(map[string]struct{}, len(attempt.RuntimeIDs))
	for _, runtimeID := range attempt.RuntimeIDs {
		allowedRuntimes[runtimeID] = struct{}{}
	}
	seenTasks := make(map[string]struct{}, len(resp.Tasks))
	for _, task := range resp.Tasks {
		if task == nil || task.ID == "" {
			return nil, errors.New("claim replay returned an empty task")
		}
		if _, duplicate := seenTasks[task.ID]; duplicate {
			return nil, fmt.Errorf("claim replay returned duplicate task %s", task.ID)
		}
		seenTasks[task.ID] = struct{}{}
		if _, allowed := allowedRuntimes[task.RuntimeID]; !allowed {
			return nil, fmt.Errorf("claim replay returned task %s for unexpected runtime %s", task.ID, task.RuntimeID)
		}
	}

	// ACK is best effort: StartTask is an implicit durable acknowledgement, so
	// an ACK transport failure must not delay execution of a received task set.
	ackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	if err := d.client.AcknowledgeClaimAttempt(ackCtx, attempt.ID, attempt.DaemonID); err != nil {
		d.logger.Debug("claim attempt ack failed; StartTask will acknowledge implicitly",
			"claim_attempt_id", attempt.ID, "error", err)
	}
	cancel()
	d.pendingClaimAttempt = nil
	return resp.Tasks, nil
}

func canonicalClaimRuntimeIDs(runtimeIDs []string) []string {
	seen := make(map[string]struct{}, len(runtimeIDs))
	out := make([]string, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		if runtimeID == "" {
			continue
		}
		if _, duplicate := seen[runtimeID]; duplicate {
			continue
		}
		seen[runtimeID] = struct{}{}
		out = append(out, runtimeID)
	}
	sort.Strings(out)
	return out
}

func isClaimReplayUnsupported(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound &&
		strings.Contains(strings.ToLower(reqErr.Body), "page not found")
}

func isClaimAttemptExpired(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusGone
}

func (d *Daemon) claimTasksV1(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	// Un-upgraded server without the batch route: a prior poll already learned
	// this (via a 404), so go straight to the legacy per-runtime claim and skip
	// the WS + batch attempts each cycle.
	if d.batchClaimUnsupported.Load() {
		return d.client.claimTasksLegacy(ctx, runtimeIDs, maxTasks)
	}
	if d.wsRPC.connected() {
		var resp struct {
			Tasks []*Task `json:"tasks"`
		}
		// batchClaimRequestTimeout is the server-side execution budget; the
		// daemon waits that plus the client's grace margin for the response.
		_, err := d.wsRPC.Call(ctx, "tasks.claim", batchClaimRequestTimeout, map[string]any{
			"daemon_id":   daemonID,
			"runtime_ids": runtimeIDs,
			"max_tasks":   maxTasks,
		}, &resp)
		if err == nil {
			return resp.Tasks, nil
		}
		if errors.Is(err, errWSRPCUncertain) {
			// The WS claim may have committed server-side; claiming the same
			// free slots again over HTTP would double-claim. Skip this cycle —
			// an orphaned server-side claim is recovered by stale reclaim and
			// the next poll picks up anything still queued.
			d.logger.Debug("ws claim outcome uncertain after disconnect; skipping fallback this cycle")
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
