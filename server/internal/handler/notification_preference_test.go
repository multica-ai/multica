package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestNotificationPreferenceAPI(t *testing.T) {
	t.Run("GetDefault", func(t *testing.T) {
		// First fetch returns empty defaults.
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/notification-preferences", nil)
		testHandler.GetNotificationPreference(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp NotificationPreferenceResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.NtfyURL != "" {
			t.Errorf("expected empty ntfy_url, got %q", resp.NtfyURL)
		}
		if len(resp.DisabledTypes) != 0 {
			t.Errorf("expected empty disabled_types, got %v", resp.DisabledTypes)
		}
	})

	t.Run("UpsertAndGet", func(t *testing.T) {
		// Save preferences.
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/notification-preferences", map[string]any{
			"ntfy_url":       "https://ntfy.sh/test-topic",
			"ntfy_token":     "secret",
			"disabled_types": []string{"reaction_added", "info"},
		})
		testHandler.UpsertNotificationPreference(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("upsert: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var upserted NotificationPreferenceResponse
		json.NewDecoder(w.Body).Decode(&upserted)
		if upserted.NtfyURL != "https://ntfy.sh/test-topic" {
			t.Errorf("ntfy_url = %q", upserted.NtfyURL)
		}
		if len(upserted.DisabledTypes) != 2 {
			t.Errorf("disabled_types len = %d, want 2", len(upserted.DisabledTypes))
		}

		// Verify GET returns saved data.
		w2 := httptest.NewRecorder()
		req2 := newRequest("GET", "/api/notification-preferences", nil)
		testHandler.GetNotificationPreference(w2, req2)
		if w2.Code != http.StatusOK {
			t.Fatalf("get after upsert: expected 200, got %d: %s", w2.Code, w2.Body.String())
		}
		var fetched NotificationPreferenceResponse
		json.NewDecoder(w2.Body).Decode(&fetched)
		if fetched.NtfyURL != "https://ntfy.sh/test-topic" {
			t.Errorf("get: ntfy_url = %q", fetched.NtfyURL)
		}

		// Cleanup.
		testHandler.Queries.UpsertNotificationPreference(req.Context(), db.UpsertNotificationPreferenceParams{
			UserID:        parseUUID(testUserID),
			NtfyUrl:       strToText(""),
			NtfyToken:     strToText(""),
			DisabledTypes: []string{},
		})
	})

	t.Run("UpsertIdempotent", func(t *testing.T) {
		// Calling upsert twice should not create duplicate rows.
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			req := newRequest("PUT", "/api/notification-preferences", map[string]any{
				"ntfy_url":       "https://ntfy.sh/idempotent",
				"ntfy_token":     "",
				"disabled_types": []string{},
			})
			testHandler.UpsertNotificationPreference(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("upsert %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
			}
		}
		// Cleanup.
		testHandler.Queries.UpsertNotificationPreference(context.Background(), db.UpsertNotificationPreferenceParams{
			UserID:        parseUUID(testUserID),
			NtfyUrl:       strToText(""),
			NtfyToken:     strToText(""),
			DisabledTypes: []string{},
		})
	})

	t.Run("TestEndpointBadURL", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/notification-preferences/test", map[string]any{
			"ntfy_url": "",
		})
		testHandler.TestNotificationPreference(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}
