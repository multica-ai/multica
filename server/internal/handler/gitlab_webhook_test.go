package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testWebhookSecret = "wh-secret-abc-123"
)

// webhookHandler returns a gitlab-enabled Handler suitable for webhook receiver
// tests. It uses the shared testPool so all DB fixtures are accessible.
func webhookHandler(t *testing.T) *Handler {
	t.Helper()
	return buildHandlerWithGitlab(t, "http://localhost:0")
}

// seedConnectionWithWebhookSecret inserts a connection row with the test
// webhook secret. Cleans up any prior test rows first.
func seedConnectionWithWebhookSecret(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testHandler.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id,
			webhook_secret, webhook_gitlab_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, $2, 11, 'connected')
	`, testWorkspaceID, testWebhookSecret); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
}

func TestReceiveGitlabWebhook_PersistsValidEvent(t *testing.T) {
	h := webhookHandler(t)
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	payload := map[string]any{
		"object_kind": "issue",
		"object_attributes": map[string]any{
			"iid":        42,
			"updated_at": "2026-04-17T10:00:00Z",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", testWebhookSecret)
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	rr := httptest.NewRecorder()

	h.ReceiveGitlabWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND event_type = 'issue' AND object_id = 42`,
		testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 queued event, got %d", count)
	}

	var lastReceivedNotNull bool
	testPool.QueryRow(context.Background(),
		`SELECT last_webhook_received_at IS NOT NULL FROM workspace_gitlab_connection WHERE workspace_id = $1::uuid`,
		testWorkspaceID).Scan(&lastReceivedNotNull)
	if !lastReceivedNotNull {
		t.Errorf("last_webhook_received_at should have been bumped")
	}
}

func TestReceiveGitlabWebhook_RejectsUnknownSecret(t *testing.T) {
	h := webhookHandler(t)
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Gitlab-Token", "wrong-secret")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	rr := httptest.NewRecorder()

	h.ReceiveGitlabWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestReceiveGitlabWebhook_DuplicateDeliveryIsNoop(t *testing.T) {
	h := webhookHandler(t)
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	payload := map[string]any{
		"object_kind": "issue",
		"object_attributes": map[string]any{
			"iid":        42,
			"updated_at": "2026-04-17T10:00:00Z",
		},
	}
	body, _ := json.Marshal(payload)

	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gitlab-Token", testWebhookSecret)
		req.Header.Set("X-Gitlab-Event", "Issue Hook")
		rr := httptest.NewRecorder()
		h.ReceiveGitlabWebhook(rr, req)
		return rr.Code
	}
	if got := post(); got != http.StatusOK {
		t.Fatalf("first delivery status = %d", got)
	}
	if got := post(); got != http.StatusOK {
		t.Fatalf("second (duplicate) delivery status = %d", got)
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND event_type = 'issue' AND object_id = 42`,
		testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row after dedupe, got %d", count)
	}
}

func TestReceiveGitlabWebhook_NoteEvent(t *testing.T) {
	h := webhookHandler(t)
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	payload := map[string]any{
		"object_kind": "note",
		"object_attributes": map[string]any{
			"id":            100,
			"updated_at":    "2026-04-17T11:00:00Z",
			"noteable_type": "Issue",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", testWebhookSecret)
	req.Header.Set("X-Gitlab-Event", "Note Hook")
	rr := httptest.NewRecorder()

	h.ReceiveGitlabWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND event_type = 'note' AND object_id = 100`,
		testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 queued note event, got %d", count)
	}

	expectedHash := sha256.Sum256(body)
	var stored []byte
	testPool.QueryRow(context.Background(),
		`SELECT payload_hash FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND object_id = 100`,
		testWorkspaceID).Scan(&stored)
	if !bytes.Equal(stored, expectedHash[:]) {
		t.Errorf("stored payload_hash differs from sha256(body)\nstored:   %x\nexpected: %x", stored, expectedHash[:])
	}
}
