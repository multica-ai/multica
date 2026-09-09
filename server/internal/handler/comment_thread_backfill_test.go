package handler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression tests for migration 457 (HYP-1750 / HYP-1754): the one-shot
// backfill of comment_thread_id for pre-451 legacy pending/in-flight rows.
// Each test simulates a legacy row the way the Tech Lead verified is
// faithful: INSERT through the normal path (the 451 trigger fills the thread
// scope), then UPDATE ... SET comment_thread_id = NULL — the trigger only
// listens to INSERT / UPDATE OF trigger_comment_id, so it does not re-fire —
// and executes the real migration file against the shared test database.
// Only this file ever NULLs comment_thread_id on a trigger-bearing row, and
// tests in a package run sequentially, so the migration's global UPDATE only
// ever touches the fixture rows of the test currently running.

const backfill457File = "457_agent_task_comment_thread_backfill.up.sql"

func backfill457SQL(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "migrations", backfill457File))
	if err != nil {
		t.Fatalf("read migration %s: %v", backfill457File, err)
	}
	return string(contents)
}

// runThreadBackfill457 executes the real 457 up migration on the shared pool.
func runThreadBackfill457(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), backfill457SQL(t)); err != nil {
		t.Fatalf("run migration 457: %v", err)
	}
}

// backfillNoticeConn opens a dedicated connection that records NOTICE
// messages, so tests can assert the migration's backfill/collision counts.
func backfillNoticeConn(t *testing.T, notices *[]string) *pgx.Conn {
	t.Helper()
	cfg, err := pgx.ParseConfig(testPool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse pool conn string: %v", err)
	}
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		*notices = append(*notices, n.Message)
	}
	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect for notice capture: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

