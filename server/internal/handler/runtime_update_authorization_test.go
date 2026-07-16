package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func promoteRuntimeTestMemberToAdmin(t *testing.T, userID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE member
		SET role = 'admin'
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("promote runtime test member to admin: %v", err)
	}
}

func TestInitiateUpdateRequiresRuntimeManager(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name       string
		actor      string
		wantStatus int
	}{
		{name: "runtime owner", actor: "runtime_owner", wantStatus: http.StatusOK},
		{name: "workspace admin", actor: "workspace_admin", wantStatus: http.StatusOK},
		{name: "plain member", actor: "plain_member", wantStatus: http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)
			actorID := runtimeOwnerID
			switch tc.actor {
			case "workspace_admin":
				promoteRuntimeTestMemberToAdmin(t, plainMemberID)
				actorID = plainMemberID
			case "plain_member":
				actorID = plainMemberID
			}

			w := httptest.NewRecorder()
			req := withURLParam(
				newRequestAs(actorID, http.MethodPost, "/api/runtimes/"+runtimeID+"/update", map[string]any{
					"target_version": "v9.9.9",
				}),
				"runtimeId",
				runtimeID,
			)
			testHandler.InitiateUpdate(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}

			hasPending, err := testHandler.UpdateStore.HasPending(context.Background(), runtimeID)
			if err != nil {
				t.Fatalf("check pending update request: %v", err)
			}
			wantPending := tc.wantStatus == http.StatusOK
			if hasPending != wantPending {
				t.Fatalf("pending update request = %v; want %v", hasPending, wantPending)
			}
		})
	}
}

func TestGetUpdateRequiresRuntimeManager(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name       string
		actor      string
		wantStatus int
	}{
		{name: "runtime owner", actor: "runtime_owner", wantStatus: http.StatusOK},
		{name: "workspace admin", actor: "workspace_admin", wantStatus: http.StatusOK},
		{name: "plain member", actor: "plain_member", wantStatus: http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)
			actorID := runtimeOwnerID
			switch tc.actor {
			case "workspace_admin":
				promoteRuntimeTestMemberToAdmin(t, plainMemberID)
				actorID = plainMemberID
			case "plain_member":
				actorID = plainMemberID
			}

			update, err := testHandler.UpdateStore.Create(context.Background(), runtimeID, "v9.9.9")
			if err != nil {
				t.Fatalf("create update request: %v", err)
			}

			w := httptest.NewRecorder()
			req := withURLParams(
				newRequestAs(actorID, http.MethodGet, "/api/runtimes/"+runtimeID+"/update/"+update.ID, nil),
				"runtimeId", runtimeID,
				"updateId", update.ID,
			)
			testHandler.GetUpdate(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}
