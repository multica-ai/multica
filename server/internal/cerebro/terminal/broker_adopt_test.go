package terminal

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// TestBroker_AdoptCreatesSessionVisibleByRuntime proves Adopt registers an
// externally-owned session that GetByRuntime can find. This is what the
// active-session HTTP endpoint relies on to answer "is there a terminal
// for this runtime?" without scanning every session.
func TestBroker_AdoptCreatesSessionVisibleByRuntime(t *testing.T) {
	b := NewBroker()
	s, err := b.Adopt(AdoptOptions{
		RuntimeID:   "rt-adopt-1",
		WorkspaceID: "ws-1",
		OwnerUserID: "u-1",
		TaskID:      "task-1",
		Command:     "claude",
	})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if !s.External() {
		t.Errorf("expected adopted session to be external")
	}
	if got := b.GetByRuntime("rt-adopt-1"); got != s {
		t.Fatalf("GetByRuntime returned %v, want %v", got, s)
	}
	if got := b.Get(s.ID); got != s {
		t.Fatalf("Get by id returned %v, want %v", got, s)
	}
}

// TestBroker_AdoptPushOutputFansOutToMultipleSubscribers proves the same
// fan-out semantics as a real subprocess session, but for an externally
// owned source.
func TestBroker_AdoptPushOutputFansOutToMultipleSubscribers(t *testing.T) {
	b := NewBroker()
	s, err := b.Adopt(AdoptOptions{RuntimeID: "rt-adopt-fanout", TaskID: "task-fanout"})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(s.ID) })

	subA, _, detachA := s.Attach()
	t.Cleanup(detachA)
	subB, _, detachB := s.Attach()
	t.Cleanup(detachB)

	s.PushOutput([]byte("hello "))
	s.PushOutput([]byte("world\n"))

	gotA := readChunks(t, subA, 2, 2*time.Second)
	gotB := readChunks(t, subB, 2, 2*time.Second)
	if !bytes.Contains(gotA, []byte("hello world\n")) {
		t.Errorf("subscriber A missed payload: %q", gotA)
	}
	if !bytes.Contains(gotB, []byte("hello world\n")) {
		t.Errorf("subscriber B missed payload: %q", gotB)
	}
}

// TestBroker_AdoptStdinChanDeliversAttachStdin proves the bridge can read
// browser-originated stdin from an adopted session. v1 won't use this in
// production, but the path is exercised end-to-end.
func TestBroker_AdoptStdinChanDeliversAttachStdin(t *testing.T) {
	b := NewBroker()
	s, err := b.Adopt(AdoptOptions{RuntimeID: "rt-stdin", TaskID: "task-stdin"})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(s.ID) })

	_, write, detach := s.Attach()
	t.Cleanup(detach)

	if err := write([]byte("hi\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	select {
	case got, ok := <-s.StdinChan():
		if !ok {
			t.Fatal("stdin channel closed before delivery")
		}
		if string(got) != "hi\n" {
			t.Errorf("got %q, want %q", got, "hi\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stdin frame")
	}
}

// TestBroker_AdoptExitClosesSessionAndRemovesFromByRuntime proves the
// runtime-index entry is cleaned up so a stale lookup doesn't keep
// returning a closed session.
func TestBroker_AdoptExitClosesSessionAndRemovesFromByRuntime(t *testing.T) {
	b := NewBroker()
	s, err := b.Adopt(AdoptOptions{RuntimeID: "rt-exit", TaskID: "task-exit"})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	sub, _, detach := s.Attach()
	t.Cleanup(detach)

	s.PushOutput([]byte("running...\n"))
	s.Exit(42, errors.New("boom"))

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session did not signal Done after Exit")
	}
	if got := s.ExitCode(); got != 42 {
		t.Errorf("ExitCode = %d, want 42", got)
	}
	if err := s.ExitErr(); err == nil || err.Error() != "boom" {
		t.Errorf("ExitErr = %v, want boom", err)
	}

	// Subscriber channel must be closed so the handler's write pump can
	// send the exit frame to the browser.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-sub.C:
			if !ok {
				goto closed
			}
		case <-deadline:
			t.Fatal("subscriber channel not closed after Exit")
		}
	}
closed:
	if got := b.GetByRuntime("rt-exit"); got != nil {
		t.Errorf("GetByRuntime returned closed session %v; expected nil", got)
	}
}

// readChunks aggregates up to `want` chunks from sub.C, stopping early on
// timeout. Always returns whatever was collected.
func readChunks(t *testing.T, sub *Subscriber, want int, deadline time.Duration) []byte {
	t.Helper()
	var buf bytes.Buffer
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for i := 0; i < want; i++ {
		select {
		case chunk, ok := <-sub.C:
			if !ok {
				return buf.Bytes()
			}
			buf.Write(chunk)
		case <-timer.C:
			return buf.Bytes()
		}
	}
	return buf.Bytes()
}
