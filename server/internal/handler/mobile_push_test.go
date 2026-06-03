package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cleanupMobilePushRegistrations(t *testing.T, userIDs ...string) {
	t.Helper()
	for _, userID := range userIDs {
		if _, err := testPool.Exec(context.Background(), `
			DELETE FROM mobile_push_registration WHERE user_id = $1
		`, userID); err != nil {
			t.Fatalf("delete mobile_push_registration: %v", err)
		}
	}
}

func TestUpsertMyMobilePushRegistration_CreateAndUpdate(t *testing.T) {
	cleanupMobilePushRegistrations(t, testUserID)
	t.Cleanup(func() { cleanupMobilePushRegistrations(t, testUserID) })

	reqBody := map[string]any{
		"installation_id":    "install-1",
		"platform":           "android",
		"provider":           "getui",
		"provider_client_id": "cid-1",
		"app_version":        "0.1.2",
	}
	w := httptest.NewRecorder()
	testHandler.UpsertMyMobilePushRegistration(w, newRequest(http.MethodPut, "/api/me/mobile-push/registrations", reqBody))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp MobilePushRegistrationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.UserID != testUserID || resp.InstallationID != "install-1" || resp.ProviderClientID != "cid-1" || !resp.Enabled {
		t.Fatalf("unexpected registration response: %#v", resp)
	}

	updateBody := map[string]any{
		"installation_id":    "install-1",
		"platform":           "android",
		"provider":           "getui",
		"provider_client_id": "cid-2",
		"app_version":        "0.1.3",
	}
	w = httptest.NewRecorder()
	testHandler.UpsertMyMobilePushRegistration(w, newRequest(http.MethodPut, "/api/me/mobile-push/registrations", updateBody))
	if w.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	var cid string
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*), max(provider_client_id)
		FROM mobile_push_registration
		WHERE user_id = $1 AND installation_id = 'install-1' AND provider = 'getui'
	`, testUserID).Scan(&count, &cid); err != nil {
		t.Fatalf("query mobile_push_registration: %v", err)
	}
	if count != 1 || cid != "cid-2" {
		t.Fatalf("expected one updated row with cid-2, got count=%d cid=%q", count, cid)
	}
}

func TestUpsertMyMobilePushRegistration_AcceptsIOSAPNS(t *testing.T) {
	cleanupMobilePushRegistrations(t, testUserID)
	t.Cleanup(func() { cleanupMobilePushRegistrations(t, testUserID) })

	reqBody := map[string]any{
		"installation_id":    "ios-install-1",
		"platform":           "ios",
		"provider":           "apns",
		"provider_client_id": "apns-token-1",
	}
	w := httptest.NewRecorder()
	testHandler.UpsertMyMobilePushRegistration(w, newRequest(http.MethodPut, "/api/me/mobile-push/registrations", reqBody))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp MobilePushRegistrationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Platform != "ios" || resp.Provider != "apns" || resp.ProviderClientID != "apns-token-1" || !resp.Enabled {
		t.Fatalf("unexpected registration response: %#v", resp)
	}
}

func TestUpsertMyMobilePushRegistration_RejectsUnknownOrMismatchedProvider(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		provider string
	}{
		{name: "unknown provider", platform: "ios", provider: "unknown"},
		{name: "ios getui", platform: "ios", provider: "getui"},
		{name: "android apns", platform: "android", provider: "apns"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]any{
				"installation_id":    "install-invalid",
				"platform":           tt.platform,
				"provider":           tt.provider,
				"provider_client_id": "token-invalid",
			}
			w := httptest.NewRecorder()
			testHandler.UpsertMyMobilePushRegistration(w, newRequest(http.MethodPut, "/api/me/mobile-push/registrations", reqBody))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestDisableMyMobilePushRegistration_UserScoped(t *testing.T) {
	otherUserID := createMobilePushTestUser(t)
	cleanupMobilePushRegistrations(t, testUserID, otherUserID)
	t.Cleanup(func() {
		cleanupMobilePushRegistrations(t, testUserID, otherUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO mobile_push_registration (
			user_id, installation_id, platform, provider, provider_client_id
		)
		VALUES
			($1, 'own-install', 'android', 'getui', 'cid-own'),
			($2, 'other-install', 'android', 'getui', 'cid-other')
	`, testUserID, otherUserID); err != nil {
		t.Fatalf("insert mobile_push_registration: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest(http.MethodDelete, "/api/me/mobile-push/registrations/other-install?provider=getui", nil),
		"installationId",
		"other-install",
	)
	testHandler.DisableMyMobilePushRegistration(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var otherEnabled bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT enabled FROM mobile_push_registration
		WHERE user_id = $1 AND installation_id = 'other-install'
	`, otherUserID).Scan(&otherEnabled); err != nil {
		t.Fatalf("query other registration: %v", err)
	}
	if !otherEnabled {
		t.Fatal("other user's registration was disabled")
	}

	w = httptest.NewRecorder()
	req = withURLParam(
		newRequest(http.MethodDelete, "/api/me/mobile-push/registrations/own-install?provider=getui", nil),
		"installationId",
		"own-install",
	)
	testHandler.DisableMyMobilePushRegistration(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected own disable 204, got %d: %s", w.Code, w.Body.String())
	}

	var ownEnabled bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT enabled FROM mobile_push_registration
		WHERE user_id = $1 AND installation_id = 'own-install'
	`, testUserID).Scan(&ownEnabled); err != nil {
		t.Fatalf("query own registration: %v", err)
	}
	if ownEnabled {
		t.Fatal("own registration was not disabled")
	}
}

func TestDisableMyMobilePushRegistration_APNSOnlyAffectsMatchingProvider(t *testing.T) {
	cleanupMobilePushRegistrations(t, testUserID)
	t.Cleanup(func() { cleanupMobilePushRegistrations(t, testUserID) })

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO mobile_push_registration (
			user_id, installation_id, platform, provider, provider_client_id
		)
		VALUES
			($1, 'shared-install', 'android', 'getui', 'cid-shared'),
			($1, 'shared-install', 'ios', 'apns', 'token-shared')
	`, testUserID); err != nil {
		t.Fatalf("insert mobile_push_registration: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest(http.MethodDelete, "/api/me/mobile-push/registrations/shared-install?provider=apns", nil),
		"installationId",
		"shared-install",
	)
	testHandler.DisableMyMobilePushRegistration(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	rows, err := testPool.Query(context.Background(), `
		SELECT provider, enabled
		FROM mobile_push_registration
		WHERE user_id = $1 AND installation_id = 'shared-install'
	`, testUserID)
	if err != nil {
		t.Fatalf("query mobile_push_registration: %v", err)
	}
	defer rows.Close()
	states := map[string]bool{}
	for rows.Next() {
		var provider string
		var enabled bool
		if err := rows.Scan(&provider, &enabled); err != nil {
			t.Fatalf("scan mobile_push_registration: %v", err)
		}
		states[provider] = enabled
	}
	if !states["getui"] || states["apns"] {
		t.Fatalf("expected getui enabled and apns disabled, got %#v", states)
	}
}

func createMobilePushTestUser(t *testing.T) string {
	t.Helper()

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ('Mobile Push Other', 'mobile-push-other@example.test')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("create mobile push test user: %v", err)
	}
	return userID
}
