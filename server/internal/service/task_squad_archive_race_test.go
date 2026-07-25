package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression for the child-done squad routing race: archival can commit after
// the handler's active-squad check but before task creation. Task creation must
// serialize with archival so an archive that wins the row lock also wins the
// enqueue decision.
func TestCreateAgentTaskWithSquadGuard_ConcurrentArchiveWins(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	suffix := time.Now().UnixNano()
	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		SELECT workspace_id, $2, id, owner_id
		FROM agent
		WHERE id = $1
		RETURNING id
	`, util.UUIDToString(fx.agentID), fmt.Sprintf("archive-race-%d", suffix)).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	archiveTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin archive transaction: %v", err)
	}
	defer archiveTx.Rollback(ctx)
	if _, err := archiveTx.Exec(ctx, `
		UPDATE squad SET archived_at = now(), updated_at = now() WHERE id = $1
	`, squadID); err != nil {
		t.Fatalf("archive squad: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, _, err := svc.createAgentTaskWithSquadGuard(ctx, db.CreateAgentTaskParams{
			AgentID:   fx.agentID,
			RuntimeID: fx.runtimeID,
			IssueID:   fx.issueID,
			Priority:  0,
			SquadID:   util.MustParseUUID(squadID),
			IsLeaderTask: pgtype.Bool{
				Bool:  true,
				Valid: true,
			},
		}, taskCreateGuards{})
		result <- err
	}()
	<-started

	select {
	case err := <-result:
		t.Fatalf("enqueue returned before the archive transaction committed: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: the active-squad guard is waiting on the archive row lock.
	}

	if err := archiveTx.Commit(ctx); err != nil {
		t.Fatalf("commit archive transaction: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, ErrSquadUnavailableForTask) {
			t.Fatalf("expected archived-squad error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue stayed blocked after archive commit")
	}

	var taskCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue WHERE squad_id = $1
	`, squadID).Scan(&taskCount); err != nil {
		t.Fatalf("count squad tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no task for archived squad, got %d", taskCount)
	}
}

// Generic squad dispatch only owns archive admission. It must not start
// rejecting tasks merely because the leader changed while the caller was
// already enqueueing; child-done has a dedicated strict+retry path.
func TestCreateAgentTaskWithSquadGuard_GenericDispatchSurvivesLeaderRotation(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	var ownerID, workspaceID string
	if err := pool.QueryRow(ctx, `
		SELECT owner_id::text, workspace_id::text FROM agent WHERE id = $1
	`, util.UUIDToString(fx.agentID)).Scan(&ownerID, &workspaceID); err != nil {
		t.Fatalf("load fixture agent scope: %v", err)
	}

	var replacementLeaderID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("rotation-winner-%d", time.Now().UnixNano()), util.UUIDToString(fx.runtimeID), ownerID).Scan(&replacementLeaderID); err != nil {
		t.Fatalf("create replacement leader: %v", err)
	}

	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("leader-rotation-race-%d", time.Now().UnixNano()), util.UUIDToString(fx.agentID), ownerID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE agent_id = $1`, replacementLeaderID)
		pool.Exec(c, `DELETE FROM squad WHERE id = $1`, squadID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, replacementLeaderID)
	})

	rotationTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin leader rotation: %v", err)
	}
	defer rotationTx.Rollback(ctx)
	if _, err := rotationTx.Exec(ctx, `
		UPDATE squad SET leader_id = $2, updated_at = now() WHERE id = $1
	`, squadID, replacementLeaderID); err != nil {
		t.Fatalf("rotate squad leader: %v", err)
	}

	issue, err := db.New(pool).GetIssue(ctx, fx.issueID)
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := svc.EnqueueTaskForSquadLeader(
			ctx,
			issue,
			fx.agentID,
			util.MustParseUUID(squadID),
			pgtype.UUID{},
		)
		result <- err
	}()
	<-started

	select {
	case err := <-result:
		t.Fatalf("enqueue returned before the leader rotation committed: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: the squad guard is waiting on the leader rotation row lock.
	}

	if err := rotationTx.Commit(ctx); err != nil {
		t.Fatalf("commit leader rotation: %v", err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("generic squad enqueue failed after leader rotation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue stayed blocked after leader rotation commit")
	}

	var staleLeaderTasks int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND squad_id = $3
	`, util.UUIDToString(fx.issueID), util.UUIDToString(fx.agentID), squadID).Scan(&staleLeaderTasks); err != nil {
		t.Fatalf("count stale leader tasks: %v", err)
	}
	if staleLeaderTasks != 1 {
		t.Fatalf("generic dispatch created %d tasks for its resolved leader, want 1", staleLeaderTasks)
	}
}

