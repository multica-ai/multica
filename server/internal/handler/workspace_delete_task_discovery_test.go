package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// workspaceDeletePathFixture builds the ownership shapes teardown has to handle:
// one task reachable from the victim workspace only through agent_id, one only
// through issue_id, one only through runtime_id, and one that belongs entirely to
// a neighbour workspace and must survive.
//
// The tokens cover the three explicit task_token paths: one owned by the victim
// workspace, one whose workspace_id points at the neighbour while its TASK is the
// victim's, one whose workspace_id points at the neighbour while its AGENT is the
// victim's, and one that is entirely the neighbour's.
type workspaceDeletePathFixture struct {
	victimID    string
	neighbourID string

	victimAgent   string
	victimIssue   string
	victimRuntime string

	neighbourAgent   string
	neighbourIssue   string
	neighbourRuntime string

	taskViaAgent   string
	taskViaIssue   string
	taskViaRuntime string
	neighbourTask  string

	victimToken        string
	crossTaskToken     string
	crossAgentToken    string
	neighbourOnlyToken string
}

func newWorkspaceDeletePathFixture(t *testing.T, slugSuffix string) workspaceDeletePathFixture {
	t.Helper()
	ctx := context.Background()

	victimSlug := "handler-tests-delete-paths-victim-" + slugSuffix
	neighbourSlug := "handler-tests-delete-paths-neighbour-" + slugSuffix
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = ANY($1::text[])`,
		[]string{victimSlug, neighbourSlug})

	newWorkspace := func(name, slug string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id
`, name, slug).Scan(&id); err != nil {
			t.Fatalf("create workspace %s: %v", slug, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
		})
		return id
	}
	f := workspaceDeletePathFixture{
		victimID:    newWorkspace("Delete Paths Victim "+slugSuffix, victimSlug),
		neighbourID: newWorkspace("Delete Paths Neighbour "+slugSuffix, neighbourSlug),
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, f.victimID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	newRuntime := func(wsID, name string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
VALUES ($1, $2, 'cloud', 'delete-test', 'offline', '', '{}'::jsonb, $3)
RETURNING id
`, wsID, name, testUserID).Scan(&id); err != nil {
			t.Fatalf("create runtime %s: %v", name, err)
		}
		return id
	}
	newAgent := func(wsID, runtimeID, name string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, owner_id)
VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, $4)
RETURNING id
`, wsID, name, runtimeID, testUserID).Scan(&id); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
		return id
	}
	newIssue := func(wsID, title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id)
VALUES ($1, $2, 'member', $3)
RETURNING id
`, wsID, title, testUserID).Scan(&id); err != nil {
			t.Fatalf("create issue %s: %v", title, err)
		}
		return id
	}

	f.victimRuntime = newRuntime(f.victimID, "victim runtime")
	f.victimAgent = newAgent(f.victimID, f.victimRuntime, "victim agent")
	f.victimIssue = newIssue(f.victimID, "victim issue")
	f.neighbourRuntime = newRuntime(f.neighbourID, "neighbour runtime")
	f.neighbourAgent = newAgent(f.neighbourID, f.neighbourRuntime, "neighbour agent")
	f.neighbourIssue = newIssue(f.neighbourID, "neighbour issue")

	f.taskViaAgent = insertDeletePathTask(t, f.victimAgent, f.neighbourIssue, f.neighbourRuntime)
	f.taskViaIssue = insertDeletePathTask(t, f.neighbourAgent, f.victimIssue, f.neighbourRuntime)
	f.taskViaRuntime = insertDeletePathTask(t, f.neighbourAgent, f.neighbourIssue, f.victimRuntime)
	f.neighbourTask = insertDeletePathTask(t, f.neighbourAgent, f.neighbourIssue, f.neighbourRuntime)

	f.victimToken = insertDeletePathToken(t, "mul5999-victim-"+slugSuffix, f.taskViaAgent, f.victimAgent, f.victimID)
	f.crossTaskToken = insertDeletePathToken(t, "mul5999-cross-task-"+slugSuffix, f.taskViaIssue, f.neighbourAgent, f.neighbourID)
	f.crossAgentToken = insertDeletePathToken(t, "mul5999-cross-agent-"+slugSuffix, f.neighbourTask, f.victimAgent, f.neighbourID)
	f.neighbourOnlyToken = insertDeletePathToken(t, "mul5999-neighbour-"+slugSuffix, f.neighbourTask, f.neighbourAgent, f.neighbourID)

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM task_token WHERE id = ANY($1::uuid[])`,
			[]string{f.victimToken, f.crossTaskToken, f.crossAgentToken, f.neighbourOnlyToken})
		_, _ = testPool.Exec(bg, `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`,
			[]string{f.taskViaAgent, f.taskViaIssue, f.taskViaRuntime, f.neighbourTask})
	})

	return f
}

