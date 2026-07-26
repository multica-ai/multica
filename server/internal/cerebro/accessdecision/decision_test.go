package accessdecision

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
)

func TestEvaluateFailsClosedUntilCanonicalPolicyAndEvidenceAgree(t *testing.T) {
	tests := []struct {
		name string
		in   EvaluationInput
		want Decision
	}{
		{
			name: "canonical allow with verified evidence",
			in: EvaluationInput{
				CanonicalCapabilityID: "platform:create_issue",
				PolicyDecision:        PolicyAllow,
				EvidenceLevel:         availabilityevidence.LevelVerified,
			},
			want: DecisionAllow,
		},
		{
			name: "unknown capability",
			in: EvaluationInput{
				PolicyDecision: PolicyAllow,
				EvidenceLevel:  availabilityevidence.LevelVerified,
			},
			want: DecisionDeny,
		},
		{
			name: "live surface discovery is callable",
			in: EvaluationInput{
				CanonicalCapabilityID: "platform:create_issue",
				PolicyDecision:        PolicyAllow,
				EvidenceLevel:         availabilityevidence.LevelDiscovered,
			},
			want: DecisionAllow,
		},
		{
			name: "declared but absent availability",
			in: EvaluationInput{
				CanonicalCapabilityID: "platform:create_issue",
				PolicyDecision:        PolicyAllow,
				EvidenceLevel:         availabilityevidence.LevelDeclared,
			},
			want: DecisionDeny,
		},
		{
			name: "approval required",
			in: EvaluationInput{
				CanonicalCapabilityID: "platform:create_issue",
				PolicyDecision:        PolicyAsk,
				EvidenceLevel:         availabilityevidence.LevelVerified,
			},
			want: DecisionDeny,
		},
		{
			name: "policy lookup failed",
			in: EvaluationInput{
				CanonicalCapabilityID: "platform:create_issue",
				PolicyDecision:        PolicyError,
				EvidenceLevel:         availabilityevidence.LevelVerified,
			},
			want: DecisionDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.in)
			if got.Decision != tt.want {
				t.Fatalf("Evaluate() decision = %q, want %q (reason: %s)", got.Decision, tt.want, got.Reason)
			}
			if got.Reason == "" {
				t.Fatal("Evaluate() returned an empty reason")
			}
		})
	}
}

func TestNewEntryRecordsIdentityAndOneCanonicalDecision(t *testing.T) {
	entry := NewEntry(Observation{
		WorkspaceID:           "workspace-1",
		AgentID:               "agent-1",
		RuntimeID:             "runtime-1",
		OnBehalfOfUserID:      "member-1",
		TaskID:                "task-1",
		IssueID:               "issue-1",
		ObservedToolName:      "create_issue",
		CanonicalCapabilityID: "platform:create_issue",
		PolicyDecision:        PolicyDeny,
		EvidenceLevel:         availabilityevidence.LevelVerified,
	})

	if entry.WorkspaceID != "workspace-1" || entry.AgentID != "agent-1" || entry.RuntimeID != "runtime-1" {
		t.Fatalf("identity was not preserved: %+v", entry)
	}
	if entry.OnBehalfOfUserID != "member-1" {
		t.Fatalf("on-behalf-of identity = %q, want member-1", entry.OnBehalfOfUserID)
	}
	if entry.Decision != DecisionDeny {
		t.Fatalf("canonical decision = %q, want deny", entry.Decision)
	}
}
