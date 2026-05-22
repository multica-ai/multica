package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotRunOnlyTaskTerminalEventsUpdateRun(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	tests := []struct {
		name       string
		finalize   func(task db.AgentTaskQueue)
		wantStatus string
		wantResult string
		wantReason string
	}{
		{
			name: "completed",
			finalize: func(task db.AgentTaskQueue) {
				if _, err := taskSvc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "", ""); err != nil {
					t.Fatalf("CompleteTask: %v", err)
				}
			},
			wantStatus: "completed",
			wantResult: "done",
		},
		{
			name: "failed",
			finalize: func(task db.AgentTaskQueue) {
				if _, err := taskSvc.FailTask(ctx, task.ID, "boom", "", "", "agent_error"); err != nil {
					t.Fatalf("FailTask: %v", err)
				}
			},
			wantStatus: "failed",
			wantReason: "boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
				WorkspaceID:        parseUUID(testWorkspaceID),
				Title:              "Run-only listener " + tc.name,
				Description:        pgtype.Text{String: "Run listener regression test", Valid: true},
				AssigneeType:       "agent",
				AssigneeID:         parseUUID(agentID),
				Status:             "active",
				ExecutionMode:      "run_only",
				IssueTitleTemplate: pgtype.Text{},
				CreatedByType:      "member",
				CreatedByID:        parseUUID(testUserID),
			})
			if err != nil {
				t.Fatalf("CreateAutopilot: %v", err)
			}
			t.Cleanup(func() {
				if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
					t.Logf("cleanup autopilot: %v", err)
				}
			})

			run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
			if err != nil {
				t.Fatalf("DispatchAutopilot: %v", err)
			}
			if !run.TaskID.Valid {
				t.Fatal("run_only dispatch did not link a task")
			}

			if _, err := testPool.Exec(ctx,
				`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
				run.TaskID,
			); err != nil {
				t.Fatalf("mark task dispatched: %v", err)
			}
			task, err := queries.StartAgentTask(ctx, run.TaskID)
			if err != nil {
				t.Fatalf("StartAgentTask: %v", err)
			}

			tc.finalize(task)

			updatedRun, err := queries.GetAutopilotRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetAutopilotRun: %v", err)
			}
			if updatedRun.Status != tc.wantStatus {
				t.Fatalf("expected run status %q, got %q", tc.wantStatus, updatedRun.Status)
			}
			if tc.wantResult != "" && !strings.Contains(string(updatedRun.Result), tc.wantResult) {
				t.Fatalf("expected run result to contain %q, got %s", tc.wantResult, string(updatedRun.Result))
			}
			if tc.wantReason != "" {
				if !updatedRun.FailureReason.Valid {
					t.Fatalf("expected failure reason %q, got invalid", tc.wantReason)
				}
				if updatedRun.FailureReason.String != tc.wantReason {
					t.Fatalf("expected failure reason %q, got %q", tc.wantReason, updatedRun.FailureReason.String)
				}
			}
		})
	}
}

// TestAutopilotDispatchSkipsWhenRuntimeOffline locks in the MUL-1899
// admission gate: when the assignee agent's runtime is not online we must
// record a `skipped` autopilot_run with a failure_reason and NOT enqueue an
// agent_task_queue row. This is the fix for "活跃 schedule 持续给离线 local
// agent 入队".
func TestAutopilotDispatchSkipsWhenRuntimeOffline(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	// Spin up a dedicated runtime + agent so we can flip the runtime to
	// offline without affecting the shared fixture used by other tests.
	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Offline runtime', 'local', 'mul1899_offline_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'mul1899-offline-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create offline agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Offline-runtime autopilot",
		Description:        pgtype.Text{String: "MUL-1899 admission test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("expected run status 'skipped', got %q", run.Status)
	}
	if !run.FailureReason.Valid || !strings.Contains(run.FailureReason.String, "offline") {
		t.Fatalf("expected failure reason mentioning 'offline', got %+v", run.FailureReason)
	}
	if run.TaskID.Valid {
		t.Fatalf("expected no task to be enqueued, got task_id %v", run.TaskID)
	}

	// Defensive: confirm at the DB layer that nothing landed on the queue.
	var taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`,
		agentID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected 0 queued tasks for offline-runtime agent, got %d", taskCount)
	}
}

