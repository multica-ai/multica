package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/trace"
)

func TestBuildTraceCallbackWritesLine(t *testing.T) {
	ctx := context.Background()
	store := newTraceStoreForTest(t)
	defer store.Close()

	cb := BuildTraceCallback(store, "task-trace", "run-trace", "codex")
	if cb == nil {
		t.Fatal("expected trace callback")
	}

	cb(trace.ChannelProviderEvent, "", `{"type":"event"}`)
	cb("", "hello", "")

	lines, err := store.ListSince(ctx, "task-trace", "run-trace", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Provider != "codex" || lines[0].Channel != trace.ChannelProviderEvent {
		t.Fatalf("unexpected first line: %+v", lines[0])
	}
	if lines[0].RawPayload != `{"type":"event"}` {
		t.Fatalf("unexpected raw payload: %q", lines[0].RawPayload)
	}
	if lines[1].Channel != trace.ChannelNormalized || lines[1].Content != "hello" {
		t.Fatalf("unexpected normalized line: %+v", lines[1])
	}
}

func TestBuildTraceCallbackNilInputs(t *testing.T) {
	store := newTraceStoreForTest(t)
	defer store.Close()

	if cb := BuildTraceCallback(nil, "task", "run", "claude"); cb != nil {
		t.Fatal("nil store should disable callback")
	}
	if cb := BuildTraceCallback(store, "", "run", "claude"); cb != nil {
		t.Fatal("empty task id should disable callback")
	}
	if cb := BuildTraceCallback(store, "task", "", "claude"); cb != nil {
		t.Fatal("empty run id should disable callback")
	}
}

func TestNewTraceRunID(t *testing.T) {
	got := newTraceRunID(time.Date(2026, 5, 2, 6, 7, 8, 9, time.FixedZone("CST", 8*60*60)))
	want := "20260501T220708.000000009Z"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
