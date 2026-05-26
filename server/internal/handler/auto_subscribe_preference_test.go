package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cleanupAutoSubscribePreferences(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM auto_subscribe_preference WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("delete auto_subscribe_preference: %v", err)
	}
}

func TestAutoSubscribePreferences_Defaults(t *testing.T) {
	cleanupAutoSubscribePreferences(t)
	t.Cleanup(func() { cleanupAutoSubscribePreferences(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/auto-subscribe-preferences", nil)

	testHandler.GetMyAutoSubscribePreferences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AutoSubscribePreferencesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", resp.WorkspaceID, testWorkspaceID)
	}
	if !resp.Preferences["issue_creator"] ||
		!resp.Preferences["issue_assignee"] ||
		!resp.Preferences["comment_author"] ||
		!resp.Preferences["quick_create_requester"] {
		t.Fatalf("expected creator/assignee/commenter/quick-create defaults enabled, got %#v", resp.Preferences)
	}
	if resp.Preferences["issue_description_mention"] || resp.Preferences["comment_mention"] {
		t.Fatalf("expected mention defaults disabled for missing row, got %#v", resp.Preferences)
	}
}

func TestUpdateAutoSubscribePreferences_PartialMerge(t *testing.T) {
	cleanupAutoSubscribePreferences(t)
	t.Cleanup(func() { cleanupAutoSubscribePreferences(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/auto-subscribe-preferences", map[string]any{
		"preferences": map[string]bool{
			"issue_description_mention": true,
			"comment_author":            false,
		},
	})

	testHandler.UpdateMyAutoSubscribePreferences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AutoSubscribePreferencesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Preferences["issue_description_mention"] {
		t.Fatalf("expected description mention enabled, got %#v", resp.Preferences)
	}
	if resp.Preferences["comment_author"] {
		t.Fatalf("expected comment_author disabled, got %#v", resp.Preferences)
	}
	if !resp.Preferences["issue_creator"] || !resp.Preferences["quick_create_requester"] {
		t.Fatalf("expected unrelated defaults preserved, got %#v", resp.Preferences)
	}
}

func TestUpdateAutoSubscribePreferences_RejectsUnknownKey(t *testing.T) {
	cleanupAutoSubscribePreferences(t)
	t.Cleanup(func() { cleanupAutoSubscribePreferences(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/auto-subscribe-preferences", map[string]any{
		"preferences": map[string]bool{
			"unknown_source": true,
		},
	})

	testHandler.UpdateMyAutoSubscribePreferences(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
