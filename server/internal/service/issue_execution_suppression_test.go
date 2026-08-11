package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func suppressedParentIssueForServiceTest(workspaceID, userID, agentID, issueID string) db.Issue {
	return db.Issue{
		ID:           util.MustParseUUID(issueID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		Metadata:     []byte(`{"workflow_role":"parent_orchestrator","execution_expected":false}`),
	}
}

// TaskService is the final fresh-task creation boundary. It must reject both
// assignee and mention enqueue paths even when a caller bypasses handler-level
// trigger computation.
func TestIntegrationTaskServiceRejectsSuppressedParentEnqueues(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET metadata = '{"workflow_role":"parent_orchestrator","execution_expected":false}'::jsonb
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue suppressed: %v", err)
	}
	issue := suppressedParentIssueForServiceTest(workspaceID, userID, agentID, issueID)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	if _, err := svc.EnqueueTaskForIssue(ctx, issue); !errors.Is(err, ErrIssueExecutionSuppressed) {
		t.Fatalf("EnqueueTaskForIssue error = %v, want ErrIssueExecutionSuppressed", err)
	}
	if _, err := svc.EnqueueTaskForMention(ctx, issue, issue.AssigneeID, pgtype.UUID{}); !errors.Is(err, ErrIssueExecutionSuppressed) {
		t.Fatalf("EnqueueTaskForMention error = %v, want ErrIssueExecutionSuppressed", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("suppressed enqueue paths created %d tasks, want 0", count)
	}
}

// Manual rerun must fail before cancelling the source task. Otherwise setting
// the metadata could stop new runs but still let a user revive the parent from
// its execution log.
func TestIntegrationRerunIssueRejectsSuppressedParentBeforeMutation(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, userID, agentID, issueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var sourceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&sourceID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET metadata = '{"workflow_role":"parent_orchestrator","execution_expected":false}'::jsonb
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue suppressed: %v", err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	_, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), sourceID, pgtype.UUID{}, util.MustParseUUID(userID), nil)
	if !errors.Is(err, ErrIssueExecutionSuppressed) {
		t.Fatalf("RerunIssue error = %v, want ErrIssueExecutionSuppressed", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, sourceID).Scan(&status); err != nil {
		t.Fatalf("read source status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("suppressed rerun mutated source status to %q, want queued", status)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("suppressed rerun left %d tasks, want only the source", count)
	}
}

func TestIntegrationTaskServiceTranslatesStaleIssueInsertionRace(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	staleIssue := suppressedParentIssueForServiceTest(workspaceID, userID, agentID, issueID)
	staleIssue.Metadata = nil
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET metadata = '{"workflow_role":"parent_orchestrator","execution_expected":false}'::jsonb
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue suppressed: %v", err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	if _, err := svc.EnqueueTaskForIssue(ctx, staleIssue); !errors.Is(err, ErrIssueExecutionSuppressed) {
		t.Fatalf("EnqueueTaskForIssue stale race error = %v, want ErrIssueExecutionSuppressed", err)
	}
	if _, err := svc.EnqueueTaskForMention(ctx, staleIssue, staleIssue.AssigneeID, pgtype.UUID{}); !errors.Is(err, ErrIssueExecutionSuppressed) {
		t.Fatalf("EnqueueTaskForMention stale race error = %v, want ErrIssueExecutionSuppressed", err)
	}
	if _, err := svc.EnqueueDeferredAssigneeFallback(
		ctx,
		staleIssue,
		staleIssue.AssigneeID,
		pgtype.UUID{},
		util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		pgtype.UUID{},
		time.Now().Add(time.Minute),
	); !errors.Is(err, ErrIssueExecutionSuppressed) {
		t.Fatalf("EnqueueDeferredAssigneeFallback stale race error = %v, want ErrIssueExecutionSuppressed", err)
	}
}

// Fresh task creation queries are public generated code and can be called
// directly. The SQL itself therefore re-checks current metadata instead of
// trusting a potentially stale db.Issue held by the service layer.
func TestIntegrationCreateTaskQueriesRejectSuppressedParent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET metadata = '{"workflow_role":"parent_orchestrator","execution_expected":false}'::jsonb
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue suppressed: %v", err)
	}

	if _, err := q.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:   util.MustParseUUID(agentID),
		RuntimeID: util.MustParseUUID(runtimeID),
		IssueID:   util.MustParseUUID(issueID),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateAgentTask error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := q.CreateDeferredChannelIssueTask(ctx, db.CreateDeferredChannelIssueTaskParams{
		AgentID:   util.MustParseUUID(agentID),
		RuntimeID: util.MustParseUUID(runtimeID),
		IssueID:   util.MustParseUUID(issueID),
		FireAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateDeferredChannelIssueTask error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := q.CreateDeferredAgentTask(ctx, db.CreateDeferredAgentTaskParams{
		AgentID:             util.MustParseUUID(agentID),
		RuntimeID:           util.MustParseUUID(runtimeID),
		IssueID:             util.MustParseUUID(issueID),
		EscalationForTaskID: util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		FireAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateDeferredAgentTask error = %v, want pgx.ErrNoRows", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("suppressed create queries created %d tasks, want 0", count)
	}
}

// Tasks admitted before suppression must become inert at every remaining queue
// boundary. Separate issues avoid the queued/dispatched partial unique index and
// let each status transition be asserted independently.
func TestIntegrationSuppressedQueuedTasksAreNotPromotedClaimedStartedOrReclaimed(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, queuedIssueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	insertIssue := func(title string) string {
		t.Helper()
		var issueID string
		if err := pool.QueryRow(ctx, `
			WITH next_number AS (
				UPDATE workspace
				SET issue_counter = GREATEST(
					issue_counter,
					(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
				) + 1
				WHERE id = $1
				RETURNING issue_counter
			)
			INSERT INTO issue (
				workspace_id, title, creator_type, creator_id,
				assignee_type, assignee_id, priority, number
			)
			SELECT $1, $2, 'member', $3, 'agent', $4, 'medium', issue_counter
			FROM next_number
			RETURNING id
		`, workspaceID, title, userID, agentID).Scan(&issueID); err != nil {
			t.Fatalf("insert %s issue: %v", title, err)
		}
		return issueID
	}
	deferredIssueID := insertIssue("suppressed deferred issue")
	channelDeferredIssueID := insertIssue("suppressed channel deferred issue")
	staleIssueID := insertIssue("suppressed stale issue")
	startedIssueID := insertIssue("suppressed start issue")

	var queuedID, deferredID, channelDeferredID, staleID, startedID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, queuedIssueID).Scan(&queuedID); err != nil {
		t.Fatalf("insert queued task: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, fire_at,
			escalation_for_task_id
		)
		VALUES ($1, $2, $3, 'deferred', 0, now() - interval '1 minute', $4)
		RETURNING id
	`, agentID, runtimeID, deferredIssueID, queuedID).Scan(&deferredID); err != nil {
		t.Fatalf("insert deferred task: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, fire_at, context
		)
		VALUES (
			$1, $2, $3, 'deferred', 0, now() + interval '1 minute',
			'{"channel_issue_media_pending":true}'::jsonb
		)
		RETURNING id
	`, agentID, runtimeID, channelDeferredIssueID).Scan(&channelDeferredID); err != nil {
		t.Fatalf("insert channel deferred task: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at,
			prepare_lease_expires_at
		)
		VALUES (
			$1, $2, $3, 'dispatched', 0, now() - interval '10 minutes',
			now() - interval '5 minutes'
		)
		RETURNING id
	`, agentID, runtimeID, staleIssueID).Scan(&staleID); err != nil {
		t.Fatalf("insert stale dispatched task: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at,
			prepare_lease_expires_at
		)
		VALUES ($1, $2, $3, 'dispatched', 0, now(), now() + interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, startedIssueID).Scan(&startedID); err != nil {
		t.Fatalf("insert dispatched task for start: %v", err)
	}

	issueIDs := []string{
		queuedIssueID,
		deferredIssueID,
		channelDeferredIssueID,
		staleIssueID,
		startedIssueID,
	}
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET metadata = '{"workflow_role":"parent_orchestrator","execution_expected":false}'::jsonb
		WHERE id = ANY($1::uuid[])
	`, issueIDs); err != nil {
		t.Fatalf("mark issues suppressed: %v", err)
	}

	if promoted, err := q.PromoteDueDeferredTasksForRuntime(ctx, util.MustParseUUID(runtimeID)); err != nil {
		t.Fatalf("PromoteDueDeferredTasksForRuntime: %v", err)
	} else if len(promoted) != 0 {
		t.Fatalf("promoted suppressed tasks = %+v, want none", promoted)
	}
	if promoted, err := q.PromoteDueDeferredTasksForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(runtimeID)}); err != nil {
		t.Fatalf("PromoteDueDeferredTasksForRuntimes: %v", err)
	} else if len(promoted) != 0 {
		t.Fatalf("batch promoted suppressed tasks = %+v, want none", promoted)
	}
	if _, err := q.PromoteDeferredChannelIssueTask(ctx, channelDeferredID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("PromoteDeferredChannelIssueTask error = %v, want pgx.ErrNoRows", err)
	}
	if candidates, err := q.ListQueuedClaimCandidatesByRuntime(ctx, util.MustParseUUID(runtimeID)); err != nil {
		t.Fatalf("ListQueuedClaimCandidatesByRuntime: %v", err)
	} else if len(candidates) != 0 {
		t.Fatalf("listed suppressed queued candidates = %+v, want none", candidates)
	}
	if candidates, err := q.ListQueuedClaimCandidatesByRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(runtimeID)}); err != nil {
		t.Fatalf("ListQueuedClaimCandidatesByRuntimes: %v", err)
	} else if len(candidates) != 0 {
		t.Fatalf("batch listed suppressed queued candidates = %+v, want none", candidates)
	}
	if _, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
		AgentID:          util.MustParseUUID(agentID),
		PrepareLeaseSecs: 30,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ClaimAgentTask error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := q.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
		RuntimeID:         util.MustParseUUID(runtimeID),
		ClaimRecoverySecs: 30,
		PrepareLeaseSecs:  30,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ReclaimStaleDispatchedTaskForRuntime error = %v, want pgx.ErrNoRows", err)
	}
	if reclaimed, err := q.ReclaimStaleDispatchedTasksForRuntimes(ctx, db.ReclaimStaleDispatchedTasksForRuntimesParams{
		RuntimeIds:        []pgtype.UUID{util.MustParseUUID(runtimeID)},
		ClaimRecoverySecs: 30,
		PrepareLeaseSecs:  30,
		MaxTasks:          10,
	}); err != nil {
		t.Fatalf("ReclaimStaleDispatchedTasksForRuntimes: %v", err)
	} else if len(reclaimed) != 0 {
		t.Fatalf("batch reclaimed suppressed tasks = %+v, want none", reclaimed)
	}
	if _, err := q.StartAgentTask(ctx, startedID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("StartAgentTask error = %v, want pgx.ErrNoRows", err)
	}

	for _, tc := range []struct {
		id   pgtype.UUID
		want string
	}{
		{id: queuedID, want: "queued"},
		{id: deferredID, want: "deferred"},
		{id: channelDeferredID, want: "deferred"},
		{id: staleID, want: "dispatched"},
		{id: startedID, want: "dispatched"},
	} {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, tc.id).Scan(&status); err != nil {
			t.Fatalf("read task status: %v", err)
		}
		if status != tc.want {
			t.Fatalf("task %s status = %q, want %q", util.UUIDToString(tc.id), status, tc.want)
		}
	}
}

// CreateRetryTask is public generated query code and has historically been
// called directly by retry paths and tests. The SQL itself therefore carries a
// defense-in-depth metadata gate, not only the TaskService eligibility check.
func TestIntegrationCreateRetryTaskRejectsSuppressedParent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var sourceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, failure_reason, attempt, max_attempts)
		VALUES ($1, $2, $3, 'failed', 0, 'timeout', 1, 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&sourceID); err != nil {
		t.Fatalf("insert failed source task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET metadata = '{"workflow_role":"parent_orchestrator","execution_expected":false}'::jsonb
		WHERE id = $1
	`, issueID); err != nil {
		t.Fatalf("mark issue suppressed: %v", err)
	}

	if _, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: sourceID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CreateRetryTask error = %v, want pgx.ErrNoRows", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, sourceID).Scan(&count); err != nil {
		t.Fatalf("count retry children: %v", err)
	}
	if count != 0 {
		t.Fatalf("suppressed retry query created %d children, want 0", count)
	}
}
