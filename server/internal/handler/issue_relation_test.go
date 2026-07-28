package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func relAdd(t *testing.T, sourceID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+sourceID+"/relations", body)
	req = withURLParam(req, "id", sourceID)
	testHandler.AddIssueRelation(w, req)
	return w
}

func relAddOK(t *testing.T, sourceID, relType, targetID string) IssueRelationResponse {
	t.Helper()
	w := relAdd(t, sourceID, map[string]any{"type": relType, "target_issue_id": targetID})
	if w.Code != http.StatusCreated {
		t.Fatalf("AddIssueRelation(%s %s->%s): want 201, got %d: %s", relType, sourceID, targetID, w.Code, w.Body.String())
	}
	var resp struct {
		Relation IssueRelationResponse `json:"relation"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode relation: %v", err)
	}
	return resp.Relation
}

func relList(t *testing.T, issueID string) []IssueRelationResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/relations", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListIssueRelations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueRelations(%s): want 200, got %d: %s", issueID, w.Code, w.Body.String())
	}
	var resp struct {
		Relations []IssueRelationResponse `json:"relations"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode relations: %v", err)
	}
	return resp.Relations
}

func relRemove(t *testing.T, issueID, relationID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+issueID+"/relations/"+relationID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	rctx.URLParams.Add("relationId", relationID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.RemoveIssueRelation(w, req)
	return w
}

func countRelationsTouching(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue_relation WHERE source_issue_id = $1 OR target_issue_id = $1`, issueID).Scan(&n); err != nil {
		t.Fatalf("count relations: %v", err)
	}
	return n
}

// insertRelIssue inserts an issue directly into an arbitrary workspace with no
// assignee. Used for cross-workspace and workspace-teardown fixtures.
func insertRelIssue(t *testing.T, workspaceID, title string) string {
	t.Helper()
	ctx := context.Background()
	var number int32
	if err := testPool.QueryRow(ctx,
		`UPDATE workspace SET issue_counter = issue_counter + 1 WHERE id = $1 RETURNING issue_counter`,
		workspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4)
		RETURNING id`, workspaceID, title, testUserID, number).Scan(&id); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	return id
}

