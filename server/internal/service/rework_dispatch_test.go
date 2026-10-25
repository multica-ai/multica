package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func reworkRequest(agentID, key, instruction string) ReworkDispatchRequest {
	return ReworkDispatchRequest{OperationKey: key, Instruction: instruction, PayloadSHA256: reworkPayloadSHA256(instruction), ExpectedAgentID: util.MustParseUUID(agentID)}
}

func reworkSourceTask(t *testing.T, pool *pgxpool.Pool, agentID, runtimeID, issueID string) pgtype.UUID {
	t.Helper()
	return seedRerunTask(t, pool, agentID, runtimeID, issueID, "completed")
}

func reworkWorkspaceID(t *testing.T, svc *TaskService, issueID string) pgtype.UUID {
	t.Helper()
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	return issue.WorkspaceID
}

func TestDispatchReworkConcurrentReplayAndTerminal(t *testing.T) {
	svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	request := reworkRequest(agentID, "correction-v1", "Use the reviewed correction package v1.")
	request.SourceTaskID = reworkSourceTask(t, pool, agentID, runtimeID, issueID)
	request.ActorUserID = util.MustParseUUID(creatorID)
	workspaceID := reworkWorkspaceID(t, svc, issueID)
	results := make(chan ReworkDispatchResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			r, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, request, nil)
			results <- r
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	var ids []string
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent dispatch: %v", err)
		}
	}
	for result := range results {
		ids = append(ids, util.UUIDToString(result.Task.ID))
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("same key created different runs: %v", ids)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=$1`, ids[0])
		pool.Exec(context.Background(), `DELETE FROM issue_rework_dispatch_receipt WHERE issue_id=$1`, issueID)
	})
	if _, err := pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, request, nil)
	if err != nil || !replay.Replayed || util.UUIDToString(replay.Task.ID) != ids[0] || replay.Task.Status != "completed" {
		t.Fatalf("terminal replay = %#v, %v", replay, err)
	}
}

// TestDispatchReworkConcurrentReplayWithBoundedPool catches pool reads hidden
// inside an open transaction. Two transactions consume this pool's only
// connections; all reads used by dispatch must therefore use q, not s.Queries.
func TestDispatchReworkConcurrentReplayWithBoundedPool(t *testing.T) {
	_, fixturePool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	request := reworkRequest(agentID, "bounded-pool-correction", "Keep the source work directory.")
	request.SourceTaskID = reworkSourceTask(t, fixturePool, agentID, runtimeID, issueID)
	request.ActorUserID = util.MustParseUUID(creatorID)

	config, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("parse test DATABASE_URL: %v", err)
	}
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("open bounded pool: %v", err)
	}
	t.Cleanup(pool.Close)
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	workspaceID := reworkWorkspaceID(t, svc, issueID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := make(chan ReworkDispatchResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			result, err := svc.DispatchRework(ctx, util.MustParseUUID(issueID), workspaceID, request, nil)
			results <- result
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)

	var taskIDs []string
	for err := range errs {
		if err != nil {
			t.Fatalf("bounded-pool dispatch: %v", err)
		}
	}
	for result := range results {
		taskIDs = append(taskIDs, util.UUIDToString(result.Task.ID))
	}
	if len(taskIDs) != 2 || taskIDs[0] == "" || taskIDs[0] != taskIDs[1] {
		t.Fatalf("bounded-pool replay task ids = %v", taskIDs)
	}
	t.Cleanup(func() {
		fixturePool.Exec(context.Background(), `DELETE FROM issue_rework_dispatch_receipt WHERE issue_id = $1`, issueID)
		fixturePool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskIDs[0])
	})
}

func TestDispatchReworkRejectsMismatchAndActiveWork(t *testing.T) {
	svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	request := reworkRequest(agentID, "correction-v1", "package v1")
	request.SourceTaskID = reworkSourceTask(t, pool, agentID, runtimeID, issueID)
	request.ActorUserID = util.MustParseUUID(creatorID)
	workspaceID := reworkWorkspaceID(t, svc, issueID)
	first, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM issue_rework_dispatch_receipt WHERE issue_id=$1`, issueID)
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
	})
	changed := request
	changed.Instruction = "package v2"
	changed.PayloadSHA256 = reworkPayloadSHA256(changed.Instruction)
	if _, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, changed, nil); !errors.Is(err, ErrReworkDispatchMismatch) {
		t.Fatalf("payload mismatch: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, first.Task.ID); err != nil {
		t.Fatal(err)
	}
	active := seedRerunTask(t, pool, agentID, runtimeID, issueID, "running")
	activeRequest := reworkRequest(agentID, "correction-v2", "new package")
	activeRequest.SourceTaskID = request.SourceTaskID
	if _, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, activeRequest, nil); !errors.Is(err, ErrReworkDispatchConflict) {
		t.Fatalf("active conflict: %v", err)
	}
	if got := taskStatus(t, pool, active); got != "running" {
		t.Fatalf("active task mutated to %s", got)
	}
}

func TestDispatchReworkRejectsBadDigestWithoutWriting(t *testing.T) {
	svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	workspaceID := reworkWorkspaceID(t, svc, issueID)
	req := reworkRequest(agentID, "bad-digest", "frozen")
	req.SourceTaskID = reworkSourceTask(t, pool, agentID, runtimeID, issueID)
	req.ActorUserID = util.MustParseUUID(creatorID)
	req.PayloadSHA256 = "not-a-digest"
	if _, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, req, nil); err == nil {
		t.Fatal("bad digest accepted")
	}
	var tasks, receipts int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND id <> $2`, issueID, req.SourceTaskID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM issue_rework_dispatch_receipt WHERE issue_id=$1`, issueID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || receipts != 0 {
		t.Fatalf("bad digest wrote tasks=%d receipts=%d", tasks, receipts)
	}
}

func TestDispatchReworkMissingRecordedTaskFailsClosed(t *testing.T) {
	svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
	workspaceID := reworkWorkspaceID(t, svc, issueID)
	req := reworkRequest(agentID, "missing-task", "frozen")
	req.SourceTaskID = reworkSourceTask(t, pool, agentID, runtimeID, issueID)
	req.ActorUserID = util.MustParseUUID(creatorID)
	created, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=$1`, created.Task.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM issue_rework_dispatch_receipt WHERE issue_id=$1`, issueID)
	})
	if _, err := svc.DispatchRework(context.Background(), util.MustParseUUID(issueID), workspaceID, req, nil); !errors.Is(err, ErrReworkDispatchMissingTask) {
		t.Fatalf("missing task replay: %v", err)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND id <> $2`, issueID, req.SourceTaskID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("missing receipt replay created %d tasks", n)
	}
}

func TestReworkIssueLockBlocksOrdinaryInsert(t *testing.T) {
	svc, pool, _, agentID, issueID, runtimeID := rerunQueueFixture(t)
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Queries.WithTx(tx).LockIssueForReworkDispatch(context.Background(), db.LockIssueForReworkDispatchParams{ID: util.MustParseUUID(issueID), WorkspaceID: issue.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	writer, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	if _, err := writer.Exec(context.Background(), "SET LOCAL lock_timeout = '50ms'"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Queries.WithTx(writer).CreateAgentTask(context.Background(), db.CreateAgentTaskParams{AgentID: util.MustParseUUID(agentID), RuntimeID: util.MustParseUUID(runtimeID), IssueID: util.MustParseUUID(issueID), Priority: 0})
	if err == nil {
		t.Fatal("ordinary CreateAgentTask bypassed rework issue lock")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("insert error = %v, want lock_not_available", err)
	}
}
