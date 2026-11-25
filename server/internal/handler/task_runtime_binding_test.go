package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Hold an enqueue after its application-level binding read but before SQL.
// The database must resolve and lock the current binding when the write lands.
type mul9393EnqueueGate struct {
	db.DBTX
	entered chan struct{}
	resume  chan struct{}
}

func (g *mul9393EnqueueGate) hold(ctx context.Context, query string) {
	if strings.HasPrefix(query, "-- name: CreateAgentTask :one") {
		close(g.entered)
		select {
		case <-g.resume:
		case <-ctx.Done():
		}
	}
}

func (g *mul9393EnqueueGate) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	g.hold(ctx, query)
	return g.DBTX.QueryRow(ctx, query, args...)
}

// Begin gates inserts that run inside the service's own transaction too, such
// as an ordinary enqueue that consumes a blocker release with its insert.
func (g *mul9393EnqueueGate) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := testPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &mul9393GatedTx{Tx: tx, gate: g}, nil
}

type mul9393GatedTx struct {
	pgx.Tx
	gate *mul9393EnqueueGate
}

func (t *mul9393GatedTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	t.gate.hold(ctx, query)
	return t.Tx.QueryRow(ctx, query, args...)
}

func TestEnqueueAfterRuntimeSwitchUsesCurrentBinding(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run("claude_to_"+provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			oldRuntime := dbfx.Runtime(t, "mul9393-enqueue-A", testutil.Cols{"provider": "claude"})
			newRuntime := dbfx.Runtime(t, "mul9393-enqueue-B", testutil.Cols{"provider": provider})
			agentID := dbfx.Agent(t, "mul9393-enqueue-agent", oldRuntime)
			issueID := dbfx.Issue(t, "enqueue concurrent with rebind", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id=$1`, agentID)
			gate := &mul9393EnqueueGate{DBTX: testPool, entered: make(chan struct{}), resume: make(chan struct{})}
			svc := service.NewTaskService(db.New(gate), gate, nil, events.New())
			type outcome struct {
				task db.AgentTaskQueue
				err  error
			}
			result := make(chan outcome, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				task, err := svc.EnqueueTaskForIssueByActor(ctx, issue, parseUUID(testUserID))
				result <- outcome{task, err}
			}()
			defer func() { cancel(); <-done }()
			select {
			case <-gate.entered:
			case got := <-result:
				t.Fatalf("enqueue returned before insert barrier: %v", got.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Enqueue has loaded A and built its insert arguments. Commit the
			// real UpdateAgent request before letting that insert acquire locks.
			w := patchAgentRuntime(t, agentID, newRuntime)
			if w.Code != http.StatusOK {
				t.Fatalf("rebind: %d %s", w.Code, w.Body.String())
			}
			close(gate.resume)
			got := <-result
			if got.err != nil {
				t.Fatalf("enqueue: %v", got.err)
			}
			// Use the ordinary service after the gated enqueue has completed.
			_, duplicateErr := testHandler.TaskService.EnqueueTaskForIssueByActor(ctx, issue, parseUUID(testUserID))
			if !errors.Is(duplicateErr, service.ErrDuplicatePendingTask) {
				t.Fatalf("second enqueue: %v, want pending-task dedup", duplicateErr)
			}
			oldClaim, oldErr := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(oldRuntime))
			newClaim, newErr := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(newRuntime))
			var binding, taskRuntime, status string
			dbfx.QueryRow(t, `SELECT a.runtime_id::text, q.runtime_id::text, q.status FROM agent a JOIN agent_task_queue q ON q.agent_id=a.id WHERE q.id=$1`, got.task.ID).Scan(&binding, &taskRuntime, &status)
			t.Logf("agent_runtime=%s B=%s task_runtime=%s A=%s status=%s old_claim=%v/%v new_claim=%v/%v duplicate=%v", binding, newRuntime, taskRuntime, oldRuntime, status, oldClaim != nil, oldErr, newClaim != nil, newErr, duplicateErr)
			if oldErr != nil || newErr != nil || oldClaim != nil {
				t.Fatalf("claim errors or stale-runtime claim: old=%v/%v new=%v/%v", oldClaim, oldErr, newClaim, newErr)
			}
			if newClaim == nil {
				t.Errorf("successful enqueue after committed rebind is stranded; B should claim it")
			}
		})
	}
}

func TestRequeueClaimAfterRuntimeSwitch(t *testing.T) {
	ctx := context.Background()
	oldRuntime := dbfx.Runtime(t, "mul9393-requeue-A")
	newRuntime := dbfx.Runtime(t, "mul9393-requeue-B")
	agentID := dbfx.Agent(t, "mul9393-requeue-agent", oldRuntime)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": oldRuntime, "issue_id": dbfx.Issue(t, "claim failure after rebind"),
		"priority": 4, "attempt": 2, "max_attempts": 3,
		"session_id": "prior-session", "work_dir": "/prior/workdir",
	})
	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(oldRuntime))
	if err != nil || claimed == nil || uuidToString(claimed.ID) != taskID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	w := patchAgentRuntime(t, agentID, newRuntime)
	if w.Code != http.StatusOK {
		t.Fatalf("rebind: %d %s", w.Code, w.Body.String())
	}
	// This recovery is used when token/receipt finalization fails before
	// the claim response is delivered. It puts the same generation back.
	requeued, err := testHandler.TaskService.RequeueTaskAfterClaimFailure(ctx, *claimed)
	if err != nil || requeued == nil {
		t.Fatalf("requeue: %+v %v", requeued, err)
	}
	if requeued.ID != claimed.ID || requeued.Priority != claimed.Priority ||
		requeued.Attempt != claimed.Attempt || requeued.MaxAttempts != claimed.MaxAttempts ||
		requeued.CreatedAt != claimed.CreatedAt || requeued.SessionID != claimed.SessionID ||
		requeued.WorkDir != claimed.WorkDir {
		t.Fatal("requeue changed task identity, ordering, retry budget, or continuity pointers")
	}
	oldClaim, oldErr := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(oldRuntime))
	newClaim, newErr := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(newRuntime))
	t.Logf("requeued_status=%s requeued_runtime=%s A=%s B=%s old_claim=%v/%v new_claim=%v/%v", requeued.Status, uuidToString(requeued.RuntimeID), oldRuntime, newRuntime, oldClaim != nil, oldErr, newClaim != nil, newErr)
	if oldErr != nil || newErr != nil || oldClaim != nil {
		t.Fatal("claim error or stale-runtime claim")
	}
	if newClaim == nil {
		t.Fatal("undelivered dispatch requeued after rebind is stranded on A")
	}
	testutil.Call(t, testHandler.StartTask, startClaimRequest(taskID, oldRuntime, claimed.DispatchedAt.Time)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(taskID, newRuntime, newClaim.DispatchedAt.Time)).Want(http.StatusOK)
}

func TestConcurrentClaimsAfterRuntimeSwitch(t *testing.T) {
	ctx := context.Background()
	oldRuntime := dbfx.Runtime(t, "mul9393-claim-A")
	newRuntime := dbfx.Runtime(t, "mul9393-claim-B")
	agentID := dbfx.Agent(t, "mul9393-claim-agent", oldRuntime)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": oldRuntime, "issue_id": dbfx.Issue(t, "concurrent claim after rebind")})
	w := patchAgentRuntime(t, agentID, newRuntime)
	if w.Code != http.StatusOK {
		t.Fatalf("rebind: %d %s", w.Code, w.Body.String())
	}
	type outcome struct {
		runtime string
		task    *db.AgentTaskQueue
		err     error
	}
	start := make(chan struct{})
	results := make(chan outcome, 8)
	for i := 0; i < 8; i++ {
		runtime := oldRuntime
		if i%2 == 0 {
			runtime = newRuntime
		}
		go func(runtime string) {
			<-start
			task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtime))
			results <- outcome{runtime, task, err}
		}(runtime)
	}
	close(start)
	claims := 0
	for i := 0; i < 8; i++ {
		got := <-results
		if got.err != nil {
			t.Errorf("claim: %v", got.err)
		}
		if got.task != nil {
			claims++
			if got.runtime != newRuntime || uuidToString(got.task.ID) != taskID {
				t.Errorf("wrong owner or task: %+v", got)
			}
		}
	}
	if claims != 1 {
		t.Errorf("claims=%d, want exactly 1 on B", claims)
	}
	t.Logf("simultaneous polls=8 total_claims=%d; original queued task migrated to B", claims)
}
