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

// ── Event filter helpers ────────────────────────────────────────────────────

func TestWebhookEventAllowedByTriggerScope_NoFiltersAllowsAll(t *testing.T) {
	if !webhookEventAllowedByTriggerScope(nil, WebhookEnvelope{Event: "github.push"}) {
		t.Fatal("nil filters should allow all")
	}
	if !webhookEventAllowedByTriggerScope([]byte{}, WebhookEnvelope{Event: "github.push"}) {
		t.Fatal("empty filters should allow all")
	}
	if !webhookEventAllowedByTriggerScope([]byte("[]"), WebhookEnvelope{Event: "github.push"}) {
		t.Fatal("empty JSON array should allow all")
	}
}

func TestWebhookEventAllowedByTriggerScope_FiltersUndeclaredEvent(t *testing.T) {
	filters := []byte(`[{"event":"workflow_run","actions":["completed"]}]`)
	env := WebhookEnvelope{Event: "github.push", EventPayload: json.RawMessage(`{"action":"pushed"}`)}
	if webhookEventAllowedByTriggerScope(filters, env) {
		t.Fatal("undeclared event should be filtered")
	}
}

func TestWebhookEventAllowedByTriggerScope_FiltersUndeclaredAction(t *testing.T) {
	filters := []byte(`[{"event":"workflow_run","actions":["completed"]}]`)
	env := WebhookEnvelope{Event: "github.workflow_run.in_progress", EventPayload: json.RawMessage(`{"action":"in_progress"}`)}
	if webhookEventAllowedByTriggerScope(filters, env) {
		t.Fatal("undeclared action should be filtered")
	}
}

func TestWebhookEventAllowedByTriggerScope_AllowsDeclaredAction(t *testing.T) {
	filters := []byte(`[{"event":"workflow_run","actions":["completed"]}]`)
	env := WebhookEnvelope{Event: "github.workflow_run.completed", EventPayload: json.RawMessage(`{"action":"completed"}`)}
	if !webhookEventAllowedByTriggerScope(filters, env) {
		t.Fatal("declared action should be allowed")
	}
}

func TestWebhookEventAllowedByTriggerScope_AnyActionWhenEmpty(t *testing.T) {
	filters := []byte(`[{"event":"workflow_run"}]`)
	env := WebhookEnvelope{Event: "github.workflow_run.in_progress", EventPayload: json.RawMessage(`{"action":"in_progress"}`)}
	if !webhookEventAllowedByTriggerScope(filters, env) {
		t.Fatal("empty actions should allow any action for the event")
	}
}

// TestWebhookEventAllowedByTriggerScope_MultipleFiltersSameEvent pins the
// fix for PR #3231 review: the matcher used to return false as soon as it
// hit the first event-name match whose actions didn't line up, which made
// later filters covering the same event but different actions unreachable
// (order-dependent silent drops). The fix is to keep scanning and only
// short-circuit on a positive match.
func TestWebhookEventAllowedByTriggerScope_MultipleFiltersSameEvent(t *testing.T) {
	filters := []byte(`[
		{"event":"workflow_run","actions":["completed"]},
		{"event":"workflow_run","actions":["requested"]}
	]`)

	completed := WebhookEnvelope{
		Event:        "github.workflow_run.completed",
		EventPayload: json.RawMessage(`{"action":"completed"}`),
	}
	if !webhookEventAllowedByTriggerScope(filters, completed) {
		t.Fatal("workflow_run.completed should match the first filter")
	}

	requested := WebhookEnvelope{
		Event:        "github.workflow_run.requested",
		EventPayload: json.RawMessage(`{"action":"requested"}`),
	}
	if !webhookEventAllowedByTriggerScope(filters, requested) {
		t.Fatal("workflow_run.requested should match the second filter — pre-fix this silently dropped")
	}

	inProgress := WebhookEnvelope{
		Event:        "github.workflow_run.in_progress",
		EventPayload: json.RawMessage(`{"action":"in_progress"}`),
	}
	if webhookEventAllowedByTriggerScope(filters, inProgress) {
		t.Fatal("workflow_run.in_progress is in neither filter and should be filtered out")
	}
}

// TestWebhookEventAllowedByTriggerScope_MalformedDenies pins the
// fail-closed behavior for corrupted rows. Strict write-time validation
// (validateWebhookEventFilters) is the primary defense; this is the
// defense-in-depth check for "what if a malformed row somehow exists".
func TestWebhookEventAllowedByTriggerScope_MalformedDenies(t *testing.T) {
	corrupt := []byte(`{not a json array}`)
	env := WebhookEnvelope{
		Event:        "github.workflow_run.completed",
		EventPayload: json.RawMessage(`{"action":"completed"}`),
	}
	if webhookEventAllowedByTriggerScope(corrupt, env) {
		t.Fatal("malformed event_filters must fail closed (deny), never widen the allowlist")
	}
}