func insertDeletePathTask(t *testing.T, agentID, issueID, runtimeID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
VALUES ($1, $2, $3, 'completed', now())
RETURNING id
`, agentID, issueID, runtimeID).Scan(&id); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return id
}

func insertDeletePathToken(t *testing.T, hash, taskID, agentID, wsID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour')
RETURNING id
`, hash, taskID, agentID, wsID, testUserID).Scan(&id); err != nil {
		t.Fatalf("create task token %s: %v", hash, err)
	}
	return id
}

func rowExists(t *testing.T, table, id string) bool {
	t.Helper()
	var found bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, id).Scan(&found); err != nil {
		t.Fatalf("check %s %s: %v", table, id, err)
	}
	return found
}

// TestDeleteWorkspaceTasks_IsSelfSufficientWithoutCascades asserts that the task
// sweep removes the rows on its own, inside the transaction and before any owner
// row is deleted. That ordering is the whole point: the legacy FK cascades are
// documented as an expand-phase safety net, so an assertion taken after the
// handler has finished cannot tell teardown apart from a cascade doing the work.
func TestDeleteWorkspaceTasks_IsSelfSufficientWithoutCascades(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "selfsufficient")

	tx, err := testHandler.TxStarter.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	qtx := testHandler.Queries.WithTx(tx)

	if err := qtx.SetWorkspaceTeardownMode(ctx); err != nil {
		t.Fatalf("set teardown mode: %v", err)
	}
	if err := lockWorkspaceTaskOwners(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("lock task owners: %v", err)
	}
	if err := deleteWorkspaceTasks(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("deleteWorkspaceTasks: %v", err)
	}

	// Still inside the transaction: no agent, issue or runtime row has been
	// deleted, so nothing here can be attributed to ON DELETE CASCADE.
	count := func(query string, args ...any) int {
		var n int
		if err := tx.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM agent_task_queue WHERE id = ANY($1::uuid[])`,
		[]string{f.taskViaAgent, f.taskViaIssue, f.taskViaRuntime}); n != 0 {
		t.Errorf("%d of the 3 ownership-path tasks survived the sweep", n)
	}
	if n := count(`SELECT count(*) FROM task_token WHERE id = ANY($1::uuid[])`,
		[]string{f.victimToken, f.crossTaskToken, f.crossAgentToken}); n != 0 {
		t.Errorf("%d of the 3 task_token paths survived the sweep", n)
	}
	if n := count(`SELECT count(*) FROM agent WHERE id = $1`, f.victimAgent); n != 1 {
		t.Fatalf("victim agent row was deleted by the task sweep; the assertions above prove nothing")
	}
	if n := count(`SELECT count(*) FROM agent_task_queue WHERE id = $1`, f.neighbourTask); n != 1 {
		t.Error("neighbour-only task was deleted by the victim's task sweep")
	}
	if n := count(`SELECT count(*) FROM task_token WHERE id = $1`, f.neighbourOnlyToken); n != 1 {
		t.Error("neighbour-only task token was deleted by the victim's task sweep")
	}
}

// TestDeleteWorkspace_CollectsTasksThroughEveryOwnershipPath is the end-to-end
// counterpart: the handler returns 204, every ownership path is gone, and the
// neighbour workspace is untouched.
func TestDeleteWorkspace_CollectsTasksThroughEveryOwnershipPath(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	f := newWorkspaceDeletePathFixture(t, "endtoend")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+f.victimID, nil)
	req = withURLParam(req, "id", f.victimID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace = %d, want 204: %s", w.Code, w.Body.String())
	}

	for name, id := range map[string]string{
		"task reachable only via agent_id":   f.taskViaAgent,
		"task reachable only via issue_id":   f.taskViaIssue,
		"task reachable only via runtime_id": f.taskViaRuntime,
	} {
		if rowExists(t, "agent_task_queue", id) {
			t.Errorf("%s survived workspace delete", name)
		}
	}
	for name, id := range map[string]string{
		"token owned by the deleted workspace": f.victimToken,
		"token whose task is the victim's":     f.crossTaskToken,
		"token whose agent is the victim's":    f.crossAgentToken,
	} {
		if rowExists(t, "task_token", id) {
			t.Errorf("%s survived workspace delete", name)
		}
	}

	if !rowExists(t, "agent_task_queue", f.neighbourTask) {
		t.Error("neighbour workspace task was deleted")
	}
	if !rowExists(t, "task_token", f.neighbourOnlyToken) {
		t.Error("neighbour workspace task token was deleted")
	}
	if !rowExists(t, "agent", f.neighbourAgent) ||
		!rowExists(t, "issue", f.neighbourIssue) ||
		!rowExists(t, "agent_runtime", f.neighbourRuntime) {
		t.Error("neighbour workspace objects were deleted")
	}
	if rowExists(t, "workspace", f.victimID) {
		t.Error("victim workspace still exists")
	}
}

// TestDeleteWorkspaceTasks_PagesPastTheBatchSize covers the bound itself: a
// single owner with more tasks than one batch must be swept by several
// iterations, and the loop must terminate. Nothing here may depend on the whole
// task set fitting in one statement or in this process (MUL-5999 review).
func TestDeleteWorkspaceTasks_PagesPastTheBatchSize(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "batched")

	// Two and a half batches on one agent, so the loop has to page and then
	// see an empty page.
	const extraTasks = workspaceDeleteTaskPageSize*2 + workspaceDeleteTaskPageSize/2
	if _, err := testPool.Exec(ctx, `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
SELECT $1, $2, $3, 'completed', now() FROM generate_series(1, $4::int)
`, f.victimAgent, f.victimIssue, f.victimRuntime, extraTasks); err != nil {
		t.Fatalf("seed %d tasks: %v", extraTasks, err)
	}

	tx, err := testHandler.TxStarter.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	qtx := testHandler.Queries.WithTx(tx)

	if err := qtx.SetWorkspaceTeardownMode(ctx); err != nil {
		t.Fatalf("set teardown mode: %v", err)
	}
	if err := lockWorkspaceTaskOwners(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("lock task owners: %v", err)
	}

	// The bound is a keyset page, not just a LIMIT: each page must return at
	// most the requested rows AND resume strictly after the previous page, so
	// the scan never re-walks what it already deleted. A LIMIT alone would still
	// read (and sort) the owner's whole task set per page.
	const probeLimit = 10
	firstPage, err := qtx.ListTaskIDsByAgentFirstPage(ctx, db.ListTaskIDsByAgentFirstPageParams{
		AgentID: parseUUID(f.victimAgent), Limit: probeLimit,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(firstPage) != probeLimit {
		t.Fatalf("first page returned %d ids for LIMIT %d; the page query is not bounded",
			len(firstPage), probeLimit)
	}
	secondPage, err := qtx.ListTaskIDsByAgentPage(ctx, db.ListTaskIDsByAgentPageParams{
		AgentID: parseUUID(f.victimAgent), ID: firstPage[len(firstPage)-1], Limit: probeLimit,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage) != probeLimit {
		t.Fatalf("second page returned %d ids, want %d", len(secondPage), probeLimit)
	}
	seen := make(map[string]struct{}, len(firstPage))
	for _, id := range firstPage {
		seen[uuidToString(id)] = struct{}{}
	}
	for _, id := range secondPage {
		if _, dup := seen[uuidToString(id)]; dup {
			t.Fatalf("page 2 re-returned id %s; the cursor is not advancing", uuidToString(id))
		}
	}
	if uuidToString(secondPage[0]) <= uuidToString(firstPage[len(firstPage)-1]) {
		t.Errorf("page 2 starts at %s, which is not after page 1's last id %s",
			uuidToString(secondPage[0]), uuidToString(firstPage[len(firstPage)-1]))
	}

	if err := deleteWorkspaceTasks(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("deleteWorkspaceTasks: %v", err)
	}

	var remaining int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM agent_task_queue
WHERE agent_id = $1 OR issue_id = $2 OR runtime_id = $3
`, f.victimAgent, f.victimIssue, f.victimRuntime).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d tasks survived a multi-batch sweep", remaining)
	}
}

