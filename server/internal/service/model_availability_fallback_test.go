package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestResolveConcreteModel_FallbackChain(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, agentID, _ := seedAttributionFixture(t, pool)

	// Set agent tier balanced and insert global fallback chain
	agentUUID := util.MustParseUUID(agentID)
	wsUUID := util.MustParseUUID(workspaceID)
	// Ensure agent service_tier balanced
	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	// Upsert global tier map with fallback chain
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a", "fallback-b"}}); err != nil {
		t.Fatalf("upsert global tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a','fallback-b')`)
	})
	svc := &TaskService{Queries: q}
	// Healthy -> should return primary
	got := svc.resolveConcreteModel(ctx, wsUUID, "balanced")
	if got != "primary-model" {
		t.Fatalf("healthy primary = %q, want primary-model", got)
	}
	// Mark primary unhealthy -> should return fallback-a
	if _, err := q.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model", Reason: pgtype.Text{String: "provider_network", Valid: true}}); err != nil {
		t.Fatalf("mark unhealthy: %v", err)
	}
	got = svc.resolveConcreteModel(ctx, wsUUID, "balanced")
	if got != "fallback-a" {
		t.Fatalf("after primary unhealthy = %q, want fallback-a", got)
	}
	// Mark fallback-a unhealthy -> should return fallback-b
	if _, err := q.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "fallback-a", Reason: pgtype.Text{String: "pricing", Valid: true}}); err != nil {
		t.Fatalf("mark fallback-a unhealthy: %v", err)
	}
	got = svc.resolveConcreteModel(ctx, wsUUID, "balanced")
	if got != "fallback-b" {
		t.Fatalf("after fallback-a unhealthy = %q, want fallback-b", got)
	}
	// Check isModelHealthy stale TTL: manipulate last_failure_at to be old
	_, _ = pool.Exec(ctx, `UPDATE model_health SET last_failure_at = now() - interval '20 minutes' WHERE concrete='primary-model'`)
	got = svc.resolveConcreteModel(ctx, wsUUID, "balanced")
	// primary now stale (10m TTL) so should be considered healthy again and return primary despite unhealthy status
	if got != "primary-model" {
		t.Fatalf("stale unhealthy should be healthy again, got %q want primary-model", got)
	}
	// Reset for next test
	_, _ = pool.Exec(ctx, `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a')`)
	_ = agentUUID
}

func TestResolveConcreteModel_AllUnhealthyReturnsPrimary_NoDeadlock(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, _, _ := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a')`)
	})
	// Mark both unhealthy fresh
	for _, c := range []string{"primary-model", "fallback-a"} {
		if _, err := q.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: c, Reason: pgtype.Text{String: "provider_server_error", Valid: true}}); err != nil {
			t.Fatalf("mark %s: %v", c, err)
		}
	}
	svc := &TaskService{Queries: q}
	got := svc.resolveConcreteModel(ctx, wsUUID, "balanced")
	if got != "primary-model" {
		t.Fatalf("all unhealthy should return primary best-effort, got %q", got)
	}
}

func TestResolveConcreteModel_WorkspaceHealthOverridesGlobal(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, _, _ := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete='primary-model'`)
	})
	// Global unhealthy
	if _, err := q.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model", Reason: pgtype.Text{String: "pricing", Valid: true}}); err != nil {
		t.Fatalf("global mark: %v", err)
	}
	// Workspace healthy overrides -> should be healthy
	if err := q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model"}); err != nil {
		t.Fatalf("workspace healthy: %v", err)
	}
	svc := &TaskService{Queries: q}
	got := svc.resolveConcreteModel(ctx, wsUUID, "balanced")
	if got != "primary-model" {
		t.Fatalf("workspace healthy override should return primary, got %q", got)
	}
	// Cleanup
	pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete='primary-model'`)
}

