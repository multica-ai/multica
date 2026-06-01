package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func createViewAs(t *testing.T, userID string, body map[string]any) (*httptest.ResponseRecorder, ViewResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateView(w, newRequestAs(userID, "POST", "/api/views", body))
	var resp ViewResponse
	if w.Code == http.StatusCreated {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode create view: %v", err)
		}
		t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM saved_view WHERE id = $1`, resp.ID) })
	}
	return w, resp
}

func listViews(t *testing.T, query string) []ViewResponse {
	t.Helper()
	return listViewsAs(t, testUserID, query)
}

func listViewsAs(t *testing.T, userID, query string) []ViewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListViews(w, newRequestAs(userID, "GET", "/api/views?"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListViews(%s): expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var resp struct {
		Views []ViewResponse `json:"views"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list views: %v", err)
	}
	return resp.Views
}

func containsViewID(views []ViewResponse, id string) bool {
	for _, v := range views {
		if v.ID == id {
			return true
		}
	}
	return false
}

func TestCreateAndListView(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	name := fmt.Sprintf("My High Pri %d", time.Now().UnixNano())
	w, view := createViewAs(t, testUserID, map[string]any{
		"name":    name,
		"page":    "issues",
		"filters": map[string]any{"priorities": []string{"high"}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateView: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if view.Name != name || view.Page != "issues" {
		t.Fatalf("unexpected view: %+v", view)
	}
	if got, ok := view.Filters["priorities"].([]any); !ok || len(got) != 1 || got[0] != "high" {
		t.Fatalf("filters not round-tripped: %+v", view.Filters)
	}

	views := listViews(t, "page=issues")
	found := false
	for _, v := range views {
		if v.ID == view.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created view %s not in list", view.ID)
	}
}

func TestCreateViewValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ts := time.Now().UnixNano()

	// invalid page
	w, _ := createViewAs(t, testUserID, map[string]any{"name": fmt.Sprintf("a%d", ts), "page": "bogus"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid page: expected 400, got %d", w.Code)
	}
	// project page without project_id
	w, _ = createViewAs(t, testUserID, map[string]any{"name": fmt.Sprintf("b%d", ts), "page": "project"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("project page without project_id: expected 400, got %d", w.Code)
	}
	// unknown filter key
	w, _ = createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("c%d", ts), "page": "issues",
		"filters": map[string]any{"bogus_key": []string{"x"}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown filter key: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// duplicate name in same scope
	name := fmt.Sprintf("dup%d", ts)
	w, _ = createViewAs(t, testUserID, map[string]any{"name": name, "page": "issues"})
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	w, _ = createViewAs(t, testUserID, map[string]any{"name": name, "page": "issues"})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate name: expected 409, got %d", w.Code)
	}
}

func TestUpdateView(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	_, view := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("upd%d", time.Now().UnixNano()), "page": "issues",
	})
	newName := "renamed"
	w := httptest.NewRecorder()
	testHandler.UpdateView(w, withURLParam(newRequestAs(testUserID, "PUT", "/api/views/"+view.ID, map[string]any{
		"name":    newName,
		"filters": map[string]any{"statuses": []string{"todo"}},
	}), "id", view.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateView: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated ViewResponse
	_ = json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != newName {
		t.Fatalf("name not updated: %+v", updated)
	}
	if _, ok := updated.Filters["statuses"]; !ok {
		t.Fatalf("filters not updated: %+v", updated.Filters)
	}
}

func TestDeleteView(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	_, view := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("del%d", time.Now().UnixNano()), "page": "issues",
	})
	w := httptest.NewRecorder()
	testHandler.DeleteView(w, withURLParam(newRequestAs(testUserID, "DELETE", "/api/views/"+view.ID, nil), "id", view.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteView: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	for _, v := range listViews(t, "page=issues") {
		if v.ID == view.ID {
			t.Fatalf("view %s still listed after delete", view.ID)
		}
	}
}

func TestDeleteDefaultViewForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO saved_view (workspace_id, creator_id, name, page, is_default)
		VALUES ($1, $2, $3, 'issues', true) RETURNING id
	`, testWorkspaceID, testUserID, fmt.Sprintf("default%d", time.Now().UnixNano())).Scan(&id); err != nil {
		t.Fatalf("seed default view: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM saved_view WHERE id = $1`, id) })

	w := httptest.NewRecorder()
	testHandler.DeleteView(w, withURLParam(newRequestAs(testUserID, "DELETE", "/api/views/"+id, nil), "id", id))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete default view: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateViewForbiddenForNonCreatorMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// A plain member who didn't create the view and isn't an admin can't edit it.
	f := seedIssueFilterFixture(t) // f.otherUserID is a plain 'member'
	_, view := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("perm%d", time.Now().UnixNano()), "page": "issues",
	})
	w := httptest.NewRecorder()
	testHandler.UpdateView(w, withURLParam(newRequestAs(f.otherUserID, "PUT", "/api/views/"+view.ID, map[string]any{
		"name": "hijack",
	}), "id", view.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-creator member update: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReorderViews(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ts := time.Now().UnixNano()
	_, v1 := createViewAs(t, testUserID, map[string]any{"name": fmt.Sprintf("r1-%d", ts), "page": "my_issues"})
	_, v2 := createViewAs(t, testUserID, map[string]any{"name": fmt.Sprintf("r2-%d", ts), "page": "my_issues"})
	_, v3 := createViewAs(t, testUserID, map[string]any{"name": fmt.Sprintf("r3-%d", ts), "page": "my_issues"})

	// Reorder to v3, v1, v2.
	w := httptest.NewRecorder()
	testHandler.ReorderViews(w, newRequestAs(testUserID, "PUT", "/api/views/reorder", map[string]any{
		"ids": []string{v3.ID, v1.ID, v2.ID},
	}))
	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("ReorderViews: expected 200/204, got %d: %s", w.Code, w.Body.String())
	}

	views := listViews(t, "page=my_issues")
	pos := map[string]int{}
	for i, v := range views {
		pos[v.ID] = i
	}
	if !(pos[v3.ID] < pos[v1.ID] && pos[v1.ID] < pos[v2.ID]) {
		t.Fatalf("reorder not reflected in list order: v1=%d v2=%d v3=%d", pos[v1.ID], pos[v2.ID], pos[v3.ID])
	}
}

// TestCreateViewAnyOfBranchValidation pins that filter-key validation recurses
// into any_of branches — the validator's contract is "reject unknown keys", so
// a bogus key hidden inside an OR branch must 400 too, and branches can't nest.
func TestCreateViewAnyOfBranchValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ts := time.Now().UnixNano()

	// unknown key inside an any_of branch → 400
	w, _ := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("ao%d", ts), "page": "my_issues",
		"filters": map[string]any{"any_of": []any{map[string]any{"bogus_key": []string{"x"}}}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("any_of bad branch key: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// well-formed any_of branch → 201
	w, _ = createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("ao2%d", ts), "page": "my_issues",
		"filters": map[string]any{"any_of": []any{map[string]any{"assignee_filters": []string{"member:{me}"}}}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("valid any_of: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// a branch may not itself contain any_of (branches are flat) → 400
	w, _ = createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("ao3%d", ts), "page": "my_issues",
		"filters": map[string]any{"any_of": []any{map[string]any{"any_of": []any{}}}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("nested any_of: expected 400, got %d", w.Code)
	}
}

// TestCreateViewDefaultsToPrivate pins the product decision that a saved view is
// private to its creator unless explicitly shared.
func TestCreateViewDefaultsToPrivate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w, view := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("priv%d", time.Now().UnixNano()), "page": "issues",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if view.Shared {
		t.Fatalf("new view should default to private (shared=false), got shared=true")
	}
}

// TestViewSharedSettable pins that shared is an explicit, settable field on both
// create and update.
func TestViewSharedSettable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w, view := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("shr%d", time.Now().UnixNano()), "page": "issues",
		"shared": true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create shared: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !view.Shared {
		t.Fatalf("created view with shared=true should be shared")
	}

	// Toggle back to private via update.
	uw := httptest.NewRecorder()
	testHandler.UpdateView(uw, withURLParam(newRequestAs(testUserID, "PUT", "/api/views/"+view.ID, map[string]any{
		"shared": false,
	}), "id", view.ID))
	if uw.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", uw.Code, uw.Body.String())
	}
	var updated ViewResponse
	if err := json.NewDecoder(uw.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Shared {
		t.Fatalf("view should be private after update shared=false")
	}
}

// TestListViewsVisibility pins the visibility model: a member sees their own
// views (shared or not) plus other members' shared views, but never another
// member's private views.
func TestListViewsVisibility(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedIssueFilterFixture(t) // f.otherUserID is a second workspace member
	ts := time.Now().UnixNano()

	_, minePrivate := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("mine-priv%d", ts), "page": "issues",
	})
	_, mineShared := createViewAs(t, testUserID, map[string]any{
		"name": fmt.Sprintf("mine-shared%d", ts), "page": "issues", "shared": true,
	})
	_, otherPrivate := createViewAs(t, f.otherUserID, map[string]any{
		"name": fmt.Sprintf("other-priv%d", ts), "page": "issues",
	})

	// The other member sees their own private view + my shared view, not my private one.
	otherView := listViewsAs(t, f.otherUserID, "page=issues")
	if !containsViewID(otherView, otherPrivate.ID) {
		t.Fatalf("creator should see their own private view")
	}
	if !containsViewID(otherView, mineShared.ID) {
		t.Fatalf("member should see another member's shared view")
	}
	if containsViewID(otherView, minePrivate.ID) {
		t.Fatalf("member must NOT see another member's private view")
	}

	// I see my own private + shared, not the other member's private view.
	mineView := listViewsAs(t, testUserID, "page=issues")
	if !containsViewID(mineView, minePrivate.ID) || !containsViewID(mineView, mineShared.ID) {
		t.Fatalf("creator should see all of their own views")
	}
	if containsViewID(mineView, otherPrivate.ID) {
		t.Fatalf("must NOT see another member's private view")
	}
}
