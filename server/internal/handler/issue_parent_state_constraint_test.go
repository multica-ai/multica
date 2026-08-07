package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	parentHasIncompleteDescendantsCode = "parent_has_incomplete_descendants"
	parentMustBeReopenedCode           = "parent_must_be_reopened"
)

type parentStateConflictResponse struct {
	Code                      string `json:"code"`
	Error                     string `json:"error"`
	ParentIssueID             string `json:"parent_issue_id"`
	IncompleteDescendantCount *int   `json:"incomplete_descendant_count,omitempty"`
}

func createParentStateIssue(t *testing.T, title, status, parentID string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	body := map[string]any{
		"title":  title + " " + time.Now().Format(time.RFC3339Nano),
		"status": status,
	}
	if parentID != "" {
		body["parent_issue_id"] = parentID
	}
	testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode created %q: %v", title, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

func updateParentStateIssue(t *testing.T, issueID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID)
	testHandler.UpdateIssue(w, req)
	return w
}

func decodeParentStateConflict(t *testing.T, w *httptest.ResponseRecorder, wantCode, wantParentID string) parentStateConflictResponse {
	t.Helper()
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 %s, got %d: %s", wantCode, w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw conflict: %v", err)
	}
	for key := range raw {
		switch key {
		case "code", "error", "parent_issue_id", "incomplete_descendant_count":
		default:
			t.Fatalf("conflict response exposed unexpected field %q: %s", key, w.Body.String())
		}
	}
	var body parentStateConflictResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if body.Code != wantCode {
		t.Fatalf("conflict code = %q, want %q: %#v", body.Code, wantCode, body)
	}
	if body.ParentIssueID != wantParentID {
		t.Fatalf("conflict parent_issue_id = %q, want %q: %#v", body.ParentIssueID, wantParentID, body)
	}
	return body
}

// TestParentStateConstraintBatchIsAtomicAndOrdersTreeTransitions proves that
// an invalid selected item rolls back earlier selected writes, while a valid
// parent-and-child completion succeeds regardless of request order.
func TestParentStateConstraintBatchIsAtomicAndOrdersTreeTransitions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	parent := createParentStateIssue(t, "parent-state batch order parent", "in_review", "")
	child := createParentStateIssue(t, "parent-state batch order child", "in_review", parent.ID)
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		// The parent deliberately appears first. The handler must perform the
		// terminal transition leaf-first inside its one transaction.
		"issue_ids": []string{parent.ID, child.ID},
		"updates":   map[string]any{"status": "done"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("ordered batch completion: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := parentStateStatus(t, parent.ID); got != "done" {
		t.Fatalf("ordered batch parent status = %q, want done", got)
	}
	if got := parentStateStatus(t, child.ID); got != "done" {
		t.Fatalf("ordered batch child status = %q, want done", got)
	}

	parentWithStaleSelection := createParentStateIssue(t, "parent-state batch stale parent", "in_review", "")
	childWithStaleSelection := createParentStateIssue(t, "parent-state batch stale child", "in_review", parentWithStaleSelection.ID)
	w = httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		// A stale selection in the middle must not stop the known child from
		// being ordered before its parent. The stale ID remains skipped under
		// the endpoint's existing batch contract.
		"issue_ids": []string{parentWithStaleSelection.ID, "00000000-0000-0000-0000-000000000001", childWithStaleSelection.ID},
		"updates":   map[string]any{"status": "done"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("ordered batch with stale selection: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := parentStateStatus(t, parentWithStaleSelection.ID); got != "done" {
		t.Fatalf("ordered stale batch parent status = %q, want done", got)
	}
	if got := parentStateStatus(t, childWithStaleSelection.ID); got != "done" {
		t.Fatalf("ordered stale batch child status = %q, want done", got)
	}

	completedParent := createParentStateIssue(t, "parent-state batch rollback parent", "in_review", "")
	terminalChild := createParentStateIssue(t, "parent-state batch rollback child", "done", completedParent.ID)
	if w := updateParentStateIssue(t, completedParent.ID, map[string]any{"status": "done"}); w.Code != http.StatusOK {
		t.Fatalf("finish rollback parent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	unrelated := createParentStateIssue(t, "parent-state batch rollback unrelated", "done", "")
	w = httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{unrelated.ID, terminalChild.ID},
		"updates":   map[string]any{"status": "todo"},
	}))
	decodeParentStateConflict(t, w, parentMustBeReopenedCode, completedParent.ID)
	if got := parentStateStatus(t, unrelated.ID); got != "done" {
		t.Fatalf("conflicted batch partially changed unrelated issue to %q, want done", got)
	}
	if got := parentStateStatus(t, terminalChild.ID); got != "done" {
		t.Fatalf("conflicted batch changed terminal child to %q, want done", got)
	}
}

