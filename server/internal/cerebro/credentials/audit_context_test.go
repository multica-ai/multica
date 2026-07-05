package credentials

// FIR-2243 (B3): unit tests for folding issue/task context into the credential
// audit metadata. Pure function — no DB or request context required.

import (
	"encoding/json"
	"testing"
)

func decodeMeta(t *testing.T, b []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("result is not valid JSON (%q): %v", string(b), err)
	}
	return m
}

func TestMergeAuditContext_AddsIssueAndTask(t *testing.T) {
	m := decodeMeta(t, mergeAuditContext(nil, "issue-1", "task-1"))
	if m["issue_id"] != "issue-1" {
		t.Fatalf("expected issue_id=issue-1, got %v", m["issue_id"])
	}
	if m["task_id"] != "task-1" {
		t.Fatalf("expected task_id=task-1, got %v", m["task_id"])
	}
}

func TestMergeAuditContext_EmptyScopeIsEmptyObject(t *testing.T) {
	if got := string(mergeAuditContext(nil, "", "")); got != "{}" {
		t.Fatalf("expected {} for no context, got %q", got)
	}
}

func TestMergeAuditContext_PreservesBaseMetadata(t *testing.T) {
	m := decodeMeta(t, mergeAuditContext([]byte(`{"binding_id":"b-9"}`), "issue-2", ""))
	if m["binding_id"] != "b-9" {
		t.Fatalf("expected binding_id preserved, got %v", m["binding_id"])
	}
	if m["issue_id"] != "issue-2" {
		t.Fatalf("expected issue_id=issue-2, got %v", m["issue_id"])
	}
	if _, ok := m["task_id"]; ok {
		t.Fatalf("did not expect task_id when empty, got %v", m["task_id"])
	}
}

func TestMergeAuditContext_OnlyIssue(t *testing.T) {
	m := decodeMeta(t, mergeAuditContext(nil, "issue-3", ""))
	if m["issue_id"] != "issue-3" {
		t.Fatalf("expected issue_id=issue-3, got %v", m["issue_id"])
	}
	if _, ok := m["task_id"]; ok {
		t.Fatalf("did not expect task_id, got %v", m["task_id"])
	}
}