// createRelWorkspace makes a throwaway workspace (deleted on cleanup) for
// cross-workspace and teardown tests. slug must be unique per caller.
func createRelWorkspace(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	var id string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, '', 'REL') RETURNING id`,
		"Rel WS "+slug, slug).Scan(&id); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		// issue_relation has no FK, so clear it explicitly before the cascade delete.
		testPool.Exec(context.Background(), `DELETE FROM issue_relation WHERE workspace_id = $1`, id)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAddAndListBlocksRelation(t *testing.T) {
	a := createTestIssue(t, "rel blocks A", "todo", "none")
	b := createTestIssue(t, "rel blocks B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b) })

	rel := relAddOK(t, a, "blocks", b)
	if rel.SourceIssueID != a || rel.TargetIssueID != b || rel.Type != "blocks" {
		t.Fatalf("unexpected relation: %+v", rel)
	}
	// created_by is populated from the authenticated actor, not the request.
	if rel.CreatedByType == nil || *rel.CreatedByType != "member" || rel.CreatedByID == nil || *rel.CreatedByID != testUserID {
		t.Fatalf("expected created_by member/%s, got %+v / %+v", testUserID, rel.CreatedByType, rel.CreatedByID)
	}

	// Both endpoints see the same canonical edge (the "blocked by" view on B is
	// derived by the client from being the target).
	if got := relList(t, a); len(got) != 1 || got[0].Type != "blocks" || got[0].SourceIssueID != a {
		t.Fatalf("source list wrong: %+v", got)
	}
	if got := relList(t, b); len(got) != 1 || got[0].SourceIssueID != a || got[0].TargetIssueID != b {
		t.Fatalf("target list wrong: %+v", got)
	}
}

func TestAddRelationSelfRejected(t *testing.T) {
	a := createTestIssue(t, "rel self", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	if w := relAdd(t, a, map[string]any{"type": "blocks", "target_issue_id": a}); w.Code != http.StatusBadRequest {
		t.Fatalf("self relation: want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddRelationCrossWorkspaceRejected(t *testing.T) {
	a := createTestIssue(t, "rel local", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	foreignWS := createRelWorkspace(t, "rel-foreign-target")
	foreignIssue := insertRelIssue(t, foreignWS, "foreign target")

	if w := relAdd(t, a, map[string]any{"type": "blocks", "target_issue_id": foreignIssue}); w.Code != http.StatusBadRequest {
		t.Fatalf("cross-workspace target: want 400, got %d: %s", w.Code, w.Body.String())
	}
	if n := countRelationsTouching(t, a); n != 0 {
		t.Fatalf("cross-workspace add should not persist, got %d rows", n)
	}
}

func TestAddRelationDuplicateRejected(t *testing.T) {
	a := createTestIssue(t, "rel dup A", "todo", "none")
	b := createTestIssue(t, "rel dup B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b) })

	relAddOK(t, a, "blocks", b)
	if w := relAdd(t, a, map[string]any{"type": "blocks", "target_issue_id": b}); w.Code != http.StatusConflict {
		t.Fatalf("duplicate: want 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRelatedCanonicalizationDedupes(t *testing.T) {
	a := createTestIssue(t, "rel related A", "todo", "none")
	b := createTestIssue(t, "rel related B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b) })

	rel := relAddOK(t, a, "related", b)
	// Stored canonically: source < target regardless of request direction.
	if !(rel.SourceIssueID < rel.TargetIssueID) {
		t.Fatalf("related edge not canonical: %s -> %s", rel.SourceIssueID, rel.TargetIssueID)
	}
	// Adding the reverse (B related A) must collapse onto the same row -> 409.
	if w := relAdd(t, b, map[string]any{"type": "related", "target_issue_id": a}); w.Code != http.StatusConflict {
		t.Fatalf("reverse related: want 409, got %d: %s", w.Code, w.Body.String())
	}
	if n := countRelationsTouching(t, a); n != 1 {
		t.Fatalf("expected 1 related row after reverse add, got %d", n)
	}
}

func TestBlocksCycleRejected(t *testing.T) {
	a := createTestIssue(t, "cycle A", "todo", "none")
	b := createTestIssue(t, "cycle B", "todo", "none")
	c := createTestIssue(t, "cycle C", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b); deleteTestIssue(t, c) })

	// Direct 2-cycle: A blocks B, then B blocks A must be refused.
	relAddOK(t, a, "blocks", b)
	if w := relAdd(t, b, map[string]any{"type": "blocks", "target_issue_id": a}); w.Code != http.StatusConflict {
		t.Fatalf("2-cycle: want 409, got %d: %s", w.Code, w.Body.String())
	}
	// Transitive cycle: B blocks C, then C blocks A closes A->B->C->A.
	relAddOK(t, b, "blocks", c)
	if w := relAdd(t, c, map[string]any{"type": "blocks", "target_issue_id": a}); w.Code != http.StatusConflict {
		t.Fatalf("transitive cycle: want 409, got %d: %s", w.Code, w.Body.String())
	}
	// A "related" edge in the reverse direction is fine — only blocks form a DAG.
	if w := relAdd(t, c, map[string]any{"type": "related", "target_issue_id": a}); w.Code != http.StatusCreated {
		t.Fatalf("related despite blocks path: want 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRemoveRelation(t *testing.T) {
	a := createTestIssue(t, "rm A", "todo", "none")
	b := createTestIssue(t, "rm B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b) })

	rel := relAddOK(t, a, "blocks", b)
	if w := relRemove(t, a, rel.ID); w.Code != http.StatusNoContent {
		t.Fatalf("remove: want 204, got %d: %s", w.Code, w.Body.String())
	}
	if got := relList(t, a); len(got) != 0 {
		t.Fatalf("expected no relations after remove, got %+v", got)
	}
	// Removing again 404s.
	if w := relRemove(t, a, rel.ID); w.Code != http.StatusNotFound {
		t.Fatalf("double remove: want 404, got %d", w.Code)
	}
	// Removing via an unrelated issue path 404s even though the relation exists.
	rel2 := relAddOK(t, a, "blocks", b)
	other := createTestIssue(t, "rm other", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, other) })
	if w := relRemove(t, other, rel2.ID); w.Code != http.StatusNotFound {
		t.Fatalf("mismatched path remove: want 404, got %d", w.Code)
	}
}

func TestDeleteIssueRemovesRelations(t *testing.T) {
	a := createTestIssue(t, "del A", "todo", "none")
	b := createTestIssue(t, "del B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, b) })

	relAddOK(t, a, "blocks", b)
	deleteTestIssue(t, a)

	if n := countRelationsTouching(t, a); n != 0 {
		t.Fatalf("expected relations cleaned up on issue delete, got %d orphans", n)
	}
	if got := relList(t, b); len(got) != 0 {
		t.Fatalf("counterpart still lists a relation to a deleted issue: %+v", got)
	}
}

func TestWorkspaceDeleteRemovesRelations(t *testing.T) {
	ws := createRelWorkspace(t, "rel-ws-teardown")
	a := insertRelIssue(t, ws, "ws del A")
	b := insertRelIssue(t, ws, "ws del B")
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO issue_relation (workspace_id, source_issue_id, target_issue_id, type) VALUES ($1, $2, $3, 'blocks')`,
		ws, a, b); err != nil {
		t.Fatalf("seed relation: %v", err)
	}

	if err := testHandler.Queries.DeleteWorkspace(context.Background(), parseUUID(ws)); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue_relation WHERE workspace_id = $1`, ws).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("workspace delete left %d relation rows", n)
	}
}

func TestListRelationsForIssuesBatched(t *testing.T) {
	a := createTestIssue(t, "batch A", "todo", "none")
	b := createTestIssue(t, "batch B", "todo", "none")
	c := createTestIssue(t, "batch C", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b); deleteTestIssue(t, c) })
	relAddOK(t, a, "blocks", b)
	relAddOK(t, b, "blocks", c)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/relations?workspace_id="+testWorkspaceID+"&issue_ids="+a+","+c, nil)
	testHandler.ListRelationsForIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batched list: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Relations []IssueRelationResponse `json:"relations"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	// a touches 1 edge (a->b), c touches 1 edge (b->c); both surface.
	if len(resp.Relations) != 2 {
		t.Fatalf("expected 2 edges for {a,c}, got %d: %+v", len(resp.Relations), resp.Relations)
	}
}

