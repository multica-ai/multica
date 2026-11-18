//go:build !windows

package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

// These tests complement kagent_http_contract_test.go (shared fixture) and
// kagent_failure_test.go. They cover the timeout-driven cancellation path and
// the durable journal state after a settled turn.

func TestKagentBackendDisabledFailsClosed(t *testing.T) {
	_, err := NewKagentBackend(KagentConfig{})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

// Timeout-driven cancellation must still acknowledge with a timeout result
// once the remote confirms an immutable terminal state.
func TestKagentBackendTimeoutCancelsWithAcknowledgment(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.nextState = kagent.StateWorking // the remote never completes on its own

	backend := f.backend("attempt")
	s, err := backend.Execute(context.Background(), "long work", ExecOptions{
		SystemPrompt: "inline runtime brief",
		// Include durable intent/identity fsyncs before the timeout under -race.
		Timeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, known := finishSliceKagent(t, s)
	if !known {
		t.Fatalf("cancellation must be acknowledged: %+v", result)
	}
	if result.Status != "timeout" || result.Error == "" {
		t.Fatalf("expected timeout, got %q (%s)", result.Status, result.Error)
	}
	if result.SessionID != sliceSessionID {
		t.Fatalf("session id mismatch after cancel: %q", result.SessionID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancels == 0 {
		t.Fatal("backend must issue a remote cancel")
	}
}

// The journal records the settled attempt and clears the active turn only
// after the remote terminal state was observed and persisted.
func TestKagentBackendJournalSettledAfterSuccess(t *testing.T) {
	f := newSliceHTTPFixture(t)
	s := executeSliceKagent(t, f.backend("attempt-1"), context.Background())
	result, known := finishSliceKagent(t, s)
	if !known || result.Status != "completed" {
		t.Fatalf("first run: %+v (known=%v)", result, known)
	}

	rec, err := f.journal.Lock(kagent.ConversationKey("workspace", sliceAgentID, "conversation"))
	if err != nil {
		t.Fatalf("journal lock: %v", err)
	}
	defer rec.Close()
	var state kagentConversation
	exists, err := rec.Load(&state)
	if err != nil {
		t.Fatalf("journal load: %v", err)
	}
	if !exists {
		t.Fatal("conversation journal missing after success")
	}
	if !state.Attempts["attempt-1"].Settled || state.ActiveAttempt != "" {
		t.Fatalf("journal must record a settled attempt with no active turn: %+v", state)
	}
	if state.SessionID != sliceSessionID {
		t.Fatalf("journal must pin the remote session: %q", state.SessionID)
	}
	if state.Attempts["attempt-1"].TaskID == "" {
		t.Fatal("journal must record the remote task id")
	}
}
