package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Setup helpers ───────────────────────────────────────────────────────────

const testSigningSecret = "this-is-a-test-secret-32-chars-x"

func setSigningSecretViaHandler(t *testing.T, apID, triggerID, secret string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", fmt.Sprintf("/api/autopilots/%s/triggers/%s/signing-secret", apID, triggerID), map[string]any{
		"signing_secret": secret,
	})
	req = withURLParams(req, "id", apID, "triggerId", triggerID)
	testHandler.SetAutopilotTriggerSigningSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set signing secret: %d body=%s", w.Code, w.Body.String())
	}
}

func setTriggerProvider(t *testing.T, triggerID, provider string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_trigger SET provider = $1 WHERE id = $2`, provider, triggerID); err != nil {
		t.Fatalf("set provider: %v", err)
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// listDeliveries calls ListAutopilotDeliveries and decodes the body.
func listDeliveries(t *testing.T, apID string) []map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/autopilots/"+apID+"/deliveries", nil)
	req = withURLParam(req, "id", apID)
	testHandler.ListAutopilotDeliveries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list deliveries: %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []map[string]any `json:"deliveries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Deliveries
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestWebhookHandler_PersistsDeliveryOnAccept(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DeliveryPersist Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"hello": "world"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["delivery_id"] == nil {
		t.Fatal("response should include delivery_id")
	}
	if resp["status"] != "accepted" {
		t.Fatalf("status: %v", resp["status"])
	}

	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}
	d := deliveries[0]
	if d["status"] != "dispatched" {
		t.Fatalf("delivery status: %v", d["status"])
	}
	if d["autopilot_run_id"] == nil {
		t.Fatal("delivery should link to run")
	}
	if d["signature_status"] != "not_required" {
		t.Fatalf("expected signature_status=not_required, got %v", d["signature_status"])
	}
}

func TestWebhookHandler_DedupeViaIdempotencyKey(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DeliveryIdem Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	body := map[string]any{"event": "demo.x", "eventPayload": map[string]any{"k": "v"}}
	headers := map[string]string{"Idempotency-Key": "demo-key-1"}

	w1 := postWebhook(t, *trig.WebhookToken, body, headers)
	if w1.Code != http.StatusOK {
		t.Fatalf("first: %d body=%s", w1.Code, w1.Body.String())
	}
	var r1 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &r1)
	if r1["status"] != "accepted" {
		t.Fatalf("first status: %v", r1["status"])
	}
	firstDeliveryID := r1["delivery_id"].(string)
	firstRunID := r1["run_id"].(string)

	// Second identical delivery should be a duplicate.
	w2 := postWebhook(t, *trig.WebhookToken, body, headers)
	if w2.Code != http.StatusOK {
		t.Fatalf("second: %d body=%s", w2.Code, w2.Body.String())
	}
	var r2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if r2["status"] != "duplicate" {
		t.Fatalf("expected duplicate, got %v body=%s", r2["status"], w2.Body.String())
	}
	if r2["delivery_id"] != firstDeliveryID {
		t.Fatalf("duplicate delivery_id mismatch: %v != %v", r2["delivery_id"], firstDeliveryID)
	}
	if r2["run_id"] != firstRunID {
		t.Fatalf("duplicate run_id mismatch: %v != %v", r2["run_id"], firstRunID)
	}

	// Only one delivery should exist; attempt_count must be 2.
	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery (dedupe), got %d", len(deliveries))
	}
	if int(deliveries[0]["attempt_count"].(float64)) != 2 {
		t.Fatalf("attempt_count: %v", deliveries[0]["attempt_count"])
	}
}

func TestWebhookHandler_DedupeViaGitHubDelivery(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DeliveryGH Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setTriggerProvider(t, trig.ID, "github")

	body := map[string]any{"action": "opened"}
	headers := map[string]string{
		"X-GitHub-Event":    "pull_request",
		"X-GitHub-Delivery": "abc-123",
	}

	w1 := postWebhook(t, *trig.WebhookToken, body, headers)
	if w1.Code != http.StatusOK {
		t.Fatalf("first: %d", w1.Code)
	}
	var r1 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &r1)
	if r1["status"] != "accepted" {
		t.Fatalf("first status: %v", r1["status"])
	}

	w2 := postWebhook(t, *trig.WebhookToken, body, headers)
	var r2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if r2["status"] != "duplicate" {
		t.Fatalf("expected duplicate, got %v", r2["status"])
	}

	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}
	if deliveries[0]["dedupe_source"] != "x-github-delivery" {
		t.Fatalf("dedupe_source: %v", deliveries[0]["dedupe_source"])
	}
}

