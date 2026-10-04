package eventrouting

import "testing"

func TestCommentClassActionability(t *testing.T) {
	tests := []struct {
		name, author, kind, content string
		source, actionable          bool
		want                        Class
	}{
		{"member", "member", "comment", "continue", false, true, MemberIntent},
		{"agent result", "agent", "comment", "done", true, true, AgentResult},
		{"result handoff", "agent", "comment", "RESULT_HANDOFF: delivered", true, true, ResultHandoff},
		{"no action", "agent", "comment", "NO_ACTION", true, false, RecoveryControl},
		{"skip delegation", "agent", "comment", "SKIP_FAILED_DELEGATION", true, false, RecoveryControl},
		{"failure diagnostic", "system", "progress_update", "Delegated task failed", true, false, SystemFailureDiagnostic},
		{"system control", "system", "system", "ACTION_REQUIRED", true, false, RecoveryControl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommentClass(tt.author, tt.kind, tt.content, tt.source)
			if got != tt.want || got.Actionable() != tt.actionable {
				t.Fatalf("class=%q actionable=%v, want %q/%v", got, got.Actionable(), tt.want, tt.actionable)
			}
		})
	}
}

func TestProviderAndRecoveryClassesAreNonActionable(t *testing.T) {
	for _, class := range []Class{ProviderError, SystemFailureDiagnostic, RecoveryControl} {
		if class.Actionable() {
			t.Fatalf("%q must be non-actionable", class)
		}
	}
}

func TestClassFromPayload(t *testing.T) {
	payload := map[string]any{"comment": map[string]any{
		"type": "comment", "content": "RESULT_HANDOFF: delivered", "source_task_id": "task-id",
	}}
	if got := ClassFromPayload("agent", payload); got != ResultHandoff {
		t.Fatalf("class=%q, want %q", got, ResultHandoff)
	}
}

func TestResultHandoffTargets(t *testing.T) {
	issueID := "01a10450-7b28-7272-acff-f2b289a746ea"
	valid := "RESULT_HANDOFF: consumer_issue_id=`" + issueID + "`, revision=2"
	if !ResultHandoffTargets(valid, issueID) {
		t.Fatal("valid handoff was rejected")
	}
	for _, invalid := range []string{
		"RESULT_HANDOFF: consumer_issue_id=`00000000-0000-0000-0000-000000000000`, revision=2",
		"RESULT_HANDOFF: consumer_issue_id=`" + issueID + "`, revision=0",
		"RESULT_HANDOFF: delivered",
	} {
		if ResultHandoffTargets(invalid, issueID) {
			t.Fatalf("invalid handoff accepted: %q", invalid)
		}
	}
}
