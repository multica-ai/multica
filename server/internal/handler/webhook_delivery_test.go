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
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
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

func installWebhookAcknowledgementFailure(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	functionName := fmt.Sprintf("webhook_ack_fail_fn_%d", suffix)
	triggerName := fmt.Sprintf("webhook_ack_fail_%d", suffix)
	t.Cleanup(func() {
		testPool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON webhook_delivery`, triggerName))
		testPool.Exec(ctx, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.event = 'test.ack_failure' AND NEW.response_status = 200 THEN
		RAISE EXCEPTION 'forced webhook acknowledgement metadata failure';
	END IF;
	RETURN NEW;
END;
$$;
`, functionName)); err != nil {
		t.Fatalf("install acknowledgement failure function: %v", err)
	}
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE UPDATE ON webhook_delivery
FOR EACH ROW EXECUTE FUNCTION %s();
`, triggerName, functionName)); err != nil {
		t.Fatalf("install acknowledgement failure trigger: %v", err)
	}
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
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))

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
	if got := int(d["response_status"].(float64)); got != http.StatusOK {
		t.Fatalf("terminal delivery must retain ingress 200 acknowledgement, got %d", got)
	}
	if got := int(d["dispatch_attempts"].(float64)); got != 1 {
		t.Fatalf("expected one worker dispatch attempt, got %d", got)
	}
	if d["signature_status"] != "not_required" {
		t.Fatalf("expected signature_status=not_required, got %v", d["signature_status"])
	}
}

func TestWebhookHandler_AcknowledgementMetadataFailureStillReturns200(t *testing.T) {
	agentID := createWebhookTestAgent(t, "AckMetadataFailure Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	installWebhookAcknowledgementFailure(t)

	w := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "test.ack_failure"}, map[string]string{
		"Idempotency-Key": "ack-metadata-failure",
	})
	deliveryID := requireAcceptedWebhookResponse(t, w)
	delivery, err := testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(deliveryID))
	if err != nil {
		t.Fatalf("load durably queued delivery: %v", err)
	}
	if delivery.ResponseStatus.Valid || delivery.ResponseBody.Valid {
		t.Fatalf("forced acknowledgement metadata write unexpectedly succeeded: %#v", delivery)
	}

	completed := processQueuedWebhookDelivery(t, deliveryID)
	if completed.Status != deliveryStatusDispatched || !completed.AutopilotRunID.Valid {
		t.Fatalf("durable delivery did not dispatch after metadata failure: status=%s run=%v", completed.Status, completed.AutopilotRunID.Valid)
	}
}

func TestWebhookHandler_DedupeViaIdempotencyKey(t *testing.T) {
	agentID := createWebhookTestAgent(t, "DeliveryIdem Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	body := map[string]any{"event": "demo.x", "eventPayload": map[string]any{"k": "v"}}
	headers := map[string]string{"Idempotency-Key": "demo-key-1"}

	w1 := postWebhook(t, *trig.WebhookToken, body, headers)
	firstDeliveryID := requireAcceptedWebhookResponse(t, w1)
	var r1 map[string]any
	if err := json.Unmarshal(w1.Body.Bytes(), &r1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	firstRunID, ok := r1["run_id"].(string)
	if !ok || firstRunID == "" {
		t.Fatalf("first response missing run_id: %s", w1.Body.String())
	}

	// Retry before the worker processes the queued delivery. The admitted run
	// must already be discoverable so the duplicate returns the same run_id.
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
	firstDelivery := processQueuedWebhookDelivery(t, firstDeliveryID)
	if got := uuidToString(firstDelivery.AutopilotRunID); got != firstRunID {
		t.Fatalf("processed delivery run_id mismatch: %v != %v", got, firstRunID)
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
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w1))

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
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, w))
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
	requireAcceptedWebhookResponse(t, post)
}

func TestReplay_CreatesNewDeliveryAndDispatchesRun(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Replay Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	// Original delivery (with dedupe key) → accepted + dispatched.
	w := postWebhook(t, *trig.WebhookToken, map[string]any{"hello": "world"}, map[string]string{
		"Idempotency-Key": "replay-original",
	})
	originalID := requireAcceptedWebhookResponse(t, w)
	originalDelivery := processQueuedWebhookDelivery(t, originalID)
	originalRunID := uuidToString(originalDelivery.AutopilotRunID)

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
	deliveryID := requireAcceptedWebhookResponse(t, w)

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

	first := postWebhook(t, *trig.WebhookToken, body, headers)
	postWebhook(t, *trig.WebhookToken, body, headers)
	postWebhook(t, *trig.WebhookToken, body, headers)
	processQueuedWebhookDelivery(t, requireAcceptedWebhookResponse(t, first))

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
	deliveries := listDeliveries(t, apID)
	if len(deliveries) != 1 || deliveries[0]["status"] != deliveryStatusDispatched {
		t.Fatalf("queued duplicate should be dispatched exactly once: %#v", deliveries)
	}
	if got := int(deliveries[0]["attempt_count"].(float64)); got != 3 {
		t.Fatalf("redeliveries should bump attempt_count to 3, got %d", got)
	}
}

func TestWebhookDeliveryWorker_PerTriggerLimitDefersWithoutDropping(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerPacing Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	prev := testHandler.WebhookRateLimiter
	testHandler.WebhookRateLimiter = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute})
	t.Cleanup(func() { testHandler.WebhookRateLimiter = prev })

	firstID := requireAcceptedWebhookResponse(t, postWebhook(t, *trig.WebhookToken, map[string]any{"n": 1}, map[string]string{
		"Idempotency-Key": "worker-pacing-1",
	}))
	secondID := requireAcceptedWebhookResponse(t, postWebhook(t, *trig.WebhookToken, map[string]any{"n": 2}, map[string]string{
		"Idempotency-Key": "worker-pacing-2",
	}))

	worked, err := testHandler.WebhookDeliveryWorker.ProcessNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("first dispatch: worked=%v err=%v", worked, err)
	}
	deliveries := make([]db.WebhookDelivery, 2)
	for i, id := range []string{firstID, secondID} {
		deliveries[i], err = testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(id))
		if err != nil {
			t.Fatalf("load delivery %s: %v", id, err)
		}
	}
	queuedIndex := -1
	for i, delivery := range deliveries {
		if delivery.Status == deliveryStatusQueued {
			queuedIndex = i
		}
	}
	if queuedIndex < 0 {
		t.Fatalf("expected one delivery to remain queued: %#v", deliveries)
	}

	worked, err = testHandler.WebhookDeliveryWorker.ProcessNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("defer paced delivery: worked=%v err=%v", worked, err)
	}
	deferred, err := testHandler.Queries.GetWebhookDelivery(context.Background(), deliveries[queuedIndex].ID)
	if err != nil {
		t.Fatalf("load deferred delivery: %v", err)
	}
	if deferred.Status != deliveryStatusQueued || deferred.DispatchAttempts != 0 {
		t.Fatalf("pacing must preserve queued work without counting an attempt: status=%s attempts=%d", deferred.Status, deferred.DispatchAttempts)
	}
	if deferred.LeaseToken.Valid || deferred.LeaseExpiresAt.Valid || !deferred.AvailableAt.Valid || !deferred.AvailableAt.Time.After(time.Now()) {
		t.Fatalf("paced delivery was not released with a future availability: %#v", deferred)
	}

	var runCount, dispatchedRunCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*), count(task_id)
		FROM autopilot_run
		WHERE webhook_delivery_id IN ($1, $2)
	`, firstID, secondID).Scan(&runCount, &dispatchedRunCount); err != nil {
		t.Fatalf("count paced runs: %v", err)
	}
	if runCount != 2 || dispatchedRunCount != 1 {
		t.Fatalf("pacing should admit both runs but dispatch only one: runs=%d dispatched=%d", runCount, dispatchedRunCount)
	}
}

