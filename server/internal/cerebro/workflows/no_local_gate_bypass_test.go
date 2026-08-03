package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentLifecycleGatesHaveProductionConnectors(t *testing.T) {
	required := map[string][]string{
		"../commentguard/guard.go":            {"HookBeforeMessageSend"},
		"../runtime/tool_executor_invoker.go": {"HookOnToolFailure"},
		"task_failure_gate.go":                {"HookOnTaskFailure"},
		"task_completion_gate.go":             {"HookBeforeTaskComplete"},
		"../wakeup/workflow_gate.go":          {"HookBeforeWakeupCreate", "HookOnWakeupFireFailure"},
		"../loops/chain_workflow_gate.go":     {"HookBeforeIssueStatus", "HookAfterWorkflowStep"},
		"../sessions/handler.go":              {"HookBeforeSessionEnd", "evaluateHandoff"},
		"../../handler/issue.go":              {"IssueStatusWorkflowGate.BeforeIssueStatusChange"},
	}
	for path, fragments := range required {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(body), fragment) {
				t.Errorf("%s has no production connector for %s", path, fragment)
			}
		}
	}
}

func TestRetiredLocalGatePathsCannotReturn(t *testing.T) {
	removedFiles := []string{
		"loop_advance.go",
		"check_gate.go",
		"../loops/gate_evaluator.go",
		"../loops/status_setter.go",
		"../commentguard/legacy.go",
		"../failrouter/router.go",
	}
	for _, path := range removedFiles {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("retired gate file exists: %s", path)
		}
	}

	repoRoot := filepath.Join("..", "..", "..", "..")
	checks := map[string][]string{
		filepath.Join(repoRoot, "server/internal/cerebro/commentguard/guard.go"): {
			"FlagCommentTargetGuard", "RejectComment(", "loadFlags(",
		},
		filepath.Join(repoRoot, "server/internal/handler/comment_workflow_gate_cerebro.go"): {
			"parent_id must equal this task's trigger comment id",
			"squad leader recorded no_action",
		},
		filepath.Join(repoRoot, "server/internal/service/task_failure_gate_cerebro.go"): {
			"legacyTaskFailureDecision", "failrouter",
		},
		filepath.Join(repoRoot, "server/internal/cerebro/wakeup/workflow_gate.go"): {
			"legacyWakeupFireDecision", "enforceLegacyCreateRules",
		},
		filepath.Join(repoRoot, "server/internal/cerebro/workflows/service.go"): {
			"OpCheckPasses", "gateEval", "\"check_passes\"",
		},
		filepath.Join(repoRoot, "packages/cerebro-feature-flags/registry.ts"): {
			// cerebro_workflow_hooks stays in the registry on purpose: it is the
			// per-workspace off switch for the Workflow engine, not a retired
			// local gate. The flags below are retired local gates.
			"cerebro_comment_target_guard", "cerebro_comment_no_unbacked_promise",
			"cerebro_sub_issue_no_owner_mention", "cerebro_wakeup_loop_guard",
		},
		filepath.Join(repoRoot, "packages/cerebro-workflows/views/workflow-form.tsx"): {
			"loopPlanningMode", "loop_planning",
		},
		filepath.Join(repoRoot, "server/internal/cerebro/runtime/firtal_gateway_tools_extended.go"): {
			"FirtalReportLoopCheckTool", "FirtalReportLoopJudgeTool", "FirtalReportLoopHumanTool",
		},
	}
	for path, forbidden := range checks {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, fragment := range forbidden {
			if strings.Contains(string(body), fragment) {
				t.Errorf("retired local gate %q found in %s", fragment, path)
			}
		}
	}
}
