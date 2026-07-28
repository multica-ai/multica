package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

type adaptiveRoutingFixture struct {
	pool            *pgxpool.Pool
	queries         *db.Queries
	userID          string
	workspaceID     string
	codexRuntimeID  string
	claudeRuntimeID string
	agentID         string
}

func newAdaptiveRoutingFixture(t *testing.T, expectedUse int, includeCodex, protected bool) adaptiveRoutingFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()

	var routingTable pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT to_regclass('provider_plan_capacity')::text`).Scan(&routingTable); err != nil ||
		!routingTable.Valid {
		t.Skip("adaptive routing migrations are not installed in the test database")
	}

	suffix := time.Now().UnixNano()
	var fixture adaptiveRoutingFixture
	fixture.pool = pool
	fixture.queries = db.New(pool)
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Adaptive Routing Test', $1)
		RETURNING id
	`, fmt.Sprintf("adaptive-routing-%d@multica.test", suffix)).Scan(&fixture.userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('adaptive-routing-test', $1)
		RETURNING id
	`, fmt.Sprintf("adaptive-routing-%d", suffix)).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'adaptive-codex', 'cloud', 'codex', 'online',
		        '', '{}'::jsonb, $2, now())
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&fixture.codexRuntimeID); err != nil {
		t.Fatalf("seed codex runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'adaptive-claude', 'cloud', 'claude', 'online',
		        '', '{}'::jsonb, $2, now())
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&fixture.claudeRuntimeID); err != nil {
		t.Fatalf("seed claude runtime: %v", err)
	}

	candidates := []adaptiveCandidateConfig{
		{
			ID:                  "claude-best",
			RuntimeID:           fixture.claudeRuntimeID,
			Model:               "claude-sonnet-test",
			ThinkingLevel:       "high",
			QualityBP:           9200,
			LatencyPenaltyBP:    100,
			ExpectedUsePermille: expectedUse,
		},
	}
	if includeCodex {
		candidates = append(candidates, adaptiveCandidateConfig{
			ID:                  "codex-baseline",
			RuntimeID:           fixture.codexRuntimeID,
			Model:               "gpt-test",
			ThinkingLevel:       "medium",
			QualityBP:           8000,
			LatencyPenaltyBP:    100,
			ExpectedUsePermille: expectedUse,
		})
	}
	runtimeConfig, err := json.Marshal(adaptiveRoutingEnvelope{
		ProviderFailoverProtected: protected,
		AdaptiveRouting: adaptiveRoutingConfig{
			Enabled:    true,
			Risk:       "medium",
			Candidates: candidates,
		},
	})
	if err != nil {
		t.Fatalf("marshal runtime config: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, model, thinking_level
		)
		VALUES ($1, 'adaptive-source', 'cloud', $2::jsonb,
		        $3, 'private', 1, $4, '', '{}'::jsonb,
		        '["--baseline"]'::jsonb, 'gpt-test', 'medium')
		RETURNING id
	`, fixture.workspaceID, runtimeConfig, fixture.codexRuntimeID, fixture.userID).Scan(&fixture.agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, fixture.agentID)
		pool.Exec(cleanupCtx, `DELETE FROM issue WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM provider_plan_capacity WHERE owner_id = $1`, fixture.userID)
		pool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, fixture.agentID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, fixture.workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, fixture.userID)
	})
	return fixture
}

func (f adaptiveRoutingFixture) seedCapacity(t *testing.T, provider string, remaining, reserve int, observedAt time.Time) {
	t.Helper()
	if _, err := f.queries.UpsertProviderPlanCapacity(context.Background(), db.UpsertProviderPlanCapacityParams{
		OwnerID:           util.MustParseUUID(f.userID),
		Provider:          provider,
		Known:             true,
		RemainingPermille: int32(remaining),
		ReservePermille:   int32(reserve),
		ObservedAt:        pgtype.Timestamptz{Time: observedAt, Valid: true},
		Source:            "adaptive-routing-test",
	}); err != nil {
		t.Fatalf("seed %s capacity: %v", provider, err)
	}
}

