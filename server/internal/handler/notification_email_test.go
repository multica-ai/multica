package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cleanupEmailBindingTest(t *testing.T) {
	t.Helper()
	cleanupNotificationSettings(t)
	// Clear any verification codes left by previous test runs to avoid rate-limit
	// false-positives (60s throttle on per-email codes).
	if _, err := testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email LIKE '%email%' OR email LIKE '%verify%' OR email LIKE '%ratelimit%'`); err != nil {
		t.Fatalf("delete verification_code: %v", err)
	}
}

func TestEmailBindingStart_InvalidEmail(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/email/start", map[string]any{
		"email": "not-an-email",
	})
	testHandler.StartMyEmailBinding(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertJSONEqual(t, w.Body.Bytes(), `{"error":"invalid email address"}`)
}

func TestEmailBindingStart_EmptyEmail(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/email/start", map[string]any{
		"email": "",
	})
	testHandler.StartMyEmailBinding(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEmailBindingStart_Success(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/email/start", map[string]any{
		"email": "test-email-bind@example.com",
	})
	testHandler.StartMyEmailBinding(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartEmailBindingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Message != "Verification code sent" {
		t.Fatalf("expected message 'Verification code sent', got %q", resp.Message)
	}
}

func TestEmailBindingStart_RateLimit(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	// First request should succeed.
	w1 := httptest.NewRecorder()
	req1 := newRequest(http.MethodPost, "/api/me/notification-bindings/email/start", map[string]any{
		"email": "ratelimit-email-test@example.com",
	})
	testHandler.StartMyEmailBinding(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second request within 60s should be rate-limited.
	w2 := httptest.NewRecorder()
	req2 := newRequest(http.MethodPost, "/api/me/notification-bindings/email/start", map[string]any{
		"email": "ratelimit-email-test@example.com",
	})
	testHandler.StartMyEmailBinding(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestEmailBindingVerify_InvalidCode(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	// First send a code.
	w1 := httptest.NewRecorder()
	req1 := newRequest(http.MethodPost, "/api/me/notification-bindings/email/start", map[string]any{
		"email": "verify-test@example.com",
	})
	testHandler.StartMyEmailBinding(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("start: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Try wrong code.
	w2 := httptest.NewRecorder()
	req2 := newRequest(http.MethodPost, "/api/me/notification-bindings/email/verify", map[string]any{
		"email": "verify-test@example.com",
		"code":  "000000",
	})
	testHandler.VerifyMyEmailBinding(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("verify: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestEmailBindingVerify_MissingFields(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/email/verify", map[string]any{
		"email": "test@example.com",
		"code":  "",
	})
	testHandler.VerifyMyEmailBinding(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEmailNotificationPreference_RequiresBinding(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	// Try to enable email preference without binding.
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":    "email",
		"event_type": "mentioned",
		"enabled":    true,
	})
	testHandler.UpdateMyNotificationPreference(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertJSONEqual(t, w.Body.Bytes(), `{"error":"email account is not connected"}`)
}

func TestEmailNotificationPreference_WithBinding(t *testing.T) {
	cleanupEmailBindingTest(t)
	t.Cleanup(func() { cleanupEmailBindingTest(t) })

	// Create an email binding.
	bindingID := createNotificationBinding(t, "email")

	// Now enable email preference.
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPatch, "/api/me/notification-preferences", map[string]any{
		"channel":    "email",
		"event_type": "mentioned",
		"enabled":    true,
	})
	testHandler.UpdateMyNotificationPreference(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pref NotificationPreferenceResponse
	if err := json.NewDecoder(w.Body).Decode(&pref); err != nil {
		t.Fatalf("decode pref response: %v", err)
	}
	if !pref.Enabled {
		t.Fatal("expected email preference to be enabled")
	}
	if pref.BindingID == nil || *pref.BindingID != bindingID {
		t.Fatalf("expected binding_id %q, got %#v", bindingID, pref.BindingID)
	}
}