// TestDeleteWorkspaceTasks_PagesOwnersBeyondOnePage covers the other unbounded
// dimension: owner enumeration. A workspace with more agents than one owner page
// must still be swept completely, and the owner ids must never all be held here at
// once (MUL-5999 review).
func TestDeleteWorkspaceTasks_PagesOwnersBeyondOnePage(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "ownerpages")

	// One more agent than a single owner page, each with a task, so the sweep has
	// to fetch a second owner page and then see an empty one.
	extraAgents := workspaceDeleteOwnerPageSize + 1
	if _, err := testPool.Exec(ctx, `
WITH new_agents AS (
    INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, owner_id)
    SELECT $1, 'owner-page-agent-' || g, 'cloud', '{}'::jsonb, $2, $3
    FROM generate_series(1, $4::int) g
    RETURNING id
)
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
SELECT id, $5, $2, 'completed', now() FROM new_agents
`, f.victimID, f.victimRuntime, testUserID, extraAgents, f.neighbourIssue); err != nil {
		t.Fatalf("seed %d agents: %v", extraAgents, err)
	}

	tx, err := testHandler.TxStarter.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	qtx := testHandler.Queries.WithTx(tx)

	if err := qtx.SetWorkspaceTeardownMode(ctx); err != nil {
		t.Fatalf("set teardown mode: %v", err)
	}
	if err := lockWorkspaceTaskOwners(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("lock task owners: %v", err)
	}

	// The owner page is bounded and advances the same way the task page does.
	firstOwners, err := qtx.ListWorkspaceAgentIDFirstPage(ctx, db.ListWorkspaceAgentIDFirstPageParams{
		WorkspaceID: parseUUID(f.victimID), Limit: workspaceDeleteOwnerPageSize,
	})
	if err != nil {
		t.Fatalf("first owner page: %v", err)
	}
	if len(firstOwners) != workspaceDeleteOwnerPageSize {
		t.Fatalf("first owner page returned %d ids, want %d", len(firstOwners), workspaceDeleteOwnerPageSize)
	}
	nextOwners, err := qtx.ListWorkspaceAgentIDPage(ctx, db.ListWorkspaceAgentIDPageParams{
		WorkspaceID: parseUUID(f.victimID), ID: firstOwners[len(firstOwners)-1], Limit: workspaceDeleteOwnerPageSize,
	})
	if err != nil {
		t.Fatalf("second owner page: %v", err)
	}
	if len(nextOwners) == 0 {
		t.Fatal("second owner page is empty; owner enumeration stopped after one page")
	}

	if err := deleteWorkspaceTasks(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("deleteWorkspaceTasks: %v", err)
	}

	var remaining int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM agent_task_queue q
