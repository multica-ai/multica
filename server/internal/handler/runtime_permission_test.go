package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// helperTestUser creates a new multica_user and returns its ID. The user is not
// added to any workspace; use addUserToWorkspace for that.
func helperTestUser(t *testing.T, name, email string) string {
	t.Helper()
	var userID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO multica_user (name, email) VALUES ($1, $2) RETURNING id`,
		name, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM multica_user WHERE id = $1`, userID)
	})
	return userID
}

// helperAddUserToWorkspace adds an existing user to the test workspace.
func helperAddUserToWorkspace(t *testing.T, userID, role string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO multica_member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
		testWorkspaceID, userID, role,
	); err != nil {
		t.Fatalf("add user to workspace: %v", err)
	}
}

// helperGrantRuntimePermission grants an explicit runtime permission to a user.
func helperGrantRuntimePermission(t *testing.T, runtimeID, userID, role string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO multica_runtime_permission (runtime_id, user_id, role) VALUES ($1, $2, $3)`,
		runtimeID, userID, role,
	); err != nil {
		t.Fatalf("grant runtime permission: %v", err)
	}
}

// ── Helper unit tests ────────────────────────────────────────────────────────

func TestResolveRuntimeRole(t *testing.T) {
	rt := db.MulticaAgentRuntime{Visibility: "private"}
	rt.OwnerID = parseUUID(testUserID)

	tests := []struct {
		name         string
		memberRole   string
		isOwner      bool
		explicitRole string
		want         RuntimePermissionRole
	}{
		{"workspace owner", "owner", false, "", RuntimeRoleOwner},
		{"workspace admin", "admin", false, "", RuntimeRoleOwner},
		{"runtime owner", "member", true, "", RuntimeRoleOwner},
		{"explicit admin", "member", false, "admin", RuntimeRoleAdmin},
		{"explicit operator", "member", false, "operator", RuntimeRoleOperator},
		{"explicit viewer", "member", false, "viewer", RuntimeRoleViewer},
		{"no permission", "member", false, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member := db.MulticaMember{Role: tt.memberRole}
			rt := rt
			if tt.isOwner {
				rt.OwnerID = parseUUID(testUserID)
				member.UserID = parseUUID(testUserID)
			} else {
				rt.OwnerID = pgtype.UUID{}
			}
			got := resolveRuntimeRole(member, rt, tt.explicitRole)
			if got != tt.want {
				t.Errorf("resolveRuntimeRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanControlRuntime(t *testing.T) {
	tests := []struct {
		name         string
		memberRole   string
		isOwner      bool
		explicitRole string
		want         bool
	}{
		{"workspace owner", "owner", false, "", true},
		{"workspace admin", "admin", false, "", true},
		{"runtime owner", "member", true, "", true},
		{"explicit admin", "member", false, "admin", true},
		{"explicit operator", "member", false, "operator", true},
		{"explicit viewer", "member", false, "viewer", false},
		{"no permission", "member", false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member := db.MulticaMember{Role: tt.memberRole}
			rt := db.MulticaAgentRuntime{Visibility: "private"}
			if tt.isOwner {
				rt.OwnerID = parseUUID(testUserID)
				member.UserID = parseUUID(testUserID)
			}
			got := canControlRuntime(member, rt, tt.explicitRole)
			if got != tt.want {
				t.Errorf("canControlRuntime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanObserveRuntime(t *testing.T) {
	tests := []struct {
		name           string
		memberRole     string
		isOwner        bool
		explicitRole   string
		visibility     string
		want           bool
	}{
		{"workspace owner private", "owner", false, "", "private", true},
		{"explicit viewer private", "member", false, "viewer", "private", true},
		{"no permission private", "member", false, "", "private", false},
		{"public runtime member", "member", false, "", "public", true},
		{"public runtime non-member", "", false, "", "public", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member := db.MulticaMember{Role: tt.memberRole}
			rt := db.MulticaAgentRuntime{Visibility: tt.visibility}
			if tt.isOwner {
				rt.OwnerID = parseUUID(testUserID)
				member.UserID = parseUUID(testUserID)
			}
			got := canObserveRuntime(member, rt, tt.explicitRole)
			if got != tt.want {
				t.Errorf("canObserveRuntime() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── Management API tests ─────────────────────────────────────────────────────

func TestListRuntimePermissions(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	viewerUser := helperTestUser(t, "List Viewer", "list-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, runtimeID, viewerUser, "viewer")

	req := newRequest("GET", "/api/runtimes/"+runtimeID+"/permissions", nil)
	req = withURLParam(req, "runtimeId", runtimeID)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Get("/api/runtimes/{runtimeId}/permissions", testHandler.ListRuntimePermissions)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Permissions []RuntimePermissionResponse `json:"permissions"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Permissions) == 0 {
		t.Fatalf("expected at least one permission, got none")
	}
	found := false
	for _, p := range resp.Permissions {
		if p.UserID == viewerUser {
			found = true
			if p.Role != "viewer" {
				t.Fatalf("expected viewer role, got %s", p.Role)
			}
		}
	}
	if !found {
		t.Fatalf("expected to find viewer user in permissions list")
	}
}