func TestWebhookDeliveryWorker_DispatchesRunAdmittedBeforePause(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerAdmittedBeforePause Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "accepted.before_pause"}, nil)
	deliveryID := requireAcceptedWebhookResponse(t, post)
	if _, err := testPool.Exec(context.Background(), `UPDATE autopilot SET status = 'paused' WHERE id = $1`, apID); err != nil {
		t.Fatalf("pause admitted autopilot: %v", err)
	}

	delivery := processQueuedWebhookDelivery(t, deliveryID)
	if delivery.Status != deliveryStatusDispatched || !delivery.AutopilotRunID.Valid {
		t.Fatalf("accepted delivery was stranded after pause: status=%s run=%v", delivery.Status, delivery.AutopilotRunID.Valid)
	}
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), delivery.AutopilotRunID)
	if err != nil {
		t.Fatalf("load admitted run: %v", err)
	}
	if !run.TaskID.Valid {
		t.Fatalf("accepted run was not dispatched after pause: %#v", run)
	}
}

func TestWebhookDeliveryWorker_RecoversExpiredLeaseAndReusesRun(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerRecovery Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "recovery"}, map[string]string{
		"Idempotency-Key": "worker-recovery",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery
		SET lease_token = gen_random_uuid(), lease_expires_at = now() - interval '1 second'
		WHERE id = $1
	`, deliveryID); err != nil {
		t.Fatalf("seed expired lease: %v", err)
	}

	first := processQueuedWebhookDelivery(t, deliveryID)
	if first.Status != deliveryStatusDispatched || !first.AutopilotRunID.Valid {
		t.Fatalf("expired lease was not recovered: status=%s run=%v", first.Status, first.AutopilotRunID.Valid)
	}
	firstRunID := uuidToString(first.AutopilotRunID)

	// Simulate a crash after the run/task side effect committed but before a
	// delivery terminal update was durable. Requeueing must reuse the run's
	// webhook_delivery_id anchor and must not create a second task.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery
		SET status = 'queued', autopilot_run_id = NULL, available_at = now(),
		    lease_token = NULL, lease_expires_at = NULL
		WHERE id = $1
	`, deliveryID); err != nil {
		t.Fatalf("requeue delivery: %v", err)
	}
	second := processQueuedWebhookDelivery(t, deliveryID)
	if got := uuidToString(second.AutopilotRunID); got != firstRunID {
		t.Fatalf("worker did not reuse idempotent run: got %s want %s", got, firstRunID)
	}

	var runCount, taskCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM autopilot_run WHERE webhook_delivery_id = $1
	`, deliveryID).Scan(&runCount); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1
	`, firstRunID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if runCount != 1 || taskCount != 1 {
		t.Fatalf("recovery duplicated side effects: runs=%d tasks=%d", runCount, taskCount)
	}
}