func (f adaptiveRoutingFixture) seedTask(t *testing.T, title string) db.AgentTaskQueue {
	t.Helper()
	ctx := context.Background()
	var issueID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		VALUES (
			$1,
			$2,
			'member',
			$3,
			(SELECT COALESCE(max(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		)
		RETURNING id
	`, f.workspaceID, title, f.userID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	var taskID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			originator_user_id, accountable_user_id
		)
		VALUES ($1, $2, $3, 'queued', 2, $4, $4)
		RETURNING id
	`, f.agentID, f.codexRuntimeID, issueID, f.userID).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	task, err := f.queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.RouteAdmissionState != "pending" {
		t.Fatalf("INSERT fence state = %q, want pending", task.RouteAdmissionState)
	}
	return task
}

func adaptiveRoutingFlags(active bool) *featureflag.Service {
	static := featureflag.NewStaticProvider()
	static.Set(featureflags.AdaptiveAgentRouting, featureflag.Rule{Default: true})
	static.Set(featureflags.AdaptiveAgentRoutingActive, featureflag.Rule{Default: active})
	return featureflag.NewService(static)
}

func TestAdaptiveAdmissionFencesRoutesAndReleasesReservation(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 150, true, false)
	now := time.Now().UTC()
	fixture.seedCapacity(t, "codex", 400, 200, now)
	fixture.seedCapacity(t, "claude", 900, 200, now)
	task := fixture.seedTask(t, "adaptive route and release")

	// A polling daemon cannot claim between INSERT and admission.
	if _, err := fixture.queries.ClaimAgentTask(context.Background(), db.ClaimAgentTaskParams{
		AgentID:          util.MustParseUUID(fixture.agentID),
		PrepareLeaseSecs: 45,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pending task claim error = %v, want pgx.ErrNoRows", err)
	}

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(true)
	svc.NotifyTaskEnqueued(context.Background(), task)

	routed, err := fixture.queries.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("load routed task: %v", err)
	}
	if routed.RouteAdmissionState != "routed" {
		t.Fatalf("route state = %q, want routed; decision=%s", routed.RouteAdmissionState, routed.RouteDecision)
	}
	if got := util.UUIDToString(routed.RuntimeID); got != fixture.claudeRuntimeID {
		t.Fatalf("runtime = %s, want Claude %s", got, fixture.claudeRuntimeID)
	}
	if routed.RouteModel.String != "claude-sonnet-test" ||
		routed.RouteThinkingLevel.String != "high" ||
		routed.RouteReservedPermille != 150 {
		t.Fatalf("persisted route mismatch: %+v", routed)
	}
	var reserved int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT reserved_inflight_permille
		FROM provider_plan_capacity
		WHERE owner_id = $1 AND provider = 'claude'
	`, fixture.userID).Scan(&reserved); err != nil {
		t.Fatalf("read reservation: %v", err)
	}
	if reserved != 150 {
		t.Fatalf("reserved = %d, want 150", reserved)
	}

	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now()
		WHERE id = $1
	`, util.UUIDToString(task.ID)); err != nil {
		t.Fatalf("complete routed task: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT reserved_inflight_permille
		FROM provider_plan_capacity
		WHERE owner_id = $1 AND provider = 'claude'
	`, fixture.userID).Scan(&reserved); err != nil {
		t.Fatalf("read released reservation: %v", err)
	}
	if reserved != 0 {
		t.Fatalf("terminal transition left reservation = %d, want 0", reserved)
	}
}

func TestAdaptiveAdmissionShadowDoesNotChangeExecution(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 100, true, false)
	now := time.Now().UTC()
	fixture.seedCapacity(t, "codex", 500, 200, now)
	fixture.seedCapacity(t, "claude", 900, 200, now)
	task := fixture.seedTask(t, "adaptive shadow")

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(false)
	svc.NotifyTaskEnqueued(context.Background(), task)

	shadow, err := fixture.queries.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("load shadow task: %v", err)
	}
	if shadow.RouteAdmissionState != "shadow" {
		t.Fatalf("route state = %q, want shadow", shadow.RouteAdmissionState)
	}
	if got := util.UUIDToString(shadow.RuntimeID); got != fixture.codexRuntimeID {
		t.Fatalf("shadow changed runtime to %s, want %s", got, fixture.codexRuntimeID)
	}
	if shadow.RouteReservedPermille != 0 {
		t.Fatalf("shadow reserved capacity: %d", shadow.RouteReservedPermille)
	}
	var record adaptiveAdmissionRecord
	if err := json.Unmarshal(shadow.RouteDecision, &record); err != nil {
		t.Fatalf("decode shadow decision: %v", err)
	}
	if record.Outcome != "would_route" || record.Selected == nil ||
		record.Selected.Provider != "claude" {
		t.Fatalf("shadow decision = %+v, want Claude would-route", record)
	}
}

func TestAdaptiveAdmissionConcurrentReservationsPreserveHeadroom(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 600, false, false)
	fixture.seedCapacity(t, "claude", 1000, 200, time.Now().UTC())
	first := fixture.seedTask(t, "adaptive concurrent one")
	second := fixture.seedTask(t, "adaptive concurrent two")

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(true)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, task := range []db.AgentTaskQueue{first, second} {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.admitAdaptiveTask(context.Background(), task)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent admission: %v", err)
		}
	}

	var routed, deferred, reserved int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE route_admission_state = 'routed'),
			count(*) FILTER (WHERE route_admission_state = 'deferred')
		FROM agent_task_queue
		WHERE id = ANY($1::uuid[])
	`, []string{util.UUIDToString(first.ID), util.UUIDToString(second.ID)}).Scan(&routed, &deferred); err != nil {
		t.Fatalf("count admission outcomes: %v", err)
	}
	if routed != 1 || deferred != 1 {
		t.Fatalf("outcomes routed=%d deferred=%d, want 1/1", routed, deferred)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT reserved_inflight_permille
		FROM provider_plan_capacity
		WHERE owner_id = $1 AND provider = 'claude'
	`, fixture.userID).Scan(&reserved); err != nil {
		t.Fatalf("read concurrent reservation: %v", err)
	}
	if reserved != 600 {
		t.Fatalf("reserved = %d, want exactly one 600-permille forecast", reserved)
	}
}

func TestAdaptiveAdmissionProtectedIdentityKeepsFixedBinding(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 100, true, true)
	fixture.seedCapacity(t, "codex", 500, 200, time.Now().UTC())
	fixture.seedCapacity(t, "claude", 900, 200, time.Now().UTC())
	task := fixture.seedTask(t, "adaptive protected")

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(true)
	svc.NotifyTaskEnqueued(context.Background(), task)

	protected, err := fixture.queries.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("load protected task: %v", err)
	}
	if protected.RouteAdmissionState != "not_applicable" ||
		util.UUIDToString(protected.RuntimeID) != fixture.codexRuntimeID {
		t.Fatalf("protected identity was rebound: state=%q runtime=%s",
			protected.RouteAdmissionState, util.UUIDToString(protected.RuntimeID))
	}
}

func TestAdaptiveAdmissionRejectsForeignOwnerRuntime(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 100, false, false)
	ctx := context.Background()
	var foreignUserID string
	if err := fixture.pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Foreign Runtime Owner', 'adaptive-foreign-' || gen_random_uuid() || '@multica.test')
		RETURNING id
	`).Scan(&foreignUserID); err != nil {
		t.Fatalf("seed foreign owner: %v", err)
	}
	t.Cleanup(func() {
		fixture.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})
	if _, err := fixture.pool.Exec(ctx, `
		UPDATE agent_runtime SET owner_id = $1 WHERE id = $2
	`, foreignUserID, fixture.claudeRuntimeID); err != nil {
		t.Fatalf("move candidate runtime to foreign owner: %v", err)
	}
	fixture.seedCapacity(t, "claude", 900, 200, time.Now().UTC())
	task := fixture.seedTask(t, "adaptive foreign owner")

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(true)
	svc.NotifyTaskEnqueued(ctx, task)

	deferred, err := fixture.queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("load foreign-owner decision: %v", err)
	}
	if deferred.Status != "deferred" || deferred.RouteAdmissionState != "deferred" {
		t.Fatalf("foreign runtime was not deferred: status=%q state=%q",
			deferred.Status, deferred.RouteAdmissionState)
	}
	var record adaptiveAdmissionRecord
	if err := json.Unmarshal(deferred.RouteDecision, &record); err != nil {
		t.Fatalf("decode foreign-owner decision: %v", err)
	}
	found := false
	for _, rejection := range record.ValidationRejections {
		if rejection.CandidateID == "claude-best" && rejection.Reason == "runtime_owner_mismatch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing foreign-owner rejection in %+v", record.ValidationRejections)
	}
}