// legacyNullThreadTask inserts a task through the normal path, asserts the
// 451 trigger filled its thread scope, then NULLs the scope to simulate a
// pre-451 legacy row.
func legacyNullThreadTask(t *testing.T, agentID string, over testutil.Cols) string {
	t.Helper()
	taskID := dbfx.Task(t, agentID, over)
	if got := threadScopeOf(t, taskID); got == "" {
		t.Fatalf("451 trigger did not fill comment_thread_id on insert of %s; legacy simulation impossible", taskID)
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET comment_thread_id = NULL WHERE id = $1", taskID)
	if got := threadScopeOf(t, taskID); got != "" {
		t.Fatalf("legacy simulation failed: comment_thread_id = %s, want NULL", got)
	}
	return taskID
}

// threadScopeOf returns the task's comment_thread_id, "" when NULL.
func threadScopeOf(t *testing.T, taskID string) string {
	t.Helper()
	var scope string
	if err := testPool.QueryRow(context.Background(),
		"SELECT COALESCE(comment_thread_id::text, '') FROM agent_task_queue WHERE id = $1", taskID).Scan(&scope); err != nil {
		t.Fatalf("read thread scope of %s: %v", taskID, err)
	}
	return scope
}

// 457 ruling §4.1: after the backfill, the 452 partial unique index intercepts
// a new pending row in the same (issue, agent, thread).
func TestThreadBackfill457RestoresPendingUniqueFence(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "457 unique-fence runtime")
	agentID := dbfx.Agent(t, "457 unique-fence agent", runtimeID)
	issueID := dbfx.Issue(t, "457 unique-fence issue")
	rootA := dbfx.Comment(t, issueID, "thread A")
	replyA := dbfx.Comment(t, issueID, "A supplement", testutil.Cols{"parent_id": rootA})
	rootB := dbfx.Comment(t, issueID, "thread B")

	legacyNullThreadTask(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": rootA})

	// Pre-backfill the fence gap is real: the legacy row sits in the zero-UUID
	// bucket, so a same-thread insert does NOT conflict with it.
	preFillID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": replyA})
	dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE id = $1", preFillID)

	runThreadBackfill457(t)

	var pgErr *pgconn.PgError
	_, err := testPool.Exec(ctx,
		"INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, trigger_comment_id) VALUES ($1, $2, $3, 'queued', $4)",
		agentID, issueID, runtimeID, replyA)
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "idx_one_pending_task_per_issue_agent_thread" {
		t.Fatalf("same-thread insert after backfill: got %v, want 23505 from idx_one_pending_task_per_issue_agent_thread", err)
	}
	// The index stays per-thread: another thread for the same issue+agent is
	// not over-blocked.
	dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": rootB})
}

// 457 ruling §4.2 + §4.3: after the backfill, MergeCommentIntoPendingTask
// folds a same-thread comment into the legacy row and
// CancelPendingTasksByIssueAndAgentInThread cancels it. Both calls miss the
// row before the backfill (the fence gap), which pins the regression.
func TestThreadBackfill457MergeAndCancelHitBackfilledRow(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	runtimeID := dbfx.Runtime(t, "457 merge/cancel runtime")
	agentID := dbfx.Agent(t, "457 merge/cancel agent", runtimeID)
	issueID := dbfx.Issue(t, "457 merge/cancel issue")
	root := dbfx.Comment(t, issueID, "thread root")
	reply := dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": root})

	legacyID := legacyNullThreadTask(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": root})

	merge := db.MergeCommentIntoPendingTaskParams{IssueID: parseUUID(issueID), AgentID: parseUUID(agentID)}
	cancel := db.CancelPendingTasksByIssueAndAgentInThreadParams{IssueID: parseUUID(issueID), AgentID: parseUUID(agentID)}

	// Before the backfill both fences miss the NULL-thread legacy row.
	merge.NewTriggerCommentID = parseUUID(reply)
	if _, err := q.MergeCommentIntoPendingTask(ctx, merge); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pre-backfill merge: got %v, want pgx.ErrNoRows (fence gap)", err)
	}
	cancel.ThreadCommentID = parseUUID(reply)
	if rows, err := q.CancelPendingTasksByIssueAndAgentInThread(ctx, cancel); err != nil || len(rows) != 0 {
		t.Fatalf("pre-backfill cancel: got %d rows / %v, want 0 rows (fence gap)", len(rows), err)
	}

	runThreadBackfill457(t)
	if got := threadScopeOf(t, legacyID); got != root {
		t.Fatalf("backfilled thread scope = %s, want root %s", got, root)
	}

	merged, err := q.MergeCommentIntoPendingTask(ctx, merge)
	if err != nil {
		t.Fatalf("post-backfill merge: %v", err)
	}
	if merged.ID != parseUUID(legacyID) {
		t.Fatalf("merge hit task %s, want legacy row %s", uuidToString(merged.ID), legacyID)
	}
	if len(merged.CoalescedCommentIds) != 1 || merged.CoalescedCommentIds[0] != parseUUID(root) {
		t.Fatalf("merge coalesced %v, want [%s]", merged.CoalescedCommentIds, root)
	}

	cancelled, err := q.CancelPendingTasksByIssueAndAgentInThread(ctx, cancel)
	if err != nil {
		t.Fatalf("post-backfill cancel: %v", err)
	}
	if len(cancelled) != 1 || cancelled[0].ID != parseUUID(legacyID) || cancelled[0].Status != "cancelled" {
		t.Fatalf("cancel returned %+v, want exactly the legacy row cancelled", cancelled)
	}
}

// 457 ruling §4.4: assignment-level rows (STRICT trigger → NULL thread scope
// by design) are not backfilled and stay in the NULL bucket.
func TestThreadBackfill457LeavesAssignmentLevelRowsNull(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "457 assignment runtime")
	agentID := dbfx.Agent(t, "457 assignment agent", runtimeID)
	issueID := dbfx.Issue(t, "457 assignment issue")

	// No trigger_comment_id: the assignment-level task path.
	assignmentID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	if got := threadScopeOf(t, assignmentID); got != "" {
		t.Fatalf("assignment-level task has thread scope %s, want NULL by design", got)
	}

	runThreadBackfill457(t)

	if got := threadScopeOf(t, assignmentID); got != "" {
		t.Fatalf("assignment-level task backfilled to %s, want NULL untouched", got)
	}
}

// 457 ruling §4.5: the collision guard mirrors the 452 partial-index WHERE
// clause verbatim — a dispatched occupant blocks the backfill, a
// deferred+media_pending occupant blocks it, and a plain deferred occupant
// (out of index scope) does not.
func TestThreadBackfill457GuardMirrorsIndexPredicate(t *testing.T) {
	variants := []struct {
		name           string
		occupantStatus string
		occupantCtx    testutil.Raw
		wantBlocked    bool
	}{
		{"dispatched occupant blocks", "dispatched", `'{}'::jsonb`, true},
		{"deferred+media occupant blocks", "deferred", `'{"channel_issue_media_pending":"true"}'::jsonb`, true},
		{"plain deferred occupant does not block", "deferred", `'{}'::jsonb`, false},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "457 guard runtime "+v.name)
			agentID := dbfx.Agent(t, "457 guard agent "+v.name, runtimeID)
			issueID := dbfx.Issue(t, "457 guard issue "+v.name)
			root := dbfx.Comment(t, issueID, "thread root")

			legacyID := legacyNullThreadTask(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": root})
			// The occupant is a post-451 row: the trigger fills its thread
			// scope, and it coexists with the legacy row because the legacy
			// row sits in the zero-UUID bucket (the audit's gap).
			occupantID := dbfx.Task(t, agentID, testutil.Cols{
				"issue_id":           issueID,
				"runtime_id":         runtimeID,
				"trigger_comment_id": root,
				"status":             v.occupantStatus,
				"context":            v.occupantCtx,
			})

			runThreadBackfill457(t)

			got := threadScopeOf(t, legacyID)
			if v.wantBlocked && got != "" {
				t.Fatalf("collision guard failed: legacy row backfilled to %s over a %s occupant", got, v.occupantStatus)
			}
			if !v.wantBlocked && got != root {
				t.Fatalf("guard over-blocked: legacy row scope = %q, want %s (occupant is out of index scope)", got, root)
			}
			if got := threadScopeOf(t, occupantID); got != root {
				t.Fatalf("occupant scope = %q, want untouched %s", got, root)
			}
		})
	}
}

