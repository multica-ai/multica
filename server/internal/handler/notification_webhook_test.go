package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateNotificationWebhookAllowsMissingWorkspaceID(t *testing.T) {
	clearNotificationWebhooksForTestUser(t)

	req := newRequest("POST", "/api/me/notification-webhooks", CreateNotificationWebhookRequest{
		Name:          "No workspace webhook",
		URL:           "https://93.184.216.34/webhook",
		ContentPrefix: "",
	})
	w := httptest.NewRecorder()
	testHandler.CreateMyNotificationWebhook(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp NotificationWebhookResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("expected webhook id in response")
	}
	if resp.WorkspaceID != nil {
		t.Fatalf("expected nil workspace_id, got %q", *resp.WorkspaceID)
	}
}

func TestCreateNotificationWebhookRejectsInvalidWorkspaceID(t *testing.T) {
	clearNotificationWebhooksForTestUser(t)

	req := newRequest("POST", "/api/me/notification-webhooks", CreateNotificationWebhookRequest{
		Name:      "Bad workspace webhook",
		URL:       "https://93.184.216.34/webhook",
		Workspace: "not-a-uuid",
	})
	w := httptest.NewRecorder()
	testHandler.CreateMyNotificationWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func clearNotificationWebhooksForTestUser(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM notification_webhook_endpoint WHERE user_id = $1`, parseUUID(testUserID)); err != nil {
		t.Fatalf("clear notification webhooks: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM notification_webhook_endpoint WHERE user_id = $1`, parseUUID(testUserID))
	})
}