func TestWebhookEventAllowedByTriggerScope_MultipleFilters(t *testing.T) {
	filters := []byte(`[{"event":"workflow_run","actions":["completed"]},{"event":"check_suite","actions":["completed","failure"]}]`)

	allowed1 := WebhookEnvelope{Event: "github.check_suite.completed", EventPayload: json.RawMessage(`{"action":"completed"}`)}
	if !webhookEventAllowedByTriggerScope(filters, allowed1) {
		t.Fatal("check_suite.completed should be allowed")
	}

	allowed2 := WebhookEnvelope{Event: "github.check_suite.failure", EventPayload: json.RawMessage(`{"action":"failure"}`)}
	if !webhookEventAllowedByTriggerScope(filters, allowed2) {
		t.Fatal("check_suite.failure should be allowed")
	}

	filtered := WebhookEnvelope{Event: "github.check_suite.requested", EventPayload: json.RawMessage(`{"action":"requested"}`)}
	if webhookEventAllowedByTriggerScope(filters, filtered) {
		t.Fatal("check_suite.requested should be filtered")
	}
}

// reviewFilters scopes pull_request deliveries to ready, labelled PRs on
// release branches that weren't opened by a bot.
const reviewFilters = `[{"event":"pull_request","actions":["opened","synchronize"],"conditions":[
	{"path":"pull_request.draft","op":"equals","value":false},
	{"path":"pull_request.labels.*.name","op":"equals","value":"needs-review","ignore_case":true},
	{"path":"pull_request.base.ref","op":"in","values":["main","release"]},
	{"path":"sender.type","op":"not_equals","value":"Bot"}
]}]`

func pullRequestEnvelope(payload string) WebhookEnvelope {
	return WebhookEnvelope{Event: "github.pull_request.opened", EventPayload: json.RawMessage(payload)}
}

