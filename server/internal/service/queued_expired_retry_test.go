package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// queued_expired retry (CODI-6 / GH #7795): a task that expired in the queue is
// a runtime availability signal, not a defect, so it joins retryableReasons.
// These tests lock in the MVP contract: the retry child is created as an
// immediate 'queued' attempt (no fire_at backoff), the failure event reports
// retry_pending=true and leaves the issue in_progress while a retry is coming,
// and once the attempt budget is spent (or max_attempts<=1 disables retry) no
// child is created, the event reports retry_pending=false, and the stuck
// in_progress issue is reset to todo. See retryableReasons in task.go for why no
// exponential backoff is added.

// queuedExpiredFixture holds the fixture rows one behavior test drives.
type queuedExpiredFixture struct {
	pool    *pgxpool.Pool
	agentID string
	issueID string
}

// newQueuedExpiredFixture builds a workspace/user/runtime/agent/issue through the
// shared testutil builders (CLAUDE.md: DB-backed tests must not open-code INSERTs)
// and marks the issue in_progress to simulate a run that has already started.
func newQueuedExpiredFixture(t *testing.T) queuedExpiredFixture {
	t.Helper()

	pool := newResolveOriginatorPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	userID := bootstrap.User(t,
		fmt.Sprintf("qexp-user-%d", suffix),
		fmt.Sprintf("qexp-user-%d@example.com", suffix),
	)
	workspaceID := bootstrap.Workspace(t,
		fmt.Sprintf("qexp-ws-%d", suffix),
		fmt.Sprintf("qexp-ws-%d", suffix),
	)

	fx := testutil.New(pool, workspaceID, userID)
	fx.Member(t, workspaceID, userID, "owner")
	runtimeID := fx.Runtime(t, "qexp-runtime")
	agentID := fx.Agent(t, "qexp-agent", runtimeID)
	issueID := fx.Issue(t, "queued expired retry", testutil.Cols{
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})

	return queuedExpiredFixture{pool: pool, agentID: agentID, issueID: issueID}
}

// failedQueuedExpiredTask seeds a failed queued_expired parent task for the
// fixture agent/issue at the given attempt/max_attempts and returns the loaded
// row.
func (f queuedExpiredFixture) failedQueuedExpiredTask(t *testing.T, q *db.Queries, attempt, maxAttempts int32) db.AgentTaskQueue {
	t.Helper()
	fx := testutil.New(f.pool, "", "")
	var runtimeID string
	fx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, f.agentID).Scan(&runtimeID)
	parentID := fx.Task(t, f.agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"issue_id":       f.issueID,
		"status":         "failed",
		"attempt":        attempt,
		"max_attempts":   maxAttempts,
		"failure_reason": "queued_expired",
		"session_id":     "src-session",
		"work_dir":       "/tmp/src-workdir",
		"completed_at":   testutil.Raw("now()"),
	})
	parent, err := q.GetAgentTask(context.Background(), util.MustParseUUID(parentID))
	if err != nil {
		t.Fatalf("load parent: %v", err)
	}
	return parent
}

// captureFailedPayloads subscribes to task:failed and returns the collected
// payloads as map[string]any for assertion.
func captureFailedPayloads(bus *events.Bus) *[]map[string]any {
	collected := &[]map[string]any{}
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		if p, ok := e.Payload.(map[string]any); ok {
			*collected = append(*collected, p)
		}
	})
	return collected
}

// TestQueuedExpiredRetryDecisionIsImmediateAndBounded is the DB-free guard for
// the retry decision itself: queued_expired is eligible with budget remaining,
// its next attempt has zero delay (immediate 'queued' child, never 'deferred'),
// and max_attempts<=1 / spent budget make it ineligible. This locks the contract
// without needing a database, complementing the DB-backed HandleFailedTasks tests
// below.
func TestQueuedExpiredRetryDecisionIsImmediateAndBounded(t *testing.T) {
	reason := string(taskfailure.ReasonQueuedExpired)

	if !retryableReasons[reason] {
		t.Fatalf("queued_expired must be in retryableReasons")
	}

	issueTask := func(attempt, maxAttempts int32) db.AgentTaskQueue {
		return db.AgentTaskQueue{
			IssueID:     pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			Attempt:     attempt,
			MaxAttempts: maxAttempts,
		}
	}

	if !retryEligible(reason, issueTask(1, 2)) {
		t.Errorf("attempt 1 of 2 should be retry-eligible")
	}
	if retryEligible(reason, issueTask(2, 2)) {
		t.Errorf("attempt 2 of 2 (budget spent) must not be retry-eligible")
	}
	if retryEligible(reason, issueTask(1, 1)) {
		t.Errorf("max_attempts<=1 must disable retry")
	}

	// No backoff: a positive delay would insert the child as 'deferred', which
	// the bounded-termination sweep does not reap for queued_expired lineage.
	if d := retryDelayForAttempt(reason, 1); d != 0 {
		t.Errorf("retryDelayForAttempt(queued_expired) = %v, want 0 (immediate queued child)", d)
	}
}

