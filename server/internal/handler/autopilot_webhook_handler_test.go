package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// All tests in this file require a working DB. testHandler / testWorkspaceID /
// testUserID / testRuntimeID are wired in TestMain (handler_test.go) and
// TestMain skips the suite if Postgres isn't reachable.

// ── Fixture helpers ─────────────────────────────────────────────────────────

func createWebhookTestAgent(t *testing.T, name string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createWebhookTestAutopilot(t *testing.T, agentID, status, mode string) string {
	t.Helper()
	var apID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (
			workspace_id, title, assignee_id, status, execution_mode,
			created_by_type, created_by_id
		) VALUES ($1, $2, $3, $4, $5, 'member', $6)
		RETURNING id
	`, testWorkspaceID, "Webhook test "+status, agentID, status, mode, testUserID).Scan(&apID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, apID)
	})
	return apID
}

func createWebhookTriggerViaHandler(t *testing.T, autopilotID string) AutopilotTriggerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+autopilotID+"/triggers", map[string]any{
		"kind": "webhook",
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTrigger: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func postWebhook(t *testing.T, token string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	switch v := body.(type) {
	case []byte:
		buf.Write(v)
	case string:
		buf.WriteString(v)
	default:
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest("POST", "/api/webhooks/autopilots/"+token, &buf)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = withURLParam(req, "token", token)
	w := httptest.NewRecorder()
	testHandler.HandleAutopilotWebhook(w, req)
	return w
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestCreateWebhookTrigger_GeneratesToken(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookGen Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	resp := createWebhookTriggerViaHandler(t, apID)
	if resp.Kind != "webhook" {
		t.Fatalf("kind: %q", resp.Kind)
	}
	if resp.WebhookToken == nil || *resp.WebhookToken == "" {
		t.Fatal("webhook_token should be present and non-empty")
	}
	if !strings.HasPrefix(*resp.WebhookToken, "awt_") {
		t.Fatalf("token prefix: %q", *resp.WebhookToken)
	}
	if resp.WebhookPath == nil {
		t.Fatal("webhook_path should be present")
	}
	if !strings.HasSuffix(*resp.WebhookPath, *resp.WebhookToken) {
		t.Fatalf("webhook_path %q should contain token %q", *resp.WebhookPath, *resp.WebhookToken)
	}
}

func TestCreateWebhookTrigger_TwoUniqueTokens(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookUnique Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	a := createWebhookTriggerViaHandler(t, apID)
	b := createWebhookTriggerViaHandler(t, apID)
	if a.WebhookToken == nil || b.WebhookToken == nil {
		t.Fatal("missing tokens")
	}
	if *a.WebhookToken == *b.WebhookToken {
		t.Fatalf("tokens should differ: %q == %q", *a.WebhookToken, *b.WebhookToken)
	}
}

func TestCreateWebhookTrigger_PublicURLAffectsResponse(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookURL Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	prev := testHandler.cfg.PublicURL
	t.Cleanup(func() { testHandler.cfg.PublicURL = prev })

	testHandler.cfg.PublicURL = ""
	respNoURL := createWebhookTriggerViaHandler(t, apID)
	if respNoURL.WebhookURL != nil {
		t.Fatalf("webhook_url should be nil when PublicURL unset, got %q", *respNoURL.WebhookURL)
	}

	testHandler.cfg.PublicURL = "https://app.example"
	respURL := createWebhookTriggerViaHandler(t, apID)
	if respURL.WebhookURL == nil {
		t.Fatal("webhook_url should be present when PublicURL set")
	}
	if !strings.HasPrefix(*respURL.WebhookURL, "https://app.example/api/webhooks/autopilots/") {
		t.Fatalf("webhook_url shape: %q", *respURL.WebhookURL)
	}
}

func TestWebhookHandler_404OnUnknownToken(t *testing.T) {
	w := postWebhook(t, "awt_unknown_token_value", map[string]any{"hello": "world"}, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_RejectsInvalidJSON(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookBadJSON Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, []byte(`not json`), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_RejectsScalarBody(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookScalar Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, []byte(`"hello"`), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_RejectsOversized(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookSize Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	big := make([]byte, maxWebhookBodyBytes+10)
	for i := range big {
		big[i] = 'a'
	}
	body := append([]byte(`{"x":"`), big...)
	body = append(body, []byte(`"}`)...)

	w := postWebhook(t, *trig.WebhookToken, body, nil)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_DisabledTriggerReturnsIgnored(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookDisabled Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	if _, err := testHandler.Queries.UpdateAutopilotTrigger(context.Background(), db.UpdateAutopilotTriggerParams{
		ID:      parseUUID(trig.ID),
		Enabled: pgtype.Bool{Bool: false, Valid: true},
	}); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"hello": "world"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ignored" {
		t.Fatalf("status: %v", resp["status"])
	}
	if resp["reason"] != "trigger_disabled" {
		t.Fatalf("reason: %v", resp["reason"])
	}
}

func TestWebhookHandler_PausedAutopilotReturnsIgnored(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookPaused Agent")
	apID := createWebhookTestAutopilot(t, agentID, "paused", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"x": 1}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["reason"] != "autopilot_paused" {
		t.Fatalf("reason: %v", resp["reason"])
	}
}

func TestWebhookHandler_ActiveDispatchesRunWithPayload(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookDispatch Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{
		"event":        "demo.received",
		"eventPayload": map[string]any{"k": "v"},
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Fatalf("expected accepted, got %v body=%s", resp["status"], w.Body.String())
	}
	runID, _ := resp["run_id"].(string)
	if runID == "" {
		t.Fatal("run_id missing from response")
	}

	// Validate the persisted run carries the normalized envelope.
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), parseUUID(runID))
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Source != "webhook" {
		t.Fatalf("run.source: %q", run.Source)
	}
	if uuidToString(run.TriggerID) != trig.ID {
		t.Fatalf("run.trigger_id mismatch: %q vs %q", uuidToString(run.TriggerID), trig.ID)
	}
	var payload struct {
		Event        string                 `json:"event"`
		EventPayload map[string]interface{} `json:"eventPayload"`
	}
	if err := json.Unmarshal(run.TriggerPayload, &payload); err != nil {
		t.Fatalf("payload decode: %v body=%s", err, string(run.TriggerPayload))
	}
	if payload.Event != "demo.received" {
		t.Fatalf("envelope event: %q", payload.Event)
	}
	if payload.EventPayload["k"] != "v" {
		t.Fatalf("envelope payload: %#v", payload.EventPayload)
	}

	// last_fired_at must have been bumped.
	trigRow, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), parseUUID(trig.ID))
	if err != nil {
		t.Fatalf("load trigger: %v", err)
	}
	if !trigRow.LastFiredAt.Valid {
		t.Fatal("last_fired_at should be set after webhook dispatch")
	}
}