func TestWebhookHandler_InvalidSignatureReturns401AndPersistsRejected(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigInvalid Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	body := map[string]any{"hello": "world"}
	w := postWebhook(t, *trig.WebhookToken, body, map[string]string{
		"X-Hub-Signature-256": "sha256=deadbeef",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "rejected" {
		t.Fatalf("status: %v", resp["status"])
	}
	if resp["delivery_id"] == nil {
		t.Fatal("delivery_id should be present on rejected response")
	}

	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}
	if deliveries[0]["status"] != "rejected" {
		t.Fatalf("expected rejected, got %v", deliveries[0]["status"])
	}
	if deliveries[0]["signature_status"] != "invalid" {
		t.Fatalf("expected signature_status=invalid, got %v", deliveries[0]["signature_status"])
	}
	if deliveries[0]["autopilot_run_id"] != nil {
		t.Fatal("rejected delivery must not link to a run")
	}
}

func TestWebhookHandler_MissingSignatureReturns401WhenSecretSet(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigMissing Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"hello": "world"}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["reason"] != "missing_signature" {
		t.Fatalf("reason: %v", resp["reason"])
	}
	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 || deliveries[0]["signature_status"] != "missing" {
		t.Fatalf("delivery missing-signature state: %#v", deliveries)
	}
}

func TestWebhookHandler_ValidSignatureDispatches(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigValid Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	bodyBytes := []byte(`{"hello":"world"}`)
	sig := signBody(testSigningSecret, bodyBytes)

	w := postWebhook(t, *trig.WebhookToken, bodyBytes, map[string]string{
		"X-Hub-Signature-256": sig,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "accepted" {
		t.Fatalf("status: %v", resp["status"])
	}
	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}
	if deliveries[0]["signature_status"] != "valid" {
		t.Fatalf("signature_status: %v", deliveries[0]["signature_status"])
	}
}

func TestSigningSecretNotEchoedInTriggerResponse(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigEcho Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	// GET the autopilot — trigger response embedded.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/autopilots/"+apID, nil)
	req = withURLParam(req, "id", apID)
	testHandler.GetAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get autopilot: %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(testSigningSecret)) {
		t.Fatalf("signing secret leaked in trigger response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"has_signing_secret":true`)) {
		t.Fatalf("has_signing_secret should be true: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"signing_secret_hint":"`+testSigningSecret[len(testSigningSecret)-4:]+`"`)) {
		t.Fatalf("hint should be last 4 chars: %s", w.Body.String())
	}
}

func TestSigningSecret_MinLengthEnforced(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigMinLen Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/autopilots/"+apID+"/triggers/"+trig.ID+"/signing-secret", map[string]any{
		"signing_secret": "short",
	})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.SetAutopilotTriggerSigningSecret(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short secret, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSigningSecret_EmptyClearsSecret(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigClear Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	// Now clear with empty string.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/autopilots/"+apID+"/triggers/"+trig.ID+"/signing-secret", map[string]any{
		"signing_secret": "",
	})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.SetAutopilotTriggerSigningSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear secret: %d body=%s", w.Code, w.Body.String())
	}
	// Unsigned request should now go through (back to not_required).
	post := postWebhook(t, *trig.WebhookToken, map[string]any{"x": 1}, nil)
	if post.Code != http.StatusOK {
		t.Fatalf("post after clear: %d body=%s", post.Code, post.Body.String())
	}
}

