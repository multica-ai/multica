package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func createIssueViewForTest(
	t *testing.T,
	userID string,
	body map[string]any,
) IssueViewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssueView(w, newRequestAs(userID, http.MethodPost, "/api/views", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueView: got %d: %s", w.Code, w.Body.String())
	}
	var view IssueViewResponse
	if err := json.NewDecoder(w.Body).Decode(&view); err != nil {
		t.Fatalf("decode created view: %v", err)
	}
	return view
}

func TestIssueViewsPermissionsDefaultsAndPinCompatibility(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM pinned_item WHERE workspace_id = $1 AND item_type = 'view'`, testWorkspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM issue_view WHERE workspace_id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM pinned_item WHERE workspace_id = $1 AND item_type = 'view'`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_view WHERE workspace_id = $1`, testWorkspaceID)
	})

	var memberID string
	email := fmt.Sprintf("saved-view-member-%s@multica.test", t.Name())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Saved View Member', $1)
		RETURNING id
	`, email).Scan(&memberID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberID); err != nil {
		t.Fatalf("create member row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, memberID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	definition := map[string]any{
		"version":       1,
		"viewMode":      "board",
		"statusFilters": []string{"blocked"},
	}
	privateView := createIssueViewForTest(t, testUserID, map[string]any{
		"name":       "Owner private",
		"icon":       "bookmark",
		"scope_type": "workspace",
		"visibility": "private",
		"definition": definition,
	})
	sharedView := createIssueViewForTest(t, testUserID, map[string]any{
		"name":       "Workspace shared",
		"scope_type": "workspace",
		"visibility": "workspace",
		"definition": definition,
	})

	listW := httptest.NewRecorder()
	testHandler.ListIssueViews(listW, newRequestAs(memberID, http.MethodGet, "/api/views?scope_type=workspace", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("ListIssueViews as member: got %d: %s", listW.Code, listW.Body.String())
	}
	var list struct {
		Views []IssueViewResponse `json:"views"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Views) != 1 || list.Views[0].ID != sharedView.ID || list.Views[0].CanEdit {
		t.Fatalf("member list = %+v, want only non-editable shared view", list.Views)
	}

	privateGetW := httptest.NewRecorder()
	privateGetReq := withURLParam(
		newRequestAs(memberID, http.MethodGet, "/api/views/"+privateView.ID, nil),
		"id", privateView.ID,
	)
	testHandler.GetIssueView(privateGetW, privateGetReq)
	if privateGetW.Code != http.StatusNotFound {
		t.Fatalf("member private view read: got %d, want 404", privateGetW.Code)
	}

	memberUpdateW := httptest.NewRecorder()
	memberUpdateReq := withURLParam(
		newRequestAs(memberID, http.MethodPatch, "/api/views/"+sharedView.ID, map[string]any{"name": "Nope"}),
		"id", sharedView.ID,
	)
	testHandler.UpdateIssueView(memberUpdateW, memberUpdateReq)
	if memberUpdateW.Code != http.StatusForbidden {
		t.Fatalf("member shared view update: got %d, want 403", memberUpdateW.Code)
	}

	clearIconW := httptest.NewRecorder()
	clearIconReq := withURLParam(
		newRequest(http.MethodPatch, "/api/views/"+privateView.ID, map[string]any{"icon": nil}),
		"id", privateView.ID,
	)
	testHandler.UpdateIssueView(clearIconW, clearIconReq)
	if clearIconW.Code != http.StatusOK {
		t.Fatalf("clear view icon: got %d: %s", clearIconW.Code, clearIconW.Body.String())
	}
	var cleared IssueViewResponse
	if err := json.NewDecoder(clearIconW.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode cleared view: %v", err)
	}
	if cleared.Icon != nil {
		t.Fatalf("cleared icon = %q, want null", *cleared.Icon)
	}

	invalidMyW := httptest.NewRecorder()
	testHandler.CreateIssueView(invalidMyW, newRequest(http.MethodPost, "/api/views", map[string]any{
		"name":       "Shared My Issues",
		"scope_type": "my",
		"visibility": "workspace",
		"definition": definition,
	}))
	if invalidMyW.Code != http.StatusBadRequest {
		t.Fatalf("shared My Issues view: got %d, want 400", invalidMyW.Code)
	}

	defaultW := httptest.NewRecorder()
	testHandler.SetDefaultIssueView(defaultW, newRequest(http.MethodPut, "/api/views/default", map[string]any{
		"scope_type": "workspace",
		"view_id":    sharedView.ID,
	}))
	if defaultW.Code != http.StatusNoContent {
		t.Fatalf("SetDefaultIssueView: got %d: %s", defaultW.Code, defaultW.Body.String())
	}
	ownerListW := httptest.NewRecorder()
	testHandler.ListIssueViews(ownerListW, newRequest(http.MethodGet, "/api/views?scope_type=workspace", nil))
	var ownerList struct {
		DefaultViewID *string `json:"default_view_id"`
	}
	if err := json.NewDecoder(ownerListW.Body).Decode(&ownerList); err != nil {
		t.Fatalf("decode owner list: %v", err)
	}
	if ownerList.DefaultViewID == nil || *ownerList.DefaultViewID != sharedView.ID {
		t.Fatalf("default_view_id = %v, want %s", ownerList.DefaultViewID, sharedView.ID)
	}

	createPinW := httptest.NewRecorder()
	testHandler.CreatePin(createPinW, newRequest(http.MethodPost, "/api/pins", map[string]any{
		"item_type": "view", "item_id": sharedView.ID,
	}))
	if createPinW.Code != http.StatusCreated {
		t.Fatalf("CreatePin(view): got %d: %s", createPinW.Code, createPinW.Body.String())
	}

	legacyPinsW := httptest.NewRecorder()
	testHandler.ListPins(legacyPinsW, newRequest(http.MethodGet, "/api/pins", nil))
	var legacyPins []PinnedItemResponse
	if err := json.NewDecoder(legacyPinsW.Body).Decode(&legacyPins); err != nil {
		t.Fatalf("decode legacy pins: %v", err)
	}
	for _, pin := range legacyPins {
		if pin.ItemType == "view" {
			t.Fatal("legacy client received a view pin")
		}
	}

	capableReq := newRequest(http.MethodGet, "/api/pins", nil)
	capableReq.Header.Set("X-Client-Capabilities", protocol.AppCapabilityIssueViewPinsV1)
	capablePinsW := httptest.NewRecorder()
	testHandler.ListPins(capablePinsW, capableReq)
	var capablePins []PinnedItemResponse
	if err := json.NewDecoder(capablePinsW.Body).Decode(&capablePins); err != nil {
		t.Fatalf("decode capable pins: %v", err)
	}
	found := false
	for _, pin := range capablePins {
		found = found || (pin.ItemType == "view" && pin.ItemID == sharedView.ID)
	}
	if !found {
		t.Fatal("capable client did not receive its view pin")
	}

	duplicateW := httptest.NewRecorder()
	duplicateReq := withURLParam(
		newRequestAs(memberID, http.MethodPost, "/api/views/"+sharedView.ID+"/duplicate", map[string]any{
			"name": "Member private copy", "visibility": "private",
		}),
		"id", sharedView.ID,
	)
	testHandler.DuplicateIssueView(duplicateW, duplicateReq)
	if duplicateW.Code != http.StatusCreated {
		t.Fatalf("DuplicateIssueView: got %d: %s", duplicateW.Code, duplicateW.Body.String())
	}
	var duplicate IssueViewResponse
	if err := json.NewDecoder(duplicateW.Body).Decode(&duplicate); err != nil {
		t.Fatalf("decode duplicate: %v", err)
	}
	if duplicate.CreatorID != memberID || duplicate.Visibility != "private" {
		t.Fatalf("duplicate creator/visibility = %s/%s, want %s/private", duplicate.CreatorID, duplicate.Visibility, memberID)
	}

	deleteW := httptest.NewRecorder()
	deleteReq := withURLParam(
		newRequest(http.MethodDelete, "/api/views/"+sharedView.ID, nil),
		"id", sharedView.ID,
	)
	testHandler.DeleteIssueView(deleteW, deleteReq)
	if deleteW.Code != http.StatusNoContent {
		t.Fatalf("DeleteIssueView: got %d: %s", deleteW.Code, deleteW.Body.String())
	}
	for table, query := range map[string]string{
		"view":       `SELECT count(*) FROM issue_view WHERE id = $1`,
		"preference": `SELECT count(*) FROM issue_view_preference WHERE default_view_id = $1`,
		"pin":        `SELECT count(*) FROM pinned_item WHERE item_type = 'view' AND item_id = $1`,
	} {
		var count int
		if err := testPool.QueryRow(ctx, query, sharedView.ID).Scan(&count); err != nil {
			t.Fatalf("count deleted %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s remaining after view delete: %d", table, count)
		}
	}
}

func TestIssueViewDefinitionValidation(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "empty", raw: nil},
		{name: "array", raw: json.RawMessage(`[]`)},
		{name: "missing version", raw: json.RawMessage(`{"viewMode":"board"}`)},
		{name: "fractional version", raw: json.RawMessage(`{"version":1.5}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateIssueViewDefinition(tc.raw); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if _, err := validateIssueViewDefinition(json.RawMessage(`{"version":1,"viewMode":"board"}`)); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}
}

func TestDeleteProjectCleansSavedViews(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	createProjectW := httptest.NewRecorder()
	testHandler.CreateProject(createProjectW, newRequest(http.MethodPost, "/api/projects", map[string]any{
		"title": "Saved view cleanup project",
	}))
	if createProjectW.Code != http.StatusCreated {
		t.Fatalf("CreateProject: got %d: %s", createProjectW.Code, createProjectW.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(createProjectW.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})

	view := createIssueViewForTest(t, testUserID, map[string]any{
		"name":       "Project launch",
		"scope_type": "project",
		"scope_id":   project.ID,
		"visibility": "private",
		"definition": map[string]any{"version": 1, "viewMode": "board"},
	})
	defaultW := httptest.NewRecorder()
	testHandler.SetDefaultIssueView(defaultW, newRequest(http.MethodPut, "/api/views/default", map[string]any{
		"scope_type": "project", "scope_id": project.ID, "view_id": view.ID,
	}))
	if defaultW.Code != http.StatusNoContent {
		t.Fatalf("SetDefaultIssueView: got %d: %s", defaultW.Code, defaultW.Body.String())
	}
	pinW := httptest.NewRecorder()
	testHandler.CreatePin(pinW, newRequest(http.MethodPost, "/api/pins", map[string]any{
		"item_type": "view", "item_id": view.ID,
	}))
	if pinW.Code != http.StatusCreated {
		t.Fatalf("CreatePin: got %d: %s", pinW.Code, pinW.Body.String())
	}

	deleteW := httptest.NewRecorder()
	deleteReq := withURLParam(
		newRequest(http.MethodDelete, "/api/projects/"+project.ID, nil),
		"id", project.ID,
	)
	testHandler.DeleteProject(deleteW, deleteReq)
	if deleteW.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: got %d: %s", deleteW.Code, deleteW.Body.String())
	}

	for table, query := range map[string]string{
		"views":       `SELECT count(*) FROM issue_view WHERE id = $1`,
		"preferences": `SELECT count(*) FROM issue_view_preference WHERE default_view_id = $1`,
		"pins":        `SELECT count(*) FROM pinned_item WHERE item_type = 'view' AND item_id = $1`,
	} {
		var count int
		if err := testPool.QueryRow(context.Background(), query, view.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s remaining after project delete: %d", table, count)
		}
	}
}