func TestWebhookDeliveryWorker_LeaseOwnershipChangeIsBenign(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerLeaseChange Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "lease-change"}, map[string]string{
		"Idempotency-Key": "worker-lease-change",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery
		SET lease_token = gen_random_uuid(), lease_expires_at = now() + interval '2 minutes'
		WHERE id = $1
	`, deliveryID); err != nil {
		t.Fatalf("seed first lease: %v", err)
	}
	staleClaim, err := testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(deliveryID))
	if err != nil {
		t.Fatalf("load first lease: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery SET lease_token = gen_random_uuid() WHERE id = $1
	`, deliveryID); err != nil {
		t.Fatalf("replace lease owner: %v", err)
	}

	worker := NewWebhookDeliveryWorker(testHandler)
	if err := worker.complete(context.Background(), staleClaim, deliveryStatusDispatched, pgtype.UUID{}, ""); err != nil {
		t.Fatalf("stale complete should be benign: %v", err)
	}
	if err := worker.retryOrFail(context.Background(), staleClaim, fmt.Errorf("forced transient failure")); err != nil {
		t.Fatalf("stale retry should be benign: %v", err)
	}
	current, err := testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(deliveryID))
	if err != nil {
		t.Fatalf("load current owner: %v", err)
	}
	if current.Status != deliveryStatusQueued || current.LeaseToken == staleClaim.LeaseToken {
		t.Fatalf("stale worker mutated the new owner's delivery: %#v", current)
	}
}

