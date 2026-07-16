package handler

// CEREBRO-PATCH(agent-capabilities-approval-test): FIR-3212 Approval slice — the
// approval-consequences card assembled from the agent's own runtime row. DB-free,
// like the swap-card tests: the classification itself is covered in
// internal/cerebro/capabilities.

import (
	"reflect"
	"testing"

	cerebrocapabilities "github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

func TestBuildAgentCapabilityApproval_ClaudeChangesTakeEffect(t *testing.T) {
	got := buildAgentCapabilityApproval(
		"agent-1",
		runtimeExecOptionsFromProvider("claude", "1.2.3", "rt-claude"),
		[]string{"instructions", "model"},
	)

	if got.AgentID != "agent-1" {
		t.Fatalf("AgentID = %q", got.AgentID)
	}
	if got.Impact.Status != cerebrocapabilities.StatusKnown {
		t.Fatalf("impact status = %q, want known", got.Impact.Status)
	}
	if len(got.Impact.Ineffective) != 0 {
		t.Fatalf("claude honours both fields; Ineffective must be empty, got %v", got.Impact.Ineffective)
	}
	// The engine's own field list rides along so the panel can name the engine
	// the consequences were computed against, without a second round-trip.
	if got.Runtime.Provider != "claude" {
		t.Fatalf("Runtime.Provider = %q, want claude", got.Runtime.Provider)
	}
}

// The slice's reason to exist, at the HTTP layer: hermes discards the system
// prompt, so approving an instructions rewrite is a no-op the field diff renders
// as a large green change.
func TestBuildAgentCapabilityApproval_HermesInstructionsAreANoOp(t *testing.T) {
	got := buildAgentCapabilityApproval(
		"agent-1",
		runtimeExecOptionsFromProvider("hermes", "", "rt-hermes"),
		[]string{"instructions"},
	)

	if !reflect.DeepEqual(got.Impact.Ineffective, []string{"instructions"}) {
		t.Fatalf("Ineffective = %v, want [instructions]", got.Impact.Ineffective)
	}
}

func TestBuildAgentCapabilityApproval_UnknownProviderEnumeratesNothing(t *testing.T) {
	got := buildAgentCapabilityApproval(
		"agent-1",
		runtimeExecOptionsFromProvider("mystery-engine", "", "rt-x"),
		[]string{"instructions", "model"},
	)

	if got.Impact.Status != cerebrocapabilities.StatusUnknown {
		t.Fatalf("impact status = %q, want unknown", got.Impact.Status)
	}
	if len(got.Impact.Fields) != 0 || len(got.Impact.Ineffective) != 0 {
		t.Fatalf("unknown provider must enumerate nothing, got %+v", got.Impact)
	}
}

func TestParseApprovalFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "model", []string{"model"}},
		{"list", "instructions,model", []string{"instructions", "model"}},
		{"trims and drops blanks", " instructions , ,model ,", []string{"instructions", "model"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseApprovalFields(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseApprovalFields(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// An unbounded field list from the query string must not become an unbounded
// response. The classifier is pure and cheap, but echoing 10k caller-supplied
// keys back as "unknown_field" rows is a free amplification primitive.
func TestParseApprovalFieldsIsBounded(t *testing.T) {
	in := ""
	for i := 0; i < approvalFieldLimit+50; i++ {
		in += "model,"
	}
	got := parseApprovalFields(in)
	if len(got) > approvalFieldLimit {
		t.Fatalf("parseApprovalFields returned %d fields, want at most %d", len(got), approvalFieldLimit)
	}
}
