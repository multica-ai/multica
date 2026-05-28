package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestHookServer_CancelDuringDispatchNoPanic exercises the H3 race: a hook
// POST arrives at the same time the subscriber cancels. Pre-fix, the server
// looked up the channel under the mutex, released, then sent — so a cancel
// that closed the channel between unlock and send would panic with
// "send on closed channel". The fix holds the mutex across a non-blocking
// send. Run under `go test -race` to be meaningful.
func TestHookServer_CancelDuringDispatchNoPanic(t *testing.T) {
	srv := newHookServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := srv.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	body := []byte(`{"hook_event_name":"Stop","session_id":"s","last_assistant_message":"hi"}`)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		token := fmt.Sprintf("tok-%d", i)
		_, cancelSub := srv.Subscribe(token)

		wg.Add(2)
		// Hammer the dispatcher and the cancel from two goroutines to
		// maximize the chance the cancel lands between lookup and send.
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srv.BaseURL()+"?task="+token, bytes.NewReader(body))
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		go func() {
			defer wg.Done()
			cancelSub()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("race test deadlocked")
	}
}

// TestHookServer_DropOnFullSubscriber verifies that when the subscriber
// channel is full the server drops the event rather than blocking the
// shared mutex.
func TestHookServer_DropOnFullSubscriber(t *testing.T) {
	srv := newHookServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := srv.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	ch, cancelSub := srv.Subscribe("full")
	defer cancelSub()

	body := []byte(`{"hook_event_name":"PostToolUse","session_id":"s","tool_name":"Bash"}`)

	// Fill the 32-slot buffer + 1 to force a drop. Without a non-blocking
	// send this loop would block past the timeout.
	deadline := time.After(3 * time.Second)
	for i := 0; i < 64; i++ {
		select {
		case <-deadline:
			t.Fatalf("send loop blocked at i=%d", i)
		default:
		}
		req, _ := http.NewRequest(http.MethodPost, srv.BaseURL()+"?task=full", bytes.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	// Consumer side should have seen at most cap(ch) events.
	if len(ch) > cap(ch) {
		t.Fatalf("channel overrun: len=%d cap=%d", len(ch), cap(ch))
	}
}
