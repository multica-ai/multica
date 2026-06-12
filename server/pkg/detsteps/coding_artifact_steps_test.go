package detsteps

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

func TestCodingCommentExtractFindsLatestMarkersAndArtifacts(t *testing.T) {
	source := readRootDettoolSource(t, "coding_comment_extract.go")
	input := map[string]any{
		"comments": []any{
			map[string]any{"content": "## Implementation Complete\nold"},
			map[string]any{"content": "## Review: FAIL\nretry"},
			map[string]any{"content": "## Implementation Plan\n```json coding-team-artifact\n{\"artifact_type\":\"implementation_plan\",\"artifact_version\":1,\"task_issue_id\":\"T-1\",\"master_issue_id\":\"M-1\",\"language\":\"csharp\",\"owning_project\":\"src/App\",\"owning_project_justification\":\"Existing owner\",\"files_to_create\":[],\"files_to_modify\":[\"src/App/Foo.cs\"],\"acceptance_criteria_coverage\":[{\"criterion\":\"Works\",\"planned_coverage\":\"Unit test\"}],\"key_decisions\":[\"Use existing service\"]}\n```"},
			map[string]any{"content": "## Implementation Complete\n```json coding-team-artifact\n{\"artifact_type\":\"implementation_summary\",\"artifact_version\":1,\"task_issue_id\":\"T-1\",\"commit_sha\":\"abc\",\"files_created\":[],\"files_modified\":[\"src/App/Foo.cs\"],\"plan_deviations\":[],\"coverage\":{\"threshold\":99,\"passed\":true}}\n```"},
		},
	}

	res := Run(context.Background(), source, input, Options{Timeout: 3 * time.Second})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status = %q, code = %q, summary = %q", res.Status, res.ErrorCode, res.Summary)
	}
	currentRound, ok := res.MachineData["current_round"].(map[string]any)
	if !ok {
		t.Fatalf("current_round = %#v, want object", res.MachineData["current_round"])
	}
	if currentRound["implementation_needed"] != false {
		t.Fatalf("implementation_needed = %#v, want false after latest implementation", currentRound["implementation_needed"])
	}
	artifacts, ok := res.MachineData["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("artifacts = %#v, want object", res.MachineData["artifacts"])
	}
	if _, ok := artifacts["implementation_plan"].(map[string]any); !ok {
		t.Fatalf("implementation_plan artifact missing: %#v", artifacts)
	}
	if _, ok := artifacts["implementation_summary"].(map[string]any); !ok {
		t.Fatalf("implementation_summary artifact missing: %#v", artifacts)
	}
}

func TestCodingPlanValidateRejectsMissingCriterionCoverage(t *testing.T) {
	source := readRootDettoolSource(t, "coding_plan_validate.go")
	input := map[string]any{
		"acceptance_criteria": []any{"Works", "Handles errors"},
		"plan": map[string]any{
			"artifact_type":                "implementation_plan",
			"artifact_version":             float64(1),
			"task_issue_id":                "T-1",
			"master_issue_id":              "M-1",
			"language":                     "csharp",
			"owning_project":               "src/App",
			"owning_project_justification": "Existing owner",
			"files_to_create":              []any{},
			"files_to_modify":              []any{"src/App/Foo.cs"},
			"acceptance_criteria_coverage": []any{map[string]any{"criterion": "Works", "planned_coverage": "Unit test"}},
			"key_decisions":                []any{"Use existing service"},
		},
	}

	res := Run(context.Background(), source, input, Options{Timeout: 3 * time.Second})
	if res.Status != dettools.StatusError || res.ErrorCode != dettools.CodePolicyFailure {
		t.Fatalf("result = %+v, want POLICY_FAILURE", res)
	}
}
