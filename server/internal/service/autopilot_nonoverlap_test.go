package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestScheduledRunOnlyAutopilotDoesNotOverlap(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id, status,
			execution_mode, created_by_type, created_by_id
		) VALUES ($1, $2, 'agent', $3, 'active', 'run_only', 'member', $4)
		RETURNING id`, workspaceID, fmt.Sprintf("non-overlap-%d", time.Now().UnixNano()), agentID, publisherID,
	).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		statements := []string{
			`DELETE FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)`,
			`DELETE FROM autopilot_run WHERE autopilot_id = $1`,
			`DELETE FROM autopilot_trigger WHERE autopilot_id = $1`,
			`DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`,
			`DELETE FROM autopilot WHERE id = $1`,
		}
		for _, statement := range statements {
			if _, err := pool.Exec(cleanupCtx, statement, autopilotID); err != nil {
				t.Errorf("cleanup autopilot fixture: %v", err)
			}
		}
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_rule_version (
			autopilot_id, workspace_id, published_by_type, published_by_id
		) VALUES ($1, $2, 'member', $3)`, autopilotID, workspaceID, publisherID,
	); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	trigger, err := q.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: "*/5 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("create autopilot trigger: %v", err)
	}
	var staleRunID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status, triggered_at, planned_at)
		VALUES ($1, $2, 'schedule', 'running', now() - interval '10 minutes', now() - interval '10 minutes')
		RETURNING id`, autopilotID, trigger.ID,
	).Scan(&staleRunID); err != nil {
		t.Fatalf("seed stale partial run: %v", err)
	}
	services := make([]*AutopilotService, 2)
	for i := range services {
		taskSvc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
		services[i] = &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: taskSvc}
	}

	type result struct {
		run *db.AutopilotRun
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	plannedAt := time.Now().UTC().Truncate(time.Second)
	for i := range services {
		wg.Add(1)
		go func(svc *AutopilotService, occurrence time.Time) {
			defer wg.Done()
			<-start
			run, err := svc.DispatchAutopilotForPlan(
				ctx,
				ap,
				trigger.ID,
				"schedule",
				nil,
				occurrence,
			)
			results <- result{run: run, err: err}
		}(services[i], plannedAt.Add(time.Duration(i)*time.Minute))
	}
	close(start)
	wg.Wait()
	close(results)

	statusCounts := map[string]int{}
	var runningRun, skippedRun *db.AutopilotRun
	for got := range results {
		if got.err != nil {
			t.Fatalf("dispatch: %v", got.err)
		}
		if got.run == nil {
			t.Fatal("dispatch returned a nil run")
		}
		statusCounts[got.run.Status]++
		switch got.run.Status {
		case "running":
			runningRun = got.run
		case "skipped":
			skippedRun = got.run
		}
	}

	if statusCounts["running"] != 1 || statusCounts["skipped"] != 1 {
		t.Fatalf("run statuses = %#v, want one running and one skipped", statusCounts)
	}
	if runningRun == nil || skippedRun == nil {
		t.Fatal("dispatch results did not include both the admitted and skipped runs")
	}
	wantSkipReason := "active autopilot run: " + util.UUIDToString(runningRun.ID)
	if !skippedRun.FailureReason.Valid || skippedRun.FailureReason.String != wantSkipReason {
		t.Fatalf("skipped failure reason = %q, want %q", skippedRun.FailureReason.String, wantSkipReason)
	}

	var staleStatus, staleFailureReason string
	var stalePlanCleared bool
	if err := pool.QueryRow(ctx, `
		SELECT status, failure_reason, planned_at IS NULL
		FROM autopilot_run
		WHERE id = $1`, staleRunID,
	).Scan(&staleStatus, &staleFailureReason, &stalePlanCleared); err != nil {
		t.Fatalf("read recovered stale run: %v", err)
	}
	if staleStatus != "failed" || staleFailureReason != "recovered stale partial dispatch before scheduled admission" || !stalePlanCleared {
		t.Fatalf("stale run = status %q, reason %q, plan cleared %v; want retryable recovered failure", staleStatus, staleFailureReason, stalePlanCleared)
	}

	var taskCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue q
		JOIN autopilot_run r ON r.id = q.autopilot_run_id
		WHERE r.autopilot_id = $1`, autopilotID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("task count = %d, want 1", taskCount)
	}

	probeRun, probeReason, err := services[0].dispatchAutopilot(
		ctx,
		ap,
		trigger.ID,
		"schedule",
		nil,
		pgtype.Timestamptz{Time: plannedAt.Add(2 * time.Minute), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("scheduled overlap probe: %v", err)
	}
	if probeReason != dispatch.ReasonAlreadyActive || probeRun.Status != "skipped" {
		t.Fatalf("scheduled overlap probe = status %q, reason %q; want skipped/already_active", probeRun.Status, probeReason)
	}
	if !probeRun.FailureReason.Valid || probeRun.FailureReason.String != wantSkipReason {
		t.Fatalf("scheduled overlap failure reason = %q, want %q", probeRun.FailureReason.String, wantSkipReason)
	}

	manualRun, manualReason, err := services[0].dispatchAutopilot(
		ctx,
		ap,
		pgtype.UUID{},
		"manual",
		nil,
		pgtype.Timestamptz{},
		pgtype.UUID{},
		util.MustParseUUID(publisherID),
	)
	if err != nil {
		t.Fatalf("manual dispatch: %v", err)
	}
	if manualRun.Status != "running" || manualReason != "" {
		t.Fatalf("manual dispatch = status %q, reason %q; want running with no skip", manualRun.Status, manualReason)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue q
		JOIN autopilot_run r ON r.id = q.autopilot_run_id
		WHERE r.autopilot_id = $1`, autopilotID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("recount tasks: %v", err)
	}
	if taskCount != 2 {
		t.Fatalf("task count after manual dispatch = %d, want 2", taskCount)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now()
		WHERE autopilot_run_id IN ($1, $2)`, runningRun.ID, manualRun.ID,
	); err != nil {
		t.Fatalf("complete linked tasks without listener sync: %v", err)
	}

	nextPlannedAt := plannedAt.Add(3 * time.Minute)
	nextRun, err := services[0].DispatchAutopilotForPlan(
		ctx,
		ap,
		trigger.ID,
		"schedule",
		nil,
		nextPlannedAt,
	)
	if err != nil {
		t.Fatalf("dispatch after terminal linked tasks: %v", err)
	}
	if nextRun.Status != "running" || !nextRun.TaskID.Valid {
		t.Fatalf("dispatch after terminal linked tasks = status %q, task valid %v; want admitted running task", nextRun.Status, nextRun.TaskID.Valid)
	}

	reusedRun, reusedOutcome, err := services[1].createAutopilotRunWithAdmission(
		ctx,
		ap,
		trigger.ID,
		"schedule",
		nil,
		pgtype.Timestamptz{Time: nextPlannedAt, Valid: true},
		pgtype.UUID{},
		"running",
	)
	if err != nil {
		t.Fatalf("same-occurrence admission reuse: %v", err)
	}
	if reusedOutcome != autopilotAdmissionReused || reusedRun.ID != nextRun.ID {
		t.Fatalf("same-occurrence admission = outcome %v, run %s; want reused %s", reusedOutcome, util.UUIDToString(reusedRun.ID), util.UUIDToString(nextRun.ID))
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue q
		JOIN autopilot_run r ON r.id = q.autopilot_run_id
		WHERE r.autopilot_id = $1`, autopilotID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks after terminal recovery: %v", err)
	}
	if taskCount != 3 {
		t.Fatalf("task count after terminal recovery and same-plan reuse = %d, want 3", taskCount)
	}
}