// TestManualTriggerDoesNotErrorOnPostAdmissionSkip locks in PR #2888 review
// fix #2: if the dispatcher decides to skip after the admission gate has
// already passed (e.g. the leader's runtime went offline between admission
// and task creation), DispatchAutopilot must return (run, nil) with
// status='skipped' rather than (nil, err). Without this, manual trigger
// surfaces a 500 to the user even though the work was correctly suppressed
// — the same regression Emacs flagged on the original PR.
//
// We synthesise the race by:
//  1. Creating an online runtime + agent so the admission gate passes.
//  2. Flipping the runtime to offline.
//  3. Triggering the autopilot. Admission has already loaded the agent +
//     runtime once with status='online' at row-fetch time, so the second
//     check inside dispatchRunOnly is what catches the offline state.
//
// In this implementation the admission gate also re-reads the runtime, so
// the same offline state actually fires the admission skip first. That is
// fine for the assertion we care about: the manual trigger must not 500 and
// the run must be `skipped`. The post-admission branch is exercised
// separately by the errDispatchSkipped unwrap unit test in the service
// package.
func TestManualTriggerDoesNotErrorOnPostAdmissionSkip(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Manual-trigger skip runtime', 'local', 'mul2429_manual_skip_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'mul2429-manual-skip-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Manual-trigger skip autopilot",
		Description:        pgtype.Text{String: "PR #2888 review fix #2", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("manual DispatchAutopilot returned error (would 500 the handler): %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("expected run status 'skipped', got %q", run.Status)
	}
}

// TestSyncRunFromIssue_RecoveryAndGuard locks in MYW-1917 regression coverage:
//   1. failed/skipped runs recover to completed when the linked issue moves to done/in_review.
//   2. previous_failure_reason preserves the original failure_reason.
//   3. failure_reason is cleared on recovery.
//   4. cancelled/blocked does NOT overwrite an already-terminal run's failure_reason.
func TestSyncRunFromIssue_RecoveryAndGuard(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	createIssueAutopilot := func(t *testing.T, title string) (db.Autopilot, db.AutopilotRun) {
		t.Helper()
		ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
			WorkspaceID:        parseUUID(testWorkspaceID),
			Title:              title,
			Description:        pgtype.Text{String: "MYW-1917 regression test", Valid: true},
			AssigneeType:       "agent",
			AssigneeID:         parseUUID(agentID),
			Status:             "active",
			ExecutionMode:      "create_issue",
			IssueTitleTemplate: pgtype.Text{String: "test", Valid: true},
			CreatedByType:      "member",
			CreatedByID:        parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("CreateAutopilot: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
		})

		run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
		if err != nil {
			t.Fatalf("DispatchAutopilot: %v", err)
		}
		return ap, *run
	}

	t.Run("failed_run_recovers_to_completed", func(t *testing.T) {
		_, run := createIssueAutopilot(t, "MYW-1917 recover failed")

		// Simulate the run being marked failed (e.g. by a timeout or cancellation attempt).
		_, err := testPool.Exec(ctx,
			`UPDATE autopilot_run SET status = 'failed', completed_at = now(), failure_reason = 'issue cancelled' WHERE issue_id = $1`,
			run.IssueID,
		)
		if err != nil {
			t.Fatalf("mark run failed: %v", err)
		}

		// Issue later completes.
		dbIssue, err := queries.GetIssue(ctx, run.IssueID)
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		dbIssue.Status = "done"

		autopilotSvc.SyncRunFromIssue(ctx, dbIssue)

		updatedRun, err := queries.GetAutopilotRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetAutopilotRun: %v", err)
		}
		if updatedRun.Status != "completed" {
			t.Fatalf("expected status completed, got %q", updatedRun.Status)
		}
		if updatedRun.FailureReason.Valid {
			t.Fatalf("expected failure_reason cleared, got %q", updatedRun.FailureReason.String)
		}
		if !updatedRun.PreviousFailureReason.Valid || updatedRun.PreviousFailureReason.String != "issue cancelled" {
			t.Fatalf("expected previous_failure_reason 'issue cancelled', got %+v", updatedRun.PreviousFailureReason)
		}
	})

	t.Run("skipped_run_recovers_to_completed", func(t *testing.T) {
		_, run := createIssueAutopilot(t, "MYW-1917 recover skipped")

		_, err := testPool.Exec(ctx,
			`UPDATE autopilot_run SET status = 'skipped', completed_at = now(), failure_reason = 'runtime offline' WHERE issue_id = $1`,
			run.IssueID,
		)
		if err != nil {
			t.Fatalf("mark run skipped: %v", err)
		}

		dbIssue, err := queries.GetIssue(ctx, run.IssueID)
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		dbIssue.Status = "in_review"

		autopilotSvc.SyncRunFromIssue(ctx, dbIssue)

		updatedRun, err := queries.GetAutopilotRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetAutopilotRun: %v", err)
		}
		if updatedRun.Status != "completed" {
			t.Fatalf("expected status completed, got %q", updatedRun.Status)
		}
		if updatedRun.FailureReason.Valid {
			t.Fatalf("expected failure_reason cleared, got %q", updatedRun.FailureReason.String)
		}
		if !updatedRun.PreviousFailureReason.Valid || updatedRun.PreviousFailureReason.String != "runtime offline" {
			t.Fatalf("expected previous_failure_reason 'runtime offline', got %+v", updatedRun.PreviousFailureReason)
		}
	})

	t.Run("cancelled_does_not_overwrite_failed_run", func(t *testing.T) {
		_, run := createIssueAutopilot(t, "MYW-1917 cancel guard")

		_, err := testPool.Exec(ctx,
			`UPDATE autopilot_run SET status = 'failed', completed_at = now(), failure_reason = 'original timeout' WHERE issue_id = $1`,
			run.IssueID,
		)
		if err != nil {
			t.Fatalf("mark run failed: %v", err)
		}

		dbIssue, err := queries.GetIssue(ctx, run.IssueID)
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		dbIssue.Status = "cancelled"

		autopilotSvc.SyncRunFromIssue(ctx, dbIssue)

		updatedRun, err := queries.GetAutopilotRunByIssue(ctx, run.IssueID)
		if err != nil {
			t.Fatalf("GetAutopilotRunByIssue: %v", err)
		}
		if updatedRun.Status != "failed" {
			t.Fatalf("expected status still failed, got %q", updatedRun.Status)
		}
		if !updatedRun.FailureReason.Valid || updatedRun.FailureReason.String != "original timeout" {
			t.Fatalf("expected failure_reason preserved as 'original timeout', got %+v", updatedRun.FailureReason)
		}
		if updatedRun.PreviousFailureReason.Valid {
			t.Fatalf("expected no previous_failure_reason, got %q", updatedRun.PreviousFailureReason.String)
		}
	})

	t.Run("blocked_does_not_overwrite_skipped_run", func(t *testing.T) {
		_, run := createIssueAutopilot(t, "MYW-1917 block guard")

		_, err := testPool.Exec(ctx,
			`UPDATE autopilot_run SET status = 'skipped', completed_at = now(), failure_reason = 'admission skip' WHERE issue_id = $1`,
			run.IssueID,
		)
		if err != nil {
			t.Fatalf("mark run skipped: %v", err)
		}

		dbIssue, err := queries.GetIssue(ctx, run.IssueID)
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		dbIssue.Status = "blocked"

		autopilotSvc.SyncRunFromIssue(ctx, dbIssue)

		updatedRun, err := queries.GetAutopilotRunByIssue(ctx, run.IssueID)
		if err != nil {
			t.Fatalf("GetAutopilotRunByIssue: %v", err)
		}
		if updatedRun.Status != "skipped" {
			t.Fatalf("expected status still skipped, got %q", updatedRun.Status)
		}
		if !updatedRun.FailureReason.Valid || updatedRun.FailureReason.String != "admission skip" {
			t.Fatalf("expected failure_reason preserved as 'admission skip', got %+v", updatedRun.FailureReason)
		}
	})
}
