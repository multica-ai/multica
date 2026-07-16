package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestWSRPCClient_CallRoundTrip: a request is framed and sent, and a matching
// response (by request_id) is decoded into respBody.
func TestWSRPCClient_CallRoundTrip(t *testing.T) {
	c := newWSRPCClient(time.Second)

	// Fake transport: capture the frame, and reply asynchronously with a 200.
	c.attach(func(frame []byte) (*wsOutbound, error) {
		var msg protocol.Message
		if err := json.Unmarshal(frame, &msg); err != nil {
			return nil, err
		}
		var req protocol.RPCRequestPayload
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return nil, err
		}
		if req.Method != "tasks.claim" {
			t.Errorf("method = %q, want tasks.claim", req.Method)
		}
		go c.deliver(protocol.RPCResponsePayload{
			RequestID: req.RequestID,
			Status:    200,
			Body:      json.RawMessage(`{"tasks":[{"id":"t1"}]}`),
		})
		return &wsOutbound{data: frame}, nil
	})
	var resp struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	status, err := c.Call(context.Background(), "tasks.claim", 0, map[string]any{"max_tasks": 3}, &resp)
	if err != nil || status != 200 {
		t.Fatalf("Call: status=%d err=%v", status, err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != "t1" {
		t.Fatalf("resp = %+v, want one task t1", resp)
	}
}

// TestWSRPCClient_Unavailable: with no connection attached, Call fails fast so
// the caller falls back to HTTP.
func TestWSRPCClient_Unavailable(t *testing.T) {
	c := newWSRPCClient(time.Second)
	if _, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil); !errors.Is(err, errWSRPCUnavailable) {
		t.Fatalf("err = %v, want errWSRPCUnavailable", err)
	}
}

// TestWSRPCClient_ReattachRequiresFreshNegotiation pins capability state to a
// specific connection. A replacement sender must remain unavailable until a
// heartbeat ack from that connection explicitly advertises rpc-v1.
func TestWSRPCClient_ReattachRequiresFreshNegotiation(t *testing.T) {
	c := newWSRPCClient(time.Second)
	firstGeneration := c.attach(func(frame []byte) (*wsOutbound, error) {
		return &wsOutbound{data: frame}, nil
	})
	c.markRPCV1Supported(firstGeneration)
	if !c.supportsRPCV1() {
		t.Fatal("first connection should support rpc-v1 after negotiation")
	}

	newConnectionCalls := 0
	secondGeneration := c.attach(func(frame []byte) (*wsOutbound, error) {
		newConnectionCalls++
		return &wsOutbound{data: frame}, nil
	})
	c.markRPCV1Supported(firstGeneration) // delayed ack from the replaced connection
	if c.supportsRPCV1() {
		t.Fatal("replacement connection inherited rpc-v1 support from a stale ack")
	}
	if _, err := c.CallIfRPCV1Supported(context.Background(), "tasks.claim", 0, nil, nil); !errors.Is(err, errWSRPCUnavailable) {
		t.Fatalf("CallIfRPCV1Supported error = %v, want errWSRPCUnavailable before fresh negotiation", err)
	}
	if newConnectionCalls != 0 {
		t.Fatalf("replacement connection received %d RPC calls before negotiation, want 0", newConnectionCalls)
	}
	c.markRPCV1Supported(secondGeneration)
	if !c.supportsRPCV1() {
		t.Fatal("replacement connection did not accept its own rpc-v1 acknowledgement")
	}
}

// TestWSRPCClient_Timeout: no response arrives within the per-request timeout.
func TestWSRPCClient_Timeout(t *testing.T) {
	c := newWSRPCClient(50 * time.Millisecond)
	c.attach(func(frame []byte) (*wsOutbound, error) { return &wsOutbound{data: frame}, nil }) // send succeeds, never replies
	status, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
	if err == nil || status != 0 {
		t.Fatalf("status=%d err=%v, want timeout (status 0, err)", status, err)
	}
}

// TestWSRPCClient_ServerError: a non-2xx response surfaces as an error with the
// server-provided message, and a non-zero status so the caller can classify.
func TestWSRPCClient_ServerError(t *testing.T) {
	c := newWSRPCClient(time.Second)
	c.attach(func(frame []byte) (*wsOutbound, error) {
		var msg protocol.Message
		json.Unmarshal(frame, &msg)
		var req protocol.RPCRequestPayload
		json.Unmarshal(msg.Payload, &req)
		go c.deliver(protocol.RPCResponsePayload{RequestID: req.RequestID, Status: 400, Error: "bad daemon_id"})
		return &wsOutbound{data: frame}, nil
	})
	status, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
	if status != 400 || err == nil {
		t.Fatalf("status=%d err=%v, want 400 + error", status, err)
	}
}

