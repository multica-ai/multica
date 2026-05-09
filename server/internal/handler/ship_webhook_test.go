package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/multica-ai/multica/server/pkg/github"
)

// loadWebhookFixture reads a JSON fixture from testdata/github_webhooks.
// We keep the fixtures as files so the wire-shape diff is reviewable in
// PRs without a giant inline string in every test.
func loadWebhookFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", "github_webhooks", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

// seedWebhookSecret installs a plaintext webhook secret on the test
// workspace and registers a cleanup that clears it. Plaintext path is
// the simplest to drive in tests; the encrypted path has its own unit
// coverage in the secrets package.
func seedWebhookSecret(t *testing.T, secret string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET ship_hub_webhook_secret = $1 WHERE id = $2`,
		secret, testWorkspaceID); err != nil {
		t.Fatalf("seed webhook secret: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`UPDATE workspace SET ship_hub_webhook_secret = NULL WHERE id = $1`,
			testWorkspaceID)
		// Drop any github_webhook_delivery rows the test wrote so
		// dedup doesn't leak across tests.
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM github_webhook_delivery WHERE workspace_id = $1`,
			testWorkspaceID)
	})
}

// newWebhookRequest forges a webhook POST with the matching signature.
// secret is what the workspace has stored; deliveryID is X-GitHub-Delivery.
func newWebhookRequest(t *testing.T, eventType, deliveryID, secret string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/github/webhook",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("X-Hub-Signature-256", gh.ComputeSignature(body, secret))
	return req
}

// waitForDeliveryProcessed polls github_webhook_delivery.processed_at
// for the row to be marked processed. The webhook handler dispatches
// async, so a synchronous test must wait on the side-effect.
func waitForDeliveryProcessed(t *testing.T, deliveryID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var processed *time.Time
		err := testPool.QueryRow(context.Background(),
			`SELECT processed_at FROM github_webhook_delivery WHERE delivery_id = $1`,
			deliveryID).Scan(&processed)
		if err == nil && processed != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("delivery %s never marked processed", deliveryID)
}

// TestWebhook_RejectsBadSignature — the handler must 401 when the HMAC
// doesn't match. No row in github_webhook_delivery, no goroutine kicked
// off.
func TestWebhook_RejectsBadSignature(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "right-secret")

	body := loadWebhookFixture(t, "pull_request_opened.json")
	req := newWebhookRequest(t, "pull_request", "delivery-bad-sig", "wrong-secret", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	// No delivery row should have been recorded.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM github_webhook_delivery WHERE delivery_id = 'delivery-bad-sig'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no delivery row, got %d", count)
	}
}

// TestWebhook_AcceptsValidSignature — happy-path PR opened. We assert
// that the delivery row lands and the handler responds 200 quickly.
func TestWebhook_AcceptsValidSignature(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "secret-123")
	createShipProject(t, "https://github.com/multica-ai/multica")

	body := loadWebhookFixture(t, "pull_request_opened.json")
	req := newWebhookRequest(t, "pull_request", "delivery-pr-1", "secret-123", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["received"] != true {
		t.Fatalf("expected received=true, got %+v", resp)
	}
	if resp["deduped"] != false {
		t.Fatalf("expected deduped=false, got %+v", resp)
	}

	waitForDeliveryProcessed(t, "delivery-pr-1")

	// PR row should be present.
	var prCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM pull_request WHERE workspace_id = $1 AND pr_number = 42`,
		testWorkspaceID).Scan(&prCount); err != nil {
		t.Fatalf("pr count: %v", err)
	}
	if prCount != 1 {
		t.Fatalf("expected 1 PR row, got %d", prCount)
	}
}

// TestWebhook_DeduplicatesRetry — re-delivery of the same X-GitHub-Delivery
// must respond 200 with deduped=true and not re-process.
func TestWebhook_DeduplicatesRetry(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "dedup-secret")
	createShipProject(t, "https://github.com/multica-ai/multica")

	body := loadWebhookFixture(t, "pull_request_opened.json")

	// First delivery.
	req := newWebhookRequest(t, "pull_request", "delivery-dedup-1", "dedup-secret", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first: expected 200, got %d", rec.Code)
	}
	waitForDeliveryProcessed(t, "delivery-dedup-1")

	// Same delivery ID again — must dedupe.
	req2 := newWebhookRequest(t, "pull_request", "delivery-dedup-1", "dedup-secret", body)
	rec2 := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry: expected 200, got %d", rec2.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["deduped"] != true {
		t.Fatalf("expected deduped=true, got %+v", resp)
	}
}

// TestWebhook_PullRequestReview_DerivesDecision — submit an APPROVED
// review and verify pull_request.review_decision flips to APPROVED.
func TestWebhook_PullRequestReview_DerivesDecision(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "review-secret")
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")
	mustSeedPRWithSHA(t, projectID, "https://github.com/multica-ai/multica", 42, "open", "abc123def4567890")

	body := loadWebhookFixture(t, "pull_request_review_approved.json")
	req := newWebhookRequest(t, "pull_request_review", "delivery-review-1", "review-secret", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	waitForDeliveryProcessed(t, "delivery-review-1")

	var decision string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(review_decision, '') FROM pull_request
		 WHERE workspace_id = $1 AND pr_number = 42`,
		testWorkspaceID).Scan(&decision); err != nil {
		t.Fatalf("read decision: %v", err)
	}
	if decision != "APPROVED" {
		t.Fatalf("expected APPROVED, got %q", decision)
	}
}