func TestWebhookEventAllowedByTriggerScope_PayloadConditions(t *testing.T) {
	filters := []byte(reviewFilters)
	const labelled = `"labels":[{"name":"backend"},{"name":"Needs-Review"}]`
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"all conditions hold", `{"action":"opened","pull_request":{"draft":false,` + labelled + `,"base":{"ref":"main"}},"sender":{"type":"User"}}`, true},
		{"draft", `{"action":"opened","pull_request":{"draft":true,` + labelled + `,"base":{"ref":"main"}},"sender":{"type":"User"}}`, false},
		{"label missing", `{"action":"opened","pull_request":{"draft":false,"labels":[{"name":"backend"}],"base":{"ref":"main"}},"sender":{"type":"User"}}`, false},
		{"no labels", `{"action":"opened","pull_request":{"draft":false,"labels":[],"base":{"ref":"main"}},"sender":{"type":"User"}}`, false},
		{"other base branch", `{"action":"opened","pull_request":{"draft":false,` + labelled + `,"base":{"ref":"dev"}},"sender":{"type":"User"}}`, false},
		{"bot sender", `{"action":"opened","pull_request":{"draft":false,` + labelled + `,"base":{"ref":"main"}},"sender":{"type":"Bot"}}`, false},
		{"missing path", `{"action":"opened","pull_request":{"draft":false,` + labelled + `}}`, false},
		{"string is not a boolean", `{"action":"opened","pull_request":{"draft":"false",` + labelled + `,"base":{"ref":"main"}},"sender":{"type":"User"}}`, false},
		{"non-object payload", `["needs-review"]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhookEventAllowedByTriggerScope(filters, pullRequestEnvelope(tc.payload)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWebhookEventAllowedByTriggerScope_ConditionsDoNotShadowLaterFilters(t *testing.T) {
	filters := []byte(`[
		{"event":"pull_request","conditions":[{"path":"pull_request.draft","op":"equals","value":false}]},
		{"event":"pull_request","conditions":[{"path":"pull_request.number","op":"equals","value":42}]}
	]`)
	if !webhookEventAllowedByTriggerScope(filters, pullRequestEnvelope(`{"pull_request":{"draft":true,"number":42}}`)) {
		t.Fatal("second filter should match when the first one's conditions fail")
	}
}

func TestWebhookEventAllowedByTriggerScope_ConditionOps(t *testing.T) {
	payload := `{"status":"Open","closed_at":null,"count":3,"items":[{"tag":"urgent"},{"tag":"docs"}],"commits":[{"files":["a.go"]},{"files":["README.md"]}]}`
	cases := []struct {
		name string
		cond string
		want bool
	}{
		{"equals is case-sensitive by default", `{"path":"status","op":"equals","value":"open"}`, false},
		{"equals ignore_case", `{"path":"status","op":"equals","value":"open","ignore_case":true}`, true},
		{"not_equals", `{"path":"status","op":"not_equals","value":"Closed"}`, true},
		{"not_equals on missing path", `{"path":"missing","op":"not_equals","value":"x"}`, false},
		{"equals number", `{"path":"count","op":"equals","value":3}`, true},
		{"contains on number", `{"path":"count","op":"contains","value":"3"}`, false},
		{"array index", `{"path":"items.0.tag","op":"equals","value":"urgent"}`, true},
		{"array index out of range", `{"path":"items.2.tag","op":"equals","value":"urgent"}`, false},
		{"wildcard any element", `{"path":"items.*.tag","op":"equals","value":"docs"}`, true},
		{"wildcard no element", `{"path":"items.*.tag","op":"equals","value":"bug"}`, false},
		{"wildcard not_equals means none", `{"path":"items.*.tag","op":"not_equals","value":"docs"}`, false},
		{"wildcard not_equals holds", `{"path":"items.*.tag","op":"not_equals","value":"bug"}`, true},
		{"nested wildcards", `{"path":"commits.*.files.*","op":"contains","value":".md"}`, true},
		{"wildcard on object", `{"path":"status.*","op":"equals","value":"Open"}`, false},
		{"in ignore_case", `{"path":"status","op":"in","values":["closed","open"],"ignore_case":true}`, true},
		{"exists", `{"path":"items.*.tag","op":"exists"}`, true},
		{"exists on missing path", `{"path":"merged_at","op":"exists"}`, false},
		{"exists ignores null", `{"path":"closed_at","op":"exists"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filters := []byte(`[{"event":"pull_request","conditions":[` + tc.cond + `]}]`)
			if got := webhookEventAllowedByTriggerScope(filters, pullRequestEnvelope(payload)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateWebhookEventFilters_Conditions(t *testing.T) {
	var valid []WebhookEventFilter
	if err := json.Unmarshal([]byte(reviewFilters), &valid); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := validateWebhookEventFilters(valid); err != nil {
		t.Fatalf("valid filters rejected: %v", err)
	}

	invalid := map[string]string{
		"empty path":        `{"path":"","op":"equals","value":"x"}`,
		"empty segment":     `{"path":"sender..type","op":"equals","value":"x"}`,
		"unknown op":        `{"path":"sender.type","op":"matches","value":"x"}`,
		"missing value":     `{"path":"sender.type","op":"equals"}`,
		"object value":      `{"path":"sender.type","op":"equals","value":{"a":1}}`,
		"contains non-text": `{"path":"sender.type","op":"contains","value":1}`,
		"empty in":          `{"path":"sender.type","op":"in","values":[]}`,
		"exists with value": `{"path":"sender.type","op":"exists","value":"x"}`,
	}
	for name, cond := range invalid {
		t.Run(name, func(t *testing.T) {
			var filters []WebhookEventFilter
			if err := json.Unmarshal([]byte(`[{"event":"pull_request","conditions":[`+cond+`]}]`), &filters); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := validateWebhookEventFilters(filters); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	tooMany := WebhookEventFilter{Event: "pull_request"}
	for range maxWebhookPayloadConditions + 1 {
		tooMany.Conditions = append(tooMany.Conditions, WebhookPayloadCondition{Path: "a", Op: payloadOpEquals, Value: "x"})
	}
	if err := validateWebhookEventFilters([]WebhookEventFilter{tooMany}); err == nil {
		t.Fatal("expected error above the condition cap")
	}
}

func TestSplitWebhookEvent(t *testing.T) {
	tests := []struct {
		input        string
		wantProvider string
		wantName     string
		wantAction   string
	}{
		{"github.workflow_run.completed", "github", "workflow_run", "completed"},
		{"github.push", "github", "push", ""},
		{"gitlab.Merge Request Hook", "gitlab", "Merge Request Hook", ""},
		{"webhook.received", "", "webhook", "received"},
		{"custom", "", "custom", ""},
	}
	for _, tc := range tests {
		p, n, a := splitWebhookEvent(tc.input)
		if p != tc.wantProvider || n != tc.wantName || a != tc.wantAction {
			t.Fatalf("splitWebhookEvent(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.input, p, n, a, tc.wantProvider, tc.wantName, tc.wantAction)
		}
	}
}
