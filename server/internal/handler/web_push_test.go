package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeWebPushManager struct {
	userID   string
	endpoint string
	p256dh   string
	auth     string
}

func (f *fakeWebPushManager) PublicKey() (string, bool) { return "public-key", true }
func (f *fakeWebPushManager) Upsert(_ context.Context, userID, endpoint, p256dh, auth string) error {
	f.userID, f.endpoint, f.p256dh, f.auth = userID, endpoint, p256dh, auth
	return nil
}
func (f *fakeWebPushManager) Delete(_ context.Context, userID, endpoint string) error {
	f.userID, f.endpoint = userID, endpoint
	return nil
}

func TestWebPushConfigExposesOnlyApplicationServerPublicKey(t *testing.T) {
	h := &Handler{WebPush: &fakeWebPushManager{}}
	recorder := httptest.NewRecorder()
	h.GetWebPushConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/web-push/config", nil))

	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["public_key"] != "public-key" || response["enabled"] != true {
		t.Fatalf("response = %#v", response)
	}
	if _, exists := response["private_key"]; exists {
		t.Fatalf("response exposed private key")
	}
}

func TestUpsertWebPushSubscriptionUsesAuthenticatedUser(t *testing.T) {
	manager := &fakeWebPushManager{}
	h := &Handler{WebPush: manager}
	recorder := httptest.NewRecorder()
	request := newRequestAs("user-1", http.MethodPost, "/api/web-push/subscriptions", map[string]any{
		"endpoint": "https://push.test/subscription-1",
		"keys":     map[string]string{"p256dh": "key", "auth": "auth"},
	})
	h.UpsertWebPushSubscription(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if manager.userID != "user-1" || manager.endpoint != "https://push.test/subscription-1" {
		t.Fatalf("manager = %#v", manager)
	}
}

func TestUpsertWebPushSubscriptionRejectsExternalShapeBeforeWrite(t *testing.T) {
	manager := &fakeWebPushManager{}
	h := &Handler{WebPush: manager}
	recorder := httptest.NewRecorder()
	request := newRequestAs("user-1", http.MethodPost, "/api/web-push/subscriptions", map[string]any{
		"endpoint": "javascript:alert(1)",
		"keys":     map[string]string{},
	})
	h.UpsertWebPushSubscription(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
	if manager.endpoint != "" {
		t.Fatalf("invalid subscription reached manager")
	}
}