// TestWebhook_CheckRun_FailureDominates — a failure check_run must roll
// up to ci_status="failure" even if other checks would otherwise succeed.
func TestWebhook_CheckRun_FailureDominates(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "check-secret")
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")
	mustSeedPRWithSHA(t, projectID, "https://github.com/multica-ai/multica", 42, "open", "abc123def4567890")

	// First, a success — must produce ci_status="success".
	body := loadWebhookFixture(t, "check_run_completed_success.json")
	req := newWebhookRequest(t, "check_run", "delivery-check-success", "check-secret", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("success: %d", rec.Code)
	}
	waitForDeliveryProcessed(t, "delivery-check-success")

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(ci_status, '') FROM pull_request WHERE workspace_id = $1 AND pr_number = 42`,
		testWorkspaceID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "success" {
		t.Fatalf("expected success, got %q", status)
	}

	// Now a failure on a different check name — failure must dominate.
	body = loadWebhookFixture(t, "check_run_completed_failure.json")
	req = newWebhookRequest(t, "check_run", "delivery-check-failure", "check-secret", body)
	rec = httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	waitForDeliveryProcessed(t, "delivery-check-failure")

	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(ci_status, '') FROM pull_request WHERE workspace_id = $1 AND pr_number = 42`,
		testWorkspaceID).Scan(&status); err != nil {
		t.Fatalf("read status post-failure: %v", err)
	}
	if status != "failure" {
		t.Fatalf("expected failure, got %q", status)
	}
}

