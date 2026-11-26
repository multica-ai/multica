package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newRelayRequest(id string) *FileChangeRelayRequest {
	now := time.Now()
	return &FileChangeRelayRequest{
		ID:        id,
		TaskID:    "task-1",
		FileID:    "file-1",
		RuntimeID: "runtime-1",
		CreatedAt: now,
		ExpiresAt: now.Add(fileChangeRelayTTL),
	}
}

func TestFileChangeRelayStoreLifecycle(t *testing.T) {
	s := NewInMemoryFileChangeRelayStore()
	ctx := context.Background()
	req := newRelayRequest("req-1")
	if err := s.Create(ctx, req); err != nil {
		t.Fatal(err)
	}
	// Claims are exclusive: the second pull sees nothing.
	first, err := s.ClaimPending(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].RequestID != "req-1" || first[0].TaskID != "task-1" || first[0].FileID != "file-1" {
		t.Fatalf("first claim: %+v", first)
	}
	second, err := s.ClaimPending(ctx, "runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("double claim: %+v", second)
	}
	// Claims are runtime-scoped.
	other := newRelayRequest("req-2")
	other.RuntimeID = "runtime-2"
	if err := s.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ClaimPending(ctx, "runtime-1"); len(got) != 0 {
		t.Fatalf("cross-runtime claim: %+v", got)
	}
	// A response wakes the waiter.
	go func() {
		if err := s.PutResponse(ctx, "req-1", &FileChangeRelayResponse{State: "purged"}); err != nil {
			t.Error(err)
		}
	}()
	resp, found := s.WaitResponse(ctx, "req-1", time.Now().Add(time.Second))
	if !found || resp == nil || resp.State != "purged" {
		t.Fatalf("wait: found=%v resp=%+v", found, resp)
	}
}

func TestFileChangeRelayStoreWaitTimesOut(t *testing.T) {
	s := NewInMemoryFileChangeRelayStore().(*memFileChangeRelayStore)
	ctx := context.Background()
	if err := s.Create(ctx, newRelayRequest("req-3")); err != nil {
		t.Fatal(err)
	}
	if _, found := s.WaitResponse(ctx, "req-3", time.Now().Add(-time.Second)); found {
		t.Fatal("unexpected response")
	}
	if len(s.waiters) != 0 {
		t.Fatal("waiter retained after timeout")
	}
	// Cancelled context ends the wait.
	ctx2, cancel := context.WithCancel(ctx)
	cancel()
	if _, found := s.WaitResponse(ctx2, "req-3", time.Now().Add(time.Second)); found {
		t.Fatal("unexpected response after cancel")
	}
	if len(s.waiters) != 0 {
		t.Fatal("waiter retained after cancellation")
	}
}

func TestFileChangeRelayStoreCollectsExpiredTraffic(t *testing.T) {
	s := NewInMemoryFileChangeRelayStore().(*memFileChangeRelayStore)
	ctx := context.Background()
	expired := newRelayRequest("expired")
	live := newRelayRequest("live")
	for _, req := range []*FileChangeRelayRequest{expired, live} {
		if err := s.Create(ctx, req); err != nil {
			t.Fatal(err)
		}
		if err := s.PutResponse(ctx, req.ID, &FileChangeRelayResponse{State: "purged"}); err != nil {
			t.Fatal(err)
		}
	}
	// Age the request just past expiry, within the old extra-TTL window.
	// Creating new traffic must release both its metadata and its body.
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if err := s.Create(ctx, newRelayRequest("next")); err != nil {
		t.Fatal(err)
	}
	if s.requests[expired.ID] != nil || s.responses[expired.ID] != nil {
		t.Fatal("expired relay request or response retained until a second TTL")
	}
	if req, err := s.Get(ctx, live.ID); err != nil || req == nil {
		t.Fatalf("live request lost during collection: req=%+v err=%v", req, err)
	}
	resp, found := s.WaitResponse(ctx, live.ID, time.Now())
	if !found || resp == nil || resp.State != "purged" {
		t.Fatalf("live response lost during collection: found=%v resp=%+v", found, resp)
	}
}

func TestFileChangeRelayResponseShape(t *testing.T) {
	// The ok envelope carries the daemon's raw File JSON unchanged.
	raw := json.RawMessage(`{"id":"file-1","path":"a.txt","before":"x","after":"y"}`)
	data, err := json.Marshal(&FileChangeRelayResponse{State: "ok", File: raw})
	if err != nil {
		t.Fatal(err)
	}
	var back FileChangeRelayResponse
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.State != "ok" || string(back.File) != string(raw) {
		t.Fatalf("roundtrip: %s", data)
	}
}