func TestCreateRuntimePermission(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	viewerUser := helperTestUser(t, "Create Viewer", "create-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")

	body := map[string]string{"user_id": viewerUser, "role": "viewer"}
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/permissions", body)
	req = withURLParam(req, "runtimeId", runtimeID)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/api/runtimes/{runtimeId}/permissions", testHandler.CreateRuntimePermission)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp RuntimePermissionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Role != "viewer" {
		t.Fatalf("expected role viewer, got %s", resp.Role)
	}
	if resp.UserID != viewerUser {
		t.Fatalf("expected user_id %s, got %s", viewerUser, resp.UserID)
	}
}

func TestCreateRuntimePermission_ForbiddenForViewer(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	viewerUser := helperTestUser(t, "Viewer Grantor", "viewer-grantor@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, runtimeID, viewerUser, "viewer")

	otherUser := helperTestUser(t, "Other User", "other-user@multica.ai")
	helperAddUserToWorkspace(t, otherUser, "member")

	body := map[string]string{"user_id": otherUser, "role": "operator"}
	req := newRequestAs(viewerUser, "POST", "/api/runtimes/"+runtimeID+"/permissions", body)
	req = withURLParam(req, "runtimeId", runtimeID)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/api/runtimes/{runtimeId}/permissions", testHandler.CreateRuntimePermission)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateRuntimePermission_Duplicate(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	viewerUser := helperTestUser(t, "Duplicate Viewer", "dup-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, runtimeID, viewerUser, "viewer")

	body := map[string]string{"user_id": viewerUser, "role": "viewer"}
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/permissions", body)
	req = withURLParam(req, "runtimeId", runtimeID)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/api/runtimes/{runtimeId}/permissions", testHandler.CreateRuntimePermission)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateRuntimePermission_SelfGrant(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	body := map[string]string{"user_id": testUserID, "role": "viewer"}
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/permissions", body)
	req = withURLParam(req, "runtimeId", runtimeID)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/api/runtimes/{runtimeId}/permissions", testHandler.CreateRuntimePermission)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateRuntimePermission(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	viewerUser := helperTestUser(t, "Update Viewer", "update-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, runtimeID, viewerUser, "viewer")

	body := map[string]string{"role": "operator"}
	req := newRequestAs(testUserID, "PATCH", "/api/runtimes/"+runtimeID+"/permissions/"+viewerUser, body)
	req = withURLParam(req, "runtimeId", runtimeID)
	req = withURLParam(req, "userId", viewerUser)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Patch("/api/runtimes/{runtimeId}/permissions/{userId}", testHandler.UpdateRuntimePermission)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp RuntimePermissionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Role != "operator" {
		t.Fatalf("expected role operator, got %s", resp.Role)
	}
}

