package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func withIntegrationBindingParams(req *http.Request, provider, connectionID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider", provider)
	rctx.URLParams.Add("connectionId", connectionID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestIntegrationSpaceBindingsAreAdminManagedAndSpaceScoped(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	spaceA := createSpaceForAccessTest(t, "Integration A", "INTAPA", "open")
	spaceB := createSpaceForAccessTest(t, "Integration B", "INTAPB", "open")
	memberID := createPlainMember(t, "integration-binding-member@multica.test")
	connection, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: 91760001,
		AccountLogin:   "phase3-binding-probe",
		AccountType:    "User",
		ConnectedByID:  parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create GitHub connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_installation WHERE id = $1`, connection.ID)
	})

	list := httptest.NewRecorder()
	testHandler.ListIntegrationBindings(list, newRequestAs(memberID, http.MethodGet, "/api/integration-bindings", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("ListIntegrationBindings: expected 200, got %d: %s", list.Code, list.Body.String())
	}
	var listed struct {
		Connections []integrationConnectionBindingResponse `json:"connections"`
		CanManage   bool                                   `json:"can_manage"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode Integration bindings: %v", err)
	}
	if listed.CanManage {
		t.Fatal("regular member unexpectedly received Integration binding management")
	}
	foundUnbound := false
	for _, item := range listed.Connections {
		if item.ConnectionID == uuidToString(connection.ID) {
			foundUnbound = len(item.SpaceIDs) == 0
		}
	}
	if !foundUnbound {
		t.Fatal("new GitHub connection was not listed as explicitly unbound")
	}

	memberReplace := httptest.NewRecorder()
	memberReq := withIntegrationBindingParams(newRequestAs(memberID, http.MethodPut, "/api/integration-bindings/github/"+uuidToString(connection.ID), map[string]any{
		"space_ids": []string{spaceA.ID},
	}), "github", uuidToString(connection.ID))
	testHandler.ReplaceIntegrationBindings(memberReplace, memberReq)
	if memberReplace.Code != http.StatusForbidden {
		t.Fatalf("ReplaceIntegrationBindings(member): expected 403, got %d: %s", memberReplace.Code, memberReplace.Body.String())
	}

	replace := httptest.NewRecorder()
	replaceReq := withIntegrationBindingParams(newRequest(http.MethodPut, "/api/integration-bindings/github/"+uuidToString(connection.ID), map[string]any{
		"space_ids": []string{spaceA.ID, spaceB.ID, spaceA.ID},
	}), "github", uuidToString(connection.ID))
	testHandler.ReplaceIntegrationBindings(replace, replaceReq)
	if replace.Code != http.StatusOK {
		t.Fatalf("ReplaceIntegrationBindings(owner): expected 200, got %d: %s", replace.Code, replace.Body.String())
	}
	rows, err := testHandler.Queries.ListIntegrationBindingsForSpace(ctx, db.ListIntegrationBindingsForSpaceParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		SpaceID:     parseUUID(spaceA.ID),
	})
	if err != nil || len(rows) != 1 || rows[0].ConnectionID != connection.ID {
		t.Fatalf("Space A binding rows = %+v, err=%v", rows, err)
	}
	var bindingCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM integration_space_binding
		WHERE workspace_id = $1 AND provider = 'github' AND connection_id = $2
	`, testWorkspaceID, connection.ID).Scan(&bindingCount); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindingCount != 2 {
		t.Fatalf("deduplicated binding count = %d, want 2", bindingCount)
	}
	activity := httptest.NewRecorder()
	testHandler.ListSpaceActivity(activity, withURLParam(newRequest(http.MethodGet, "/api/spaces/"+spaceA.ID+"/activity", nil), "id", spaceA.ID))
	if activity.Code != http.StatusOK {
		t.Fatalf("ListSpaceActivity after binding: expected 200, got %d: %s", activity.Code, activity.Body.String())
	}
	var activityResponse struct {
		Activities []SpaceActivityResponse `json:"activities"`
	}
	if err := json.Unmarshal(activity.Body.Bytes(), &activityResponse); err != nil {
		t.Fatalf("decode Space binding activity: %v", err)
	}
	foundBindingActivity := false
	for _, item := range activityResponse.Activities {
		if item.Action == "integration_space_bindings_replaced" {
			foundBindingActivity = true
			break
		}
	}
	if !foundBindingActivity {
		t.Fatal("Space activity did not include its Integration binding change")
	}
	removeFromA := httptest.NewRecorder()
	removeFromAReq := withIntegrationBindingParams(newRequest(http.MethodPut, "/api/integration-bindings/github/"+uuidToString(connection.ID), map[string]any{
		"space_ids": []string{spaceB.ID},
	}), "github", uuidToString(connection.ID))
	testHandler.ReplaceIntegrationBindings(removeFromA, removeFromAReq)
	if removeFromA.Code != http.StatusOK {
		t.Fatalf("remove Space A binding: expected 200, got %d: %s", removeFromA.Code, removeFromA.Body.String())
	}
	activity = httptest.NewRecorder()
	testHandler.ListSpaceActivity(activity, withURLParam(newRequest(http.MethodGet, "/api/spaces/"+spaceA.ID+"/activity", nil), "id", spaceA.ID))
	if activity.Code != http.StatusOK {
		t.Fatalf("ListSpaceActivity after unbinding: expected 200, got %d: %s", activity.Code, activity.Body.String())
	}
	if err := json.Unmarshal(activity.Body.Bytes(), &activityResponse); err != nil {
		t.Fatalf("decode Space unbinding activity: %v", err)
	}
	bindingActivityCount := 0
	for _, item := range activityResponse.Activities {
		if item.Action == "integration_space_bindings_replaced" {
			bindingActivityCount++
		}
	}
	if bindingActivityCount != 2 {
		t.Fatalf("Space A binding activity count = %d, want add and removal", bindingActivityCount)
	}

	archive := httptest.NewRecorder()
	testHandler.ArchiveSpace(archive, withURLParam(newRequest(http.MethodDelete, "/api/spaces/"+spaceB.ID, nil), "id", spaceB.ID))
	if archive.Code != http.StatusOK {
		t.Fatalf("ArchiveSpace: expected 200, got %d: %s", archive.Code, archive.Body.String())
	}
	rejectArchived := httptest.NewRecorder()
	rejectReq := withIntegrationBindingParams(newRequest(http.MethodPut, "/api/integration-bindings/github/"+uuidToString(connection.ID), map[string]any{
		"space_ids": []string{spaceB.ID},
	}), "github", uuidToString(connection.ID))
	testHandler.ReplaceIntegrationBindings(rejectArchived, rejectReq)
	if rejectArchived.Code != http.StatusBadRequest {
		t.Fatalf("bind archived Space: expected 400, got %d: %s", rejectArchived.Code, rejectArchived.Body.String())
	}

	var auditCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM activity_log
		WHERE workspace_id = $1
		  AND action = 'integration_space_bindings_replaced'
		  AND details->>'connection_id' = $2
	`, testWorkspaceID, uuidToString(connection.ID)).Scan(&auditCount); err != nil {
		t.Fatalf("count binding audit: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("binding audit count = %d, want 2", auditCount)
	}

	if _, err := testPool.Exec(ctx, `DELETE FROM github_installation WHERE id = $1`, connection.ID); err != nil {
		t.Fatalf("delete GitHub connection: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM integration_space_binding WHERE connection_id = $1
	`, connection.ID).Scan(&bindingCount); err != nil {
		t.Fatalf("count bindings after connection delete: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("orphan binding count = %d, want 0", bindingCount)
	}
}
