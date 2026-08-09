package workflows

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func captureAudit(t *testing.T, emit func()) string {
	t.Helper()
	var buffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	emit()
	return buffer.String()
}

func TestHookLifecycleAuditRecordsIdentifiersOnly(t *testing.T) {
	policy := HookPolicy{
		ID:          "policy-9",
		FamilyID:    "family-1",
		Version:     4,
		Revision:    2,
		Mode:        HookModeEnforce,
		Name:        "Block risky merges",
		Description: "Stops the payout pipeline",
		Conditions:  []Condition{{Field: "issue.title", Op: "contains", Value: "payout secret"}},
		Bindings:    []HookBinding{{Kind: HookScopeProject, ID: "project-7"}},
		Handlers:    []HookHandler{{ID: "handler-1", Requirement: "Ask finance before merging"}},
		Lifecycle:   HookLifecycle{State: HookLifecycleLive, LivePolicyID: "policy-9", LiveVersion: 4},
	}
	actor := HookPermissionActor{Type: "member", ID: "user-3"}

	line := captureAudit(t, func() {
		hookLifecycleAudit(hookAuditPublish, "workspace-2", "policy-9", actor, &policy, nil)
	})

	for _, want := range []string{`"action":"publish"`, `"outcome":"ok"`, `"workspace_id":"workspace-2"`, `"family_id":"family-1"`, `"policy_id":"policy-9"`, `"version":4`, `"actor_id":"user-3"`} {
		if !strings.Contains(line, want) {
			t.Errorf("audit record is missing %s: %s", want, line)
		}
	}
	// Content the record must never carry out of the workspace.
	for _, leaked := range []string{"Block risky merges", "Stops the payout pipeline", "payout secret", "issue.title", "Ask finance before merging", "project-7"} {
		if strings.Contains(line, leaked) {
			t.Errorf("audit record leaked workspace content %q: %s", leaked, line)
		}
	}
}

func TestHookLifecycleAuditClassifiesFailuresWithoutTheMessage(t *testing.T) {
	line := captureAudit(t, func() {
		hookLifecycleAudit(hookAuditDisable, "workspace-2", "policy-9", HookPermissionActor{Type: "agent", ID: "agent-1"}, nil,
			errors.New("hook \"Block risky merges\" violates constraint"))
	})

	if !strings.Contains(line, `"outcome":"failed"`) || !strings.Contains(line, `"failure":"internal"`) {
		t.Errorf("expected a classified failure: %s", line)
	}
	if strings.Contains(line, "Block risky merges") {
		t.Errorf("audit record leaked the raw error message: %s", line)
	}
}

func TestHookFailureKindMapsSentinelErrors(t *testing.T) {
	for _, testCase := range []struct {
		err  error
		want string
	}{
		{ErrHookPolicyNotFound, "not_found"},
		{ErrHookPublishPrerequisite, "publish_prerequisite"},
		{ErrHookDraftRevisionStale, "draft_revision_stale"},
		{ErrManagedHookLocked, "managed_locked"},
		{errors.New("boom"), "internal"},
	} {
		if got := hookFailureKind(testCase.err); got != testCase.want {
			t.Errorf("hookFailureKind(%v) = %q, want %q", testCase.err, got, testCase.want)
		}
	}
}
