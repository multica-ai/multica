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

	cb := BuildTraceCallback(store, "task-trace", "run-trace", "codex", false)
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

	if cb := BuildTraceCallback(nil, "task", "run", "claude", false); cb != nil {
		t.Fatal("nil store should disable callback")
	}
	if cb := BuildTraceCallback(store, "", "run", "claude", false); cb != nil {
		t.Fatal("empty task id should disable callback")
	}
	if cb := BuildTraceCallback(store, "task", "", "claude", false); cb != nil {
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

func TestBuildTraceCallbackStreamDisplayFiltersRaw(t *testing.T) {
	ctx := context.Background()
	store := newTraceStoreForTest(t)
	defer store.Close()

	cb := BuildTraceCallback(store, "task-sd", "run-sd", "opencode", true)
	if cb == nil {
		t.Fatal("expected trace callback")
	}

	cb(trace.ChannelRawStdout, "raw line", "")
	cb(trace.ChannelProviderEvent, "", `{"type":"event"}`)
	cb(trace.ChannelDisplayEvent, `{"type":"assistant_text","content":"hello"}`, "")
	cb(trace.ChannelApprovalRequest, "approve?", "")
	cb(trace.ChannelNormalized, "norm", "")

	lines, err := store.ListSince(ctx, "task-sd", "run-sd", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}

	// raw_stdout and provider_event should be filtered; display_event,
	// approval_request, and normalized should pass through.
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (raw channels filtered), got %d", len(lines))
	}

	gotChannels := make(map[string]bool)
	for _, l := range lines {
		gotChannels[l.Channel] = true
	}
	for _, ch := range []string{trace.ChannelRawStdout, trace.ChannelProviderEvent} {
		if gotChannels[ch] {
			t.Errorf("channel %q should have been filtered", ch)
		}
	}
	for _, ch := range []string{trace.ChannelDisplayEvent, trace.ChannelApprovalRequest, trace.ChannelNormalized} {
		if !gotChannels[ch] {
			t.Errorf("channel %q should have passed through", ch)
		}
	}
}

func TestBuildTraceCallbackNoFilterWhenStreamDisplayOff(t *testing.T) {
	ctx := context.Background()
	store := newTraceStoreForTest(t)
	defer store.Close()

	cb := BuildTraceCallback(store, "task-raw", "run-raw", "unknown", false)
	if cb == nil {
		t.Fatal("expected trace callback")
	}

	cb(trace.ChannelRawStdout, "raw line", "")
	cb(trace.ChannelProviderEvent, "", `{"type":"event"}`)
	cb(trace.ChannelDisplayEvent, `{"type":"status"}`, "")

	lines, err := store.ListSince(ctx, "task-raw", "run-raw", 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (no filtering), got %d", len(lines))
	}
}