// TestQueuedExpiredRetryCreatesImmediateQueuedChild verifies that a
// queued_expired failure with remaining budget produces a retry child inserted
// as 'queued' (no fire_at), the failure event carries retry_pending=true, and
// the issue stays in_progress rather than being reset to todo.
func TestQueuedExpiredRetryCreatesImmediateQueuedChild(t *testing.T) {
	f := newQueuedExpiredFixture(t)
	ctx := context.Background()
	q := db.New(f.pool)

	bus := events.New()
	payloads := captureFailedPayloads(bus)
	svc := NewTaskService(q, f.pool, nil, bus)

	// attempt 1 of max 2: one retry remaining.
	parent := f.failedQueuedExpiredTask(t, q, 1, 2)

	svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{parent})

	var childStatus string
	var fireAt pgtype.Timestamptz
	if err := f.pool.QueryRow(ctx, `
		SELECT status, fire_at FROM agent_task_queue WHERE parent_task_id = $1
	`, util.UUIDToString(parent.ID)).Scan(&childStatus, &fireAt); err != nil {
		t.Fatalf("load retry child: %v", err)
	}
	if childStatus != "queued" {
		t.Fatalf("retry child status = %q, want queued (no backoff)", childStatus)
	}
	if fireAt.Valid {
		t.Fatalf("retry child carried fire_at %v, want NULL (immediate, not deferred)", fireAt.Time)
	}

	if len(*payloads) != 1 {
		t.Fatalf("task:failed events = %d, want 1", len(*payloads))
	}
	if got := (*payloads)[0]["retry_pending"]; got != true {
		t.Fatalf("retry_pending = %v, want true", got)
	}
	if got := (*payloads)[0]["failure_reason"]; got != "queued_expired" {
		t.Fatalf("failure_reason = %v, want queued_expired", got)
	}

	var issueStatus string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, f.issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "in_progress" {
		t.Fatalf("issue status = %q, want in_progress while a retry is pending", issueStatus)
	}
}

// TestQueuedExpiredRetryBudgetExhaustedResetsIssue verifies the terminal side:
// once the attempt budget is spent, no retry child is created, the failure event
// reports retry_pending=false, and the stuck in_progress issue is reset to todo.
func TestQueuedExpiredRetryBudgetExhaustedResetsIssue(t *testing.T) {
	f := newQueuedExpiredFixture(t)
	ctx := context.Background()
	q := db.New(f.pool)

	bus := events.New()
	payloads := captureFailedPayloads(bus)
	svc := NewTaskService(q, f.pool, nil, bus)

	// attempt 2 of max 2: budget exhausted, no retry allowed.
	parent := f.failedQueuedExpiredTask(t, q, 2, 2)

	svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{parent})

	childCount := testutil.New(f.pool, "", "").Count(t,
		`SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, util.UUIDToString(parent.ID))
	if childCount != 0 {
		t.Fatalf("retry children = %d, want 0 once budget is exhausted", childCount)
	}

	if len(*payloads) != 1 {
		t.Fatalf("task:failed events = %d, want 1", len(*payloads))
	}
	if got := (*payloads)[0]["retry_pending"]; got != false {
		t.Fatalf("retry_pending = %v, want false", got)
	}

	var issueStatus string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, f.issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("issue status = %q, want todo after terminal queued_expired", issueStatus)
	}
}

// TestQueuedExpiredRetryDisabledByMaxAttempts verifies max_attempts<=1 keeps its
// documented meaning (055_task_lease_and_retry: "1 disables retry") even now that
// queued_expired is retryable: no child, retry_pending=false, issue reset to todo.
func TestQueuedExpiredRetryDisabledByMaxAttempts(t *testing.T) {
	f := newQueuedExpiredFixture(t)
	ctx := context.Background()
	q := db.New(f.pool)

	bus := events.New()
	payloads := captureFailedPayloads(bus)
	svc := NewTaskService(q, f.pool, nil, bus)

	// max_attempts=1 disables auto-retry regardless of reason.
	parent := f.failedQueuedExpiredTask(t, q, 1, 1)

	svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{parent})

	childCount := testutil.New(f.pool, "", "").Count(t,
		`SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, util.UUIDToString(parent.ID))
	if childCount != 0 {
		t.Fatalf("retry children = %d, want 0 when max_attempts<=1 disables retry", childCount)
	}
	if len(*payloads) != 1 || (*payloads)[0]["retry_pending"] != false {
		t.Fatalf("expected one task:failed event with retry_pending=false, got %+v", *payloads)
	}

	var issueStatus string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, f.issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if issueStatus != "todo" {
		t.Fatalf("issue status = %q, want todo when retry is disabled", issueStatus)
	}
}
