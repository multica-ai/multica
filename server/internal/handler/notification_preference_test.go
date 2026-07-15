package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func notificationPreferenceRequest(
	t *testing.T,
	method string,
	preferences map[string]string,
) *http.Request {
	t.Helper()

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(testUserID),
			WorkspaceID: parseUUID(testWorkspaceID),
		},
	)
	if err != nil {
		t.Fatalf("load test member: %v", err)
	}

	req := newRequest(method, "/api/notification-preferences", map[string]any{
		"preferences": preferences,
	})
	return req.WithContext(
		middleware.SetMemberContext(req.Context(), testWorkspaceID, member),
	)
}

func TestPatchNotificationPreferencesMergesWithoutReplacing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		DELETE FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("reset notification preference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	putRecorder := httptest.NewRecorder()
	testHandler.UpdateNotificationPreferences(
		putRecorder,
		notificationPreferenceRequest(t, http.MethodPut, map[string]string{
			"status_changes": "muted",
		}),
	)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("seed preference: status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	patchRecorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		patchRecorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"comments": "muted",
		}),
	)
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch preference: status=%d body=%s", patchRecorder.Code, patchRecorder.Body.String())
	}

	var response struct {
		WorkspaceID string            `json:"workspace_id"`
		Preferences map[string]string `json:"preferences"`
	}
	if err := json.NewDecoder(patchRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if response.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", response.WorkspaceID, testWorkspaceID)
	}
	if response.Preferences["status_changes"] != "muted" {
		t.Fatalf("status_changes was replaced: %#v", response.Preferences)
	}
	if response.Preferences["comments"] != "muted" {
		t.Fatalf("comments patch missing: %#v", response.Preferences)
	}

	enableRecorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		enableRecorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"status_changes": "all",
		}),
	)
	if enableRecorder.Code != http.StatusOK {
		t.Fatalf("enable preference: status=%d body=%s", enableRecorder.Code, enableRecorder.Body.String())
	}

	var persisted []byte
	if err := testPool.QueryRow(ctx, `
		SELECT preferences
		FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&persisted); err != nil {
		t.Fatalf("load persisted preference: %v", err)
	}
	var preferences map[string]string
	if err := json.Unmarshal(persisted, &preferences); err != nil {
		t.Fatalf("decode persisted preference: %v", err)
	}
	if preferences["status_changes"] != "all" || preferences["comments"] != "muted" {
		t.Fatalf("unexpected persisted preferences: %#v", preferences)
	}
}

func TestPatchNotificationPreferencesRejectsUnknownGroups(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	recorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		recorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"unknown_group": "muted",
		}),
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