JOIN agent a ON a.id = q.agent_id
WHERE a.workspace_id = $1
`, f.victimID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d tasks survived; owners past the first page were not swept", remaining)
	}
}

// TestSweepTasksForOwner_FailsClosedWhenTasksKeepArriving pins the behaviour that
// makes the sweep safe without an application-wide enqueue protocol: if tasks keep
// appearing for an owner that has already been swept, teardown refuses to commit
// instead of leaving a half-cleaned tenant behind.
//
// The page function stands in for a broken fence — it keeps producing a task the
// sweep cannot get rid of.
func TestSweepTasksForOwner_FailsClosedWhenTasksKeepArriving(t *testing.T) {
	ctx := context.Background()
	owner := parseUUID("11111111-2222-3333-4444-555555555555")
	stubTask := parseUUID("66666666-7777-8888-9999-aaaaaaaaaaaa")

	// Faithful model of a broken fence: each pass finds one page of tasks and
	// then reports the owner clean, so the next pass starts from the beginning
	// and finds another one. Honours the page query's `id > cursor` contract, so
	// the only thing that can stop this is the pass cap.
	pages := 0
	firstPage := func(_ context.Context, _ pgtype.UUID, _ int32) ([]pgtype.UUID, error) {
		pages++
		if pages > 100 {
			t.Fatal("sweep did not stop; the pass cap is not working")
		}
		return []pgtype.UUID{stubTask}, nil
	}
	nextPage := func(context.Context, pgtype.UUID, pgtype.UUID, int32) ([]pgtype.UUID, error) {
		return nil, nil
	}
	noop := func(context.Context, []pgtype.UUID) error { return nil }

	err := sweepTasksForOwner(ctx, "agent", owner, firstPage, nextPage, noop, noop)
	if err == nil {
		t.Fatal("sweep succeeded while tasks kept arriving; teardown would commit a partial cleanup")
	}
	if !strings.Contains(err.Error(), "refusing to commit a partial teardown") {
		t.Errorf("error = %v, want a fail-closed partial-teardown error", err)
	}
}

// TestAgentTaskQueueOwnerFKsStillExist guards the precondition the write fence
// leans on. Blocking a concurrent enqueue depends on an INSERT taking FOR KEY
// SHARE on the agent / issue / runtime it references, which is a property of these
// foreign keys. The file header calls the legacy FKs an expand-phase safety net,
// so if a later migration removes them this test is what turns a silently missing
// fence into a red build — the replacement has to be an explicit protocol shared
// with every enqueue path.
func TestAgentTaskQueueOwnerFKsStillExist(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	for column, referenced := range map[string]string{
		"agent_id":   "agent",
		"issue_id":   "issue",
		"runtime_id": "agent_runtime",
	} {
		var exists bool
		if err := testPool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_constraint c
    JOIN pg_class child ON child.oid = c.conrelid
    JOIN pg_class parent ON parent.oid = c.confrelid
    JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
    WHERE c.contype = 'f'
      AND child.relname = 'agent_task_queue'
      AND parent.relname = $1
      AND a.attname = $2
      AND array_length(c.conkey, 1) = 1
)
`, referenced, column).Scan(&exists); err != nil {
			t.Fatalf("look up FK on %s: %v", column, err)
		}
		if !exists {
			t.Errorf("agent_task_queue.%s no longer has an FK to %s: the workspace-delete write fence "+
				"relied on that FK's implicit FOR KEY SHARE to block concurrent enqueues, so it needs an "+
				"explicit application-layer replacement before this FK goes away", column, referenced)
		}
	}
}

