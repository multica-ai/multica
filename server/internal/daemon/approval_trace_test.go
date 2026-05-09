package daemon

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/trace"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestWithApprovalTraceWritesEvents(t *testing.T) {
	ctx := context.Background()
	store := newTraceStoreForTest(t)
	defer store.Close()

	taskID := "task-approval"
	runID := "run-approval"
	provider := "claude"

	var callCount int
	mockCB := func(_ context.Context, req agent.ApprovalRequest) (string, bool, error) {
		callCount++
		if req.Type != "command_approval" {
			t.Fatalf("expected type command_approval, got %s", req.Type)
		}
		return "allow", true, nil
	}

	wrapped := WithApprovalTrace(mockCB, store, taskID, runID, provider)

	req := agent.ApprovalRequest{
		Type:   "command_approval",
		Title:  "Run command: rm -rf /",
		Detail: "This command will delete everything",
	}
	chosen, approved, err := wrapped(ctx, req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if chosen != "allow" || !approved {
		t.Fatalf("expected allow/true, got %s/%v", chosen, approved)
	}
	if callCount != 1 {
		t.Fatalf("expected mock called once, got %d", callCount)
	}

	// Verify trace store has request + response.
	lines, err := store.ListSince(ctx, taskID, runID, 0)
	if err != nil {
		t.Fatalf("ListSince: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 trace lines (request + response), got %d", len(lines))
	}

	if lines[0].Channel != trace.ChannelApprovalRequest {
		t.Fatalf("line 0: expected channel %q, got %q", trace.ChannelApprovalRequest, lines[0].Channel)
	}
	if lines[0].Content != "Run command: rm -rf /" {
		t.Fatalf("line 0: unexpected content %q", lines[0].Content)
	}

	if lines[1].Channel != trace.ChannelApprovalResponse {
		t.Fatalf("line 1: expected channel %q, got %q", trace.ChannelApprovalResponse, lines[1].Channel)
	}
}

func TestWithApprovalTraceNilStore(t *testing.T) {
	ctx := context.Background()

	var callCount int
	mockCB := agent.ApprovalCallback(func(_ context.Context, _ agent.ApprovalRequest) (string, bool, error) {
		callCount++
		return "deny", false, nil
	})

	// nil store should return cb unchanged.
	wrapped := WithApprovalTrace(mockCB, nil, "t", "r", "p")
	if wrapped == nil {
		t.Fatal("expected non-nil callback")
	}

	chosen, approved, err := wrapped(ctx, agent.ApprovalRequest{Type: "test", Title: "test"})
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if chosen != "deny" || approved {
		t.Fatalf("expected deny/false, got %s/%v", chosen, approved)
	}
	if callCount != 1 {
		t.Fatalf("expected mock called once, got %d", callCount)
	}
}

func TestWithApprovalTraceNilCallback(t *testing.T) {
	store := newTraceStoreForTest(t)
	defer store.Close()

	// nil callback with non-nil store should return nil.
	wrapped := WithApprovalTrace(nil, store, "t", "r", "p")
	if wrapped != nil {
		t.Fatal("expected nil for nil callback")
	}
}

// newTraceStoreForTest creates a temporary JSONLStore for testing.
func newTraceStoreForTest(t testing.TB) *trace.JSONLStore {
	t.Helper()
	s, err := trace.NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	return s
}