func TestAddRelationMalformed(t *testing.T) {
	a := createTestIssue(t, "malformed A", "todo", "none")
	b := createTestIssue(t, "malformed B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b) })

	cases := []struct {
		name string
		body any
	}{
		{"unknown type", map[string]any{"type": "duplicate", "target_issue_id": b}},
		{"blocked_by not storable", map[string]any{"type": "blocked_by", "target_issue_id": b}},
		{"empty type", map[string]any{"type": "", "target_issue_id": b}},
		{"missing target", map[string]any{"type": "blocks"}},
		{"bad target uuid", map[string]any{"type": "blocks", "target_issue_id": "not-a-uuid"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := relAdd(t, a, tc.body); w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	// Non-JSON body.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+a+"/relations", nil)
	req.Body = http.NoBody
	req = withURLParam(req, "id", a)
	testHandler.AddIssueRelation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty body: want 400, got %d", w.Code)
	}
}

// TestConcurrentBlocksCycleAllowsExactlyOne exercises the workspace advisory
// lock: two requests racing to add A->B and B->A must not both commit (that
// would be a 2-cycle). Exactly one succeeds; the other is refused. Looped with
// fresh issues so the race window is hit repeatedly — if the advisory lock were
// removed, some iteration would commit both edges and fail here.
func TestConcurrentBlocksCycleAllowsExactlyOne(t *testing.T) {
	for iter := 0; iter < 15; iter++ {
		a := createTestIssue(t, fmt.Sprintf("conc A %d", iter), "todo", "none")
		b := createTestIssue(t, fmt.Sprintf("conc B %d", iter), "todo", "none")

		var wg sync.WaitGroup
		codes := make([]int, 2)
		wg.Add(2)
		go func() { defer wg.Done(); codes[0] = relAdd(t, a, map[string]any{"type": "blocks", "target_issue_id": b}).Code }()
		go func() { defer wg.Done(); codes[1] = relAdd(t, b, map[string]any{"type": "blocks", "target_issue_id": a}).Code }()
		wg.Wait()

		created, conflict := 0, 0
		for _, c := range codes {
			switch c {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
				conflict++
			}
		}
		if created != 1 || conflict != 1 {
			t.Fatalf("iter %d: want exactly one 201 and one 409, got %v", iter, codes)
		}
		if n := countRelationsTouching(t, a); n != 1 {
			t.Fatalf("iter %d: want exactly 1 committed edge, got %d", iter, n)
		}
		deleteTestIssue(t, a)
		deleteTestIssue(t, b)
	}
}

// TestConcurrentAddVsDeleteNoOrphan exercises the advisory lock across the add
// and delete paths: adding A->B while A is being deleted must never leave a
// relation pointing at the gone issue. Looped to hit the race window repeatedly.
func TestConcurrentAddVsDeleteNoOrphan(t *testing.T) {
	b := createTestIssue(t, "orphan B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, b) })

	for iter := 0; iter < 10; iter++ {
		a := createTestIssue(t, fmt.Sprintf("orphan A %d", iter), "todo", "none")

		var wg sync.WaitGroup
		var addCode int
		wg.Add(2)
		go func() {
			defer wg.Done()
			addCode = relAdd(t, a, map[string]any{"type": "blocks", "target_issue_id": b}).Code
		}()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/issues/"+a, nil)
			req = withURLParam(req, "id", a)
			testHandler.DeleteIssue(w, req)
		}()
		wg.Wait()

		if n := countRelationsTouching(t, a); n != 0 {
			t.Fatalf("iter %d: orphan relation references deleted issue: %d rows", iter, n)
		}
		// The delete side always wins deletion of A (it either removes A's just-
		// added edge, or A had no edge yet).
		var exists bool
		if err := testPool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM issue WHERE id = $1)`, a).Scan(&exists); err != nil {
			t.Fatalf("iter %d: existence check: %v", iter, err)
		}
		if exists {
			t.Fatalf("iter %d: issue A should have been deleted", iter)
		}
		// However the race resolves, the add must return a sane status — never a 500.
		switch addCode {
		case http.StatusCreated, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		default:
			t.Fatalf("iter %d: add-vs-delete produced an unexpected status %d", iter, addCode)
		}
	}
}

// TestConcurrentAddVsWorkspaceDeleteNoOrphan proves the workspace-delete path
// takes the same advisory lock as the add path, so an add committing alongside a
// whole-workspace teardown can't orphan a relation. Both sides here replicate
// their handlers' lock-then-mutate protocol at the query layer (the DeleteWorkspace
// handler needs full membership context that a throwaway workspace lacks).
func TestConcurrentAddVsWorkspaceDeleteNoOrphan(t *testing.T) {
	ws := createRelWorkspace(t, "rel-ws-race")
	a := insertRelIssue(t, ws, "wsrace A")
	b := insertRelIssue(t, ws, "wsrace B")
	wsUUID := parseUUID(ws)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)
	// Add side: lock -> insert (mirrors AddIssueRelation).
	go func() {
		defer wg.Done()
		tx, err := testHandler.TxStarter.Begin(ctx)
		if err != nil {
			return
		}
		defer tx.Rollback(ctx)
		q := testHandler.Queries.WithTx(tx)
		if err := q.LockWorkspaceRelations(ctx, ws); err != nil {
			return
		}
		if _, err := q.CreateIssueRelation(ctx, db.CreateIssueRelationParams{
			WorkspaceID:   wsUUID,
			SourceIssueID: parseUUID(a),
			TargetIssueID: parseUUID(b),
			Type:          "blocks",
		}); err != nil {
			return
		}
		tx.Commit(ctx)
	}()
	// Delete side: lock -> DeleteWorkspace (mirrors the DeleteWorkspace handler).
	go func() {
		defer wg.Done()
		tx, err := testHandler.TxStarter.Begin(ctx)
		if err != nil {
			return
		}
		defer tx.Rollback(ctx)
		q := testHandler.Queries.WithTx(tx)
		if err := q.LockWorkspaceRelations(ctx, ws); err != nil {
			return
		}
		if err := q.DeleteWorkspace(ctx, wsUUID); err != nil {
			return
		}
		tx.Commit(ctx)
	}()
	wg.Wait()

	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue_relation WHERE workspace_id = $1`, ws).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("orphan relation after add-vs-workspace-delete race: %d rows", n)
	}
}

// ---------------------------------------------------------------------------
// Realtime event contract
// ---------------------------------------------------------------------------

type relationEventCapture struct {
	mu       sync.Mutex
	payloads [][]string
}

// captureRelationEvents records every issue_relations:changed event whose
// issue_ids mention issueID. The bus has no Unsubscribe, so the closure filters
// by id; events from other tests are ignored.
func captureRelationEvents(t *testing.T, issueID string) *relationEventCapture {
	t.Helper()
	c := &relationEventCapture{}
	testHandler.Bus.Subscribe(protocol.EventIssueRelationsChanged, func(e events.Event) {
		m, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		ids, ok := m["issue_ids"].([]string)
		if !ok {
			return
		}
		for _, id := range ids {
			if id == issueID {
				c.mu.Lock()
				c.payloads = append(c.payloads, ids)
				c.mu.Unlock()
				return
			}
		}
	})
	return c
}

func (c *relationEventCapture) snapshot() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]string, len(c.payloads))
	copy(out, c.payloads)
	return out
}