func TestReplay_CreatesNewDeliveryAndDispatchesRun(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Replay Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	// Original delivery (with dedupe key) → accepted + dispatched.
	w := postWebhook(t, *trig.WebhookToken, map[string]any{"hello": "world"}, map[string]string{
		"Idempotency-Key": "replay-original",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("original: %d body=%s", w.Code, w.Body.String())
	}
	var orig map[string]any
	json.Unmarshal(w.Body.Bytes(), &orig)
	originalID := orig["delivery_id"].(string)
	originalRunID := orig["run_id"].(string)

	// Replay the original.
	wr := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/autopilots/%s/deliveries/%s/replay", apID, originalID), nil)
	req = withURLParams(req, "id", apID, "deliveryId", originalID)
	testHandler.ReplayAutopilotDelivery(wr, req)
	if wr.Code != http.StatusCreated {
		t.Fatalf("replay: %d body=%s", wr.Code, wr.Body.String())
	}
	var replay map[string]any
	json.Unmarshal(wr.Body.Bytes(), &replay)
	if replay["id"] == originalID {
		t.Fatal("replay should create a NEW delivery, not return the original")
	}
	if replay["replayed_from_delivery_id"] != originalID {
		t.Fatalf("replayed_from_delivery_id: %v", replay["replayed_from_delivery_id"])
	}
	if replay["autopilot_run_id"] == nil {
		t.Fatal("replay should dispatch a run")
	}
	if replay["autopilot_run_id"] == originalRunID {
		t.Fatal("replay should produce a NEW run, not reuse the original")
	}

	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 deliveries (original + replay), got %d", len(deliveries))
	}
}

func TestReplay_RejectsInvalidSignatureDelivery(t *testing.T) {
	agentID := createWebhookTestAgent(t, "ReplayReject Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	// Send an invalid-signature request → rejected delivery created.
	w := postWebhook(t, *trig.WebhookToken, map[string]any{"x": 1}, map[string]string{
		"X-Hub-Signature-256": "sha256=baadf00d",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("setup: expected 401, got %d", w.Code)
	}
	var rej map[string]any
	json.Unmarshal(w.Body.Bytes(), &rej)
	rejectedID := rej["delivery_id"].(string)

	// Replay the rejected delivery → 400.
	wr := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/autopilots/%s/deliveries/%s/replay", apID, rejectedID), nil)
	req = withURLParams(req, "id", apID, "deliveryId", rejectedID)
	testHandler.ReplayAutopilotDelivery(wr, req)
	if wr.Code != http.StatusBadRequest {
		t.Fatalf("replay of rejected: expected 400, got %d body=%s", wr.Code, wr.Body.String())
	}
}

func TestGetDelivery_ReturnsFullPayload(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DeliveryDetail Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "demo", "eventPayload": map[string]any{"answer": 42}}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("seed: %d", w.Code)
	}
	var seed map[string]any
	json.Unmarshal(w.Body.Bytes(), &seed)
	deliveryID := seed["delivery_id"].(string)

	// List response should NOT include raw_body / selected_headers.
	wList := httptest.NewRecorder()
	reqList := newRequest("GET", "/api/autopilots/"+apID+"/deliveries", nil)
	reqList = withURLParam(reqList, "id", apID)
	testHandler.ListAutopilotDeliveries(wList, reqList)
	if bytes.Contains(wList.Body.Bytes(), []byte(`"raw_body"`)) {
		t.Fatalf("list response should not include raw_body, body=%s", wList.Body.String())
	}

	// Detail response SHOULD include raw_body and selected_headers.
	wDetail := httptest.NewRecorder()
	reqDetail := newRequest("GET", "/api/autopilots/"+apID+"/deliveries/"+deliveryID, nil)
	reqDetail = withURLParams(reqDetail, "id", apID, "deliveryId", deliveryID)
	testHandler.GetAutopilotDelivery(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("detail: %d body=%s", wDetail.Code, wDetail.Body.String())
	}
	// raw_body is serialised as a JSON string (escaped); decode the response
	// and assert against the decoded payload so we don't rely on a brittle
	// substring search against the escaped form.
	var detail WebhookDeliveryResponse
	if err := json.Unmarshal(wDetail.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v body=%s", err, wDetail.Body.String())
	}
	if detail.RawBody == nil {
		t.Fatalf("detail should include raw_body: %s", wDetail.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(*detail.RawBody), &raw); err != nil {
		t.Fatalf("raw_body should be valid JSON: %v body=%q", err, *detail.RawBody)
	}
	payload, ok := raw["eventPayload"].(map[string]any)
	if !ok {
		t.Fatalf("eventPayload missing or wrong type in raw_body: %#v", raw)
	}
	if v, ok := payload["answer"].(float64); !ok || v != 42 {
		t.Fatalf("raw_body eventPayload.answer should be 42, got %#v", payload["answer"])
	}
}

func TestGetDelivery_CrossAutopilotReturns404(t *testing.T) {
	// A delivery_id from one autopilot must not be readable via another
	// autopilot's URL — defense in depth even though both rows are in the
	// same workspace.
	agentID := createWebhookTestAgent(t, "CrossAP Agent")
	apA := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	apB := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apA)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"x": 1}, nil)
	var seed map[string]any
	json.Unmarshal(w.Body.Bytes(), &seed)
	deliveryID := seed["delivery_id"].(string)

	// Try reading via the OTHER autopilot's URL.
	wDetail := httptest.NewRecorder()
	reqDetail := newRequest("GET", "/api/autopilots/"+apB+"/deliveries/"+deliveryID, nil)
	reqDetail = withURLParams(reqDetail, "id", apB, "deliveryId", deliveryID)
	testHandler.GetAutopilotDelivery(wDetail, reqDetail)
	if wDetail.Code != http.StatusNotFound {
		t.Fatalf("cross-autopilot GET: expected 404, got %d", wDetail.Code)
	}
}

