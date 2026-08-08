package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestScheduledAutopilotOfflineRuntimeRetryContract(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	svc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id::text, a.runtime_id::text
		  FROM agent a
		 WHERE a.workspace_id = $1 AND a.runtime_id IS NOT NULL
		 ORDER BY a.created_at ASC LIMIT 1`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load fixture agent/runtime: %v", err)
	}
	var originalStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&originalStatus); err != nil {
		t.Fatalf("load runtime status: %v", err)
	}
	setStatus := func(status string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET status = $1 WHERE id = $2`, status, runtimeID); err != nil {
			t.Fatalf("set runtime %s: %v", status, err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE agent_runtime SET status = $1 WHERE id = $2`, originalStatus, runtimeID)
	})

	createAP := func(assigneeType string, assigneeID pgtype.UUID) (db.Autopilot, db.AutopilotTrigger) {
		t.Helper()
		ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
			WorkspaceID: parseUUID(testWorkspaceID), Title: "offline retry contract",
			AssigneeType: assigneeType, AssigneeID: assigneeID, Status: "active",
			ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("CreateAutopilot: %v", err)
		}
		trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
			AutopilotID: ap.ID, Kind: "schedule", Enabled: true,
			CronExpression: pgtype.Text{String: "*/5 * * * *", Valid: true},
			Timezone:       pgtype.Text{String: "UTC", Valid: true},
		})
		if err != nil {
			t.Fatalf("CreateAutopilotTrigger: %v", err)
		}
		t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID) })
		return ap, trigger
	}

	agentAP, agentTrigger := createAP("agent", parseUUID(agentID))
	plan := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	setStatus("offline")
	if run, err := svc.DispatchAutopilotForPlanAttempt(ctx, agentAP, agentTrigger.ID, "schedule", nil, plan, true); err == nil || run != nil {
		t.Fatalf("temporary offline must return retryable error without run: run=%v err=%v", run, err)
	}
	var count int
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_run WHERE trigger_id=$1`, agentTrigger.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("offline retry attempt created %d runs, want 0", count)
	}

	setStatus("online")
	run, err := svc.DispatchAutopilotForPlanAttempt(ctx, agentAP, agentTrigger.ID, "schedule", nil, plan, true)
	if err != nil || run == nil || !run.TaskID.Valid {
		t.Fatalf("online retry did not dispatch: run=%v err=%v", run, err)
	}

	persistentPlan := plan.Add(5 * time.Minute)
	setStatus("offline")
	for attempt := 1; attempt < 3; attempt++ {
		if run, err = svc.DispatchAutopilotForPlanAttempt(ctx, agentAP, agentTrigger.ID, "schedule", nil, persistentPlan, true); err == nil || run != nil {
			t.Fatalf("offline attempt %d must retry: run=%v err=%v", attempt, run, err)
		}
	}
	run, err = svc.DispatchAutopilotForPlanAttempt(ctx, agentAP, agentTrigger.ID, "schedule", nil, persistentPlan, false)
	if err != nil || run == nil || run.Status != "skipped" || !strings.Contains(run.FailureReason.String, "offline at dispatch time") {
		t.Fatalf("final offline attempt must persist terminal reason: run=%v err=%v", run, err)
	}
	duplicate, err := svc.DispatchAutopilotForPlanAttempt(ctx, agentAP, agentTrigger.ID, "schedule", nil, persistentPlan, false)
	if err != nil || duplicate.ID != run.ID {
		t.Fatalf("duplicate plan did not reuse run: duplicate=%v err=%v", duplicate, err)
	}
	_ = testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_run WHERE trigger_id=$1 AND planned_at=$2`, agentTrigger.ID, persistentPlan).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate plan created %d runs, want 1", count)
	}

	squad, err := queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: parseUUID(testWorkspaceID), Name: "offline retry squad", LeaderID: parseUUID(agentID), CreatorID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateSquad: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM squad WHERE id=$1`, squad.ID) })
	squadAP, squadTrigger := createAP("squad", squad.ID)
	squadPlan := persistentPlan.Add(5 * time.Minute)
	if run, err = svc.DispatchAutopilotForPlanAttempt(ctx, squadAP, squadTrigger.ID, "schedule", nil, squadPlan, true); err == nil || run != nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("offline squad leader must retry: run=%v err=%v", run, err)
	}
	setStatus("online")
	run, err = svc.DispatchAutopilotForPlanAttempt(ctx, squadAP, squadTrigger.ID, "schedule", nil, squadPlan, true)
	if err != nil || run == nil || !run.TaskID.Valid || !run.SquadID.Valid || run.SquadID != squad.ID {
		t.Fatalf("squad retry did not dispatch to attributed leader: run=%v err=%v", run, err)
	}
	var taskAgent pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT agent_id FROM agent_task_queue WHERE id=$1`, run.TaskID).Scan(&taskAgent); err != nil || taskAgent != parseUUID(agentID) {
		t.Fatalf("squad task agent mismatch: got=%v err=%v", taskAgent, err)
	}
}