func TestWebhookDeliveryWorker_RunStopsBoundedPool(t *testing.T) {
	worker := NewWebhookDeliveryWorker(testHandler)
	ctx, cancel := context.WithCancel(context.Background())
	go worker.Run(ctx)
	cancel()
	if !worker.WaitWithTimeout(time.Second) {
		t.Fatal("bounded worker pool did not stop after cancellation")
	}
}

func TestWebhookDeliveryWorker_RepairsCreateIssueTaskCrashWindow(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerIssueRecovery Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "issue-recovery"}, map[string]string{
		"Idempotency-Key": "worker-issue-recovery",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	first := processQueuedWebhookDelivery(t, deliveryID)
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil || !run.IssueID.Valid {
		t.Fatalf("load create_issue run: run=%#v err=%v", run, err)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Remove the task to model a process exit after the issue/run transaction
	// but before task enqueue, then make the delivery recoverable again.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("remove issue task: %v", err)
	}
	// Binding run.task_id + advancing to running is the LAST step of create_issue
	// dispatch (MUL-4809 §4.1), so a crash before it leaves the run in
	// issue_created with a NULL task_id. Model exactly that state so the repair
	// path (issue exists, task missing) is exercised.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_run SET status = 'issue_created', task_id = NULL WHERE id = $1`,
		first.AutopilotRunID); err != nil {
		t.Fatalf("reset run to pre-bind crash state: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery
		SET status = 'queued', autopilot_run_id = NULL, available_at = now(),
		    lease_token = NULL, lease_expires_at = NULL
		WHERE id = $1
	`, deliveryID); err != nil {
		t.Fatalf("requeue delivery: %v", err)
	}
	second := processQueuedWebhookDelivery(t, deliveryID)
	if got := uuidToString(second.AutopilotRunID); got != uuidToString(first.AutopilotRunID) {
		t.Fatalf("repair should reuse run: got %s want %s", got, uuidToString(first.AutopilotRunID))
	}
	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&taskCount); err != nil {
		t.Fatalf("count repaired task: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected one repaired issue task, got %d", taskCount)
	}
}

// TestWebhookDeliveryWorker_PendingCollisionFailsDeliveryAndRun drives the REAL
// WebhookDeliveryWorker (not just the service entry point) through a create_issue
// dispatch collision and asserts the persisted delivery row, closing the P0-1 loop at
// the worker level (MUL-4809 §4.1 P0-1). The dispatched task's crash-window repair
// collides with an unrelated pending comment task the agent already holds, so the run
// is finalized as a traceable dispatch collision. The forbidden
// `delivery=dispatched && run active/unbound` state must never appear: the delivery
// row must read `failed` and the run must be `failed`, not bound to the stray task.
func TestWebhookDeliveryWorker_PendingCollisionFailsDeliveryAndRun(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerCollision Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "collision"}, map[string]string{
		"Idempotency-Key": "worker-collision",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	first := processQueuedWebhookDelivery(t, deliveryID)
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil || !run.IssueID.Valid {
		t.Fatalf("load create_issue run: run=%#v err=%v", run, err)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Model a pre-bind crash: drop the stamped dispatched task and reset the run to
	// issue_created + NULL task_id so the worker re-enters the repair path.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("remove dispatched task: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_run SET status = 'issue_created', task_id = NULL WHERE id = $1`,
		first.AutopilotRunID); err != nil {
		t.Fatalf("reset run to pre-bind crash state: %v", err)
	}

	// The agent now holds its one pending task per (issue, agent): an UNSTAMPED comment
	// task. The repair's dispatched-task enqueue collides with it (idx_one_pending_task
	// _per_issue_agent), leaving the run no provably-dispatched task to bind.
	var strayID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		SELECT a.id, a.runtime_id, $2::uuid, 'queued', 0, now() FROM agent a WHERE a.id = $1
		RETURNING id`,
		agentID, issueID).Scan(&strayID); err != nil {
		t.Fatalf("insert stray pending task: %v", err)
	}

	// Requeue the delivery so the worker reclaims it and re-runs dispatch → repair.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery
		SET status = 'queued', autopilot_run_id = NULL, available_at = now(),
		    lease_token = NULL, lease_expires_at = NULL
		WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("requeue delivery: %v", err)
	}

	final := processQueuedWebhookDelivery(t, deliveryID)
	if final.Status != deliveryStatusFailed {
		t.Fatalf("dispatch collision must record delivery=failed, got %q", final.Status)
	}
	got, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil {
		t.Fatalf("load run after collision: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("dispatch collision must fail the run, got %q", got.Status)
	}
	if got.TaskID.Valid && uuidToString(got.TaskID) == strayID {
		t.Fatal("run must not be bound to the unrelated pending comment task")
	}
}

// installAutopilotRunFailTransitionFault makes every UPDATE that moves an autopilot_run
// to 'failed' raise, simulating a transient error during the collision fail-transition.
func installAutopilotRunFailTransitionFault(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
CREATE OR REPLACE FUNCTION mul4809_run_fail_fault() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.status = 'failed' THEN
		RAISE EXCEPTION 'forced autopilot_run fail-transition fault';
	END IF;
	RETURN NEW;
END;
$$;`); err != nil {
		t.Fatalf("install run-fail fault fn: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
CREATE TRIGGER mul4809_run_fail_fault_trg
BEFORE UPDATE ON autopilot_run
FOR EACH ROW EXECUTE FUNCTION mul4809_run_fail_fault();`); err != nil {
		t.Fatalf("install run-fail fault trigger: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DROP TRIGGER IF EXISTS mul4809_run_fail_fault_trg ON autopilot_run`)
		testPool.Exec(context.Background(), `DROP FUNCTION IF EXISTS mul4809_run_fail_fault()`)
	})
}

