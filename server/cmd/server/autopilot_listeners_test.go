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
				if _, err := taskSvc.FailTask(ctx, task.ID, "boom", "", "", "agent_error", ""); err != nil {
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