func TestAdaptiveAdmissionFailsClosedOnStaleCapacity(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 100, false, false)
	fixture.seedCapacity(t, "claude", 900, 200, time.Now().UTC().Add(-adaptiveCapacityMaxAge-time.Minute))
	task := fixture.seedTask(t, "adaptive stale capacity")

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(true)
	svc.NotifyTaskEnqueued(context.Background(), task)

	deferred, err := fixture.queries.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("load stale-capacity decision: %v", err)
	}
	if deferred.Status != "deferred" || deferred.RouteAdmissionState != "deferred" {
		t.Fatalf("stale capacity did not defer: status=%q state=%q",
			deferred.Status, deferred.RouteAdmissionState)
	}
	var record adaptiveAdmissionRecord
	if err := json.Unmarshal(deferred.RouteDecision, &record); err != nil {
		t.Fatalf("decode stale-capacity decision: %v", err)
	}
	found := false
	for _, rejection := range record.Rejections {
		if rejection.CandidateID == "claude-best" && rejection.Reason == "provider_capacity_unknown" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stale-capacity rejection in %+v", record.Rejections)
	}
}

func TestAdaptiveAdmissionSweeperReevaluatesDeferredWorkGlobally(t *testing.T) {
	fixture := newAdaptiveRoutingFixture(t, 100, false, false)
	fixture.seedCapacity(t, "claude", 900, 200, time.Now().UTC().Add(-adaptiveCapacityMaxAge-time.Minute))
	task := fixture.seedTask(t, "adaptive deferred sweeper")

	svc := NewTaskService(fixture.queries, fixture.pool, nil, events.New())
	svc.FeatureFlags = adaptiveRoutingFlags(true)
	svc.NotifyTaskEnqueued(context.Background(), task)

	if _, err := fixture.pool.Exec(context.Background(), `
		UPDATE agent_task_queue SET fire_at = now() - interval '1 second' WHERE id = $1
	`, util.UUIDToString(task.ID)); err != nil {
		t.Fatalf("make deferred task due: %v", err)
	}
	fixture.seedCapacity(t, "claude", 900, 200, time.Now().UTC())
	svc.SweepPendingAdaptiveAdmissions(context.Background())

	routed, err := fixture.queries.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("load swept task: %v", err)
	}
	if routed.Status != "queued" || routed.RouteAdmissionState != "routed" ||
		util.UUIDToString(routed.RuntimeID) != fixture.claudeRuntimeID {
		t.Fatalf("global sweep did not re-admit: status=%q state=%q runtime=%s",
			routed.Status, routed.RouteAdmissionState, util.UUIDToString(routed.RuntimeID))
	}
}
