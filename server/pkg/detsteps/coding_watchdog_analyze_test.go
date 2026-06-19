package detsteps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

func readWatchdogDettoolSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "dettools", "coding_watchdog_analyze.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read coding_watchdog_analyze.go: %v", err)
	}
	return string(b)
}

func runWatchdog(t *testing.T, input map[string]any) dettools.Result {
	t.Helper()
	src := readWatchdogDettoolSource(t)
	return RunSubprocess(context.Background(), os.Args[0], src, input, 3*time.Second)
}

func TestCodingWatchdogAnalyze_ReviewPassedRecoveryIncludesIssueDoneStatus(t *testing.T) {
	res := runWatchdog(t, map[string]any{
		"master_issue_id": "master-1",
		"state": map[string]any{
			"tasks": []any{map[string]any{
				"task_issue_id": "task-1",
				"status":        "pending",
			}},
		},
		"task_comments": map[string]any{
			"task-1": []any{
				map[string]any{"content": "## Review: PASS", "created_at": "2026-06-14T10:00:00Z"},
			},
		},
		"master_comments": []any{},
		"agent_ids": map[string]any{
			"orchestrator": "orch-1",
		},
		"now":                             "2026-06-14T10:06:00Z",
		"assume_stale_without_timestamps": true,
	})

	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	actions, ok := res.MachineData["actions"].([]any)
	if !ok {
		t.Fatalf("actions missing or wrong type: %#v", res.MachineData["actions"])
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %#v", len(actions), actions)
	}
	action, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("action wrong type: %#v", actions[0])
	}
	if action["type"] != "master_task_complete_comment" {
		t.Fatalf("expected master_task_complete_comment, got %#v", action["type"])
	}
	if action["issue_status"] != "done" {
		t.Fatalf("expected watchdog to request done task status, got %#v", action["issue_status"])
	}

	patches, ok := res.MachineData["state_patches"].([]any)
	if !ok {
		t.Fatalf("state_patches missing or wrong type: %#v", res.MachineData["state_patches"])
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d: %#v", len(patches), patches)
	}
	first, ok := patches[0].(map[string]any)
	if !ok {
		t.Fatalf("patch wrong type: %#v", patches[0])
	}
	if first["task_issue_id"] != "task-1" {
		t.Fatalf("expected task-1 patch, got %#v", first["task_issue_id"])
	}
	if first["status"] != "committed" {
		t.Fatalf("expected committed patch status, got %#v", first["status"])
	}
}

func TestCodingWatchdogAnalyze_SkipsDoneStatusTasks(t *testing.T) {
	res := runWatchdog(t, map[string]any{
		"master_issue_id": "master-1",
		"state": map[string]any{
			"tasks": []any{map[string]any{
				"task_issue_id": "task-done",
				"status":        "done",
			}},
		},
		"task_comments": map[string]any{
			"task-done": []any{
				map[string]any{"content": "## Review: PASS", "created_at": "2026-06-14T10:00:00Z"},
			},
		},
		"master_comments": []any{},
		"agent_ids": map[string]any{
			"orchestrator": "orch-1",
		},
		"now": "2026-06-14T10:06:00Z",
	})

	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	actions, ok := res.MachineData["actions"].([]any)
	if !ok {
		t.Fatalf("actions missing or wrong type: %#v", res.MachineData["actions"])
	}
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions, got %d: %#v", len(actions), actions)
	}
	skips, ok := res.MachineData["skips"].([]any)
	if !ok {
		t.Fatalf("skips missing or wrong type: %#v", res.MachineData["skips"])
	}
	if len(skips) != 1 {
		t.Fatalf("expected 1 skip, got %d: %#v", len(skips), skips)
	}
}

func TestCodingWatchdogAnalyze_ReviewCompleteMasterCommentWithDoneStatusIsIdempotent(t *testing.T) {
	res := runWatchdog(t, map[string]any{
		"master_issue_id": "master-1",
		"state": map[string]any{
			"tasks": []any{map[string]any{
				"task_issue_id": "task-3",
				"status":        "pending",
			}},
		},
		"task_comments": map[string]any{
			"task-3": []any{
				map[string]any{"content": "## Review: PASS", "created_at": "2026-06-14T10:00:00Z"},
			},
		},
		"master_comments": []any{
			map[string]any{"content": "[@Coding Team Orchestrator](mention://agent/orch-1)\n\nTASK_COMPLETE\ntask_issue_id: task-3\nstatus: done"},
		},
		"agent_ids": map[string]any{
			"orchestrator": "orch-1",
		},
		"now": "2026-06-14T10:06:00Z",
	})

	if res.Status != dettools.StatusOK {
		t.Fatalf("status=%q summary=%q", res.Status, res.Summary)
	}
	actions, ok := res.MachineData["actions"].([]any)
	if !ok {
		t.Fatalf("actions missing or wrong type: %#v", res.MachineData["actions"])
	}
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions, got %d: %#v", len(actions), actions)
	}
	patches, ok := res.MachineData["state_patches"].([]any)
	if !ok {
		t.Fatalf("state_patches missing or wrong type: %#v", res.MachineData["state_patches"])
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d: %#v", len(patches), patches)
	}
	first, ok := patches[0].(map[string]any)
	if !ok {
		t.Fatalf("patch wrong type: %#v", patches[0])
	}
	if first["task_issue_id"] != "task-3" {
		t.Fatalf("expected task-3 patch, got %#v", first["task_issue_id"])
	}
	if first["status"] != "committed" {
		t.Fatalf("expected committed patch status, got %#v", first["status"])
	}
}
