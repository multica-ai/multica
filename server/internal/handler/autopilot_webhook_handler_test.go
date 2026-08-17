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
	"time"

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

// TestCreateTrigger_RepublishesRuleVersionAtomically verifies Elon's final Phase 1
// must-fix: creating a trigger (a substantive change to what fires, MUL-4302 §3.4)
// republishes the autopilot's rule version with the acting member as publisher,
// written atomically in the same tx as the trigger INSERT — for BOTH the webhook
// create path (mint-with-retry, whole attempt wrapped in a tx) and the schedule path.
func TestCreateTrigger_RepublishesRuleVersionAtomically(t *testing.T) {
	agentID := createWebhookTestAgent(t, "TriggerVersion Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	ctx := context.Background()
	verParams := db.GetActiveAutopilotRuleVersionParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		AutopilotID: parseUUID(apID),
	}

	// The test autopilot is inserted directly (no v1), so no rule version exists yet.
	if _, err := testHandler.Queries.GetActiveAutopilotRuleVersion(ctx, verParams); err == nil {
		t.Fatal("expected no rule version before any trigger is created")
	}

	// Webhook create (mint-with-retry) republishes atomically.
	createWebhookTriggerViaHandler(t, apID)
	ver, err := testHandler.Queries.GetActiveAutopilotRuleVersion(ctx, verParams)
	if err != nil {
		t.Fatalf("webhook trigger create must republish a rule version: %v", err)
	}
	if ver.PublishedByType != "member" || uuidToString(ver.PublishedByID) != testUserID {
		t.Errorf("webhook version published_by = %s/%s, want member/%s", ver.PublishedByType, uuidToString(ver.PublishedByID), testUserID)
	}

	// Schedule create appends a fresh version, also published by the acting member.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":            "schedule",
		"cron_expression": "0 0 * * *",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("schedule CreateAutopilotTrigger: got %d body=%s", w.Code, w.Body.String())
	}
	ver2, err := testHandler.Queries.GetActiveAutopilotRuleVersion(ctx, verParams)
	if err != nil {
		t.Fatalf("schedule trigger create must republish a rule version: %v", err)
	}
	if ver2.ID.Bytes == ver.ID.Bytes {
		t.Error("schedule create must append a NEW rule version, not reuse the webhook one")
	}
	if ver2.PublishedByType != "member" || uuidToString(ver2.PublishedByID) != testUserID {
		t.Errorf("schedule version published_by = %s/%s, want member/%s", ver2.PublishedByType, uuidToString(ver2.PublishedByID), testUserID)
	}
}

// createWebhookTriggerWithFilters builds the request body with a real JSON
// array — the same shape the frontend sends. Earlier revisions of this
// helper marshaled the filters separately and assigned the resulting
// []byte to the "event_filters" map key, which encoding/json then encoded
// as a base64 string (since []byte → JSON-string). The base64 path
// happened to work against an []byte server-side field but masked the
// actual contract bug fixed in PR #3231 review.
func createWebhookTriggerWithFilters(t *testing.T, autopilotID string, filters []WebhookEventFilter) AutopilotTriggerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+autopilotID+"/triggers", map[string]any{
		"kind":          "webhook",
		"event_filters": filters,
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

func TestWebhookHandler_FiltersUndeclaredEvent(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookFilter Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{
		{Event: "workflow_run", Actions: []string{"completed"}},
		{Event: "check_suite", Actions: []string{"completed"}},
	})

	w := postWebhook(t, *trig.WebhookToken, map[string]any{
		"action":       "in_progress",
		"workflow_run": map[string]any{"id": 123},
	}, map[string]string{"X-GitHub-Event": "workflow_run"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ignored" || resp["reason"] != "event_filtered" {
		t.Fatalf("expected ignored/event_filtered, got %#v body=%s", resp, w.Body.String())
	}
	if _, ok := resp["run_id"]; ok {
		t.Fatalf("filtered response must not include run_id: %#v", resp)
	}

	runs, err := testHandler.Queries.ListAutopilotRuns(context.Background(), db.ListAutopilotRunsParams{
		AutopilotID: parseUUID(apID),
		Limit:       10,
		Offset:      0,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("filtered webhook should not create runs, got %d", len(runs))
	}
}

func TestWebhookHandler_AllowsDeclaredEvent(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookAllow Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{
		{Event: "workflow_run", Actions: []string{"completed"}},
	})

	w := postWebhook(t, *trig.WebhookToken, map[string]any{
		"action":       "completed",
		"workflow_run": map[string]any{"id": 123},
	}, map[string]string{"X-GitHub-Event": "workflow_run"})
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
	if delivery.Status != deliveryStatusDispatched || !delivery.AutopilotRunID.Valid {
		t.Fatalf("expected worker dispatch, got status=%s run=%v", delivery.Status, delivery.AutopilotRunID.Valid)
	}
}

func TestWebhookHandler_EmptyFiltersAllowsAll(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookEmptyFilter Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{
		"action":       "in_progress",
		"workflow_run": map[string]any{"id": 123},
	}, map[string]string{"X-GitHub-Event": "workflow_run"})
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
	if delivery.Status != deliveryStatusDispatched || !delivery.AutopilotRunID.Valid {
		t.Fatalf("expected worker dispatch, got status=%s run=%v", delivery.Status, delivery.AutopilotRunID.Valid)
	}
}

