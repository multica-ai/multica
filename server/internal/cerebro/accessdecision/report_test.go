package accessdecision

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
)

func TestSummarizeGroupsPerAgentRuntimeAndToolWithZeroDiffAcrossDifferentPolicies(t *testing.T) {
	entries := []Entry{
		NewEntry(Observation{
			WorkspaceID:           "workspace-1",
			AgentID:               "agent-allow",
			RuntimeID:             "runtime-1",
			ObservedToolName:      "create_issue",
			CanonicalCapabilityID: "platform:create_issue",
			LegacyDecision:        DecisionAllow,
			LegacyPath:            "legacy_capability",
			PolicyDecision:        PolicyAllow,
			EvidenceLevel:         availabilityevidence.LevelVerified,
		}),
		NewEntry(Observation{
			WorkspaceID:           "workspace-1",
			AgentID:               "agent-deny",
			RuntimeID:             "runtime-1",
			ObservedToolName:      "create_issue",
			CanonicalCapabilityID: "platform:create_issue",
			LegacyDecision:        DecisionDeny,
			LegacyPath:            "legacy_capability",
			PolicyDecision:        PolicyDeny,
			EvidenceLevel:         availabilityevidence.LevelVerified,
		}),
	}

	report := Summarize(entries)
	if report.Total != 2 || report.Diffs != 0 {
		t.Fatalf("report totals = (%d, %d diffs), want (2, 0 diffs)", report.Total, report.Diffs)
	}
	if len(report.Groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(report.Groups))
	}
	if report.Groups[0].AgentID != "agent-allow" || report.Groups[0].PolicyDecision != PolicyAllow {
		t.Fatalf("first group = %+v, want allow agent", report.Groups[0])
	}
	if report.Groups[1].AgentID != "agent-deny" || report.Groups[1].PolicyDecision != PolicyDeny {
		t.Fatalf("second group = %+v, want deny agent", report.Groups[1])
	}
}

func TestSummarizeUsesObservedNameWhenCanonicalizationFails(t *testing.T) {
	entry := NewEntry(Observation{
		WorkspaceID:      "workspace-1",
		AgentID:          "agent-1",
		RuntimeID:        "runtime-1",
		ObservedToolName: "mystery_tool",
		LegacyDecision:   DecisionAllow,
		LegacyPath:       "legacy_capability",
		PolicyDecision:   PolicyError,
		EvidenceLevel:    availabilityevidence.LevelDeclared,
	})

	report := Summarize([]Entry{entry})
	if len(report.Groups) != 1 || report.Groups[0].Tool != "mystery_tool" {
		t.Fatalf("groups = %+v, want mystery_tool fallback", report.Groups)
	}
	if report.Diffs != 1 {
		t.Fatalf("diffs = %d, want 1", report.Diffs)
	}
}