func TestCreateAutopilotTrigger_RejectsUnknownProvider(t *testing.T) {
	agentID := createWebhookTestAgent(t, "ProviderInvalid Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":     "webhook",
		"provider": "stripe",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown provider, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotTrigger_AcceptsGitHubProvider(t *testing.T) {
	agentID := createWebhookTestAgent(t, "ProviderGH Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":     "webhook",
		"provider": "github",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AutopilotTriggerResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Provider == nil || *resp.Provider != "github" {
		t.Fatalf("provider: %v", resp.Provider)
	}
}

// run_only autopilots have no issue-title duplicate guard, so dedupe via
// the delivery layer is the only thing keeping a retried provider event
// from re-running the agent. This regression test pins that path
// explicitly — it's the largest concrete win over the v1 ingress flow.
func TestWebhookHandler_RunOnlyDedupeOnGitHubDelivery(t *testing.T) {
	agentID := createWebhookTestAgent(t, "RunOnlyDedupe Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setTriggerProvider(t, trig.ID, "github")

	headers := map[string]string{
		"X-GitHub-Event":    "pull_request",
		"X-GitHub-Delivery": "pin-redelivery",
	}
	body := map[string]any{"action": "opened"}

	postWebhook(t, *trig.WebhookToken, body, headers)
	postWebhook(t, *trig.WebhookToken, body, headers)
	postWebhook(t, *trig.WebhookToken, body, headers)

	// Count autopilot_run rows linked to this trigger.
	rows, err := testHandler.Queries.ListAutopilotRuns(context.Background(), db.ListAutopilotRunsParams{
		AutopilotID: parseUUID(apID),
		Limit:       50,
		Offset:      0,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	runCount := 0
	for _, r := range rows {
		if r.TriggerID.Valid && uuidToString(r.TriggerID) == trig.ID {
			runCount++
		}
	}
	if runCount != 1 {
		t.Fatalf("expected exactly 1 run from 3 retried deliveries, got %d", runCount)
	}
}

func TestWebhookHandler_InvalidSignatureCountsAgainstRateLimit(t *testing.T) {
	// A stream of bad-signature attempts must not let an attacker bypass
	// per-token rate limiting; the limiter increment happens before
	// signature check.
	agentID := createWebhookTestAgent(t, "SigRateLimit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	prev := testHandler.WebhookRateLimiter
	testHandler.WebhookRateLimiter = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 2, Window: 60_000_000_000})
	t.Cleanup(func() { testHandler.WebhookRateLimiter = prev })

	bad := map[string]string{"X-Hub-Signature-256": "sha256=baad"}
	for i := 0; i < 2; i++ {
		w := postWebhook(t, *trig.WebhookToken, map[string]any{"i": i}, bad)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: expected 401, got %d", i, w.Code)
		}
	}
	w := postWebhook(t, *trig.WebhookToken, map[string]any{"i": "third"}, bad)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request expected 429 (rate-limited despite bad sig), got %d", w.Code)
	}
}

func TestWebhookHandler_IgnoredPathStillPersistsDelivery(t *testing.T) {
	// An ignored delivery (paused autopilot) must still leave a row so the
	// operator can see "yes the request arrived, here's why we did nothing".
	agentID := createWebhookTestAgent(t, "IgnoredPersist Agent")
	apID := createWebhookTestAutopilot(t, agentID, "paused", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"x": 1}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery on paused autopilot, got %d", len(deliveries))
	}
	if deliveries[0]["status"] != "ignored" {
		t.Fatalf("status: %v", deliveries[0]["status"])
	}
}

// A `failed` delivery (e.g. transient dispatch error) must NOT permanently
// dedupe-block the provider's retry of the same event. GitHub keeps
// `X-GitHub-Delivery` stable across retries; if the unique index trapped
// the `failed` row, the second attempt would come back as `duplicate` and
// the event would be lost.
//
// The handler-level failure path is hard to force in tests (most reasons
// route through the admission check and produce a skipped run instead),
// so we exercise the partial unique index directly: insert a `failed`
// row, then a fresh `dispatched` row with the same dedupe_key — the
// index excludes both `rejected` and `failed`, so both INSERTs must
// succeed.
func TestWebhookDelivery_FailedRowDoesNotBlockDedupe(t *testing.T) {
	ctx := context.Background()
	agentID := createWebhookTestAgent(t, "FailedRetry Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	first, err := testHandler.Queries.CreateWebhookDelivery(ctx, db.CreateWebhookDeliveryParams{
		WorkspaceID:     parseUUID(testWorkspaceID),
		AutopilotID:     parseUUID(apID),
		TriggerID:       parseUUID(trig.ID),
		Provider:        "github",
		Event:           "github.pull_request",
		SignatureStatus: "not_required",
		Status:          "failed",
		SelectedHeaders: []byte("{}"),
		DedupeKey:       pgtype.Text{String: "retry-key", Valid: true},
		DedupeSource:    pgtype.Text{String: "x-github-delivery", Valid: true},
	})
	if err != nil {
		t.Fatalf("insert failed row: %v", err)
	}

	// Same dedupe_key, status=dispatched. Must succeed: the partial unique
	// index excludes both `rejected` and `failed`, so the prior `failed`
	// row does not consume the slot.
	second, err := testHandler.Queries.CreateWebhookDelivery(ctx, db.CreateWebhookDeliveryParams{
		WorkspaceID:     parseUUID(testWorkspaceID),
		AutopilotID:     parseUUID(apID),
		TriggerID:       parseUUID(trig.ID),
		Provider:        "github",
		Event:           "github.pull_request",
		SignatureStatus: "not_required",
		Status:          "dispatched",
		SelectedHeaders: []byte("{}"),
		DedupeKey:       pgtype.Text{String: "retry-key", Valid: true},
		DedupeSource:    pgtype.Text{String: "x-github-delivery", Valid: true},
	})
	if err != nil {
		t.Fatalf("retry insert blocked by stale failed row: %v", err)
	}
	if uuidToString(second.ID) == uuidToString(first.ID) {
		t.Fatal("retry should produce a fresh row, not reuse the failed one")
	}

	// And the dedupe lookup MUST prefer the non-terminal (dispatched) row,
	// not the stale `failed` one, so a third attempt collapses onto the
	// successful delivery rather than the failure.
	got, err := testHandler.Queries.GetWebhookDeliveryByTriggerAndDedupe(ctx,
		db.GetWebhookDeliveryByTriggerAndDedupeParams{
			TriggerID: parseUUID(trig.ID),
			DedupeKey: pgtype.Text{String: "retry-key", Valid: true},
		})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Status != "dispatched" {
		t.Fatalf("lookup should prefer non-terminal row, got status=%q (id=%s)",
			got.Status, uuidToString(got.ID))
	}
}

// Confirm a column-level write — sqlc params for narg('signing_secret')
// must allow nullable NULL to clear the column, not just non-NULL strings.
func TestSetSigningSecretParams_NullableWrite(t *testing.T) {
	agentID := createWebhookTestAgent(t, "SigSqlcNull Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	if _, err := testHandler.Queries.SetAutopilotTriggerSigningSecret(context.Background(),
		db.SetAutopilotTriggerSigningSecretParams{
			ID:            parseUUID(trig.ID),
			SigningSecret: pgtype.Text{}, // explicit NULL
		}); err != nil {
		t.Fatalf("sqlc NULL write: %v", err)
	}
}
