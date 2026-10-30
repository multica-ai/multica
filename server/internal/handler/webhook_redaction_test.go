package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

// #9481: credentials embedded in inbound JSON (including escaped URLs and keys)
// must be removed before delivery persistence and run/issue projection.
func TestWebhookRedactionNestedCredential(t *testing.T) {
	const token = "awt_REDACTION_TEST_SENTINEL"
	input := []byte(`{"hook":{"config":{"url":"https://example.invalid/api/webhooks/autopilots/awt_REDACTION_TEST_SENTINEL"}},"escaped":"\u0061wt_REDACTION_TEST_SENTINEL","array":["awt_REDACTION_TEST_SENTINEL"],"awt_REDACTION_TEST_SENTINEL":"key","id":9007199254740993}`)
	safe, err := redactWebhookJSON(input, token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(safe), token) {
		t.Fatalf("credential retained: %s", safe)
	}
	if !strings.Contains(string(safe), "9007199254740993") {
		t.Fatalf("provider identifier changed: %s", safe)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(safe, &object); err != nil {
		t.Fatal(err)
	}
	if string(object["escaped"]) != `"[REDACTED]"` {
		t.Fatalf("escaped credential retained: %s", object["escaped"])
	}
	if _, ok := object["[REDACTED]"]; !ok {
		t.Fatal("credential in key retained")
	}
	if !strings.Contains(string(safe), "https://example.invalid/api/webhooks/autopilots/[REDACTED]") {
		t.Fatal("URL structure was lost")
	}
	if !strings.Contains(string(input), token) {
		t.Fatal("signature input mutated")
	}
}

func TestWebhookRedactionNoCredential(t *testing.T) {
	safe, err := redactWebhookJSON([]byte(`{"event":"ping","id":9007199254740993}`), "awt_absent")
	if err != nil || string(safe) != `{"event":"ping","id":9007199254740993}` {
		t.Fatalf("unrelated payload changed: %s %v", safe, err)
	}
}