// TestWebhookDeliveryWorker_CollisionTransientErrorRetriesThenConverges is the P0-1
// "transient collision error stays retryable" counter-example driven through the real
// WebhookDeliveryWorker (MUL-4809 §4.1 P0-1). When the dispatch collision's
// fail-transition hits a transient error, the delivery must NOT be recorded failed over
// a still-active run — it must be retried. After the error clears, the next processing
// converges the run and the delivery to failed together.
func TestWebhookDeliveryWorker_CollisionTransientErrorRetriesThenConverges(t *testing.T) {
	agentID := createWebhookTestAgent(t, "WorkerCollisionRetry Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "collision-retry"}, map[string]string{
		"Idempotency-Key": "worker-collision-retry",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	first := processQueuedWebhookDelivery(t, deliveryID)
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil || !run.IssueID.Valid {
		t.Fatalf("load create_issue run: run=%#v err=%v", run, err)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Pre-bind crash + a stray pending comment task so the repair's dispatched enqueue
	// collides and takes the fail-transition path.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("remove dispatched task: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_run SET status = 'issue_created', task_id = NULL WHERE id = $1`, first.AutopilotRunID); err != nil {
		t.Fatalf("reset run: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		SELECT a.id, a.runtime_id, $2::uuid, 'queued', 0, now() FROM agent a WHERE a.id = $1`,
		agentID, issueID); err != nil {
		t.Fatalf("insert stray pending task: %v", err)
	}

	// The collision's fail-transition errors on this pass.
	installAutopilotRunFailTransitionFault(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE webhook_delivery SET status = 'queued', autopilot_run_id = NULL, available_at = now(),
		    dispatch_attempts = 0, lease_token = NULL, lease_expires_at = NULL WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("requeue delivery: %v", err)
	}

	worked, err := testHandler.WebhookDeliveryWorker.ProcessNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("fault pass: worked=%v err=%v", worked, err)
	}
	deferred, err := testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(deliveryID))
	if err != nil {
		t.Fatalf("load deferred delivery: %v", err)
	}
	if deferred.Status == deliveryStatusFailed {
		t.Fatal("a transient fail-transition error must not terminate the delivery")
	}
	if deferred.Status != deliveryStatusQueued {
		t.Fatalf("expected the delivery to stay retryable (queued), got %q", deferred.Status)
	}
	runMid, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil {
		t.Fatalf("load run mid: %v", err)
	}
	if runMid.Status != "issue_created" {
		t.Fatalf("run must stay active while the delivery is retryable, got %q", runMid.Status)
	}

	// Clear the fault + backoff and reprocess: the fail-transition now succeeds, so the
	// run and the delivery converge to failed together.
	if _, err := testPool.Exec(context.Background(), `DROP TRIGGER IF EXISTS mul4809_run_fail_fault_trg ON autopilot_run`); err != nil {
		t.Fatalf("clear fault: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE webhook_delivery SET available_at = now() WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("clear backoff: %v", err)
	}
	worked, err = testHandler.WebhookDeliveryWorker.ProcessNext(context.Background())
	if err != nil || !worked {
		t.Fatalf("recovery pass: worked=%v err=%v", worked, err)
	}
	final, err := testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(deliveryID))
	if err != nil {
		t.Fatalf("load final delivery: %v", err)
	}
	if final.Status != deliveryStatusFailed {
		t.Fatalf("recovered delivery must be failed, got %q", final.Status)
	}
	got, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil {
		t.Fatalf("load final run: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("run must converge to failed together with the delivery, got %q", got.Status)
	}
}

// TestReconcileFailsDispatchlessRunWhenDeliveryPermanentlyFailed pins the durable
// backstop for the P0-1 exhaustion invariant (MUL-4809 §4.1 P0-1): if a collision run's
// fail-transition kept erroring until its delivery exhausted its retries (delivery
// permanently failed, run still active with no dispatched task), the reconcile converges
// the run off the delivery's durable terminal state — no failed delivery is left beside a
// live run.
func TestReconcileFailsDispatchlessRunWhenDeliveryPermanentlyFailed(t *testing.T) {
	agentID := createWebhookTestAgent(t, "ReconcileDispatchless Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "dispatchless"}, map[string]string{
		"Idempotency-Key": "reconcile-dispatchless",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	first := processQueuedWebhookDelivery(t, deliveryID)
	run, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil || !run.IssueID.Valid {
		t.Fatalf("load create_issue run: run=%#v err=%v", run, err)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Strand it: no dispatched task, run active, delivery permanently failed.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("remove dispatched task: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot_run SET status = 'issue_created', task_id = NULL WHERE id = $1`, first.AutopilotRunID); err != nil {
		t.Fatalf("reset run: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE webhook_delivery SET status = 'failed' WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("fail delivery: %v", err)
	}

	if _, err := testHandler.AutopilotService.ReconcileAutopilotRuns(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, err := testHandler.Queries.GetAutopilotRun(context.Background(), first.AutopilotRunID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("reconcile did not converge the dispatchless run off the failed delivery: %q", got.Status)
	}
}

// withTaskDrivenGateOff forces the FIRST rollout phase (FF_AUTOPILOT_TASK_DRIVEN_RUNS
// default off) for the duration of a test.
func withTaskDrivenGateOff(t *testing.T) {
	t.Helper()
	prev := testHandler.AutopilotService.FeatureFlags
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.AutopilotTaskDrivenRuns, featureflag.Rule{Default: false})
	testHandler.AutopilotService.FeatureFlags = featureflag.NewService(provider)
	t.Cleanup(func() { testHandler.AutopilotService.FeatureFlags = prev })
}

// TestWebhookDeliveryExhaustionConvergesRunWhileGateOff is the P0-1 two-phase-rollout
// counter-example (MUL-4809 §4.1 P0-1). Deployed as the PR requires — task-driven gate
// OFF — a collision whose fail-transition fault persists all the way to delivery
// exhaustion leaves `delivery=failed + run=active/unbound`. The durable backstop must NOT
// be gated behind the task-driven flag: once the fault clears, the reconcile has to
// converge the run WITHOUT flipping the gate.
func TestWebhookDeliveryExhaustionConvergesRunWhileGateOff(t *testing.T) {
	withTaskDrivenGateOff(t)
	ctx := context.Background()

	agentID := createWebhookTestAgent(t, "GateOffExhaustion Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	trig := createWebhookTriggerViaHandler(t, apID)

	post := postWebhook(t, *trig.WebhookToken, map[string]any{"event": "gate-off-exhaustion"}, map[string]string{
		"Idempotency-Key": "gate-off-exhaustion",
	})
	deliveryID := requireAcceptedWebhookResponse(t, post)
	first := processQueuedWebhookDelivery(t, deliveryID)
	run, err := testHandler.Queries.GetAutopilotRun(ctx, first.AutopilotRunID)
	if err != nil || !run.IssueID.Valid {
		t.Fatalf("load create_issue run: run=%#v err=%v", run, err)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Pre-bind crash + stray pending task so the repair collides, and a fault that keeps
	// the collision's fail-transition erroring for every attempt.
	if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("remove dispatched task: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_run SET status = 'issue_created', task_id = NULL WHERE id = $1`, first.AutopilotRunID); err != nil {
		t.Fatalf("reset run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		SELECT a.id, a.runtime_id, $2::uuid, 'queued', 0, now() FROM agent a WHERE a.id = $1`,
		agentID, issueID); err != nil {
		t.Fatalf("insert stray pending task: %v", err)
	}
	installAutopilotRunFailTransitionFault(t)
	if _, err := testPool.Exec(ctx, `
		UPDATE webhook_delivery SET status = 'queued', autopilot_run_id = NULL, available_at = now(),
		    dispatch_attempts = 0, lease_token = NULL, lease_expires_at = NULL WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("requeue delivery: %v", err)
	}

	// Drive the worker until the delivery exhausts its retries.
	var delivery db.WebhookDelivery
	for i := 0; i < webhookWorkerMaxAttempts+1; i++ {
		if _, err := testPool.Exec(ctx, `UPDATE webhook_delivery SET available_at = now() WHERE id = $1`, deliveryID); err != nil {
			t.Fatalf("clear backoff: %v", err)
		}
		worked, err := testHandler.WebhookDeliveryWorker.ProcessNext(ctx)
		if err != nil || !worked {
			t.Fatalf("attempt %d: worked=%v err=%v", i, worked, err)
		}
		delivery, err = testHandler.Queries.GetWebhookDelivery(ctx, parseUUID(deliveryID))
		if err != nil {
			t.Fatalf("load delivery: %v", err)
		}
		if delivery.Status == deliveryStatusFailed {
			break
		}
	}
	if delivery.Status != deliveryStatusFailed {
		t.Fatalf("delivery should have exhausted its retries, got %q", delivery.Status)
	}
	// The fault also blocked the worker's immediate convergence, so the run is stranded.
	stranded, err := testHandler.Queries.GetAutopilotRun(ctx, first.AutopilotRunID)
	if err != nil {
		t.Fatalf("load stranded run: %v", err)
	}
	if stranded.Status != "issue_created" {
		t.Fatalf("precondition: expected the run to still be active, got %q", stranded.Status)
	}

	// Fault clears. The gate stays OFF — the reconcile must still converge the run.
	if _, err := testPool.Exec(ctx, `DROP TRIGGER IF EXISTS mul4809_run_fail_fault_trg ON autopilot_run`); err != nil {
		t.Fatalf("clear fault: %v", err)
	}
	if _, err := testHandler.AutopilotService.ReconcileAutopilotRuns(ctx); err != nil {
		t.Fatalf("gate-off reconcile: %v", err)
	}
	got, err := testHandler.Queries.GetAutopilotRun(ctx, first.AutopilotRunID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("gate-off reconcile did not converge the stranded run: %q", got.Status)
	}
}

func TestWebhookHandler_InvalidSignatureCountsAgainstRateLimit(t *testing.T) {
	// Only requests classified as bad credentials consume the shared-IP debt
	// budget; valid traffic behind the same NAT does not.
	agentID := createWebhookTestAgent(t, "SigRateLimit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createWebhookTriggerViaHandler(t, apID)
	setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)

	prev := testHandler.WebhookIPRateLimiter
	testHandler.WebhookIPRateLimiter = NewMemoryWebhookIPRateLimiter(WebhookRateLimit{Limit: 2, Window: 60_000_000_000})
	t.Cleanup(func() { testHandler.WebhookIPRateLimiter = prev })

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
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 must include Retry-After")
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
