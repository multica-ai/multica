package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The tests below prove the GAP-25 help-signal forwarding without a live
// server: an httptest.Server captures the JSON body the daemon client posts to
// /api/daemon/tasks/{id}/fail and /complete, so we can assert the help fields
// actually travel over the wire.

// TestClientFailTaskForwardsHelpSignal proves a blocked agent result's help
// signal reaches /fail as blocked_reason / needs / confidence. A precise
// blocker report is what lets the server route the task to a human via
// agent_requested_help instead of auto-retrying.
func TestClientFailTaskForwardsHelpSignal(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	blocked := "missing VAULT_TOKEN secret"
	conf := 0.3
	err := c.FailTask(context.Background(), "task-1", "stuck", "", "", "", "agent_error.unknown", false, "", "", HelpSignal{
		BlockedReason: &blocked,
		Needs:         []string{"vault credential", "clarification on scope"},
		Confidence:    &conf,
	})
	if err != nil {
		t.Fatalf("FailTask returned error: %v", err)
	}
	if gotBody["blocked_reason"] != blocked {
		t.Errorf("blocked_reason = %v, want %q", gotBody["blocked_reason"], blocked)
	}
	needs, ok := gotBody["needs"].([]any)
	if !ok || len(needs) != 2 {
		t.Errorf("needs = %v, want 2 entries", gotBody["needs"])
	}
	if gotBody["confidence"] != conf {
		t.Errorf("confidence = %v, want %v", gotBody["confidence"], conf)
	}
}

// TestClientCompleteTaskForwardsHelpSignal proves the same forwarding on the
// success path: an agent can finish yet still flag an open question for a
// human, and it must not be dropped.
func TestClientCompleteTaskForwardsHelpSignal(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete") {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	blocked := "untested on Windows"
	err := c.CompleteTask(context.Background(), "task-1", "done", "", "", "", false, "", "", HelpSignal{
		BlockedReason: &blocked,
		Needs:         []string{"Windows test run"},
	})
	if err != nil {
		t.Fatalf("CompleteTask returned error: %v", err)
	}
	if gotBody["blocked_reason"] != blocked {
		t.Errorf("blocked_reason = %v, want %q", gotBody["blocked_reason"], blocked)
	}
	if _, ok := gotBody["needs"].([]any); !ok {
		t.Errorf("needs missing on complete path: %v", gotBody["needs"])
	}
}

// TestClientTerminalOmitsEmptyHelpSignal proves a legacy result with no help
// signal does NOT send blocked_reason / needs / confidence at all, so the
// server keeps treating the task as an ordinary terminal result.
func TestClientTerminalOmitsEmptyHelpSignal(t *testing.T) {
	var gotFail map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			_ = json.NewDecoder(r.Body).Decode(&gotFail)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.FailTask(context.Background(), "task-1", "boom", "", "", "", "agent_error.unknown", false, "", "", HelpSignal{}); err != nil {
		t.Fatalf("FailTask returned error: %v", err)
	}
	for _, key := range []string{"blocked_reason", "needs", "confidence"} {
		if _, ok := gotFail[key]; ok {
			t.Errorf("empty help signal must not send %q, got body %v", key, gotFail)
		}
	}
}

// TestResultHelpSignalLiftsFields proves the adapter that copies a TaskResult's
// help signal onto the terminal report preserves every field and yields an
// empty signal (HasHelp == false) when the result set none.
func TestResultHelpSignalLiftsFields(t *testing.T) {
	blocked := "no access"
	conf := 0.5
	result := TaskResult{
		Status:        "blocked",
		BlockedReason: &blocked,
		Needs:         []string{"a", "b"},
		Confidence:    &conf,
	}
	h := resultHelpSignal(result)
	if !h.HasHelp() {
		t.Fatal("expected HasHelp true for a populated result")
	}
	if h.BlockedReason == nil || *h.BlockedReason != blocked || len(h.Needs) != 2 || h.Confidence == nil || *h.Confidence != conf {
		t.Fatalf("resultHelpSignal dropped fields: %+v", h)
	}

	empty := resultHelpSignal(TaskResult{Status: "completed"})
	if empty.HasHelp() {
		t.Fatal("empty result must have HasHelp == false")
	}
}