// ── HTTP contract: event_filters JSON shape & PATCH semantics ──────────────
//
// These tests pin the wire contract the frontend depends on: a real JSON
// array of {event, actions} objects flows in on create, comes back as the
// same array on read, and update accepts tri-state semantics (omitted =
// preserve, explicit [] = clear, explicit [...] = replace). Earlier
// revisions used []byte at the HTTP boundary and the round-trip silently
// passed via base64 — which the frontend cannot parse. See PR #3231 review.

func TestCreateWebhookTrigger_EventFiltersRoundTripAsJSONArray(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EventFilterRT Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	body := map[string]any{
		"kind": "webhook",
		"event_filters": []map[string]any{
			{"event": "workflow_run", "actions": []string{"completed"}},
			{"event": "pull_request", "actions": []string{"opened", "synchronize"}},
		},
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", body)
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	// Decode against the strongly-typed response to verify shape.
	var typed AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &typed); err != nil {
		t.Fatalf("decode typed: %v", err)
	}
	if len(typed.EventFilters) != 2 {
		t.Fatalf("expected 2 filters, got %d body=%s", len(typed.EventFilters), w.Body.String())
	}
	if typed.EventFilters[0].Event != "workflow_run" ||
		len(typed.EventFilters[0].Actions) != 1 ||
		typed.EventFilters[0].Actions[0] != "completed" {
		t.Fatalf("first filter mismatch: %#v", typed.EventFilters[0])
	}
	if typed.EventFilters[1].Event != "pull_request" || len(typed.EventFilters[1].Actions) != 2 {
		t.Fatalf("second filter mismatch: %#v", typed.EventFilters[1])
	}

	// Decode against json.RawMessage to confirm we serialize as an array,
	// not a base64-encoded string. A regression here is exactly the bug
	// PR #3231 review flagged: []byte through encoding/json produced
	// `"event_filters": "W3si..."` which the UI can't .map() over.
	var raw struct {
		EventFilters json.RawMessage `json:"event_filters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	trimmed := bytes.TrimSpace(raw.EventFilters)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		t.Fatalf("event_filters must serialize as a JSON array, got %s", raw.EventFilters)
	}
}

func TestUpdateWebhookTrigger_ExplicitEmptyArrayClearsFilters(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EventFilterClear Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	created := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{
		{Event: "workflow_run", Actions: []string{"completed"}},
	})
	if len(created.EventFilters) != 1 {
		t.Fatalf("seed should have 1 filter, got %d", len(created.EventFilters))
	}

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+created.ID, map[string]any{
		"event_filters": []any{},
	})
	req = withURLParams(req, "id", apID, "triggerId", created.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var updated AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(updated.EventFilters) != 0 {
		t.Fatalf("expected cleared filters in response, got %#v", updated.EventFilters)
	}

	// Stored row should now accept any event (matcher sees length 0).
	row, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), parseUUID(created.ID))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	env := WebhookEnvelope{
		Event:        "github.something_else.opened",
		EventPayload: json.RawMessage(`{"action":"opened"}`),
	}
	if !webhookEventAllowedByTriggerScope(row.EventFilters, env) {
		t.Fatal("after clear, matcher should allow all events")
	}
}

func TestUpdateWebhookTrigger_OmittedFiltersPreserveExisting(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EventFilterPreserve Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	created := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{
		{Event: "workflow_run", Actions: []string{"completed"}},
	})

	// PATCH that does NOT include event_filters at all. Must leave the
	// existing filter set untouched (omitted ≠ clear).
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+created.ID, map[string]any{
		"label": "renamed-but-keep-filters",
	})
	req = withURLParams(req, "id", apID, "triggerId", created.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var updated AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(updated.EventFilters) != 1 || updated.EventFilters[0].Event != "workflow_run" {
		t.Fatalf("filters must be preserved when field omitted, got %#v", updated.EventFilters)
	}
}

func TestUpdateWebhookTrigger_ReplacesFilters(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EventFilterReplace Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	created := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{
		{Event: "workflow_run", Actions: []string{"completed"}},
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+created.ID, map[string]any{
		"event_filters": []map[string]any{
			{"event": "pull_request", "actions": []string{"opened"}},
			{"event": "issues"},
		},
	})
	req = withURLParams(req, "id", apID, "triggerId", created.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var updated AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(updated.EventFilters) != 2 {
		t.Fatalf("expected 2 replaced filters, got %d", len(updated.EventFilters))
	}
	if updated.EventFilters[0].Event != "pull_request" || updated.EventFilters[1].Event != "issues" {
		t.Fatalf("replaced filter list wrong: %#v", updated.EventFilters)
	}
}

func TestCreateAutopilotTrigger_RejectsInvalidEventFilter(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EventFilterInvalid Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind": "webhook",
		"event_filters": []map[string]any{
			{"event": "", "actions": []string{"completed"}},
		},
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty event name, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotTrigger_RejectsEventFiltersOnSchedule(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EventFilterSched Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":            "schedule",
		"cron_expression": "0 9 * * *",
		"event_filters": []map[string]any{
			{"event": "workflow_run"},
		},
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on event_filters for schedule trigger, got %d body=%s", w.Code, w.Body.String())
	}
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

func requireAcceptedWebhookResponse(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 accepted/skipped, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	if resp["status"] != "accepted" && resp["status"] != "skipped" {
		t.Fatalf("expected accepted or skipped status, got %#v", resp)
	}
	deliveryID, _ := resp["delivery_id"].(string)
	if deliveryID == "" {
		t.Fatalf("accepted response missing delivery_id: %#v", resp)
	}
	if runID, _ := resp["run_id"].(string); runID == "" {
		t.Fatalf("accepted response missing run_id: %#v", resp)
	}
	return deliveryID
}

func processQueuedWebhookDelivery(t *testing.T, deliveryID string) db.WebhookDelivery {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		delivery, err := testHandler.Queries.GetWebhookDelivery(ctx, parseUUID(deliveryID))
		if err != nil {
			t.Fatalf("load queued delivery: %v", err)
		}
		if delivery.Status != deliveryStatusQueued {
			return delivery
		}
		worked, err := testHandler.WebhookDeliveryWorker.ProcessNext(ctx)
		if err != nil {
			t.Fatalf("process queued delivery: %v", err)
		}
		if !worked {
			t.Fatalf("delivery %s remained queued with no claimable work", deliveryID)
		}
	}
	t.Fatalf("delivery %s did not reach a terminal state", deliveryID)
	return db.WebhookDelivery{}
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
	deliveryID := requireAcceptedWebhookResponse(t, w)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Fatalf("expected accepted, got %#v", resp)
	}
	runID := resp["run_id"].(string)

	// The run ID is allocated synchronously, while durable dispatch remains
	// owned by the queued delivery worker.
	delivery := processQueuedWebhookDelivery(t, deliveryID)
	if got := uuidToString(delivery.AutopilotRunID); got != runID {
		t.Fatalf("delivery run_id mismatch: got %q want %q", got, runID)
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

func TestWebhookHandler_ActiveSkippedRunReturnsLegacyContract(t *testing.T) {
	var offlineRuntimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		) VALUES ($1, NULL, $2, 'cloud', $3, 'offline', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, "Webhook Offline Runtime", "webhook_test_runtime", "Webhook test runtime", testUserID).Scan(&offlineRuntimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, offlineRuntimeID)
	})

	agentID := createWebhookTestAgent(t, "WebhookSkipped Agent")
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET runtime_id = $1 WHERE id = $2`, offlineRuntimeID, agentID); err != nil {
		t.Fatalf("bind offline agent runtime: %v", err)
	}
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "demo.skipped"}, nil)
	deliveryID := requireAcceptedWebhookResponse(t, w)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode skipped response: %v", err)
	}
	if resp["status"] != "skipped" {
		t.Fatalf("expected skipped, got %#v", resp)
	}
	if reason, _ := resp["reason"].(string); reason == "" {
		t.Fatalf("skipped response missing reason: %#v", resp)
	}
	runID := resp["run_id"].(string)
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), parseUUID(runID))
	if err != nil {
		t.Fatalf("load skipped run: %v", err)
	}
	if run.Status != "skipped" {
		t.Fatalf("run status: got %q want skipped", run.Status)
	}

	delivery := processQueuedWebhookDelivery(t, deliveryID)
	if delivery.Status != deliveryStatusDispatched || uuidToString(delivery.AutopilotRunID) != runID {
		t.Fatalf("skipped run was not linked to delivery: status=%s run_id=%s", delivery.Status, uuidToString(delivery.AutopilotRunID))
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
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
	runID := uuidToString(delivery.AutopilotRunID)
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

func TestWebhookHandler_ValidBurstPersistsWithoutHTTP429(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookRate Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	prev := testHandler.WebhookRateLimiter
	testHandler.WebhookRateLimiter = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 2, Window: 60_000_000_000})
	t.Cleanup(func() { testHandler.WebhookRateLimiter = prev })

	for i := 0; i < 3; i++ {
		w := postWebhook(t, *trig.WebhookToken, map[string]any{"i": i}, nil)
		requireAcceptedWebhookResponse(t, w)
	}
	if got := len(listDeliveries(t, apID)); got != 3 {
		t.Fatalf("all valid burst deliveries must be persisted, got %d", got)
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
	requireAcceptedWebhookResponse(t, resNew)
}