// 457 ruling §4.6: a task triggered by a deeply nested reply is backfilled to
// the thread ROOT id, not its direct parent (exercises the recursive CTE).
func TestThreadBackfill457BackfillsNestedReplyToRootID(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "457 nested runtime")
	agentID := dbfx.Agent(t, "457 nested agent", runtimeID)
	issueID := dbfx.Issue(t, "457 nested issue")
	root := dbfx.Comment(t, issueID, "root")
	reply := dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": root})
	nested := dbfx.Comment(t, issueID, "nested reply", testutil.Cols{"parent_id": reply})

	legacyID := legacyNullThreadTask(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": nested})

	runThreadBackfill457(t)

	if got := threadScopeOf(t, legacyID); got != root {
		t.Fatalf("nested-reply legacy row backfilled to %s, want thread root %s (not parent %s)", got, root, reply)
	}
}

// 457 ruling §4.7: the conditional UPDATE is idempotent — a second run
// changes nothing, errors on nothing, and reports 0/0 in its NOTICE counts.
func TestThreadBackfill457SecondRunIsIdempotent(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "457 idempotent runtime")
	agentID := dbfx.Agent(t, "457 idempotent agent", runtimeID)
	issueID := dbfx.Issue(t, "457 idempotent issue")
	root := dbfx.Comment(t, issueID, "thread root")

	legacyID := legacyNullThreadTask(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": root})

	var firstNotices []string
	if _, err := backfillNoticeConn(t, &firstNotices).Exec(ctx, backfill457SQL(t)); err != nil {
		t.Fatalf("first backfill run: %v", err)
	}
	if len(firstNotices) != 1 || !strings.Contains(firstNotices[0], "backfilled 1 legacy row(s)") || !strings.Contains(firstNotices[0], "skipped 0 collision row(s)") {
		t.Fatalf("first run notices = %v, want one notice reporting backfilled 1 / skipped 0", firstNotices)
	}
	if got := threadScopeOf(t, legacyID); got != root {
		t.Fatalf("after first run scope = %q, want %s", got, root)
	}

	var secondNotices []string
	if _, err := backfillNoticeConn(t, &secondNotices).Exec(ctx, backfill457SQL(t)); err != nil {
		t.Fatalf("second backfill run: %v", err)
	}
	if len(secondNotices) != 1 || !strings.Contains(secondNotices[0], "backfilled 0 legacy row(s)") || !strings.Contains(secondNotices[0], "skipped 0 collision row(s)") {
		t.Fatalf("second run notices = %v, want one notice reporting backfilled 0 / skipped 0", secondNotices)
	}
	if got := threadScopeOf(t, legacyID); got != root {
		t.Fatalf("second run changed scope to %q, want stable %s", got, root)
	}
}

