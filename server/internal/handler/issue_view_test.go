package handler

import (
	"strings"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

)

func createIssueViewForTest(t *testing.T, body map[string]any) (IssueViewResponse, int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-views", body)
	testHandler.CreateIssueView(w, req)
	var view IssueViewResponse
	if w.Code == http.StatusCreated {
		json.NewDecoder(w.Body).Decode(&view)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM issue_view WHERE id = $1`, view.ID)
		})
	}
	return view, w.Code, w.Body.String()
}

func TestCreateAndListIssueView(t *testing.T) {

	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Needs review",
		"scope_type": "workspace",
		"visibility": "workspace",
		"query":      map[string]any{"statusFilters": []string{"in_review"}},
		"display":    map[string]any{"viewMode": "board"},
	})
	if code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", code, body)
	}
	if view.Revision != 1 || view.DefinitionVersion != 1 {
		t.Fatalf("expected revision=1 definition_version=1, got %d/%d", view.Revision, view.DefinitionVersion)
	}
	if view.Visibility != "workspace" {
		t.Fatalf("expected workspace visibility, got %s", view.Visibility)
	}

	w := httptest.NewRecorder()
	testHandler.ListIssueViews(w, newRequest("GET", "/api/issue-views?scope_type=workspace", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var views []IssueViewResponse
	json.NewDecoder(w.Body).Decode(&views)
	found := false
	for _, v := range views {
		if v.ID == view.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created view %s missing from list", view.ID)
	}
}

func TestMyIssueViewForcedPrivate(t *testing.T) {

	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":          "My assigned",
		"scope_type":    "my",
		"scope_variant": "assigned",
		"visibility":    "workspace", // must be overridden
		"query":         map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	if view.Visibility != "private" {
		t.Fatalf("my view must be forced private, got %s", view.Visibility)
	}
	if view.ScopeVariant == nil || *view.ScopeVariant != "assigned" {
		t.Fatalf("scope_variant not persisted: %+v", view.ScopeVariant)
	}
}

func TestIssueViewPrivateInvisibleToOthers(t *testing.T) {

	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Mine only",
		"scope_type": "workspace",
		"visibility": "private",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}

	// A second member of the same workspace must neither list nor fetch it.
	otherID := createSecondWorkspaceMember(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issue-views?scope_type=workspace", nil)
	req.Header.Set("X-User-ID", otherID)
	testHandler.ListIssueViews(w, req)
	var views []IssueViewResponse
	json.NewDecoder(w.Body).Decode(&views)
	for _, v := range views {
		if v.ID == view.ID {
			t.Fatalf("private view leaked into another member's list")
		}
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-views/"+view.ID, nil)
	req.Header.Set("X-User-ID", otherID)
	req = withURLParam(req, "id", view.ID)
	testHandler.GetIssueViewByID(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for foreign private view, got %d", w.Code)
	}
}

func TestUpdateIssueViewRevisionConflict(t *testing.T) {

	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Conflict target",
		"scope_type": "workspace",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}

	patch := func(rev int32, name string) int {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/issue-views/"+view.ID, map[string]any{
			"name":              name,
			"expected_revision": rev,
		})
		req = withURLParam(req, "id", view.ID)
		testHandler.UpdateIssueView(w, req)
		return w.Code
	}

	if got := patch(1, "First rename"); got != http.StatusOK {
		t.Fatalf("first patch: expected 200, got %d", got)
	}
	// Same expected_revision again → the row is at revision 2 now.
	if got := patch(1, "Stale rename"); got != http.StatusConflict {
		t.Fatalf("stale patch: expected 409, got %d", got)
	}
	if got := patch(2, "Second rename"); got != http.StatusOK {
		t.Fatalf("fresh patch: expected 200, got %d", got)
	}
}

// createSecondWorkspaceMember inserts another user + membership in the test
// workspace and returns the user id.
func createSecondWorkspaceMember(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := fmt.Sprintf("issue-view-other-%s@test.local", t.Name())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (email, name) VALUES ($1, 'View Other')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		ON CONFLICT DO NOTHING
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("create second membership: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func TestWorkspaceIssueViewVariant(t *testing.T) {

	// Save from the Agents tab: variant persists.
	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":          "Agent work",
		"scope_type":    "workspace",
		"scope_variant": "agents",
		"query":         map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	if view.ScopeVariant == nil || *view.ScopeVariant != "agents" {
		t.Fatalf("workspace variant not persisted: %+v", view.ScopeVariant)
	}

	// "all" normalizes to NULL (no restriction).
	view2, code, body := createIssueViewForTest(t, map[string]any{
		"name":          "Everything",
		"scope_type":    "workspace",
		"scope_variant": "all",
		"query":         map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	if view2.ScopeVariant != nil {
		t.Fatalf("'all' must normalize to NULL, got %v", *view2.ScopeVariant)
	}

	// My-only variants are rejected on workspace views.
	_, code, _ = createIssueViewForTest(t, map[string]any{
		"name":          "Bad variant",
		"scope_type":    "workspace",
		"scope_variant": "assigned",
		"query":         map[string]any{},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for my-variant on workspace view, got %d", code)
	}

	// Switching the variant via PATCH works; the scope_type stays fixed.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issue-views/"+view.ID, map[string]any{
		"scope_variant":     "members",
		"expected_revision": 1,
	})
	req = withURLParam(req, "id", view.ID)
	testHandler.UpdateIssueView(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch variant: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated IssueViewResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.ScopeVariant == nil || *updated.ScopeVariant != "members" {
		t.Fatalf("variant switch lost: %+v", updated.ScopeVariant)
	}
}

func TestProjectIssueViewVariant(t *testing.T) {

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "variant project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	// Save from the project page's Members tab: variant persists.
	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":          "Member work",
		"scope_type":    "project",
		"scope_id":      project.ID,
		"scope_variant": "members",
		"query":         map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	if view.ScopeVariant == nil || *view.ScopeVariant != "members" {
		t.Fatalf("project variant not persisted: %+v", view.ScopeVariant)
	}

	// "all" normalizes to NULL, same as workspace views.
	view2, code, body := createIssueViewForTest(t, map[string]any{
		"name":          "Whole project",
		"scope_type":    "project",
		"scope_id":      project.ID,
		"scope_variant": "all",
		"query":         map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	if view2.ScopeVariant != nil {
		t.Fatalf("'all' must normalize to NULL, got %v", *view2.ScopeVariant)
	}

	// My-only variants are rejected on project views.
	_, code, _ = createIssueViewForTest(t, map[string]any{
		"name":          "Bad variant",
		"scope_type":    "project",
		"scope_id":      project.ID,
		"scope_variant": "involved",
		"query":         map[string]any{},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for my-variant on project view, got %d", code)
	}
}

func TestPinIssueView(t *testing.T) {

	shared, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Pinnable",
		"scope_type": "workspace",
		"visibility": "workspace",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	private, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Secret",
		"scope_type": "workspace",
		"visibility": "private",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM pinned_item WHERE item_type = 'view'`)
	})

	pinView := func(userID, viewID string) int {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/pins", map[string]any{
			"item_type": "view",
			"item_id":   viewID,
		})
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		testHandler.CreatePin(w, req)
		return w.Code
	}

	// Shared view: any member can pin it.
	if got := pinView("", shared.ID); got != http.StatusCreated {
		t.Fatalf("pin shared view: expected 201, got %d", got)
	}
	// Own private view: pinnable by its owner.
	if got := pinView("", private.ID); got != http.StatusCreated {
		t.Fatalf("pin own private view: expected 201, got %d", got)
	}
	// Foreign private view: 404 — a pin must not confirm its existence.
	otherID := createSecondWorkspaceMember(t)
	if got := pinView(otherID, private.ID); got != http.StatusNotFound {
		t.Fatalf("pin foreign private view: expected 404, got %d", got)
	}
	// Unknown type still rejected.
	w := httptest.NewRecorder()
	testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
		"item_type": "label",
		"item_id":   shared.ID,
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown item_type, got %d", w.Code)
	}
}