// A parent assignment that commits before an origin continuation is inserted
// must win over the unassigned-parent fallback. The parent lock makes the
// precedence decision linearizable across server replicas.
func TestOriginContinuationGuard_ConcurrentParentAssignmentWins(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	var ownerID, workspaceID string
	if err := pool.QueryRow(ctx, `
		SELECT owner_id::text, workspace_id::text FROM agent WHERE id = $1
	`, util.UUIDToString(fx.agentID)).Scan(&ownerID, &workspaceID); err != nil {
		t.Fatalf("load fixture agent scope: %v", err)
	}

	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("parent-assignment-race-%d", time.Now().UnixNano()), util.UUIDToString(fx.agentID), ownerID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}

	var originTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task,
			squad_id, originator_user_id, accountable_user_id, originator_source
		)
		VALUES ($1, $2, $3, 'completed', 0, true, $4, $5, $5, 'direct_human')
		RETURNING id
	`, util.UUIDToString(fx.agentID), util.UUIDToString(fx.runtimeID), util.UUIDToString(fx.issueID), squadID, ownerID).Scan(&originTaskID); err != nil {
		t.Fatalf("create origin task: %v", err)
	}
	originTask, err := db.New(pool).GetAgentTask(ctx, util.MustParseUUID(originTaskID))
	if err != nil {
		t.Fatalf("load origin task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE squad_id = $1`, squadID)
		pool.Exec(c, `DELETE FROM squad WHERE id = $1`, squadID)
	})

	issue, err := db.New(pool).GetIssue(ctx, fx.issueID)
	if err != nil {
		t.Fatalf("load unassigned parent: %v", err)
	}
	assignmentTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin parent assignment: %v", err)
	}
	defer assignmentTx.Rollback(ctx)
	if _, err := assignmentTx.Exec(ctx, `
		UPDATE issue
		SET assignee_type = 'agent', assignee_id = $2, updated_at = now()
		WHERE id = $1
	`, util.UUIDToString(fx.issueID), util.UUIDToString(fx.agentID)); err != nil {
		t.Fatalf("assign parent: %v", err)
	}

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := svc.EnqueueTaskForSquadLeaderFromOriginTask(
			ctx,
			issue,
			fx.agentID,
			util.MustParseUUID(squadID),
			pgtype.UUID{},
			originTask,
		)
		result <- err
	}()
	<-started

	select {
	case err := <-result:
		t.Fatalf("continuation returned before the parent assignment committed: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: the continuation is waiting on the parent assignment lock.
	}

	if err := assignmentTx.Commit(ctx); err != nil {
		t.Fatalf("commit parent assignment: %v", err)
	}

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("origin continuation succeeded after explicit parent assignment committed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("continuation stayed blocked after parent assignment commit")
	}

	var continuationTasks int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND squad_id = $2 AND id <> $3
	`, util.UUIDToString(fx.issueID), squadID, originTaskID).Scan(&continuationTasks); err != nil {
		t.Fatalf("count continuation tasks: %v", err)
	}
	if continuationTasks != 0 {
		t.Fatalf("explicitly assigned parent received %d origin continuation tasks, want 0", continuationTasks)
	}
}

// Rerunning a historical leader task for an archived squad must fail without
// cancelling another active task for the same agent and issue. Validation,
// cancellation, and replacement creation form one atomic operation.
func TestRerunIssue_ArchivedSquadDoesNotCancelExistingTask(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	var ownerID, workspaceID string
	if err := pool.QueryRow(ctx, `
		SELECT owner_id::text, workspace_id::text FROM agent WHERE id = $1
	`, util.UUIDToString(fx.agentID)).Scan(&ownerID, &workspaceID); err != nil {
		t.Fatalf("load fixture agent scope: %v", err)
	}

	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id, archived_at, archived_by)
		VALUES ($1, $2, $3, $4, now(), $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("archived-rerun-%d", time.Now().UnixNano()), util.UUIDToString(fx.agentID), ownerID).Scan(&squadID); err != nil {
		t.Fatalf("create archived squad: %v", err)
	}

	var sourceTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task,
			squad_id, completed_at
		)
		VALUES ($1, $2, $3, 'failed', 0, true, $4, now())
		RETURNING id
	`, util.UUIDToString(fx.agentID), util.UUIDToString(fx.runtimeID), util.UUIDToString(fx.issueID), squadID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create historical leader task: %v", err)
	}

	var activeTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, util.UUIDToString(fx.agentID), util.UUIDToString(fx.runtimeID), util.UUIDToString(fx.issueID)).Scan(&activeTaskID); err != nil {
		t.Fatalf("create active task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE squad_id = $1 OR id = $2`, squadID, activeTaskID)
		pool.Exec(c, `DELETE FROM squad WHERE id = $1`, squadID)
	})

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	if _, err := svc.RerunIssue(
		ctx,
		fx.issueID,
		util.MustParseUUID(sourceTaskID),
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	); err == nil {
		t.Fatal("rerun unexpectedly accepted an archived historical squad")
	}

	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM agent_task_queue WHERE id = $1
	`, activeTaskID).Scan(&status); err != nil {
		t.Fatalf("load pre-existing active task: %v", err)
	}
	if status != "queued" {
		t.Fatalf("pre-existing task status = %q, want queued after failed rerun", status)
	}
}

// The transactional squad rerun path must still replace active work when the
// squad is valid, including historical worker tasks that are not the current
// leader. This covers the successful half of the atomic operation whose
// rollback behavior is asserted above without turning the leader check into a
// blanket squad-member rejection.
func TestRerunIssue_ActiveSquadWorkerReplacesExistingTaskAtomically(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	var ownerID, workspaceID string
	if err := pool.QueryRow(ctx, `
		SELECT owner_id::text, workspace_id::text FROM agent WHERE id = $1
	`, util.UUIDToString(fx.agentID)).Scan(&ownerID, &workspaceID); err != nil {
		t.Fatalf("load fixture agent scope: %v", err)
	}

	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("active-rerun-%d", time.Now().UnixNano()), util.UUIDToString(fx.agentID), ownerID).Scan(&squadID); err != nil {
		t.Fatalf("create active squad: %v", err)
	}

	var sourceTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task,
			squad_id, completed_at
		)
		VALUES ($1, $2, $3, 'failed', 0, false, $4, now())
		RETURNING id
	`, util.UUIDToString(fx.agentID), util.UUIDToString(fx.runtimeID), util.UUIDToString(fx.issueID), squadID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create historical worker task: %v", err)
	}

	var activeTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, util.UUIDToString(fx.agentID), util.UUIDToString(fx.runtimeID), util.UUIDToString(fx.issueID)).Scan(&activeTaskID); err != nil {
		t.Fatalf("create active task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE squad_id = $1 OR id = $2`, squadID, activeTaskID)
		pool.Exec(c, `DELETE FROM squad WHERE id = $1`, squadID)
	})

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	replacement, err := svc.RerunIssue(
		ctx,
		fx.issueID,
		util.MustParseUUID(sourceTaskID),
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("rerun active squad task: %v", err)
	}
	if replacement == nil {
		t.Fatal("rerun returned no replacement task")
	}
	if replacement.Status != "queued" {
		t.Fatalf("replacement status = %q, want queued", replacement.Status)
	}
	if util.UUIDToString(replacement.SquadID) != squadID {
		t.Fatalf("replacement squad = %q, want %q", util.UUIDToString(replacement.SquadID), squadID)
	}
	if util.UUIDToString(replacement.RerunOfTaskID) != sourceTaskID {
		t.Fatalf("replacement rerun_of_task_id = %q, want %q", util.UUIDToString(replacement.RerunOfTaskID), sourceTaskID)
	}
	if replacement.IsLeaderTask {
		t.Fatal("worker rerun unexpectedly became a leader task")
	}

	var oldStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM agent_task_queue WHERE id = $1
	`, activeTaskID).Scan(&oldStatus); err != nil {
		t.Fatalf("load replaced active task: %v", err)
	}
	if oldStatus != "cancelled" {
		t.Fatalf("replaced task status = %q, want cancelled", oldStatus)
	}
}

// A manual rerun targets the historical source agent and role. Leader rotation
// must not silently change that established per-row retry contract; the
// current-leader guard belongs to fresh leader dispatch, not historical rerun.
func TestRerunIssue_HistoricalLeaderSurvivesSquadLeaderRotation(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	fx := createHeadShaDedupFixture(t, ctx, pool, "", "")

	var ownerID, workspaceID string
	if err := pool.QueryRow(ctx, `
		SELECT owner_id::text, workspace_id::text FROM agent WHERE id = $1
	`, util.UUIDToString(fx.agentID)).Scan(&ownerID, &workspaceID); err != nil {
		t.Fatalf("load fixture agent scope: %v", err)
	}

	var replacementLeaderID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("historical-rerun-replacement-%d", time.Now().UnixNano()), util.UUIDToString(fx.runtimeID), ownerID).Scan(&replacementLeaderID); err != nil {
		t.Fatalf("create replacement leader: %v", err)
	}

	var squadID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("historical-rerun-%d", time.Now().UnixNano()), replacementLeaderID, ownerID).Scan(&squadID); err != nil {
		t.Fatalf("create rotated squad: %v", err)
	}

	var sourceTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, is_leader_task,
			squad_id, completed_at
		)
		VALUES ($1, $2, $3, 'failed', 0, true, $4, now())
		RETURNING id
	`, util.UUIDToString(fx.agentID), util.UUIDToString(fx.runtimeID), util.UUIDToString(fx.issueID), squadID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("create historical leader task: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE squad_id = $1`, squadID)
		pool.Exec(c, `DELETE FROM squad WHERE id = $1`, squadID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, replacementLeaderID)
	})

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	replacement, err := svc.RerunIssue(
		ctx,
		fx.issueID,
		util.MustParseUUID(sourceTaskID),
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("rerun historical leader after rotation: %v", err)
	}
	if replacement == nil {
		t.Fatal("rerun returned no replacement task")
	}
	if util.UUIDToString(replacement.AgentID) != util.UUIDToString(fx.agentID) {
		t.Fatalf("replacement agent = %q, want historical agent %q", util.UUIDToString(replacement.AgentID), util.UUIDToString(fx.agentID))
	}
	if !replacement.IsLeaderTask {
		t.Fatal("historical leader rerun lost its leader role")
	}
}
