package terminal

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// TestBrokerEndToEnd_StdinStdoutRoundTrip proves the broker actually streams
// stdin and stdout through a child shell process. The child echoes a known
// line, then reads from stdin and echoes what it read — so the test
// succeeds only if both directions of the stream work.
func TestBrokerEndToEnd_StdinStdoutRoundTrip(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := b.Create(ctx, CreateOptions{
		RuntimeID:   "rt-1",
		WorkspaceID: "ws-1",
		OwnerUserID: "user-1",
		Command:     []string{"sh", "-c", "echo hello; read x; echo got $x"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(s.ID) })

	sub, write, detach := s.Attach()
	t.Cleanup(detach)

	if err := write([]byte("world\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	collected, err := collectUntil(t, sub, "got world", 3*time.Second)
	if err != nil {
		t.Fatalf("collect: %v\ngot: %q", err, collected)
	}
	if !strings.Contains(collected, "hello") {
		t.Errorf("expected stdout to contain 'hello'; got %q", collected)
	}
	if !strings.Contains(collected, "got world") {
		t.Errorf("expected stdout to contain 'got world' (stdin roundtrip); got %q", collected)
	}

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("session never reported done")
	}
}

// TestBrokerEndToEnd_MultipleSubscribersBothSeeOutput proves the fan-out:
// attaching a second subscriber to the same session also receives the
// stdout stream so multiple Multica tabs can watch the same terminal.
func TestBrokerEndToEnd_MultipleSubscribersBothSeeOutput(t *testing.T) {
	b := NewBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := b.Create(ctx, CreateOptions{
		Command: []string{"sh", "-c", "for i in 1 2 3; do echo line$i; done"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(s.ID) })

	subA, _, detachA := s.Attach()
	t.Cleanup(detachA)
	subB, _, detachB := s.Attach()
	t.Cleanup(detachB)

	gotA, errA := collectUntil(t, subA, "line3", 2*time.Second)
	gotB, errB := collectUntil(t, subB, "line3", 2*time.Second)
	if errA != nil || errB != nil {
		t.Fatalf("collect failed: a=%v b=%v\ngotA=%q\ngotB=%q", errA, errB, gotA, gotB)
	}
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(gotA, want) {
			t.Errorf("subscriber A missing %q in %q", want, gotA)
		}
		if !strings.Contains(gotB, want) {
			t.Errorf("subscriber B missing %q in %q", want, gotB)
		}
	}
}

// TestBrokerEndToEnd_CloseTerminatesChild proves Close kills the child so a
// long-running session doesn't leak when the caller is done with it.
func TestBrokerEndToEnd_CloseTerminatesChild(t *testing.T) {
	b := NewBroker()
	s, err := b.Create(context.Background(), CreateOptions{
		// Sleep would outlive the test if Close didn't kill it.
		Command: []string{"sh", "-c", "echo started; sleep 60"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sub, _, detach := s.Attach()
	t.Cleanup(detach)

	if _, err := collectUntil(t, sub, "started", 2*time.Second); err != nil {
		t.Fatalf("never saw 'started': %v", err)
	}

	if err := b.Close(s.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("session did not terminate after Close")
	}
	if got := b.Get(s.ID); got != nil {
		t.Errorf("session still in broker after Close")
	}
}

// collectUntil reads from sub.C until the accumulated text contains needle
// or the deadline expires. Returns whatever was collected so far on
// timeout. Buffer + deadline are explicit so tests fail loudly rather than
// hang.
func collectUntil(t *testing.T, sub *Subscriber, needle string, deadline time.Duration) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-sub.C:
			if !ok {
				if strings.Contains(buf.String(), needle) {
					return buf.String(), nil
				}
				return buf.String(), errChannelClosed
			}
			buf.Write(chunk)
			if strings.Contains(buf.String(), needle) {
				return buf.String(), nil
			}
		case <-timer.C:
			return buf.String(), errDeadlineExceeded
		}
	}
}

var (
	errChannelClosed    = simpleErr("subscriber channel closed before needle observed")
	errDeadlineExceeded = simpleErr("deadline exceeded waiting for needle")
)

type simpleErr string

func (e simpleErr) Error() string { return string(e) }
