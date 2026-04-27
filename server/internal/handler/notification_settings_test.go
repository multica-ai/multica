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
			}
		]
	}`)
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
	if len(bindingsResp.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindingsResp.Bindings))
	}
	if bindingsResp.Bindings[0].ID != bindingID {
		t.Fatalf("expected binding id %q, got %q", bindingID, bindingsResp.Bindings[0].ID)
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
			}
		]
	}`)
}