// Old clients (built before saved views) classify every non-issue pin as a
// project pin and permanently auto-unpin it on a 404 — the legacy list
// contract must therefore never expose view pins without the capability
// opt-in.
func TestListPinsHidesViewPinsWithoutOptIn(t *testing.T) {

	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Pinned for compat",
		"scope_type": "workspace",
		"visibility": "workspace",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM pinned_item WHERE item_type = 'view'`)
	})

	w := httptest.NewRecorder()
	testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
		"item_type": "view",
		"item_id":   view.ID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("pin view: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	listPins := func(path string) []PinnedItemResponse {
		rec := httptest.NewRecorder()
		testHandler.ListPins(rec, newRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("list pins: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var pins []PinnedItemResponse
		json.NewDecoder(rec.Body).Decode(&pins)
		return pins
	}

	// Legacy contract: the view pin must be invisible.
	for _, p := range listPins("/api/pins") {
		if p.ItemType == "view" {
			t.Fatalf("view pin leaked into the legacy list contract")
		}
	}
	// Opted-in contract: the view pin is present.
	found := false
	for _, p := range listPins("/api/pins?include=view") {
		if p.ItemType == "view" && p.ItemID == view.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("view pin missing from the opted-in list")
	}
}

func TestIssueViewWriteLimits(t *testing.T) {

	// JSON null for a definition blob is rejected up front, not left to
	// become a DB CHECK 500.
	_, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Null query",
		"scope_type": "workspace",
		"query":      nil,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("null query: expected 400, got %d: %s", code, body)
	}

	// Oversized body: a query blob past the byte cap is a 400.
	big := strings.Repeat("x", issueViewBodyMaxBytes)
	_, code, _ = createIssueViewForTest(t, map[string]any{
		"name":       "Huge",
		"scope_type": "workspace",
		"query":      map[string]any{"blob": big},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("oversized body: expected 400, got %d", code)
	}

	// Per-owner quota: fill to the cap directly, then one more create fails.
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_view (workspace_id, owner_id, name, scope_type, visibility, definition_version, query, display)
		SELECT $1::uuid, $2::uuid, 'quota filler ' || g, 'workspace', 'private', 1, '{}', '{}'
		FROM generate_series(1, $3::int) AS g
	`, testWorkspaceID, testUserID, issueViewsPerOwnerMax); err != nil {
		t.Fatalf("seed quota rows: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_view WHERE name LIKE 'quota filler %'`)
	})
	_, code, body = createIssueViewForTest(t, map[string]any{
		"name":       "One too many",
		"scope_type": "workspace",
		"query":      map[string]any{},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("quota: expected 400, got %d: %s", code, body)
	}
	if !strings.Contains(body, "limit") {
		t.Fatalf("quota error should mention the limit, got: %s", body)
	}
}