func payloadHasPair(payloads [][]string, a, b string) bool {
	for _, ids := range payloads {
		var haveA, haveB bool
		for _, id := range ids {
			if id == a {
				haveA = true
			}
			if id == b {
				haveB = true
			}
		}
		if haveA && haveB {
			return true
		}
	}
	return false
}

func TestRelationEventsPublished(t *testing.T) {
	a := createTestIssue(t, "evt A", "todo", "none")
	b := createTestIssue(t, "evt B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, b) }) // a is deleted within the test
	// b appears in every relation event here (as endpoint or surviving counterpart).
	cap := captureRelationEvents(t, b)

	// Add publishes both endpoints.
	rel := relAddOK(t, a, "blocks", b)
	if !payloadHasPair(cap.snapshot(), a, b) {
		t.Fatalf("add did not publish issue_relations:changed for both endpoints: %v", cap.snapshot())
	}

	// Explicit remove publishes both endpoints.
	beforeRemove := len(cap.snapshot())
	if w := relRemove(t, a, rel.ID); w.Code != http.StatusNoContent {
		t.Fatalf("remove: %d", w.Code)
	}
	after := cap.snapshot()
	if len(after) <= beforeRemove || !payloadHasPair(after, a, b) {
		t.Fatalf("remove did not publish a relations event for both endpoints: %v", after)
	}

	// Deleting issue A notifies the surviving counterpart B, and excludes the
	// deleted issue from the payload.
	relAddOK(t, a, "blocks", b)
	beforeDelete := len(cap.snapshot())
	deleteTestIssue(t, a)
	final := cap.snapshot()
	if len(final) <= beforeDelete {
		t.Fatalf("issue delete did not publish a relations event for the counterpart")
	}
	last := final[len(final)-1]
	var haveA, haveB bool
	for _, id := range last {
		if id == a {
			haveA = true
		}
		if id == b {
			haveB = true
		}
	}
	if haveA || !haveB {
		t.Fatalf("issue-delete event should carry counterpart %s but not deleted %s: %v", b, a, last)
	}
}

// ---------------------------------------------------------------------------
// Database CHECK invariants (defense in depth, independent of the handler)
// ---------------------------------------------------------------------------

func TestRelationCheckConstraintsRejectRawInserts(t *testing.T) {
	a := createTestIssue(t, "chk A", "todo", "none")
	b := createTestIssue(t, "chk B", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, a); deleteTestIssue(t, b) })
	ctx := context.Background()
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}

	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{
			"self loop",
			`INSERT INTO issue_relation (workspace_id, source_issue_id, target_issue_id, type) VALUES ($1,$2,$2,'blocks')`,
			[]any{testWorkspaceID, a},
		},
		{
			"non-canonical related (source > target)",
			`INSERT INTO issue_relation (workspace_id, source_issue_id, target_issue_id, type) VALUES ($1,$2,$3,'related')`,
			[]any{testWorkspaceID, hi, lo},
		},
		{
			"half-null created_by",
			`INSERT INTO issue_relation (workspace_id, source_issue_id, target_issue_id, type, created_by_type) VALUES ($1,$2,$3,'blocks','member')`,
			[]any{testWorkspaceID, a, b},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testPool.Exec(ctx, tc.sql, tc.args...); err == nil {
				testPool.Exec(ctx, `DELETE FROM issue_relation WHERE workspace_id = $1 AND (source_issue_id = $2 OR target_issue_id = $2)`, testWorkspaceID, a)
				t.Fatalf("expected CHECK constraint to reject the insert, but it succeeded")
			}
		})
	}
}