func TestWebhookHandler_GitHubHeaderInferredEvent(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookGH Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 42,
		},
	}, map[string]string{"X-GitHub-Event": "pull_request"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	runID := resp["run_id"].(string)
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), parseUUID(runID))
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	var env struct {
		Event string `json:"event"`
	}
	json.Unmarshal(run.TriggerPayload, &env)
	if env.Event != "github.pull_request.opened" {
		t.Fatalf("event inference: got %q", env.Event)
	}
}

func TestWebhookHandler_RateLimitReturns429(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookRate Agent")
	apID := createWebhookTestAutopilot(t, agentID, "paused", "run_only") // paused → cheap ignored path
	trig := createWebhookTriggerViaHandler(t, apID)

	prev := testHandler.WebhookRateLimiter
	testHandler.WebhookRateLimiter = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 2, Window: 60_000_000_000})
	t.Cleanup(func() { testHandler.WebhookRateLimiter = prev })

	for i := 0; i < 2; i++ {
		w := postWebhook(t, *trig.WebhookToken, map[string]any{"i": i}, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}
	w := postWebhook(t, *trig.WebhookToken, map[string]any{"i": "third"}, nil)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateWebhookToken_ReplacesOldToken(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookRotate Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	oldToken := *trig.WebhookToken

	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/autopilots/%s/triggers/%s/rotate-webhook-token", apID, trig.ID), nil)
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.RotateAutopilotTriggerWebhookToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var rotated AutopilotTriggerResponse
	json.Unmarshal(w.Body.Bytes(), &rotated)
	if rotated.WebhookToken == nil || *rotated.WebhookToken == oldToken {
		t.Fatalf("rotate did not change token: old=%q new=%v", oldToken, rotated.WebhookToken)
	}

	// Old token should now 404.
	resOld := postWebhook(t, oldToken, map[string]any{"x": 1}, nil)
	if resOld.Code != http.StatusNotFound {
		t.Fatalf("old token should be 404, got %d", resOld.Code)
	}
	// New token should accept.
	resNew := postWebhook(t, *rotated.WebhookToken, map[string]any{"x": 1}, nil)
	if resNew.Code != http.StatusOK {
		t.Fatalf("new token should be 200, got %d body=%s", resNew.Code, resNew.Body.String())
	}
}