// 457 ruling §4.8: a deferred+media_pending legacy row (the longest-lingering
// class) is backfilled; terminal legacy rows are excluded; a backfilled
// running row is hit by the RegisterPlannedCommentForActiveTask fence.
func TestThreadBackfill457ScopeVariantsAndRunningFence(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	runtimeID := dbfx.Runtime(t, "457 scope runtime")
	agentID := dbfx.Agent(t, "457 scope agent", runtimeID)
	issueID := dbfx.Issue(t, "457 scope issue")
	root := dbfx.Comment(t, issueID, "thread root")
	followup := dbfx.Comment(t, issueID, "follow-up", testutil.Cols{"parent_id": root})

	deferredID := legacyNullThreadTask(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": root,
		"status": "deferred", "context": testutil.Raw(`'{"channel_issue_media_pending":"true"}'::jsonb`),
	})
	runningID := legacyNullThreadTask(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": root, "status": "running",
	})
	terminalIDs := map[string]string{}
	for _, status := range []string{"completed", "cancelled", "failed"} {
		terminalIDs[status] = legacyNullThreadTask(t, agentID, testutil.Cols{
			"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": root, "status": status,
		})
	}

	// Pre-backfill the active-task fence misses the NULL running row too.
	planned := db.RegisterPlannedCommentForActiveTaskParams{
		CommentID: parseUUID(followup), IssueID: parseUUID(issueID), AgentID: parseUUID(agentID),
	}
	if _, err := q.RegisterPlannedCommentForActiveTask(ctx, planned); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pre-backfill RegisterPlannedCommentForActiveTask: got %v, want pgx.ErrNoRows (fence gap)", err)
	}

	runThreadBackfill457(t)

	if got := threadScopeOf(t, deferredID); got != root {
		t.Fatalf("deferred+media legacy row scope = %q, want backfilled to %s", got, root)
	}
	if got := threadScopeOf(t, runningID); got != root {
		t.Fatalf("running legacy row scope = %q, want backfilled to %s", got, root)
	}
	for status, id := range terminalIDs {
		if got := threadScopeOf(t, id); got != "" {
			t.Fatalf("terminal (%s) legacy row backfilled to %s, want excluded / NULL", status, got)
		}
	}

	registered, err := q.RegisterPlannedCommentForActiveTask(ctx, planned)
	if err != nil {
		t.Fatalf("post-backfill RegisterPlannedCommentForActiveTask: %v", err)
	}
	if registered.ID != parseUUID(runningID) {
		t.Fatalf("planned comment registered on %s, want running legacy row %s", uuidToString(registered.ID), runningID)
	}
	if len(registered.CoalescedCommentIds) != 1 || registered.CoalescedCommentIds[0] != parseUUID(followup) {
		t.Fatalf("registered planned ids %v, want [%s]", registered.CoalescedCommentIds, followup)
	}
}

// 457 ruling §4 addendum: a legacy row whose trigger comment was hard-deleted
// (anchor missing) leaves the NULL bucket deterministically —
// comment_thread_root_id COALESCEs back to the comment id itself. Normally the
// ON DELETE SET NULL FK clears trigger_comment_id first and excludes the row;
// the dangling-anchor path needs the FK bypassed, so the test skips when the
// test role cannot toggle session_replication_role.
func TestThreadBackfill457DeletedTriggerCoalesceFallback(t *testing.T) {
	ctx := context.Background()

	// The function-level fallback needs no privileges: an anchor that never
	// existed resolves to the id itself.
	dangling := "00000000-0000-0000-0000-000000000042"
	var resolved string
	if err := testPool.QueryRow(ctx, "SELECT comment_thread_root_id($1)::text", dangling).Scan(&resolved); err != nil {
		t.Fatalf("comment_thread_root_id on dangling anchor: %v", err)
	}
	if resolved != dangling {
		t.Fatalf("comment_thread_root_id(%s) = %s, want COALESCE fallback to the id itself", dangling, resolved)
	}

	runtimeID := dbfx.Runtime(t, "457 dangling runtime")
	agentID := dbfx.Agent(t, "457 dangling agent", runtimeID)
	issueID := dbfx.Issue(t, "457 dangling issue")
	trigger := dbfx.Comment(t, issueID, "soon deleted trigger")
	legacyID := legacyNullThreadTask(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "trigger_comment_id": trigger})

	var notices []string
	conn := backfillNoticeConn(t, &notices)
	if _, err := conn.Exec(ctx, "SET session_replication_role = replica"); err != nil {
		t.Skipf("cannot bypass the FK to simulate a hard-deleted trigger comment: %v", err)
	}
	// Bypass the ON DELETE SET NULL FK so trigger_comment_id keeps pointing at
	// the deleted comment — the dangling-anchor case the migration documents.
	if _, err := conn.Exec(ctx, "DELETE FROM comment WHERE id = $1", trigger); err != nil {
		t.Fatalf("hard-delete trigger comment: %v", err)
	}
	if _, err := conn.Exec(ctx, "SET session_replication_role = DEFAULT"); err != nil {
		t.Fatalf("reset session_replication_role: %v", err)
	}
	var stillPointing string
	if err := testPool.QueryRow(ctx, "SELECT COALESCE(trigger_comment_id::text, '') FROM agent_task_queue WHERE id = $1", legacyID).Scan(&stillPointing); err != nil {
		t.Fatalf("read trigger_comment_id: %v", err)
	}
	if stillPointing != trigger {
		t.Fatalf("trigger_comment_id = %q after hard delete, want dangling %s", stillPointing, trigger)
	}

	runThreadBackfill457(t)

	if got := threadScopeOf(t, legacyID); got != trigger {
		t.Fatalf("dangling-anchor legacy row scope = %q, want %s (deterministic singleton bucket, out of the NULL bucket)", got, trigger)
	}
}
