package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// ── Token generation ────────────────────────────────────────────────────────

func TestGenerateWebhookToken_PrefixAndLength(t *testing.T) {
	token, err := generateWebhookToken()
	if err != nil {
		t.Fatalf("generateWebhookToken: %v", err)
	}
	if !strings.HasPrefix(token, "awt_") {
		t.Fatalf("expected awt_ prefix, got %q", token)
	}
	// 32 random bytes -> 43 base64-url chars (no padding).
	if len(token) != len("awt_")+43 {
		t.Fatalf("unexpected token length: %d (token=%q)", len(token), token)
	}
}

func TestGenerateWebhookToken_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 128)
	for i := 0; i < 128; i++ {
		token, err := generateWebhookToken()
		if err != nil {
			t.Fatalf("generateWebhookToken: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("duplicate token after %d generations: %q", i, token)
		}
		seen[token] = struct{}{}
	}
}

func TestGenerateWebhookToken_NoUnsafeURLChars(t *testing.T) {
	token, err := generateWebhookToken()
	if err != nil {
		t.Fatalf("generateWebhookToken: %v", err)
	}
	if strings.ContainsAny(token, "+/= ") {
		t.Fatalf("token has unsafe characters: %q", token)
	}
}

// ── Payload normalization ───────────────────────────────────────────────────

func TestNormalizeWebhookPayload_PreservesCallerProvidedEnvelope(t *testing.T) {
	body := []byte(`{"event":"caller.event","eventPayload":{"k":"v"}}`)
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	env, err := normalizeWebhookPayload(body, headers)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "caller.event" {
		t.Fatalf("event: got %q want %q", env.Event, "caller.event")
	}
	var inner map[string]string
	if err := json.Unmarshal(env.EventPayload, &inner); err != nil {
		t.Fatalf("eventPayload not preserved: %v", err)
	}
	if inner["k"] != "v" {
		t.Fatalf("eventPayload contents lost: %#v", inner)
	}
	if env.Request.ContentType != "application/json" {
		t.Fatalf("contentType: %q", env.Request.ContentType)
	}
	if env.Request.ReceivedAt == "" {
		t.Fatal("receivedAt not set")
	}
}

func TestNormalizeWebhookPayload_GitHubHeaderInferEvent(t *testing.T) {
	body := []byte(`{"action":"opened","pull_request":{"number":7}}`)
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-GitHub-Event", "pull_request")

	env, err := normalizeWebhookPayload(body, headers)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "github.pull_request.opened" {
		t.Fatalf("github event: got %q", env.Event)
	}
	// Original body preserved in eventPayload.
	if !strings.Contains(string(env.EventPayload), `"pull_request"`) {
		t.Fatalf("body not preserved in eventPayload: %s", env.EventPayload)
	}
}

func TestNormalizeWebhookPayload_GitLabHeader(t *testing.T) {
	body := []byte(`{"object_kind":"push"}`)
	headers := http.Header{}
	headers.Set("X-Gitlab-Event", "Push Hook")

	env, err := normalizeWebhookPayload(body, headers)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "gitlab.Push Hook" {
		t.Fatalf("gitlab event: got %q", env.Event)
	}
}

func TestNormalizeWebhookPayload_BodyEventField(t *testing.T) {
	body := []byte(`{"event":"demo.received","data":{"x":1}}`)
	headers := http.Header{}

	env, err := normalizeWebhookPayload(body, headers)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "demo.received" {
		t.Fatalf("event: %q", env.Event)
	}
}

func TestNormalizeWebhookPayload_BodyTypeFallback(t *testing.T) {
	body := []byte(`{"type":"foo.bar"}`)
	env, err := normalizeWebhookPayload(body, http.Header{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "foo.bar" {
		t.Fatalf("event: %q", env.Event)
	}
}

func TestNormalizeWebhookPayload_BodyActionFallback(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	env, err := normalizeWebhookPayload(body, http.Header{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "opened" {
		t.Fatalf("event: %q", env.Event)
	}
}

func TestNormalizeWebhookPayload_DefaultEvent(t *testing.T) {
	body := []byte(`{"foo":"bar"}`)
	env, err := normalizeWebhookPayload(body, http.Header{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "webhook.received" {
		t.Fatalf("event: %q", env.Event)
	}
	if !strings.Contains(string(env.EventPayload), `"foo"`) {
		t.Fatalf("event payload not preserved: %s", env.EventPayload)
	}
}

func TestNormalizeWebhookPayload_PreservesArray(t *testing.T) {
	body := []byte(`[{"a":1},{"b":2}]`)
	env, err := normalizeWebhookPayload(body, http.Header{})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "webhook.received" {
		t.Fatalf("array event: %q", env.Event)
	}
	var arr []map[string]int
	if err := json.Unmarshal(env.EventPayload, &arr); err != nil {
		t.Fatalf("array not preserved: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("array length: %d", len(arr))
	}
}

func TestNormalizeWebhookPayload_RejectsInvalidJSON(t *testing.T) {
	if _, err := normalizeWebhookPayload([]byte(`not json`), http.Header{}); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestNormalizeWebhookPayload_RejectsScalarBody(t *testing.T) {
	// Bare scalar JSON ("hello", 42) is not a useful webhook payload.
	if _, err := normalizeWebhookPayload([]byte(`"hello"`), http.Header{}); err == nil {
		t.Fatal("expected error on scalar JSON body")
	}
}

func TestNormalizeWebhookPayload_GitHubHeaderWithoutAction(t *testing.T) {
	body := []byte(`{"some":"thing"}`)
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")
	env, err := normalizeWebhookPayload(body, headers)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "github.push" {
		t.Fatalf("event: %q", env.Event)
	}
}

func TestNormalizeWebhookPayload_XEventTypeHeader(t *testing.T) {
	body := []byte(`{"a":1}`)
	headers := http.Header{}
	headers.Set("X-Event-Type", "custom.thing")
	env, err := normalizeWebhookPayload(body, headers)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if env.Event != "custom.thing" {
		t.Fatalf("event: %q", env.Event)
	}
}