// Member removal must sweep the departed member's private views and their
// view-bar preferences in the revoke transaction; shared views stay for the
// workspace. (The migration comments promise exactly this.)
func TestRevokeMemberSweepsPrivateViewsAndPreferences(t *testing.T) {

	ctx := context.Background()
	otherID := createSecondWorkspaceMember(t)

	seedView := func(name, visibility string) {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue_view (workspace_id, owner_id, name, scope_type, visibility, definition_version, query, display)
			VALUES ($1, $2, $3, 'workspace', $4, 1, '{}', '{}')
		`, testWorkspaceID, otherID, name, visibility); err != nil {
			t.Fatalf("seed view: %v", err)
		}
	}
	seedView("revoke private", "private")
	seedView("revoke shared", "workspace")
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_view_preference (workspace_id, user_id, scope_type, scope_id, prefs)
		VALUES ($1, $2, 'workspace', $1, '{"order": []}')
		ON CONFLICT (workspace_id, user_id, scope_type, scope_id) DO UPDATE SET prefs = EXCLUDED.prefs
	`, testWorkspaceID, otherID); err != nil {
		t.Fatalf("seed preference: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO pinned_item (workspace_id, user_id, item_type, item_id, position)
		SELECT $1::uuid, $2::uuid, 'view', id, 1 FROM issue_view WHERE name = 'revoke private'
	`, testWorkspaceID, otherID); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_view WHERE name LIKE 'revoke %'`)
		testPool.Exec(ctx, `DELETE FROM issue_view_preference WHERE user_id = $1`, otherID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, otherID).Scan(&memberID); err != nil {
		t.Fatalf("load member id: %v", err)
	}
	if _, err := testHandler.revokeAndRemoveMember(
		ctx, parseUUID(testWorkspaceID), parseUUID(otherID), parseUUID(memberID), parseUUID(testUserID),
	); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	var privateCount, sharedCount, prefCount, pinCount int
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue_view WHERE name = 'revoke private'`).Scan(&privateCount)
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue_view WHERE name = 'revoke shared'`).Scan(&sharedCount)
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue_view_preference WHERE user_id = $1`, otherID).Scan(&prefCount)
	if privateCount != 0 {
		t.Fatal("private view survived member removal")
	}
	if sharedCount != 1 {
		t.Fatal("shared view must survive member removal")
	}
	if prefCount != 0 {
		t.Fatal("view-bar preferences survived member removal")
	}
	testPool.QueryRow(ctx, `SELECT COUNT(*) FROM pinned_item WHERE item_type = 'view' AND user_id = $1`, otherID).Scan(&pinCount)
	if pinCount != 0 {
		t.Fatal("pins on the removed member's private views survived")
	}
}

// Deleting a view — directly, or via its project's deletion — must sweep the
// sidebar pins that point at it: view pins never auto-unpin client-side, so a
// surviving row would be invisible and unremovable forever.
func TestDeletingViewsSweepsTheirPins(t *testing.T) {

	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM pinned_item WHERE item_type = 'view'`)
	})
	countViewPins := func() int {
		var n int
		testPool.QueryRow(ctx, `SELECT COUNT(*) FROM pinned_item WHERE item_type = 'view'`).Scan(&n)
		return n
	}
	pinView := func(viewID string) {
		w := httptest.NewRecorder()
		testHandler.CreatePin(w, newRequest("POST", "/api/pins", map[string]any{
			"item_type": "view",
			"item_id":   viewID,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("pin view: expected 201, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 1) Direct handler delete.
	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Sweep on delete",
		"scope_type": "workspace",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	pinView(view.ID)
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("DELETE", "/api/issue-views/"+view.ID, nil), "id", view.ID)
	testHandler.DeleteIssueView(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete view: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if n := countViewPins(); n != 0 {
		t.Fatalf("pin survived direct view delete: %d rows", n)
	}

	// 2) Project deletion cascades through its scoped views.
	w = httptest.NewRecorder()
	testHandler.CreateProject(w, newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "pin sweep project",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	projView, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Project pinned view",
		"scope_type": "project",
		"scope_id":   project.ID,
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", code, body)
	}
	pinView(projView.ID)

	w = httptest.NewRecorder()
	req = withURLParam(newRequest("DELETE", "/api/projects/"+project.ID, nil), "id", project.ID)
	testHandler.DeleteProject(w, req)
	if w.Code >= 300 {
		t.Fatalf("delete project: got %d: %s", w.Code, w.Body.String())
	}
	if n := countViewPins(); n != 0 {
		t.Fatalf("pin survived project deletion: %d rows", n)
	}
}