func TestFailTask_MarksUnhealthyAndRetryFallback(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)
	// Setup tier with fallback
	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a')`)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Ensure task concrete is primary
	if !task.ConcreteModel.Valid || task.ConcreteModel.String != "primary-model" {
		t.Fatalf("task concrete = %q, want primary-model", task.ConcreteModel.String)
	}
	// Need to set task to running state before fail: simulate claim->start
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("set running: %v", err)
	}
	// Fail with provider_network (should mark unhealthy and create retry with fallback)
	failed, err := svc.FailTask(ctx, task.ID, "connection closed mid-response", "", "", "", string(taskfailure.ReasonAgentProviderNetwork), false, "", "")
	if err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("failed status %q", failed.Status)
	}
	// Check health marked unhealthy (workspace-specific)
	h, err := q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: wsUUID, Concrete: "primary-model"})
	if err != nil {
		// fallback to global if not found
		h, err = q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
		if err != nil {
			t.Fatalf("get health: %v", err)
		}
	}
	if h.Status != "unhealthy" {
		t.Fatalf("health status %q, want unhealthy", h.Status)
	}
	// Check retry child exists and concrete differs (fallback-a)
	var retryID pgtype.UUID
	var retryConcrete pgtype.Text
	err = pool.QueryRow(ctx, `SELECT id, concrete_model FROM agent_task_queue WHERE parent_task_id=$1 AND status IN ('queued','deferred')`, task.ID).Scan(&retryID, &retryConcrete)
	if err != nil {
		// Maybe retry was deferred? Check for any retry child
		t.Fatalf("find retry child: %v", err)
	}
	if !retryConcrete.Valid || retryConcrete.String != "fallback-a" {
		t.Fatalf("retry concrete = %q, want fallback-a", retryConcrete.String)
	}
	// Also check requested vs concrete observable
	var req pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT requested_concrete_model FROM agent_task_queue WHERE id=$1`, retryID).Scan(&req); err == nil && req.Valid {
		// requested should be primary-model (original), but we set retryRequested not overriding so parent's requested is primary? For retry child, requested is copied from parent? Check.
		// For this test, we don't assert requested strictly, just that fallback was used.
	}
	// Test other failure reasons also mark: provider_server_error, capacity, model_not_found
	for idx, reason := range []taskfailure.Reason{taskfailure.ReasonAgentProviderServerError, taskfailure.ReasonAgentProviderCapacityOrRateLimit, taskfailure.ReasonAgentModelNotFoundOrUnavailable} {
		// Ensure primary healthy before next enqueue so task picks primary
		q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model"})
		q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
		// Use fresh issue per reason to avoid pending-slot collisions
		var newIssueID2 string
		if err := pool.QueryRow(ctx, `INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number) VALUES ($1, 'reason test', 'member', $2, 'agent', $3, 'medium', $4) RETURNING id`, workspaceID, userID, agentID, 9000+idx).Scan(&newIssueID2); err != nil {
			t.Fatalf("new issue2: %v", err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, newIssueID2) })
		issue2 := db.Issue{
			ID:           util.MustParseUUID(newIssueID2),
			AssigneeID:   util.MustParseUUID(agentID),
			Priority:     "medium",
			CreatorType:  "member",
			CreatorID:    util.MustParseUUID(userID),
			WorkspaceID:  wsUUID,
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
		}
		task2, err := svc.EnqueueTaskForIssue(ctx, issue2)
		if err != nil {
			t.Fatalf("enqueue2: %v", err)
		}
		// Set running
		pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, task2.ID)
		// ensure primary healthy before next fail (workspace)
		q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model"})
		q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
		// Fail with reason
		_, err = svc.FailTask(ctx, task2.ID, "error for "+string(reason), "", "", "", string(reason), false, "", "")
		if err != nil {
			t.Fatalf("FailTask %s: %v", reason, err)
		}
		h, err = q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: wsUUID, Concrete: "primary-model"})
		if err != nil {
			h, err = q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
		}
		if err != nil || h.Status != "unhealthy" {
			t.Fatalf("reason %s should mark unhealthy, got %v %v", reason, h, err)
		}
		// cleanup: mark healthy for next iteration
		q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model"})
		q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
		// cleanup tasks
		pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id=$1`, task2.ID)
		// Find and delete retry child
		pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE parent_task_id=$1`, task2.ID)
	}
}

func TestCompleteTask_RecoversHealthy(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)
	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete='primary-model'`)
	})
	// Mark primary unhealthy first (workspace-specific to match recovery path)
	if _, err := q.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model", Reason: pgtype.Text{String: "provider_network", Valid: true}}); err != nil {
		t.Fatalf("mark unhealthy: %v", err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// task should be fallback-a because primary unhealthy
	if task.ConcreteModel.String != "fallback-a" {
		t.Fatalf("expected fallback-a due to unhealthy primary, got %q", task.ConcreteModel.String)
	}
	// Simulate running and then complete successfully with fallback-a
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("set running: %v", err)
	}
	completed, err := svc.CompleteTask(ctx, task.ID, []byte(`{"output":"done"}`), "sess", "/tmp", "branch", false, "", "")
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("status %q", completed.Status)
	}
	// Check health of fallback-a is now healthy (was it marked? It was fallback, not previously unhealthy. But we should check primary still unhealthy? Actually success on fallback should mark fallback healthy, not primary. To test recovery, we should make a task that uses primary and succeeds after primary was unhealthy but now considered stale? Simpler: create a task that explicitly uses primary-model as concrete (even though fallback would be used, we force primary).
	// For this test, we want to ensure that completing a task with concrete primary-model clears its unhealthy.
	// So we need to directly insert a task with concrete primary-model and then complete it.
	var directTaskID pgtype.UUID
	// Insert direct task with primary-model concrete despite health (bypass resolver)
	var newID string
	if err := pool.QueryRow(ctx, `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, concrete_model) VALUES ($1, (SELECT runtime_id FROM agent WHERE id=$1), $2, 'running', 0, 'primary-model') RETURNING id`, agentID, issueID).Scan(&newID); err != nil {
		t.Fatalf("insert direct: %v", err)
	}
	directTaskID = util.MustParseUUID(newID)
	// Complete it
	if _, err := svc.CompleteTask(ctx, directTaskID, []byte(`{"output":"ok"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("complete direct: %v", err)
	}
	h, err := q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: wsUUID, Concrete: "primary-model"})
	if err != nil {
		// fallback to global
		h, err = q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
		if err != nil {
			t.Fatalf("get health after success: %v", err)
		}
	}
	if h.Status != "healthy" {
		t.Fatalf("health should be healthy after success, got %q", h.Status)
	}
}

func TestPricingBreachAndRecovery(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	// Setup tier with fallback
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_pricing WHERE concrete='primary-model'`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete='primary-model'`)
	})
	// Insert pricing with breach: input 0.01, threshold 0.005 (via string scan)
	var thr, pr pgtype.Numeric
	if err := thr.Scan("0.005"); err != nil {
		t.Fatalf("scan thr: %v", err)
	}
	if err := pr.Scan("0.01"); err != nil {
		t.Fatalf("scan price: %v", err)
	}
	if _, err := q.UpsertModelPricing(ctx, db.UpsertModelPricingParams{Concrete: "primary-model", InputUsdPerMtok: pr, ThresholdInputUsdPerMtok: thr}); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	watcher := &ModelPricingWatcher{Queries: q}
	if err := watcher.CheckOnce(ctx); err != nil {
		t.Fatalf("watcher check: %v", err)
	}
	h, err := q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	if h.Status != "unhealthy" || h.Reason.String != "pricing" {
		t.Fatalf("expected pricing unhealthy, got status=%q reason=%q", h.Status, h.Reason.String)
	}
	// Resolver should skip primary and return fallback
	svc := &TaskService{Queries: q}
	// Need workspace
	workspaceID, _, _, _ := seedAttributionFixture(t, pool)
	_ = workspaceID
	// Use empty workspace (global) for resolver
	got := svc.resolveConcreteModel(ctx, pgtype.UUID{}, "balanced")
	if got != "fallback-a" {
		t.Fatalf("pricing breach resolver should return fallback-a, got %q", got)
	}
	// Now recovery: lower price below threshold
	var low pgtype.Numeric
	if err := low.Scan("0.001"); err != nil {
		t.Fatalf("scan low: %v", err)
	}
	if _, err := q.UpsertModelPricing(ctx, db.UpsertModelPricingParams{Concrete: "primary-model", InputUsdPerMtok: low, ThresholdInputUsdPerMtok: thr}); err != nil {
		t.Fatalf("upsert low: %v", err)
	}
	if err := watcher.CheckOnce(ctx); err != nil {
		t.Fatalf("watcher second check: %v", err)
	}
	h, err = q.GetModelHealth(ctx, db.GetModelHealthParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
	if err != nil {
		t.Fatalf("get health after recovery: %v", err)
	}
	if h.Status != "healthy" {
		t.Fatalf("expected healthy after recovery, got %q", h.Status)
	}
	got = svc.resolveConcreteModel(ctx, pgtype.UUID{}, "balanced")
	if got != "primary-model" {
		t.Fatalf("after recovery resolver should return primary, got %q", got)
	}
}

func TestRequestedConcreteModel_Observable(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)
	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(context.Background(), `DELETE FROM model_health WHERE concrete='primary-model'`)
	})
	// Mark primary unhealthy to trigger fallback
	if _, err := q.UpsertModelHealthUnhealthy(ctx, db.UpsertModelHealthUnhealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model", Reason: pgtype.Text{String: "provider_network", Valid: true}}); err != nil {
		t.Fatalf("mark: %v", err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var req, conc pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT requested_concrete_model, concrete_model FROM agent_task_queue WHERE id=$1`, task.ID).Scan(&req, &conc); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !req.Valid || req.String != "primary-model" {
		t.Fatalf("requested = %q, want primary-model", req.String)
	}
	if !conc.Valid || conc.String != "fallback-a" {
		t.Fatalf("concrete = %q, want fallback-a", conc.String)
	}
	if req.String == conc.String {
		t.Fatalf("requested should differ from concrete when fallback used")
	}
	// When healthy, they should be equal (or requested same as concrete, but we store both)
	q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: pgtype.UUID{}, Concrete: "primary-model"})
	// Need new issue for second enqueue (since pending slot)
	var issueID2 string
	if err := pool.QueryRow(ctx, `INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number) VALUES ($1,'second','member',$2,'agent',$3,'medium', 9002) RETURNING id`, workspaceID, userID, agentID).Scan(&issueID2); err != nil {
		t.Fatalf("second issue: %v", err)
	}
	issue2 := db.Issue{
		ID:           util.MustParseUUID(issueID2),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task2, err := svc.EnqueueTaskForIssue(ctx, issue2)
	if err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	var req2, conc2 pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT requested_concrete_model, concrete_model FROM agent_task_queue WHERE id=$1`, task2.ID).Scan(&req2, &conc2); err != nil {
		t.Fatalf("read2: %v", err)
	}
	// When healthy, both should be primary-model (requested and concrete same)
	if req2.String != "primary-model" || conc2.String != "primary-model" {
		t.Fatalf("healthy requested/concrete should both be primary, got req=%q conc=%q", req2.String, conc2.String)
	}
	_ = time.Now()
}
