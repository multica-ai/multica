package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The issue explicitly requests a disposable-credential ingress regression,
// signature/filter/dedupe preservation, and both execution modes.
func TestWebhookPayloadIngressPolicy(t *testing.T) {
	for _, mode := range []string{"run_only", "create_issue"} {
		for _, policy := range []string{"redacted", "metadata_only"} {
			t.Run(mode+"/"+policy, func(t *testing.T) {
				agentID := createWebhookTestAgent(t, "Payload policy agent")
				apID := createWebhookTestAutopilot(t, agentID, "active", mode)
				trig := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{{Event: "ping"}})
				if trig.WebhookPayloadPolicy != "redacted" {
					t.Fatalf("default policy = %q", trig.WebhookPayloadPolicy)
				}
				patch := httptest.NewRecorder()
				req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{"webhook_payload_policy": policy})
				testHandler.UpdateAutopilotTrigger(patch, withURLParams(req, "id", apID, "triggerId", trig.ID))
				if patch.Code != http.StatusOK {
					t.Fatalf("policy patch: %d %s", patch.Code, patch.Body.String())
				}
				setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)
				// This is the actual disposable trigger credential, not an unrelated marker.
				token := *trig.WebhookToken
				body := []byte(fmt.Sprintf(`{"hook":{"config":{"url":"https://example.invalid/api/webhooks/autopilots/%s"}},"private":"body marker"}`, token))
				headers := map[string]string{"X-GitHub-Event": "ping", "Idempotency-Key": "payload-policy-test", "X-Hub-Signature-256": signBody(testSigningSecret, body)}
				first := postWebhook(t, token, body, headers)
				deliveryID := requireAcceptedWebhookResponse(t, first)
				duplicate := postWebhook(t, token, body, headers)
				if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), `"duplicate"`) {
					t.Fatalf("dedupe changed: %s", duplicate.Body.String())
				}
				delivery := processQueuedWebhookDelivery(t, deliveryID)
				if delivery.SignatureStatus != sigStatusValid {
					t.Fatalf("original-byte signature failed: %s", delivery.SignatureStatus)
				}
				run, err := testHandler.Queries.GetAutopilotRun(context.Background(), delivery.AutopilotRunID)
				if err != nil {
					t.Fatal(err)
				}
				for name, payload := range map[string][]byte{"delivery": delivery.RawBody, "run": run.TriggerPayload} {
					if strings.Contains(string(payload), token) {
						t.Fatalf("%s retained credential", name)
					}
					if policy == "metadata_only" && strings.Contains(string(payload), "body marker") {
						t.Fatalf("%s retained body", name)
					}
					var envelope WebhookEnvelope
					if err := json.Unmarshal(payload, &envelope); err != nil {
						t.Fatal(err)
					}
					if policy == "metadata_only" && string(envelope.EventPayload) != "null" {
						t.Fatalf("%s did not discard payload", name)
					}
				}
				if mode == "create_issue" {
					if !run.IssueID.Valid {
						t.Fatalf("expected generated issue projection, run status=%s reason=%s", run.Status, run.FailureReason.String)
					}
					var description string
					if err := testPool.QueryRow(context.Background(), "SELECT description FROM issue WHERE id = $1", run.IssueID).Scan(&description); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(description, token) || (policy == "metadata_only" && strings.Contains(description, "body marker")) {
						t.Fatal("issue retained sensitive payload")
					}
				}
				if policy == "metadata_only" {
					replay := httptest.NewRecorder()
					replayReq := newRequest("POST", "/api/autopilots/"+apID+"/deliveries/"+deliveryID+"/replay", nil)
					testHandler.ReplayAutopilotDelivery(replay, withURLParams(replayReq, "id", apID, "deliveryId", deliveryID))
					if replay.Code != http.StatusBadRequest || !strings.Contains(replay.Body.String(), "metadata-only") {
						t.Fatalf("replay must explicitly reject discarded bodies: %d %s", replay.Code, replay.Body.String())
					}
				}
			})
		}
	}
}

func TestWebhookPayloadMetadataScopeAndUntrustedInput(t *testing.T) {
	envelope, err := normalizeWebhookPayload([]byte(`{"event":"pull_request","eventPayload":{"action":"opened"}}`), http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	safe, err := prepareWebhookPayload(envelope, "awt_test", "metadata_only")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(safe)
	recovered, err := storedWebhookPayload(encoded, http.Header{})
	if err != nil || !webhookEventAllowedByTriggerScope([]byte(`[{"event":"pull_request","actions":["opened"]}]`), recovered) {
		t.Fatal("durable event action filter changed")
	}
	forged, _ := normalizeWebhookPayload([]byte(`{"event":"ping","eventPayload":{"private":true},"payloadPolicy":"metadata_only","eventActions":["forged"]}`), http.Header{})
	if forged.PayloadPolicy != "" || len(forged.EventActions) != 0 {
		t.Fatal("caller controlled retention metadata")
	}
}

func TestWebhookPayloadNonDispatchRetention(t *testing.T) {
	for _, reason := range []string{"disabled", "filtered", "rejected"} {
		t.Run(reason, func(t *testing.T) {
			agentID := createWebhookTestAgent(t, "Payload non-dispatch agent")
			apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
			trig := createWebhookTriggerWithFilters(t, apID, []WebhookEventFilter{{Event: "ping"}})
			settings := map[string]any{"webhook_payload_policy": "metadata_only"}
			if reason == "disabled" {
				settings["enabled"] = false
			}
			patch := httptest.NewRecorder()
			req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, settings)
			testHandler.UpdateAutopilotTrigger(patch, withURLParams(req, "id", apID, "triggerId", trig.ID))
			if patch.Code != http.StatusOK {
				t.Fatalf("patch failed: %s", patch.Body.String())
			}
			headers := map[string]string{"X-GitHub-Event": "ping"}
			if reason == "filtered" {
				headers["X-GitHub-Event"] = "push"
			}
			if reason == "rejected" {
				setSigningSecretViaHandler(t, apID, trig.ID, testSigningSecret)
				headers["X-Hub-Signature-256"] = "sha256=invalid"
			}
			response := postWebhook(t, *trig.WebhookToken, map[string]any{"secret": *trig.WebhookToken, "body": "discard me"}, headers)
			expected := http.StatusOK
			if reason == "rejected" {
				expected = http.StatusUnauthorized
			}
			if response.Code != expected {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var acknowledgement struct {
				DeliveryID string `json:"delivery_id"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &acknowledgement); err != nil {
				t.Fatal(err)
			}
			delivery, err := testHandler.Queries.GetWebhookDelivery(context.Background(), parseUUID(acknowledgement.DeliveryID))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(delivery.RawBody), *trig.WebhookToken) || strings.Contains(string(delivery.RawBody), "discard me") {
				t.Fatal("non-dispatched delivery retained body")
			}
		})
	}
}
