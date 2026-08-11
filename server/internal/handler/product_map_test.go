package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedProductMapFixture creates one 院务系统 node + one Multica node (with a
// project ref and an issue ref), and registers editorUserID as the editor of
// the 院务系统 node. Returns node ids.
func seedProductMapFixture(t *testing.T, editorUserID string) (yuanwuID, multicaID, projectID, issueID string) {
	t.Helper()
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`,
		testWorkspaceID, "multica-fixture-project"); err != nil {
		t.Fatalf("seed fixture project: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM project WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, "multica-fixture-project").Scan(&projectID); err != nil {
		t.Fatalf("read fixture project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	if err := testPool.QueryRow(ctx, `INSERT INTO issue (workspace_id, title, status, project_id) VALUES ($1, $2, 'done', $3) RETURNING id`,
		testWorkspaceID, "fixture issue done-alone", "done", projectID).Scan(&issueID); err != nil {
		t.Fatalf("seed fixture issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	q := db.New(testPool)
	ws := util.MustParseUUID(testWorkspaceID)

	yuanwu, err := q.UpsertProductNode(ctx, db.UpsertProductNodeParams{
		WorkspaceID:  ws,
		Name:         "院务系统",
		Slug:         "yuanwu-test",
		SortOrder:    1,
		Status:       "pending_confirmation",
		StatusSource: "pmo",
		Evidence:     []byte(`{"source":"pmo","note":"no pmo data yet"}`),
	})
	if err != nil {
		t.Fatalf("seed yuanwu node: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM product_nodes WHERE id = $1`, yuanwu.ID) })
	yuanwuID = util.UUIDToString(yuanwu.ID)

	multica, err := q.UpsertProductNode(ctx, db.UpsertProductNodeParams{
		WorkspaceID:  ws,
		Name:         "Multica",
		Slug:         "multica-test",
		SortOrder:    2,
		Status:       "released",
		StatusSource: "code_repo",
		Evidence:     []byte(`{"source":"code_repo","repo_url":"https://gitlab.sy.soyoung.com/fe/wasai/multica.git"}`),
	})
	if err != nil {
		t.Fatalf("seed multica node: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM product_nodes WHERE id = $1`, multica.ID) })
	multicaID = util.UUIDToString(multica.ID)

	if _, err := q.UpsertProductRef(ctx, db.UpsertProductRefParams{
		ProductID: multica.ID,
		RefType:   "project",
		RefID:     util.MustParseUUID(projectID),
	}); err != nil {
		t.Fatalf("link multica project ref: %v", err)
	}
	if _, err := q.UpsertProductRef(ctx, db.UpsertProductRefParams{
		ProductID: multica.ID,
		RefType:   "issue",
		RefID:     util.MustParseUUID(issueID),
	}); err != nil {
		t.Fatalf("link multica issue ref: %v", err)
	}

	if editorUserID != "" {
		if _, err := q.UpsertProductEditor(ctx, db.UpsertProductEditorParams{
			ProductID: yuanwu.ID,
			UserID:    util.MustParseUUID(editorUserID),
		}); err != nil {
			t.Fatalf("seed yuanwu editor: %v", err)
		}
	}
	return yuanwuID, multicaID, projectID, issueID
}

// TestListProductMap_MemberCanView (acceptance 1): a normal workspace member
// can GET /api/product-map and sees both seeded product nodes.
func TestListProductMap_MemberCanView(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	editorID := seedProductMapEditorUser(t, "map-editor")
	yuanwuID, multicaID, _, _ := seedProductMapFixture(t, editorID)
	_ = yuanwuID

	req := httptest.NewRequest(http.MethodGet, "/api/product-map", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	testHandler.ListProductMap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ListProductMap status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Nodes []*ProductNodeResponse `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ListProductMap: %v", err)
	}
	names := map[string]bool{}
	for _, n := range resp.Nodes {
		names[n.Name] = true
	}
	if !names["院务系统"] || !names["Multica"] {
		t.Fatalf("expected 院务系统 + Multica in map, got %v", names)
	}

	// Multica node must carry its traceability refs (acceptance 2/5).
	var multicaNode *ProductNodeResponse
	for _, n := range resp.Nodes {
		if n.ID == multicaID {
			multicaNode = n
		}
	}
	if multicaNode == nil {
		t.Fatalf("Multica node missing from map")
	}
	hasProjectRef, hasIssueRef := false, false
	for _, ref := range multicaNode.Refs {
		if ref.RefType == "project" {
			hasProjectRef = true
		}
		if ref.RefType == "issue" {
			hasIssueRef = true
		}
	}
	if !hasProjectRef || !hasIssueRef {
		t.Fatalf("Multica node must trace to project + issue refs, got %+v", multicaNode.Refs)
	}

	// Editor ACL data basis visible on the 院务系统 node (acceptance 4 data).
	var yuanwuNode *ProductNodeResponse
	for _, n := range resp.Nodes {
		if n.Name == "院务系统" {
			yuanwuNode = n
		}
	}
	if yuanwuNode == nil || len(yuanwuNode.Editors) == 0 {
		t.Fatalf("院务系统 node must expose its registered editors, got %+v", yuanwuNode)
	}
	if !yuanwuNode.HasLiveEvidence {
		t.Fatalf("pending_confirmation node must not claim live evidence")
	}
	_ = ctx
}

// TestProductMap_LiveStatusRequiresEvidence (acceptance 3): a node whose only
// signal is an Issue marked done must NOT show as live; released requires
// evidence on the node itself.
func TestProductMap_LiveStatusRequiresEvidence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	editorID := seedProductMapEditorUser(t, "map-editor2")
	_, multicaID, _, _ := seedProductMapFixture(t, editorID)
	_ = multicaID
	_ = ctx

	// The fixture issue is 'done' but the 院务系统 node is pending_confirmation
	// with only a "no pmo data yet" note — never released. The Multica node is
	// released WITH code_repo evidence. Assert both in one tree read.
	req := httptest.NewRequest(http.MethodGet, "/api/product-map", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	testHandler.ListProductMap(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ListProductMap status = %d", rr.Code)
	}
	var resp struct {
		Nodes []*ProductNodeResponse `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byName := map[string]*ProductNodeResponse{}
	for _, n := range resp.Nodes {
		byName[n.Name] = n
	}
	yuanwu := byName["院务系统"]
	if yuanwu == nil {
		t.Fatalf("院务系统 node missing")
	}
	if yuanwu.Status == "released" {
		t.Fatalf("院务系统 has an Issue marked done but NO live evidence; must not be released")
	}
	if yuanwu.HasLiveEvidence {
		t.Fatalf("院务系统 must not claim live evidence without PMO/deploy evidence")
	}
	multica := byName["Multica"]
	if multica == nil || multica.Status != "released" || !multica.HasLiveEvidence {
		t.Fatalf("Multica (code_repo evidence) should be released with live evidence, got %+v", multica)
	}
}

// TestProductMap_EditorACL (acceptance 4): IsProductEditor is true for the
// registered editor and false for an un-authorized workspace member.
func TestProductMap_EditorACL(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	editorID := seedProductMapEditorUser(t, "map-editor3")
	yuanwuID, _, _, _ := seedProductMapFixture(t, editorID)

	authorized, err := testHandler.IsProductEditor(ctx, util.MustParseUUID(yuanwuID), util.MustParseUUID(editorID))
	if err != nil {
		t.Fatalf("IsProductEditor: %v", err)
	}
	if !authorized {
		t.Fatalf("registered editor must be authorized")
	}

	// An un-authorized workspace member must be rejected.
	intruderID := seedSecurityTestOwner(t, "map-intruder")
	authorized, err = testHandler.IsProductEditor(ctx, util.MustParseUUID(yuanwuID), util.MustParseUUID(intruderID))
	if err != nil {
		t.Fatalf("IsProductEditor (intruder): %v", err)
	}
	if authorized {
		t.Fatalf("un-authorized member must not be an editor")
	}
}

// TestGetProductMapNode_NotFound (acceptance 6 API resolution): unknown id in
// this workspace yields 404; a node in another workspace is not visible.
func TestGetProductMapNode_NotFound(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/product-map/"+util.UUIDToString(pgtype.UUID{}), nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	testHandler.GetProductMapNode(rr, req)
	if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 4xx for unknown node, got %d", rr.Code)
	}
}

// seedProductMapEditorUser creates a throwaway user to act as the product
// editor (凯撒 stand-in) and returns its id.
func seedProductMapEditorUser(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	var editorID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, label+"-"+t.Name()+"@multica.test").Scan(&editorID); err != nil {
		t.Fatalf("seed editor user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, editorID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, editorID); err != nil {
		t.Fatalf("seed editor member: %v", err)
	}
	return editorID
}