func TestBatchedRelationsEndpointEdges(t *testing.T) {
	// Too many ids -> 400.
	ids := make([]string, 0, listRelationsByIssuesLimit+1)
	for i := 0; i < listRelationsByIssuesLimit+1; i++ {
		ids = append(ids, "00000000-0000-0000-0000-000000000000")
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/relations?workspace_id="+testWorkspaceID+"&issue_ids="+joinComma(ids), nil)
	testHandler.ListRelationsForIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("too many issue_ids: want 400, got %d", w.Code)
	}

	// Malformed uuid -> 400.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/relations?workspace_id="+testWorkspaceID+"&issue_ids=not-a-uuid", nil)
	testHandler.ListRelationsForIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed issue_ids: want 400, got %d", w.Code)
	}

	// Empty -> 200 with no relations.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/relations?workspace_id="+testWorkspaceID+"&issue_ids=", nil)
	testHandler.ListRelationsForIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty issue_ids: want 200, got %d", w.Code)
	}
}

// TestBatchedRelationsNoCrossWorkspaceLeak pins the SQL workspace filter on the
// bulk endpoint: a relation in another workspace must not surface even when its
// issue ids are supplied. (The handler is called directly here, bypassing the
// membership middleware, so this guards the query filter itself.)
func TestBatchedRelationsNoCrossWorkspaceLeak(t *testing.T) {
	foreignWS := createRelWorkspace(t, "rel-batch-foreign")
	fa := insertRelIssue(t, foreignWS, "foreign batch A")
	fb := insertRelIssue(t, foreignWS, "foreign batch B")
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO issue_relation (workspace_id, source_issue_id, target_issue_id, type) VALUES ($1,$2,$3,'blocks')`,
		foreignWS, fa, fb); err != nil {
		t.Fatalf("seed foreign relation: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/relations?workspace_id="+testWorkspaceID+"&issue_ids="+fa+","+fb, nil)
	testHandler.ListRelationsForIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Relations []IssueRelationResponse `json:"relations"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Relations) != 0 {
		t.Fatalf("cross-workspace relation leaked: %+v", resp.Relations)
	}
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
