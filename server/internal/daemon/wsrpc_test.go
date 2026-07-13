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
	c.attach(func(frame []byte) error {
		var msg protocol.Message
		if err := json.Unmarshal(frame, &msg); err != nil {
			return err
		}
		var req protocol.RPCRequestPayload
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return err
		}
		if req.Method != "tasks.claim" {
			t.Errorf("method = %q, want tasks.claim", req.Method)
		}
		go c.deliver(protocol.RPCResponsePayload{
			RequestID: req.RequestID,
			Status:    200,
			Body:      json.RawMessage(`{"tasks":[{"id":"t1"}]}`),
		})
		return nil
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

// TestWSRPCClient_Timeout: no response arrives within the per-request timeout.
func TestWSRPCClient_Timeout(t *testing.T) {
	c := newWSRPCClient(50 * time.Millisecond)
	c.attach(func([]byte) error { return nil }) // send succeeds, never replies
	status, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
	if err == nil || status != 0 {
		t.Fatalf("status=%d err=%v, want timeout (status 0, err)", status, err)
	}
}

// TestWSRPCClient_ServerError: a non-2xx response surfaces as an error with the
// server-provided message, and a non-zero status so the caller can classify.
func TestWSRPCClient_ServerError(t *testing.T) {
	c := newWSRPCClient(time.Second)
	c.attach(func(frame []byte) error {
		var msg protocol.Message
		json.Unmarshal(frame, &msg)
		var req protocol.RPCRequestPayload
		json.Unmarshal(msg.Payload, &req)
		go c.deliver(protocol.RPCResponsePayload{RequestID: req.RequestID, Status: 400, Error: "bad daemon_id"})
		return nil
	})
	status, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
	if status != 400 || err == nil {
		t.Fatalf("status=%d err=%v, want 400 + error", status, err)
	}
}

// TestWSRPCClient_DetachFailsPending: detaching (disconnect) unblocks in-flight
// Calls with errWSRPCUnavailable so they fall back to HTTP.
func TestWSRPCClient_DetachFailsPending(t *testing.T) {
	c := newWSRPCClient(2 * time.Second)
	c.attach(func([]byte) error { return nil })
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "tasks.claim", 0, nil, nil)
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	c.attach(nil) // detach
	select {
	case err := <-done:
		if !errors.Is(err, errWSRPCUnavailable) {
			t.Fatalf("err = %v, want errWSRPCUnavailable", err)
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
		c.attach(func([]byte) error { return nil })
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
