package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cleanupNotificationSettings(t *testing.T) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM notification_channel_preference WHERE user_id = $1
	`, testUserID); err != nil {
		t.Fatalf("delete notification_channel_preference: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM external_account_binding WHERE user_id = $1
	`, testUserID); err != nil {
		t.Fatalf("delete external_account_binding: %v", err)
	}
}

func createNotificationBinding(t *testing.T, provider string) string {
	t.Helper()

	var bindingID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO external_account_binding (
			user_id, provider, external_user_id, display_name, status, metadata
		)
		VALUES ($1, $2, $3, $4, 'active', '{}'::jsonb)
		RETURNING id
	`, testUserID, provider, provider+"-user", "Bound "+provider).Scan(&bindingID); err != nil {
		t.Fatalf("insert external_account_binding: %v", err)
	}
	return bindingID
}

func TestNotificationPreferences_Defaults(t *testing.T) {
	cleanupNotificationSettings(t)
	t.Cleanup(func() { cleanupNotificationSettings(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/me/notification-preferences", nil)

	testHandler.GetMyNotificationPreferences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	assertJSONEqual(t, w.Body.Bytes(), `{
		"preferences": [
			{
				"channel": "inbox",
				"event_type": "mentioned",
				"enabled": true,
				"binding_id": null,
				"requires_binding": false
			},
			{
				"channel": "dingtalk",
				"event_type": "mentioned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": true
			},
			{
				"channel": "email",
				"event_type": "mentioned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": true
			},
			{
				"channel": "custom_webhook",
				"event_type": "mentioned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": false
			},
			{
				"channel": "custom_webhook",
				"event_type": "issue_assigned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": false
			},
			{
				"channel": "custom_webhook",
				"event_type": "subscribed_issue_updated",
				"enabled": false,
				"binding_id": null,
				"requires_binding": false
			}
		]
	}`)
}

// TestUpdateNotificationPreference_RenderModeOnly verifies that updating only
// render_mode (without passing enabled) succeeds and preserves the existing enabled value.
func TestUpdateNotificationPreference_RenderModeOnly(t *testing.T) {
	cleanupNotificationSettings(t)
	t.Cleanup(func() { cleanupNotificationSettings(t) })

	// First, create a preference with enabled=true
	w1 := httptest.NewRecorder()
	req1 := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":    "inbox",
		"event_type": "mentioned",
		"enabled":    true,
	})
	testHandler.UpdateMyNotificationPreference(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Now update only render_mode — no enabled field
	w2 := httptest.NewRecorder()
	req2 := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":     "inbox",
		"event_type":  "mentioned",
		"render_mode": "compact",
	})
	testHandler.UpdateMyNotificationPreference(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("render_mode-only update: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp NotificationPreferenceResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RenderMode != "compact" {
		t.Errorf("expected render_mode 'compact', got %q", resp.RenderMode)
	}
	if !resp.Enabled {
		t.Errorf("expected enabled to remain true after render_mode-only update")
	}
}

// TestUpdateNotificationPreference_RenderModeOnly_NoExisting verifies that
// updating render_mode when no preference exists uses the spec default for enabled.
func TestUpdateNotificationPreference_RenderModeOnly_NoExisting(t *testing.T) {
	cleanupNotificationSettings(t)
	t.Cleanup(func() { cleanupNotificationSettings(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":     "inbox",
		"event_type":  "mentioned",
		"render_mode": "detail",
	})
	testHandler.UpdateMyNotificationPreference(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp NotificationPreferenceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RenderMode != "detail" {
		t.Errorf("expected render_mode 'detail', got %q", resp.RenderMode)
	}
	// inbox/mentioned spec default is enabled=true
	if !resp.Enabled {
		t.Errorf("expected enabled to be spec default (true) for new preference")
	}
}

func TestUpdateNotificationPreference_RequiresBinding(t *testing.T) {
	cleanupNotificationSettings(t)
	t.Cleanup(func() { cleanupNotificationSettings(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":    "dingtalk",
		"event_type": "mentioned",
		"enabled":    true,
	})

	testHandler.UpdateMyNotificationPreference(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	assertJSONEqual(t, w.Body.Bytes(), `{"error":"dingtalk account is not connected"}`)
}

func TestNotificationBindingsLifecycle(t *testing.T) {
	cleanupNotificationSettings(t)
	t.Cleanup(func() { cleanupNotificationSettings(t) })

	bindingID := createNotificationBinding(t, "dingtalk")

	wBindings := httptest.NewRecorder()
	reqBindings := newRequest(http.MethodGet, "/api/me/notification-bindings", nil)
	testHandler.GetMyNotificationBindings(wBindings, reqBindings)
	if wBindings.Code != http.StatusOK {
		t.Fatalf("GetMyNotificationBindings: expected 200, got %d: %s", wBindings.Code, wBindings.Body.String())
	}

	var bindingsResp ListNotificationBindingsResponse
	if err := json.NewDecoder(wBindings.Body).Decode(&bindingsResp); err != nil {
		t.Fatalf("decode bindings response: %v", err)
	}
	if len(bindingsResp.Bindings) < 1 {
		t.Fatalf("expected at least 1 binding, got %d", len(bindingsResp.Bindings))
	}
	// Find the DingTalk binding we explicitly created.
	var foundDingTalk bool
	for _, b := range bindingsResp.Bindings {
		if b.ID == bindingID {
			foundDingTalk = true
		}
	}
	if !foundDingTalk {
		t.Fatalf("expected dingtalk binding %q in response", bindingID)
	}

	wUpdate := httptest.NewRecorder()
	reqUpdate := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":    "dingtalk",
		"event_type": "mentioned",
		"enabled":    true,
	})
	testHandler.UpdateMyNotificationPreference(wUpdate, reqUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("UpdateMyNotificationPreference: expected 200, got %d: %s", wUpdate.Code, wUpdate.Body.String())
	}

	var updatedPref NotificationPreferenceResponse
	if err := json.NewDecoder(wUpdate.Body).Decode(&updatedPref); err != nil {
		t.Fatalf("decode preference response: %v", err)
	}
	if !updatedPref.Enabled {
		t.Fatal("expected dingtalk preference to be enabled")
	}
	if updatedPref.BindingID == nil || *updatedPref.BindingID != bindingID {
		t.Fatalf("expected binding_id %q, got %#v", bindingID, updatedPref.BindingID)
	}

	wDelete := httptest.NewRecorder()
	reqDelete := withURLParam(newRequest(http.MethodDelete, "/api/me/notification-bindings/"+bindingID, nil), "bindingId", bindingID)
	testHandler.DeleteMyNotificationBinding(wDelete, reqDelete)
	if wDelete.Code != http.StatusNoContent {
		t.Fatalf("DeleteMyNotificationBinding: expected 204, got %d: %s", wDelete.Code, wDelete.Body.String())
	}

	wPrefs := httptest.NewRecorder()
	reqPrefs := newRequest(http.MethodGet, "/api/me/notification-preferences", nil)
	testHandler.GetMyNotificationPreferences(wPrefs, reqPrefs)
	if wPrefs.Code != http.StatusOK {
		t.Fatalf("GetMyNotificationPreferences: expected 200, got %d: %s", wPrefs.Code, wPrefs.Body.String())
	}
	assertJSONEqual(t, wPrefs.Body.Bytes(), `{
		"preferences": [
			{
				"channel": "inbox",
				"event_type": "mentioned",
				"enabled": true,
				"binding_id": null,
				"requires_binding": false
			},
			{
				"channel": "dingtalk",
				"event_type": "mentioned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": true
			},
			{
				"channel": "email",
				"event_type": "mentioned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": true
			},
			{
				"channel": "custom_webhook",
				"event_type": "mentioned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": false
			},
			{
				"channel": "custom_webhook",
				"event_type": "issue_assigned",
				"enabled": false,
				"binding_id": null,
				"requires_binding": false
			},
			{
				"channel": "custom_webhook",
				"event_type": "subscribed_issue_updated",
				"enabled": false,
				"binding_id": null,
				"requires_binding": false
			}
		]
	}`)
}
