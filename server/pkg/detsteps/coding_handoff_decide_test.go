package detsteps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

func loadHandoffDettoolSource(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", "dettools", "coding_handoff_decide.go")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read coding_handoff_decide.go: %v", err)
	}
	return string(b)
}

func runDecision(t *testing.T, input map[string]any) dettools.Result {
	src := loadHandoffDettoolSource(t)
	return RunSubprocess(context.Background(), os.Args[0], src, input, 3*time.Second)
}

func decisionData(res dettools.Result) map[string]any {
	d, _ := res.MachineData["decision"].(map[string]any)
	return d
}

func agentIDsForTest() map[string]any {
	return map[string]any{
		"planner":      "planner-1",
		"implementer":  "impl-1",
		"test_writer":  "tw-1",
		"reviewer":     "rev-1",
		"orchestrator": "orch-1",
		"pr_writer":    "prw-1",
	}
}

func comment(content string) map[string]any {
	return map[string]any{"content": content}
}

func TestCodingHandoffDecide_NotStartedRoutesToPlanner(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-1",
		"task_comments": []any{},
		"agent_ids":     agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "planner" {
		t.Fatalf("next_role=%v, want planner", got)
	}
	if dec["route_kind"] != "not_started" {
		t.Fatalf("route_kind=%v, want not_started", dec["route_kind"])
	}
}