func TestDeleteRuntimePermission(t *testing.T) {
	runtimeID := handlerTestRuntimeID(t)
	viewerUser := helperTestUser(t, "Delete Viewer", "delete-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, runtimeID, viewerUser, "viewer")

	req := newRequestAs(testUserID, "DELETE", "/api/runtimes/"+runtimeID+"/permissions/"+viewerUser, nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	req = withURLParam(req, "userId", viewerUser)

	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Delete("/api/runtimes/{runtimeId}/permissions/{userId}", testHandler.DeleteRuntimePermission)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ── Session permission tests ─────────────────────────────────────────────────

func TestGetSessionPermission_ReturnsRoleAndCapabilities(t *testing.T) {
	nodeRunID, _, sessionID := seedHandbackNodeRun(t)

	req := withURLParam(
		newRequest("GET", "/api/sessions/"+sessionID+"/permission", nil),
		"sessionId", sessionID,
	)
	w := httptest.NewRecorder()
	testHandler.GetSessionPermission(w, req)

	if w.Code != 200 {
		t.Fatalf("get session permission: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SessionPermissionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NodeRunID != nodeRunID {
		t.Fatalf("node_run_id: expected %q, got %q", nodeRunID, resp.NodeRunID)
	}
	if resp.Role != "owner" {
		t.Fatalf("role: expected owner, got %s", resp.Role)
	}
	if !resp.CanControl || !resp.CanObserve {
		t.Fatalf("owner should have control and observe, got control=%v observe=%v", resp.CanControl, resp.CanObserve)
	}
}

func TestGetSessionPermission_ViewerCanObserveNotControl(t *testing.T) {
	nodeRunID, _, sessionID := seedHandbackNodeRun(t)
	viewerUser := helperTestUser(t, "Session Viewer", "session-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, testRuntimeID, viewerUser, "viewer")

	req := withURLParam(
		newRequestAs(viewerUser, "GET", "/api/sessions/"+sessionID+"/permission", nil),
		"sessionId", sessionID,
	)
	w := httptest.NewRecorder()
	testHandler.GetSessionPermission(w, req)

	if w.Code != 200 {
		t.Fatalf("get session permission: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SessionPermissionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NodeRunID != nodeRunID {
		t.Fatalf("node_run_id: expected %q, got %q", nodeRunID, resp.NodeRunID)
	}
	if resp.Role != "viewer" {
		t.Fatalf("role: expected viewer, got %s", resp.Role)
	}
	if resp.CanControl {
		t.Fatalf("viewer should not have control")
	}
	if !resp.CanObserve {
		t.Fatalf("viewer should be able to observe")
	}
}

func TestGetSessionPermission_ForbiddenForNonMember(t *testing.T) {
	_, _, sessionID := seedHandbackNodeRun(t)
	foreignUser := helperTestUser(t, "Foreign User", "foreign-user@multica.ai")

	req := withURLParam(
		newRequestAs(foreignUser, "GET", "/api/sessions/"+sessionID+"/permission", nil),
		"sessionId", sessionID,
	)
	w := httptest.NewRecorder()
	testHandler.GetSessionPermission(w, req)

	if w.Code != 403 {
		t.Fatalf("foreign user: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Takeover/handback/finalize permission gating tests ───────────────────────

func TestTakeoverNodeRun_ForbiddenForViewer(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "working")
	viewerUser := helperTestUser(t, "Takeover Viewer", "takeover-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, testRuntimeID, viewerUser, "viewer")

	req := withURLParam(
		newRequestAs(viewerUser, "POST", "/api/node-runs/"+nodeRunID+"/blocked", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.TakeoverNodeRun(w, req)

	if w.Code != 403 {
		t.Fatalf("viewer takeover: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandbackNodeRun_ForbiddenForViewer(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "blocked")
	viewerUser := helperTestUser(t, "Handback Viewer", "handback-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, testRuntimeID, viewerUser, "viewer")

	req := withURLParam(
		newRequestAs(viewerUser, "POST", "/api/node-runs/"+nodeRunID+"/working", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.HandbackNodeRun(w, req)

	if w.Code != 403 {
		t.Fatalf("viewer handback: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFinalizeNodeRun_ForbiddenForViewer(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "blocked")
	approved := true
	viewerUser := helperTestUser(t, "Finalize Viewer", "finalize-viewer@multica.ai")
	helperAddUserToWorkspace(t, viewerUser, "member")
	helperGrantRuntimePermission(t, testRuntimeID, viewerUser, "viewer")

	req := withURLParam(
		newRequestAs(viewerUser, "POST", "/api/node-runs/"+nodeRunID+"/finalize", FinalizeNodeRunRequest{Approved: &approved}),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.FinalizeNodeRun(w, req)

	if w.Code != 403 {
		t.Fatalf("viewer finalize: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTakeoverNodeRun_AllowedForOperator(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "working")
	operatorUser := helperTestUser(t, "Takeover Operator", "takeover-operator@multica.ai")
	helperAddUserToWorkspace(t, operatorUser, "member")
	helperGrantRuntimePermission(t, testRuntimeID, operatorUser, "operator")

	req := withURLParam(
		newRequestAs(operatorUser, "POST", "/api/node-runs/"+nodeRunID+"/blocked", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.TakeoverNodeRun(w, req)

	if w.Code != 200 {
		t.Fatalf("operator takeover: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
