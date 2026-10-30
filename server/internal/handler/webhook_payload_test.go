package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebhookPayloadMetadataOnly(t *testing.T) {
	const token = "awt_REDACTION_TEST_SENTINEL"
	envelope := WebhookEnvelope{Event: "pull_request", EventPayload: json.RawMessage(`{"action":"opened","hook":{"config":{"url":"https://example.invalid/awt_REDACTION_TEST_SENTINEL"}},"private":"discard me"}`), Request: WebhookRequest{ReceivedAt: "2026-10-23T16:00:00Z"}}
	safe, err := prepareWebhookPayload(envelope, token, "metadata_only")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(safe)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "discard me") || string(safe.EventPayload) != "null" {
		t.Fatalf("payload retained: %s", encoded)
	}
	if len(safe.EventActions) != 1 || safe.EventActions[0] != "opened" {
		t.Fatal("event action metadata lost")
	}
	var recovered WebhookEnvelope
	if err := json.Unmarshal(encoded, &recovered); err != nil {
		t.Fatal(err)
	}
	recovered, err = prepareWebhookPayload(recovered, token, "redacted")
	if err != nil || recovered.PayloadPolicy != "metadata_only" || string(recovered.EventPayload) != "null" {
		t.Fatal("policy change resurrected discarded payload")
	}
}

func TestWebhookPayloadDefaultRetainsRedactedBody(t *testing.T) {
	envelope := WebhookEnvelope{Event: "ping", EventPayload: json.RawMessage(`{"url":"https://example.invalid/awt_sentinel","other":"retained"}`)}
	safe, err := prepareWebhookPayload(envelope, "awt_sentinel", "redacted")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(safe.EventPayload), "awt_sentinel") || !strings.Contains(string(safe.EventPayload), "retained") || safe.PayloadPolicy != "" {
		t.Fatalf("default compatibility: %+v", safe)
	}
}