// TestDeleteWorkspaceTasks_FencesConcurrentEnqueueAndReassignment covers the two
// races a per-batch sweep would otherwise lose:
//
//   - enqueue after the sweep: a task inserted for an already-swept owner would
//     be left behind with its own leaf rows;
//   - reassignment during the sweep: a task moved to another workspace's runtime
//     between read and delete would be deleted on a stale ownership claim.
//
// Both are closed by locks the sweep takes, so the assertion is that a concurrent
// writer is blocked (and hits its own lock_timeout) while teardown holds them.
func TestDeleteWorkspaceTasks_FencesConcurrentEnqueueAndReassignment(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := newWorkspaceDeletePathFixture(t, "fence")

	tearDownTx, err := testHandler.TxStarter.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown tx: %v", err)
	}
	defer tearDownTx.Rollback(ctx)
	qtx := testHandler.Queries.WithTx(tearDownTx)
	if err := lockWorkspaceTaskOwners(ctx, qtx, parseUUID(f.victimID)); err != nil {
		t.Fatalf("lock task owners: %v", err)
	}

	// A second transaction standing in for a concurrent writer. Its own short
	// lock_timeout turns "blocked forever" into an assertable error.
	blocked := func(name, sql string, args ...any) {
		t.Helper()
		other, err := testPool.Begin(ctx)
		if err != nil {
			t.Fatalf("%s: begin: %v", name, err)
		}
		defer other.Rollback(ctx)
		if _, err := other.Exec(ctx, "SET LOCAL lock_timeout = 750"); err != nil {
			t.Fatalf("%s: set lock_timeout: %v", name, err)
		}
		_, err = other.Exec(ctx, sql, args...)
		if err == nil {
			t.Errorf("%s: succeeded while teardown held the owner locks; the fence is not working", name)
			return
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Errorf("%s: got %v, want lock_not_available (55P03)", name, err)
		}
	}

	blocked("enqueue for a locked agent", `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
VALUES ($1, $2, $3, 'completed', now())
`, f.victimAgent, f.neighbourIssue, f.neighbourRuntime)

	blocked("enqueue against a locked runtime", `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
VALUES ($1, $2, $3, 'completed', now())
`, f.neighbourAgent, f.neighbourIssue, f.victimRuntime)

	blocked("reassignment onto a locked runtime", `
UPDATE agent_task_queue SET runtime_id = $1 WHERE id = $2
`, f.victimRuntime, f.neighbourTask)

	// A writer that has nothing to do with the workspace under teardown must
	// still get through, so the fence is not a global stop-the-world.
	unrelated, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unrelated tx: %v", err)
	}
	defer unrelated.Rollback(ctx)
	if _, err := unrelated.Exec(ctx, "SET LOCAL lock_timeout = 750"); err != nil {
		t.Fatalf("unrelated: set lock_timeout: %v", err)
	}
	if _, err := unrelated.Exec(ctx, `
INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, completed_at)
VALUES ($1, $2, $3, 'completed', now())
`, f.neighbourAgent, f.neighbourIssue, f.neighbourRuntime); err != nil {
		t.Errorf("neighbour-only enqueue was blocked by the victim's teardown locks: %v", err)
	}
}