func TestCodingHandoffDecide_ImplementerCompleteToTestWriter(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-2",
		"task_comments": []any{
			comment("## Implementation Complete"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "test_writer" {
		t.Fatalf("next_role=%v, want test_writer", got)
	}
	if dec["target_status"] != "implemented" {
		t.Fatalf("target_status=%v, want implemented", dec["target_status"])
	}
	if got := dec["route_kind"]; got != "normal" {
		t.Fatalf("route_kind=%v, want normal", got)
	}
}

func TestCodingHandoffDecide_ImplementationPlanToImplementer(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "test_writer",
		"task_issue_id": "task-3",
		"task_comments": []any{
			comment("## Implementation Plan"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "implementer" {
		t.Fatalf("next_role=%v, want implementer", got)
	}
}

func TestCodingHandoffDecide_ReviewFixRoutesToReviewer(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-4",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Review: FAIL"),
			comment("## Implementation Complete"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "reviewer" {
		t.Fatalf("next_role=%v, want reviewer", got)
	}
	if dec["route_kind"] != "review_fix" && dec["route_kind"] != "review_fix_duplicate_or_recovery" {
		t.Fatalf("route_kind=%v, want review_fix*, got", dec["route_kind"])
	}
}

func TestCodingHandoffDecide_ReviewFixWithRequiresTestWriterMarker(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-5",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Review: FAIL\nrequires_test_writer: true"),
			comment("## Implementation Complete"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "test_writer" {
		t.Fatalf("next_role=%v, want test_writer", got)
	}
	if got := dec["route_kind"]; got != "review_fix_requires_tests" && got != "review_fix_requires_tests_duplicate_or_recovery" {
		t.Fatalf("route_kind=%v, want review_fix_requires_tests*", got)
	}
}

func TestCodingHandoffDecide_TestsWrittenToReviewer(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "test_writer",
		"task_issue_id": "task-6",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Tests Written"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "reviewer" {
		t.Fatalf("next_role=%v, want reviewer", got)
	}
	if got := dec["target_status"]; got != "tested" {
		t.Fatalf("target_status=%v, want tested", got)
	}
}

func TestCodingHandoffDecide_ReviewerFailToImplementer(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":    "reviewer",
		"task_issue_id":   "task-7",
		"master_issue_id": "master-1",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Tests Written"),
			comment("## Review: FAIL"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "implementer" {
		t.Fatalf("next_role=%v, want implementer", got)
	}
}

func TestCodingHandoffDecide_ReviewerPassToOrchestrator(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":    "reviewer",
		"task_issue_id":   "task-8",
		"master_issue_id": "master-8",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Tests Written"),
			comment("## Review: PASS"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "orchestrator" {
		t.Fatalf("next_role=%v, want orchestrator", got)
	}
	if got := dec["comment_field"]; got != "body" {
		t.Fatalf("comment_field=%v, want body", got)
	}
	c, ok := dec["comment_payload"].(map[string]any)
	if !ok {
		t.Fatalf("comment_payload should be map")
	}
	if _, ok := c["body"]; !ok {
		t.Fatalf("comment_payload[body] missing")
	}
}

func TestCodingHandoffDecide_ReviewerPassPrefersPRWriter(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":    "reviewer",
		"task_issue_id":   "task-9",
		"master_issue_id": "master-9",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Tests Written"),
			comment("## Review: PASS"),
		},
		"agent_ids": agentIDsForTest(),
		"options": map[string]any{
			"prefer_pr_writer_after_review_pass": true,
		},
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "pr_writer" {
		t.Fatalf("next_role=%v, want pr_writer", got)
	}
}

func TestCodingHandoffDecide_EventMismatchFailsClosed(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-10",
		"event":         "review_pass",
		"task_comments": []any{
			comment("## Implementation Complete"),
			comment("## Tests Written"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusError {
		t.Fatalf("status=%q want error", res.Status)
	}
	if res.ErrorCode != "EVENT_MARKER_MISMATCH" {
		t.Fatalf("error_code=%q want EVENT_MARKER_MISMATCH", res.ErrorCode)
	}
}

func TestCodingHandoffDecide_EventWithoutMarkerMismatch(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "reviewer",
		"task_issue_id": "task-11",
		"event":         "implementation_complete",
		"task_comments": []any{},
		"agent_ids":     agentIDsForTest(),
	})
	if res.Status != dettools.StatusError {
		t.Fatalf("status=%q want error", res.Status)
	}
	if res.ErrorCode != "EVENT_MARKER_MISMATCH" {
		t.Fatalf("error_code=%q want EVENT_MARKER_MISMATCH", res.ErrorCode)
	}
}

func TestCodingHandoffDecide_PlanningBlockedNoHandoff(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-12",
		"task_comments": []any{
			comment("## Planning Blocked: Clarification Needed"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["route_kind"]; got != "blocked" {
		t.Fatalf("route_kind=%v, want blocked", got)
	}
	if got := dec["target_status"]; got != "awaiting_clarification" {
		t.Fatalf("target_status=%v, want awaiting_clarification", got)
	}
}

func TestCodingHandoffDecide_DuplicateRecoverySuffix(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-13",
		"task_comments": []any{
			comment("## Implementation Plan"),
			comment("random re-mention [@Coding Team Implementer](mention://agent/impl-1) after handoff"),
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["route_kind"]; got == "implementation_plan" {
		t.Fatalf("route_kind %v should be recovery-marked", got)
	}
}

func TestCodingHandoffDecide_MalformedCommentPayloadsAllowRecoverySuffix(t *testing.T) {
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-15",
		"task_comments": []any{
			map[string]any{},
			map[string]any{"content": 123},
			comment("## Implementation Complete"),
			map[string]any{"content": map[string]any{"nested": true}},
			map[string]any{"body": "[@Coding Team Test Writer](mention://agent/tw-1) already pinged"},
		},
		"agent_ids": agentIDsForTest(),
	})
	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	dec := decisionData(res)
	if got := dec["next_role"]; got != "test_writer" {
		t.Fatalf("next_role=%v, want test_writer", got)
	}
	if got := dec["route_kind"]; got != "normal_duplicate_or_recovery" {
		t.Fatalf("route_kind=%v, want normal_duplicate_or_recovery", got)
	}
}

func TestCodingHandoffDecide_MissingPlannerAgentIdFails(t *testing.T) {
	ids := agentIDsForTest()
	delete(ids, "planner")
	res := runDecision(t, map[string]any{
		"current_role":  "implementer",
		"task_issue_id": "task-14",
		"task_comments": []any{},
		"agent_ids":     ids,
	})
	if res.Status != dettools.StatusError {
		t.Fatalf("status=%q want error", res.Status)
	}
}