func parentStateStatus(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read issue %s status: %v", issueID, err)
	}
	return status
}

// TestParentStateConstraintRejectsTerminalParentWithIncompleteDescendant
// proves the user-visible core rule and that the rejected request does not
// partially change the parent row. The grandchild makes this a recursive,
// rather than direct-child-only, regression test.
func TestParentStateConstraintRejectsTerminalParentWithIncompleteDescendant(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	parent := createParentStateIssue(t, "parent-state recursive parent", "in_review", "")
	child := createParentStateIssue(t, "parent-state recursive child", "in_review", parent.ID)
	grandchild := createParentStateIssue(t, "parent-state recursive grandchild", "todo", child.ID)

	var commentCountBefore int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM comment WHERE issue_id = $1`, parent.ID).Scan(&commentCountBefore); err != nil {
		t.Fatalf("count parent comments before rejected completion: %v", err)
	}
	var eventMu sync.Mutex
	parentUpdateEvents := 0
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(IssueResponse)
		if ok && issue.ID == parent.ID {
			eventMu.Lock()
			parentUpdateEvents++
			eventMu.Unlock()
		}
	})

	conflict := decodeParentStateConflict(t,
		updateParentStateIssue(t, parent.ID, map[string]any{"status": "done"}),
		parentHasIncompleteDescendantsCode,
		parent.ID,
	)
	if conflict.IncompleteDescendantCount == nil || *conflict.IncompleteDescendantCount != 2 {
		t.Fatalf("incomplete descendant count = %#v, want 2", conflict.IncompleteDescendantCount)
	}
	if got := parentStateStatus(t, parent.ID); got != "in_review" {
		t.Fatalf("rejected parent update changed status to %q, want in_review", got)
	}
	var commentCountAfter int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM comment WHERE issue_id = $1`, parent.ID).Scan(&commentCountAfter); err != nil {
		t.Fatalf("count parent comments after rejected completion: %v", err)
	}
	if commentCountAfter != commentCountBefore {
		t.Fatalf("rejected parent completion changed comment count from %d to %d", commentCountBefore, commentCountAfter)
	}
	eventMu.Lock()
	rejectedParentUpdateEvents := parentUpdateEvents
	eventMu.Unlock()
	if rejectedParentUpdateEvents != 0 {
		t.Fatalf("rejected parent completion emitted %d issue update event(s)", rejectedParentUpdateEvents)
	}

	if w := updateParentStateIssue(t, grandchild.ID, map[string]any{"status": "done"}); w.Code != http.StatusOK {
		t.Fatalf("finish grandchild: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := updateParentStateIssue(t, child.ID, map[string]any{"status": "done"}); w.Code != http.StatusOK {
		t.Fatalf("finish child: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := updateParentStateIssue(t, parent.ID, map[string]any{"status": "done"}); w.Code != http.StatusOK {
		t.Fatalf("finish parent after terminal descendants: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestParentStateConstraintRejectsActiveChildMutationUnderDoneParent covers
// create, reparent, and reopen. An explicit parent move to Review
// (in_review) permits work again; the server never silently reopens it.
func TestParentStateConstraintRejectsActiveChildMutationUnderDoneParent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	parent := createParentStateIssue(t, "parent-state completed parent", "in_review", "")
	if w := updateParentStateIssue(t, parent.ID, map[string]any{"status": "done"}); w.Code != http.StatusOK {
		t.Fatalf("finish parent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "parent-state blocked create " + time.Now().Format(time.RFC3339Nano),
		"status":          "todo",
		"parent_issue_id": parent.ID,
	}))
	decodeParentStateConflict(t, w, parentMustBeReopenedCode, parent.ID)

	terminalChild := createParentStateIssue(t, "parent-state terminal child", "done", parent.ID)
	root := createParentStateIssue(t, "parent-state reparent source", "todo", "")
	decodeParentStateConflict(t,
		updateParentStateIssue(t, root.ID, map[string]any{"parent_issue_id": parent.ID}),
		parentMustBeReopenedCode,
		parent.ID,
	)
	if got := parentStateStatus(t, root.ID); got != "todo" {
		t.Fatalf("rejected reparent changed source status to %q, want todo", got)
	}
	decodeParentStateConflict(t,
		updateParentStateIssue(t, terminalChild.ID, map[string]any{"status": "todo"}),
		parentMustBeReopenedCode,
		parent.ID,
	)
	if got := parentStateStatus(t, terminalChild.ID); got != "done" {
		t.Fatalf("rejected reopen changed child status to %q, want done", got)
	}

	if w := updateParentStateIssue(t, parent.ID, map[string]any{"status": "in_review"}); w.Code != http.StatusOK {
		t.Fatalf("explicit parent reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := updateParentStateIssue(t, root.ID, map[string]any{"parent_issue_id": parent.ID}); w.Code != http.StatusOK {
		t.Fatalf("reparent after explicit reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := updateParentStateIssue(t, terminalChild.ID, map[string]any{"status": "todo"}); w.Code != http.StatusOK {
		t.Fatalf("reopen child after explicit parent reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestParentStateConstraintRejectsBatchAndDirectWriters ensures neither the
// public batch endpoint nor a non-HTTP writer can bypass the same invariant.
func TestParentStateConstraintRejectsBatchAndDirectWriters(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	parent := createParentStateIssue(t, "parent-state batch parent", "in_review", "")
	child := createParentStateIssue(t, "parent-state batch child", "done", parent.ID)
	if w := updateParentStateIssue(t, parent.ID, map[string]any{"status": "done"}); w.Code != http.StatusOK {
		t.Fatalf("finish parent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{child.ID},
		"updates":   map[string]any{"status": "todo"},
	}))
	decodeParentStateConflict(t, w, parentMustBeReopenedCode, parent.ID)
	if got := parentStateStatus(t, child.ID); got != "done" {
		t.Fatalf("rejected batch reopen changed child status to %q, want done", got)
	}

	_, err := testPool.Exec(context.Background(), `UPDATE issue SET status = 'todo' WHERE id = $1`, child.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Message != parentMustBeReopenedCode {
		t.Fatalf("direct writer error = %v, want PostgreSQL %q", err, parentMustBeReopenedCode)
	}
	if got := parentStateStatus(t, child.ID); got != "done" {
		t.Fatalf("rejected direct reopen changed child status to %q, want done", got)
	}

	_, err = testPool.Exec(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			parent_issue_id, position, number
		)
		SELECT $1, $2, 'todo', 'none', 'member', $3, $4, 0,
		       COALESCE(MAX(number), 0) + 1
		FROM issue
		WHERE workspace_id = $1
		GROUP BY $1
	`, testWorkspaceID, "parent-state direct insert "+time.Now().Format(time.RFC3339Nano), testUserID, parent.ID)
	if !errors.As(err, &pgErr) || pgErr.Message != parentMustBeReopenedCode {
		t.Fatalf("direct insert error = %v, want PostgreSQL %q", err, parentMustBeReopenedCode)
	}
}

// TestParentStateConstraintSerializesConcurrentParentDoneAndChildReopen is a
// two-connection regression test. One request can win, but the final tree
// cannot contain a done parent and an active child.
func TestParentStateConstraintSerializesConcurrentParentDoneAndChildReopen(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	parent := createParentStateIssue(t, "parent-state concurrent parent", "in_review", "")
	child := createParentStateIssue(t, "parent-state concurrent child", "done", parent.ID)

	ctx := context.Background()
	parentConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire parent connection: %v", err)
	}
	defer parentConn.Release()
	childConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire child connection: %v", err)
	}
	defer childConn.Release()

	parentTx, err := parentConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin parent transaction: %v", err)
	}
	defer parentTx.Rollback(ctx)
	if _, err := parentTx.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, parent.ID); err != nil {
		t.Fatalf("parent completion setup: %v", err)
	}

	childResult := make(chan error, 1)
	go func() {
		tx, err := childConn.Begin(ctx)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE issue SET status = 'todo' WHERE id = $1`, child.ID)
			if err == nil {
				err = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
		}
		childResult <- err
	}()

	select {
	case err := <-childResult:
		t.Fatalf("child reopen completed before parent transaction committed: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	if err := parentTx.Commit(ctx); err != nil {
		t.Fatalf("commit parent completion: %v", err)
	}
	if err := <-childResult; err == nil {
		t.Fatal("child reopen succeeded after concurrent parent completion")
	}
	if got := parentStateStatus(t, parent.ID); got != "done" {
		t.Fatalf("parent status = %q, want done", got)
	}
	if got := parentStateStatus(t, child.ID); got != "done" {
		t.Fatalf("child status = %q, want done", got)
	}
}

// TestParentStateConstraintRechecksAncestorLocksAfterConcurrentReparent
// exercises the stale-lock-set race: while a descendant activation waits on a
// reparent, the trigger must discover and lock the newly committed ancestor
// before it validates. A third transaction must therefore be unable to take
// that new ancestor's advisory lock while the first update is still open.
func TestParentStateConstraintRechecksAncestorLocksAfterConcurrentReparent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	parent := createParentStateIssue(t, "parent-state recheck old parent", "in_review", "")
	child := createParentStateIssue(t, "parent-state recheck child", "in_review", parent.ID)
	grandchild := createParentStateIssue(t, "parent-state recheck grandchild", "done", child.ID)
	newParent := createParentStateIssue(t, "parent-state recheck new parent", "in_review", "")
	ctx := context.Background()

	reparentConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire reparent connection: %v", err)
	}
	defer reparentConn.Release()
	activateConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire activation connection: %v", err)
	}
	defer activateConn.Release()
	probeConn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire probe connection: %v", err)
	}
	defer probeConn.Release()

	reparentTx, err := reparentConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin reparent transaction: %v", err)
	}
	defer reparentTx.Rollback(ctx)
	if _, err := reparentTx.Exec(ctx, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, newParent.ID, child.ID); err != nil {
		t.Fatalf("reparent child setup: %v", err)
	}

	activateTx, err := activateConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin activation transaction: %v", err)
	}
	defer activateTx.Rollback(ctx)
	activateResult := make(chan error, 1)
	go func() {
		_, err := activateTx.Exec(ctx, `UPDATE issue SET status = 'todo' WHERE id = $1`, grandchild.ID)
		activateResult <- err
	}()
	select {
	case err := <-activateResult:
		t.Fatalf("activation completed before reparent committed: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	if err := reparentTx.Commit(ctx); err != nil {
		t.Fatalf("commit reparent: %v", err)
	}
	if err := <-activateResult; err != nil {
		t.Fatalf("activation after legal reparent: %v", err)
	}

	probeTx, err := probeConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin advisory-lock probe: %v", err)
	}
	defer probeTx.Rollback(ctx)
	var acquired bool
	if err := probeTx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1::text, 0))`, newParent.ID).Scan(&acquired); err != nil {
		t.Fatalf("probe new ancestor lock: %v", err)
	}
	if acquired {
		t.Fatal("activation did not retain the newly reparented ancestor lock")
	}
}