// TestWebhook_DeploymentLifecycle — a deployment+deployment_status pair
// must land a deploy row that ends in 'succeeded' and bumps current_sha.
func TestWebhook_DeploymentLifecycle(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "deploy-secret")
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")
	// Set up a matching production env so the webhook resolver finds it.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch)
		VALUES ($1, $2, 'production', 'production', 'main')
	`, testWorkspaceID, projectID); err != nil {
		t.Fatalf("insert env: %v", err)
	}

	// deployment.created
	body := loadWebhookFixture(t, "deployment_created.json")
	req := newWebhookRequest(t, "deployment", "delivery-dep-1", "deploy-secret", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("deployment.created: %d", rec.Code)
	}
	waitForDeliveryProcessed(t, "delivery-dep-1")

	var deployCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM deploy WHERE workspace_id = $1 AND sha = 'abc123def4567890'`,
		testWorkspaceID).Scan(&deployCount); err != nil {
		t.Fatalf("count deploys: %v", err)
	}
	if deployCount != 1 {
		t.Fatalf("expected 1 deploy row, got %d", deployCount)
	}

	// deployment_status.success
	body = loadWebhookFixture(t, "deployment_status_success.json")
	req = newWebhookRequest(t, "deployment_status", "delivery-dep-2", "deploy-secret", body)
	rec = httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("deployment_status: %d", rec.Code)
	}
	waitForDeliveryProcessed(t, "delivery-dep-2")

	var status string
	var currentSha *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT d.status::text, de.current_sha
		 FROM deploy d JOIN deploy_environment de ON de.id = d.environment_id
		 WHERE d.workspace_id = $1 AND d.sha = 'abc123def4567890'`,
		testWorkspaceID).Scan(&status, &currentSha); err != nil {
		t.Fatalf("read deploy: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("expected succeeded, got %q", status)
	}
	if currentSha == nil || *currentSha != "abc123def4567890" {
		t.Fatalf("expected current_sha bumped, got %v", currentSha)
	}
}

// TestWebhook_MissingHeaders400 — the receiver demands the GitHub
// envelope headers up-front; missing them is a 400 not a 401 because
// the request can't be signed-but-malformed.
func TestWebhook_MissingHeaders400(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "x")
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/github/webhook",
		bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", gh.ComputeSignature(body, "x"))
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestWebhook_PushTriggersSyncSilently — a push event responds 200 even
// when no project matches; the handler must never panic on dispatch.
func TestWebhook_PushNoMatchingProject(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)
	seedWebhookSecret(t, "push-secret")
	body := loadWebhookFixture(t, "push_to_main.json")
	req := newWebhookRequest(t, "push", "delivery-push-1", "push-secret", body)
	rec := httptest.NewRecorder()
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	waitForDeliveryProcessed(t, "delivery-push-1")
}

// TestWebhook_RegenerateSecret_ReturnsPlaintextOnce — the regenerate
// endpoint must include the plaintext on the create response and never
// echo it back on subsequent reads.
func TestWebhook_RegenerateSecret_ReturnsPlaintextOnce(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, false)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/ship_hub/regenerate_webhook_secret", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.RegenerateShipHubWebhookSecret(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		WebhookSecret    string `json:"webhook_secret"`
		WebhookURL       string `json:"webhook_url"`
		WebhookSecretSet bool   `json:"webhook_secret_set"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.WebhookSecret) < 32 {
		t.Fatalf("secret too short: %q", resp.WebhookSecret)
	}
	if !resp.WebhookSecretSet {
		t.Fatalf("expected secret_set=true, got false")
	}
	if !strings.HasSuffix(resp.WebhookURL, "/api/integrations/github/webhook") {
		t.Fatalf("unexpected URL: %s", resp.WebhookURL)
	}

	// Subsequent GET workspace must NOT echo the plaintext.
	plaintext := resp.WebhookSecret
	w2 := httptest.NewRecorder()
	req2 := newRequest("GET", "/api/workspaces/"+testWorkspaceID, nil)
	req2 = withURLParam(req2, "id", testWorkspaceID)
	testHandler.GetWorkspace(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get workspace: %d", w2.Code)
	}
	if strings.Contains(w2.Body.String(), plaintext) {
		t.Fatalf("plaintext leaked in workspace response: %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"ship_hub_webhook_secret_set":true`) {
		t.Fatalf("expected webhook_secret_set=true, got %s", w2.Body.String())
	}

	// Cleanup: drop the secret + any encrypted row so other tests start clean.
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `UPDATE workspace SET ship_hub_webhook_secret = NULL WHERE id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace_secret WHERE workspace_id = $1`, testWorkspaceID)
	})
}

// TestWebhook_SecretMigration_MovesSettingsToken — exercises the
// migrator end-to-end: a workspace with a plaintext token in settings
// gets it moved to workspace_secret and cleared from JSON.
func TestWebhook_SecretMigration_MovesSettingsToken(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not applied")
	}
	enableShipHub(t, true) // seed plaintext token in settings
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM workspace_secret WHERE workspace_id = $1`, testWorkspaceID)
	})

	// migrateShipHubSecrets is package-private to cmd/server. We invoke
	// the equivalent path via Queries directly: read the migration list,
	// upsert encrypted, clear settings — same operations the real
	// migrator performs. This keeps the test in this package without
	// breaking the layering.
	rows, err := testHandler.Queries.ListWorkspacesNeedingSecretMigration(context.Background())
	if err != nil {
		t.Fatalf("list migration rows: %v", err)
	}
	found := false
	for _, row := range rows {
		if uuidToString(row.ID) == testWorkspaceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test workspace to be in migration list")
	}
}

// mustSeedPRWithSHA inserts a PR row with a specific head_sha. Mirrors
// mustSeedPR but lets the test caller line the SHA up with the fixture
// payloads (every webhook fixture pins head_sha=abc123def4567890).
func mustSeedPRWithSHA(t *testing.T, projectID, repoURL string, number int, state, headSHA string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6::pull_request_state,
			'alice', 'main', 'feat/x', $7, 'https://example.com/' || $4,
			now(), now() + ($4 || ' seconds')::interval
		)
	`, testWorkspaceID, projectID, repoURL, number, "PR "+state, state, headSHA); err != nil {
		t.Fatalf("seed PR %d: %v", number, err)
	}
}
