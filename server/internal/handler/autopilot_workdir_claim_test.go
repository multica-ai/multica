package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// GH #8532: autopilot run_only tasks must be offered the previous run's
// workdir so the daemon can reuse the path and the provider's prompt cache
// survives across runs. Issue and chat follow-ups already get PriorWorkDir;
// this extends the same contract to the autopilot claim branch.
func TestAutopilotClaimOffersPriorWorkDir(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("latest prior task of the same autopilot is offered", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, context.Background())
		autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"title":           "cache-stable autopilot",
			"assignee_id":     agentID,
			"execution_mode":  "run_only",
			"created_by_type": "member",
			"created_by_id":   testUserID,
		})
		run1 := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID, "source": "schedule", "status": "completed",
		})
		run2 := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID, "source": "schedule", "status": "running",
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "autopilot_run_id": run1, "status": "completed",
			"work_dir": "/tmp/mc-ap-prior/workdir",
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "autopilot_run_id": run2, "status": "queued",
		})

		claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if claimed.PriorWorkDir != "/tmp/mc-ap-prior/workdir" {
			t.Fatalf("claim PriorWorkDir = %q, want the prior run's workdir", claimed.PriorWorkDir)
		}
		if claimed.PriorSessionID != "" {
			t.Fatalf("autopilot claim must not resume a session, got %q", claimed.PriorSessionID)
		}
	})

	t.Run("another autopilot's workdir is not offered", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, context.Background())
		autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"title":           "autopilot A",
			"assignee_id":     agentID,
			"execution_mode":  "run_only",
			"created_by_type": "member",
			"created_by_id":   testUserID,
		})
		otherAutopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"title":           "autopilot B",
			"assignee_id":     agentID,
			"execution_mode":  "run_only",
			"created_by_type": "member",
			"created_by_id":   testUserID,
		})
		otherRun := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": otherAutopilotID, "source": "schedule", "status": "completed",
		})
		run2 := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID, "source": "schedule", "status": "running",
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "autopilot_run_id": otherRun, "status": "completed",
			"work_dir": "/tmp/mc-ap-other/workdir",
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "autopilot_run_id": run2, "status": "queued",
		})

		claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if claimed.PriorWorkDir != "" {
			t.Fatalf("claim leaked another autopilot's workdir %q", claimed.PriorWorkDir)
		}
	})

	t.Run("fresh-session task starts on a new path", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, context.Background())
		autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"title":           "fresh autopilot",
			"assignee_id":     agentID,
			"execution_mode":  "run_only",
			"created_by_type": "member",
			"created_by_id":   testUserID,
		})
		run1 := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID, "source": "schedule", "status": "completed",
		})
		run2 := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID, "source": "schedule", "status": "running",
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "autopilot_run_id": run1, "status": "completed",
			"work_dir": "/tmp/mc-ap-fresh/workdir",
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "autopilot_run_id": run2, "status": "queued",
			"force_fresh_session": true,
		})

		claimed := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if claimed.PriorWorkDir != "" {
			t.Fatalf("fresh session must not be offered a workdir, got %q", claimed.PriorWorkDir)
		}
	})
}