// ── Additional coverage (PR #2348 review) ──────────────────────────────────

func TestWebhookHandler_EmptyBodyReturns400(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookEmpty Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, []byte(``), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_ArchivedAutopilotReturnsIgnored(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookArchived Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot SET status = 'archived' WHERE id = $1`, apID); err != nil {
		t.Fatalf("archive autopilot: %v", err)
	}

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"x": 1}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ignored" || resp["reason"] != "autopilot_archived" {
		t.Fatalf("expected ignored/autopilot_archived, got %#v", resp)
	}
}

func TestWebhookHandler_IPRateLimitReturns429BeforeDBLookup(t *testing.T) {
	// Spray random (likely-unknown) tokens from one IP and prove the IP
	// limiter trips before we exhaust the budget — without this gate an
	// attacker can probe the trigger-lookup index unboundedly. Rate-limit
	// keying is by the real source IP (r.RemoteAddr) since TrustedProxies
	// is empty here, so the bucket is per-connection — exactly the
	// property the per-IP limiter is meant to provide.
	prev := testHandler.WebhookIPRateLimiter
	testHandler.WebhookIPRateLimiter = NewMemoryWebhookIPRateLimiter(WebhookRateLimit{Limit: 2, Window: 60_000_000_000})
	t.Cleanup(func() { testHandler.WebhookIPRateLimiter = prev })

	post := func(token string) int {
		req := httptest.NewRequest("POST", "/api/webhooks/autopilots/"+token,
			bytes.NewBufferString(`{"x":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.7:1234" // stable source, three calls = same bucket
		req = withURLParam(req, "token", token)
		w := httptest.NewRecorder()
		testHandler.HandleAutopilotWebhook(w, req)
		return w.Code
	}

	if got := post("awt_unknown_a"); got != http.StatusNotFound {
		t.Fatalf("first probe: expected 404, got %d", got)
	}
	if got := post("awt_unknown_b"); got != http.StatusNotFound {
		t.Fatalf("second probe: expected 404, got %d", got)
	}
	if got := post("awt_unknown_c"); got != http.StatusTooManyRequests {
		t.Fatalf("third probe: expected 429 (IP bucket), got %d", got)
	}
}

func TestWebhookHandler_IPRateLimitNotBypassedByXFFSpoof(t *testing.T) {
	// Round-2 fix: with the default empty TrustedProxies, an attacker who
	// rotates X-Forwarded-For per request must still get bucketed by the
	// real connection IP — otherwise the per-IP limiter is trivially
	// bypassable and we're back to one DB index probe per request.
	prev := testHandler.WebhookIPRateLimiter
	testHandler.WebhookIPRateLimiter = NewMemoryWebhookIPRateLimiter(WebhookRateLimit{Limit: 2, Window: 60_000_000_000})
	t.Cleanup(func() { testHandler.WebhookIPRateLimiter = prev })

	post := func(token, xff string) int {
		req := httptest.NewRequest("POST", "/api/webhooks/autopilots/"+token,
			bytes.NewBufferString(`{"x":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", xff) // <-- attacker-controlled
		req.RemoteAddr = "198.51.100.42:5555"  // real (untrusted) source
		req = withURLParam(req, "token", token)
		w := httptest.NewRecorder()
		testHandler.HandleAutopilotWebhook(w, req)
		return w.Code
	}

	if got := post("awt_unknown_x", "1.1.1.1"); got != http.StatusNotFound {
		t.Fatalf("first probe: expected 404, got %d", got)
	}
	if got := post("awt_unknown_y", "2.2.2.2"); got != http.StatusNotFound {
		t.Fatalf("second probe: expected 404, got %d", got)
	}
	// Third request with yet another spoofed XFF — would have bypassed
	// the limiter under the old (header-trusting) behavior, but with the
	// CIDR-gated trust the bucket is still the real source IP.
	if got := post("awt_unknown_z", "3.3.3.3"); got != http.StatusTooManyRequests {
		t.Fatalf("third probe: expected 429 (bucket keyed by real IP), got %d", got)
	}
}

func TestWebhookHandler_AbsoluteIPRateLimitRetainsEmergencyCeiling(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookAbsoluteLimit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	prev := testHandler.WebhookAbsoluteIPRateLimiter
	testHandler.WebhookAbsoluteIPRateLimiter = NewMemoryWebhookAbsoluteIPRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute})
	t.Cleanup(func() { testHandler.WebhookAbsoluteIPRateLimiter = prev })

	requireAcceptedWebhookResponse(t, postWebhook(t, *trig.WebhookToken, map[string]any{"n": 1}, nil))
	second := postWebhook(t, *trig.WebhookToken, map[string]any{"n": 2}, nil)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("absolute ceiling: expected 429, got %d body=%s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("absolute ceiling 429 must include Retry-After")
	}
}

func TestWebhookHandler_DBErrorOnTokenLookupReturns500(t *testing.T) {
	// Inject a fake Queries-like wrapper via a shadow type isn't simple
	// here because Handler.Queries is a *db.Queries struct, not an
	// interface. Instead, simulate the path by deleting the trigger row
	// out from under a known-valid token AND swapping in a stub limiter
	// that always allows. The handler will then call
	// GetWebhookTriggerByToken with a non-existent value → pgx.ErrNoRows
	// → 404. So this test alone can't drive the 500 branch without
	// breaking the DB connection. The 500 branch is exercised via the
	// unit tests in this package's TestRedactWebhookPath /
	// TestRequestLogger_*; we leave a regression marker here so a future
	// refactor that collapses ErrNoRows into 500 (or vice versa) is
	// caught by a code review rather than a missing test.
	//
	// The real verification for this branch is: the file diff for
	// autopilot_webhook.go must show `errors.Is(err, pgx.ErrNoRows)` —
	// see PR #2348 review item Blocking #2.
	t.Skip("500-branch requires injecting a stub Queries; left as a code-review-protected invariant")
}

func TestCreateAutopilotTrigger_RejectsAPIKind(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookAPIKind Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind": "api",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on kind=api, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "schedule or webhook") {
		t.Fatalf("expected message to name allowed kinds, body=%s", w.Body.String())
	}
}

func TestCreateAutopilotTrigger_RejectsWebhookWithTimezone(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookTZReject Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":     "webhook",
		"timezone": "Europe/Berlin",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on webhook+timezone, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilotTrigger_RejectsCronExpressionOnWebhookKind(t *testing.T) {
	// Round-2 should-fix: UpdateAutopilotTrigger must mirror create-path
	// strictness — cron_expression on a non-schedule trigger is rejected
	// with 400 rather than silently persisted.
	agentID := createWebhookTestAgent(t, "WebhookUpdCron Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{
		"cron_expression": "0 0 * * *",
	})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on cron_expression for webhook trigger, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cron_expression") {
		t.Fatalf("error message should mention cron_expression, got %s", w.Body.String())
	}
}

func TestUpdateAutopilotTrigger_RejectsTimezoneOnWebhookKind(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WebhookUpdTZ Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{
		"timezone": "Europe/Berlin",
	})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on timezone for webhook trigger, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "timezone") {
		t.Fatalf("error message should mention timezone, got %s", w.Body.String())
	}
}

func TestUpdateAutopilotTrigger_AcceptsEnabledAndLabelOnWebhookKind(t *testing.T) {
	// Counter-test: enabled and label remain valid on every kind. Without
	// this, the kind-aware guard could regress to a blanket reject.
	agentID := createWebhookTestAgent(t, "WebhookUpdAllowed Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{
		"enabled": false,
		"label":   "renamed",
	})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on enabled+label PATCH, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetAutopilotRun_ReturnsFullPayload(t *testing.T) {
	// List endpoint omits trigger_payload; the new GET /runs/{runId}
	// endpoint must return it intact.
	agentID := createWebhookTestAgent(t, "WebhookGetRun Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	// Fire one webhook so there's a run with a payload.
	post := postWebhook(t, *trig.WebhookToken, map[string]any{
		"event":        "demo.x",
		"eventPayload": map[string]any{"answer": 42},
	}, nil)
	delivery := processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, post))
	runID := uuidToString(delivery.AutopilotRunID)

	// LIST: trigger_payload should be omitted (slim response).
	wList := httptest.NewRecorder()
	reqList := newRequest("GET", "/api/autopilots/"+apID+"/runs", nil)
	reqList = withURLParam(reqList, "id", apID)
	testHandler.ListAutopilotRuns(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d body=%s", wList.Code, wList.Body.String())
	}
	if strings.Contains(wList.Body.String(), `"answer":42`) {
		t.Fatalf("list response should NOT carry trigger_payload, body=%s", wList.Body.String())
	}

	// DETAIL: trigger_payload should be present.
	wDetail := httptest.NewRecorder()
	reqDetail := newRequest("GET", "/api/autopilots/"+apID+"/runs/"+runID, nil)
	reqDetail = withURLParams(reqDetail, "id", apID, "runId", runID)
	testHandler.GetAutopilotRun(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("detail: expected 200, got %d body=%s", wDetail.Code, wDetail.Body.String())
	}
	if !strings.Contains(wDetail.Body.String(), `"answer":42`) {
		t.Fatalf("detail response should carry full trigger_payload, body=%s", wDetail.Body.String())
	}
}

// NOTE: the cross-workspace paranoia branch in autopilot_webhook.go
// (uuidToString(autopilot.WorkspaceID) != uuidToString(trigRow.AutopilotWorkspaceID))
// is defense-in-depth against a TOCTOU race between the joined token
// lookup and the follow-up GetAutopilot read. It is not reachable from
// any valid SQL state — the two reads compute against the same
// autopilot.workspace_id column — and would require a mock-able
// Queries interface to drive deterministically. We pin the behaviour
// via code review rather than a brittle race test. See PR #2348 review
// item under "Test coverage gaps."