// TestWSRPCClient_DetachFailsPending: detaching (disconnect) unblocks an
// in-flight Call whose frame was already sent with errWSRPCUncertain — the
// caller must not blindly re-claim over HTTP (MUL-4257).
func TestWSRPCClient_DetachFailsPending(t *testing.T) {
	c := newWSRPCClient(2 * time.Second)
	var mu sync.Mutex
	var item *wsOutbound
	c.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	// Simulate the writer having put the frame on the wire before the
	// disconnect, so the server may have processed it → outcome is uncertain.
	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	c.attach(nil) // detach
	select {
	case err := <-done:
		if !errors.Is(err, errWSRPCUncertain) {
			t.Fatalf("err = %v, want errWSRPCUncertain", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Call did not return after detach")
	}
}

// TestWSRPCClient_DeliverDetachRaceNoPanic hammers deliver racing with
// attach(nil) (disconnect). Before the fix, deliver could send on a channel
// attach(nil) had just closed → "send on closed channel" panic. Run under
// -race; passing means the two are serialized under the mutex.
func TestWSRPCClient_DeliverDetachRaceNoPanic(t *testing.T) {
	for iter := 0; iter < 300; iter++ {
		c := newWSRPCClient(time.Second)
		c.attach(func(frame []byte) (*wsOutbound, error) { return &wsOutbound{data: frame}, nil })
		id := "req"
		ch := make(chan protocol.RPCResponsePayload, 1)
		c.mu.Lock()
		c.pending[id] = ch
		c.mu.Unlock()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.deliver(protocol.RPCResponsePayload{RequestID: id, Status: 200})
		}()
		go func() {
			defer wg.Done()
			c.attach(nil) // closes + deletes pending under the same mutex
		}()
		wg.Wait()
	}
}

// TestWSOutbound_CancelBeforeWriteDropsFrame: a caller that gives up before the
// writer sends the frame cancels it, and the writer then skips it (never
// delivered) — the core guarantee that a delayed frame cannot double-claim
// after an HTTP fallback (MUL-4257).
func TestWSOutbound_CancelBeforeWriteDropsFrame(t *testing.T) {
	o := &wsOutbound{data: []byte("x")}
	if !o.cancel() {
		t.Fatal("cancel of a pending frame should succeed")
	}
	if o.beginWrite() {
		t.Fatal("writer must skip a cancelled frame")
	}
}

// TestWSOutbound_WriteBeforeCancelDelivers: once the writer has begun sending a
// frame it can no longer be cancelled, so the caller must treat the outcome as
// uncertain rather than falling back.
func TestWSOutbound_WriteBeforeCancelDelivers(t *testing.T) {
	o := &wsOutbound{data: []byte("x")}
	if !o.beginWrite() {
		t.Fatal("writer should send a pending frame")
	}
	if o.cancel() {
		t.Fatal("cancel must fail once the frame has been sent")
	}
}

// TestWSRPCClient_TimeoutCancelsUnsentFrame reproduces the Sol-Boy backpressure
// blocker: the frame is enqueued but the writer is stalled, so the client times
// out before it is sent. The timeout must cancel the queued frame (so the
// stalled writer later DROPS it) and report a not-sent outcome that is safe to
// HTTP-fall-back — never delivering the stale claim on top of the fallback.
func TestWSRPCClient_TimeoutCancelsUnsentFrame(t *testing.T) {
	c := newWSRPCClient(20 * time.Millisecond)
	var mu sync.Mutex
	var item *wsOutbound
	c.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil // enqueued; no writer ever drains it
	})
	status, err := c.Call(context.Background(), "tasks.claim", 30*time.Millisecond, nil, nil)
	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
	if !errors.Is(err, errWSRPCUnavailable) {
		t.Fatalf("err = %v, want errWSRPCUnavailable (not-sent → safe fallback)", err)
	}
	if errors.Is(err, errWSRPCUncertain) {
		t.Fatal("unsent frame must not be reported uncertain")
	}
	// The stalled writer now wakes up: the frame must have been cancelled so it
	// is dropped, not delivered after the fallback.
	mu.Lock()
	sent := item.beginWrite()
	mu.Unlock()
	if sent {
		t.Fatal("timed-out frame must be dropped by the writer to avoid double-claim")
	}
}

// TestWSRPCClient_TimeoutUncertainWhenAlreadySent: if the writer already put the
// frame on the wire, a subsequent client timeout is uncertain (the server may
// have it) and must NOT fall back.
func TestWSRPCClient_TimeoutUncertainWhenAlreadySent(t *testing.T) {
	c := newWSRPCClient(30 * time.Millisecond)
	var mu sync.Mutex
	var item *wsOutbound
	c.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "tasks.claim", 40*time.Millisecond, nil, nil)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	item.beginWrite() // writer sends it before the timeout fires
	mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, errWSRPCUncertain) {
			t.Fatalf("err = %v, want errWSRPCUncertain", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return")
	}
}
