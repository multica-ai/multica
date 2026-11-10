package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
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
			"comments":     "muted",
			"chat_replies": "muted",
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
	if response.Preferences["chat_replies"] != "muted" {
		t.Fatalf("chat_replies patch missing: %#v", response.Preferences)
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

// TestPatchNotificationPreferencesAcceptsMentions covers the group added by
// #6468. The whitelist rejects unknown keys, so a client shipped ahead of the
// server would 400 on this key — the reason the server must deploy first.
func TestPatchNotificationPreferencesAcceptsMentions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	})

	recorder := httptest.NewRecorder()
	testHandler.PatchNotificationPreferences(
		recorder,
		notificationPreferenceRequest(t, http.MethodPatch, map[string]string{
			"mentions": "muted",
		}),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Preferences map[string]string `json:"preferences"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Preferences["mentions"] != "muted" {
		t.Fatalf("mentions not persisted: %#v", response.Preferences)
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

func TestNotificationPreferencesFocusedInboxKeys(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	resetPrefs := func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	}
	resetPrefs()
	t.Cleanup(resetPrefs)

	focused := map[string]string{
		"inbox_mode":             "focused",
		"focused_assigned":       "off",
		"focused_blocked":        "on",
		"focused_mentioned":      "on",
		"focused_thread_reply":   "off",
		"focused_agent_activity": "on",
		"comments":               "muted",
	}

	t.Run("PUT accepts the new keys", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		testHandler.UpdateNotificationPreferences(recorder,
			notificationPreferenceRequest(t, http.MethodPut, focused))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			Preferences map[string]string `json:"preferences"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		for k, v := range focused {
			if response.Preferences[k] != v {
				t.Fatalf("%s = %q, want %q (%#v)", k, response.Preferences[k], v, response.Preferences)
			}
		}
	})

	t.Run("PATCH merges a mode change", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		testHandler.PatchNotificationPreferences(recorder,
			notificationPreferenceRequest(t, http.MethodPatch, map[string]string{"inbox_mode": "all"}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			Preferences map[string]string `json:"preferences"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if response.Preferences["inbox_mode"] != "all" || response.Preferences["focused_assigned"] != "off" {
			t.Fatalf("unexpected merged preferences: %#v", response.Preferences)
		}
	})

	rejects := []struct {
		name  string
		prefs map[string]string
	}{
		{"bad inbox_mode", map[string]string{"inbox_mode": "muted"}},
		{"focused rule with group value", map[string]string{"focused_assigned": "all"}},
		{"focused rule with mute value", map[string]string{"focused_blocked": "muted"}},
		{"group key with rule value", map[string]string{"comments": "off"}},
		{"group key with mode value", map[string]string{"mentions": "focused"}},
		{"unknown focused key", map[string]string{"focused_everything": "on"}},
		{"unknown key", map[string]string{"unknown_group": "all"}},
		{"one bad key among valid ones", map[string]string{"inbox_mode": "focused", "focused_mentioned": "yes"}},
	}
	for _, tc := range rejects {
		for _, method := range []string{http.MethodPut, http.MethodPatch} {
			t.Run(method+" rejects "+tc.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				req := notificationPreferenceRequest(t, method, tc.prefs)
				if method == http.MethodPut {
					testHandler.UpdateNotificationPreferences(recorder, req)
				} else {
					testHandler.PatchNotificationPreferences(recorder, req)
				}
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
			})
		}
	}

	// Rejected writes must leave the stored map untouched.
	var persisted []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT preferences FROM notification_preference
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID).Scan(&persisted); err != nil {
		t.Fatalf("load persisted preference: %v", err)
	}
	var stored map[string]string
	if err := json.Unmarshal(persisted, &stored); err != nil {
		t.Fatalf("decode persisted preference: %v", err)
	}
	if stored["inbox_mode"] != "all" || stored["focused_mentioned"] != "on" {
		t.Fatalf("rejected write changed stored preferences: %#v", stored)
	}
}

// The DB constraints are the last guard for writers that bypass the handler.
func TestNotificationPreferenceConstraintsRejectNonStringValues(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	resetPrefs := func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM notification_preference
			WHERE workspace_id = $1 AND user_id = $2
		`, testWorkspaceID, testUserID)
	}
	resetPrefs()
	t.Cleanup(resetPrefs)

	insert := func(raw string) error {
		_, err := testPool.Exec(context.Background(), `
			INSERT INTO notification_preference (workspace_id, user_id, preferences)
			VALUES ($1, $2, $3::jsonb)
		`, testWorkspaceID, testUserID, raw)
		return err
	}

	rejects := []struct{ name, raw string }{
		{"array of strings", `{"comments":["muted"]}`},
		{"empty array", `{"comments":[]}`},
		{"focused rule as array", `{"focused_blocked":["on"]}`},
		{"number", `{"comments":1}`},
		{"nested object", `{"comments":{"a":"muted"}}`},
		{"top-level array", `["muted"]`},
	}
	for _, tc := range rejects {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			err := insert(tc.raw)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" ||
				pgErr.ConstraintName != "notification_preference_values_are_strings" {
				resetPrefs()
				t.Fatalf("insert %s: want values_are_strings check violation, got %v", tc.raw, err)
			}
		})
	}

	for _, raw := range []string{`{}`, `{"comments":"muted","inbox_mode":"focused","focused_blocked":"on"}`} {
		if err := insert(raw); err != nil {
			t.Fatalf("insert %s: %v", raw, err)
		}
		resetPrefs()
	}
}
