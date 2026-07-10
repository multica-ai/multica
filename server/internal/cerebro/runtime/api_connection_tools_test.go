package runtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
)

func TestAPIToolName(t *testing.T) {
	cases := []struct {
		conn, method, path, want string
	}{
		{"infisical-admin", "GET", "/secrets", "infisical_admin__get_secrets"},
		{"Infisical Admin", "post", "/secrets/{id}", "infisical_admin__post_secrets_id"},
		{"orders-api", "DELETE", "/orders/{orderId}/lines", "orders_api__delete_orders_orderid_lines"},
		{"", "GET", "/x", ""},
	}
	for _, c := range cases {
		if got := apiToolName(c.conn, c.method, c.path); got != c.want {
			t.Errorf("apiToolName(%q,%q,%q)=%q want %q", c.conn, c.method, c.path, got, c.want)
		}
	}
}

func TestAPIToolSchema(t *testing.T) {
	// GET with a path param: required path param + query object, no body.
	schema := apiToolSchema("GET", "/secrets/{id}")
	props := schema["properties"].(map[string]any)
	if _, ok := props["id"]; !ok {
		t.Fatalf("expected path param 'id' property, got %v", props)
	}
	if _, ok := props["query"]; !ok {
		t.Fatalf("expected 'query' property")
	}
	if _, ok := props["body"]; ok {
		t.Fatalf("GET must not expose a body property")
	}
	req, _ := schema["required"].([]string)
	if len(req) != 1 || req[0] != "id" {
		t.Fatalf("expected required=[id], got %v", req)
	}

	// POST: gains a body property.
	post := apiToolSchema("POST", "/secrets")
	pprops := post["properties"].(map[string]any)
	if _, ok := pprops["body"]; !ok {
		t.Fatalf("POST must expose a body property")
	}
	if _, ok := post["required"]; ok {
		t.Fatalf("POST /secrets has no path params, must have no required list")
	}
}

func TestBuildAPIConnectionTools(t *testing.T) {
	conns := []connections.Connection{
		{
			Name: "infisical-admin", DisplayName: "Infisical (admin)", Type: connections.TypeAPI,
			URL: "http://backend.internal:8080/", Enabled: true,
			EndpointPermissions: []connections.EndpointPermission{
				{Path: "/secrets", Methods: []string{"GET", "POST"}},
				{Path: "/status", Methods: []string{"GET"}},
			},
		},
		// MCP connection: must be ignored.
		{Name: "cs", Type: connections.TypeMCPHTTP, URL: "http://cs.internal", Enabled: true,
			EndpointPermissions: []connections.EndpointPermission{{Path: "/x", Methods: []string{"GET"}}}},
		// Disabled API connection: ignored.
		{Name: "off", Type: connections.TypeAPI, URL: "http://off.internal", Enabled: false,
			EndpointPermissions: []connections.EndpointPermission{{Path: "/x", Methods: []string{"GET"}}}},
		// API connection with no URL: ignored.
		{Name: "nourl", Type: connections.TypeAPI, URL: "", Enabled: true,
			EndpointPermissions: []connections.EndpointPermission{{Path: "/x", Methods: []string{"GET"}}}},
	}
	tools := buildAPIConnectionTools(conns, nil, nil)
	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.Name()] = true
	}
	want := []string{
		"infisical_admin__get_secrets",
		"infisical_admin__post_secrets",
		"infisical_admin__get_status",
	}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools %v, want %d", len(tools), keys(got), len(want))
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing tool %q (got %v)", w, keys(got))
		}
	}
	// Base URL must be trailing-slash trimmed on the built tool.
	for _, tl := range tools {
		at := tl.(*APIConnectionTool)
		if strings.HasSuffix(at.baseURL, "/") {
			t.Errorf("baseURL not trimmed: %q", at.baseURL)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFillPathParams(t *testing.T) {
	got, err := fillPathParams("/secrets/{id}/versions/{v}", map[string]any{"id": "abc", "v": 3})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "/secrets/abc/versions/3" {
		t.Fatalf("got %q", got)
	}
	// Missing param fails closed.
	if _, err := fillPathParams("/secrets/{id}", map[string]any{}); err == nil {
		t.Fatalf("expected error for missing path param")
	}
	// Value is path-escaped so it can't break out of the segment.
	esc, err := fillPathParams("/p/{x}", map[string]any{"x": "a/b"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Contains(strings.TrimPrefix(esc, "/p/"), "/") {
		t.Fatalf("path param not escaped: %q", esc)
	}
}

func TestApplyConnectionAuth(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://x", nil)
	applyConnectionAuth(req, connections.AuthConfig{BearerToken: "tok"})
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("bearer not applied: %q", req.Header.Get("Authorization"))
	}

	req2, _ := http.NewRequest("GET", "http://x", nil)
	applyConnectionAuth(req2, connections.AuthConfig{APIKey: "k"})
	if req2.Header.Get("X-API-Key") != "k" {
		t.Errorf("default api-key header not applied: %q", req2.Header.Get("X-API-Key"))
	}

	req3, _ := http.NewRequest("GET", "http://x", nil)
	applyConnectionAuth(req3, connections.AuthConfig{APIKey: "k", APIKeyHeader: "X-Custom"})
	if req3.Header.Get("X-Custom") != "k" {
		t.Errorf("custom api-key header not applied")
	}

	req4, _ := http.NewRequest("GET", "http://x", nil)
	applyConnectionAuth(req4, connections.AuthConfig{CFAccessID: "id", CFAccessSecret: "sec"})
	if req4.Header.Get("CF-Access-Client-Id") != "id" || req4.Header.Get("CF-Access-Client-Secret") != "sec" {
		t.Errorf("cf access headers not applied")
	}
}

// TestAPIConnectionToolCall exercises the full server-side dispatch against a
// local test server: path-param substitution, query string, JSON body, auth
// header, and response passthrough — no DB required.
func TestAPIConnectionToolCall(t *testing.T) {
	var gotAuth, gotMethod, gotPath, gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("limit")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	tool := &APIConnectionTool{
		toolName: "t", connName: "c", method: "POST", path: "/secrets/{id}",
		baseURL: srv.URL, auth: connections.AuthConfig{BearerToken: "tok"},
		client: srv.Client(),
	}
	out, err := tool.Call(context.Background(), map[string]any{
		"id":    "abc",
		"query": map[string]any{"limit": 5},
		"body":  map[string]any{"value": "x"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotMethod != "POST" || gotPath != "/secrets/abc" {
		t.Errorf("method/path = %s %s", gotMethod, gotPath)
	}
	if gotQuery != "5" {
		t.Errorf("query limit = %q", gotQuery)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil || body["value"] != "x" {
		t.Errorf("body = %q", gotBody)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("response passthrough = %q", out)
	}
}

// TestAPIConnectionToolCallNon2xx confirms a non-2xx response fails the call with
// the status + body surfaced (so the agent sees the upstream error, not silence).
func TestAPIConnectionToolCallNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no token")
	}))
	defer srv.Close()
	tool := &APIConnectionTool{
		toolName: "t", connName: "c", method: "GET", path: "/secrets",
		baseURL: srv.URL, client: srv.Client(),
	}
	_, err := tool.Call(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

// TestAuthScheme confirms the gateway-trace scheme label names the auth scheme(s)
// without ever exposing a credential value, and reports "none" when unauthenticated.
func TestAuthScheme(t *testing.T) {
	cases := []struct {
		name string
		auth connections.AuthConfig
		want string
	}{
		{"none", connections.AuthConfig{}, "none"},
		{"bearer", connections.AuthConfig{BearerToken: "tok"}, "bearer"},
		{"api_key", connections.AuthConfig{APIKey: "k"}, "api_key"},
		{"cf", connections.AuthConfig{CFAccessID: "id", CFAccessSecret: "sec"}, "cf_access"},
		{"combo", connections.AuthConfig{BearerToken: "t", APIKey: "k", CFAccessID: "id"}, "bearer+api_key+cf_access"},
	}
	for _, c := range cases {
		if got := authScheme(c.auth); got != c.want {
			t.Errorf("authScheme(%s)=%q want %q", c.name, got, c.want)
		}
	}
}

// TestAPIConnectionToolGatewayTrace confirms a dispatched call emits one
// structured gateway-trace line carrying the connection/tool, target, credential
// identity, permission decision, HTTP outcome, and run identity — and that the
// credential VALUE never appears in the log. (FIR-2243 B2.)
func TestAPIConnectionToolGatewayTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	rec := &captureHandler{}
	tool := &APIConnectionTool{
		toolName: "infisical_admin__get_secrets", connName: "infisical-admin",
		connID: "conn-123",
		method: "GET", path: "/secrets",
		baseURL: srv.URL, auth: connections.AuthConfig{BearerToken: "super-secret-token-value"},
		client: srv.Client(),
	}
	tool.attachTrace(slog.New(rec), GatewayRequestMeta{
		AgentID: "agent-1", AgentName: "Mia", TaskID: "task-9", IssueID: "issue-7", Surface: "issue",
	}, "allow")

	if _, err := tool.Call(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(rec.records) != 1 {
		t.Fatalf("expected exactly 1 trace line, got %d", len(rec.records))
	}
	attrs := rec.records[0]
	want := map[string]string{
		"event":               "api_connection_call",
		"connection":          "infisical-admin",
		"tool":                "infisical_admin__get_secrets",
		"method":              "GET",
		"path":                "/secrets",
		"credential_id":       "conn-123",
		"credential_name":     "infisical-admin",
		"credential_scheme":   "bearer",
		"permission_decision": "allow",
		"agent_id":            "agent-1",
		"task_id":             "task-9",
		"issue_id":            "issue-7",
	}
	for k, v := range want {
		if got := attrs[k]; got != v {
			t.Errorf("trace attr %q = %q, want %q", k, got, v)
		}
	}
	if attrs["http_status"] != "200" || attrs["ok"] != "true" {
		t.Errorf("expected http_status=200 ok=true, got status=%q ok=%q", attrs["http_status"], attrs["ok"])
	}
	for k, v := range attrs {
		if strings.Contains(v, "super-secret-token-value") {
			t.Fatalf("credential value leaked into trace attr %q=%q", k, v)
		}
	}
}

// captureHandler is a minimal slog.Handler that records each log record's
// attributes (flattened to string) for assertion in tests.
type captureHandler struct {
	records []map[string]string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	h.records = append(h.records, m)
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// redactCredentials must strip every non-empty secret half from a response body
// so an endpoint that echoes its own auth cannot leak the credential to the
// agent. Short/empty config values are left untouched. (FIR-2166 C review fix.)
func TestRedactCredentials(t *testing.T) {
	auth := connections.AuthConfig{
		BearerToken:    "super-secret-bearer-token-1234",
		APIKey:         "ak_live_abcdef123456",
		CFAccessID:     "cf-access-client-id-value",
		CFAccessSecret: "cf-access-client-secret-value",
	}
	body := `{"echoed_bearer":"super-secret-bearer-token-1234",` +
		`"echoed_key":"ak_live_abcdef123456",` +
		`"cf_id":"cf-access-client-id-value",` +
		`"cf_secret":"cf-access-client-secret-value",` +
		`"data":"ok"}`

	got := redactCredentials(body, auth)
	for _, leak := range []string{auth.BearerToken, auth.APIKey, auth.CFAccessID, auth.CFAccessSecret} {
		if strings.Contains(got, leak) {
			t.Fatalf("credential leaked through redaction: %q still present in %q", leak, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder, got %q", got)
	}
	if !strings.Contains(got, `"data":"ok"`) {
		t.Fatalf("non-secret payload must survive redaction, got %q", got)
	}
}

// A short or empty secret config must not turn the whole body into placeholders.
func TestRedactCredentialsSkipsShortAndEmpty(t *testing.T) {
	auth := connections.AuthConfig{BearerToken: "ab", APIKey: ""}
	body := `{"data":"abc"}`
	if got := redactCredentials(body, auth); got != body {
		t.Fatalf("short/empty secrets must leave body unchanged, got %q", got)
	}
}

// FIR-2441: the cloud gateway appends APIConnectionArgHint to the system prompt
// only when an api-connection tool is actually offered — detected via the
// registry, not a name pattern — so the cloud and local first prompts agree.
func TestRegistryHasAPIConnectionTool(t *testing.T) {
	if registryHasAPIConnectionTool(nil, []string{"anything"}) {
		t.Fatal("nil registry must report no api-connection tool")
	}

	reg := NewRegistry(nil)
	reg.Register(&APIConnectionTool{toolName: "infisical_admin__get_secrets", connName: "infisical-admin", method: "GET", path: "/secrets"})

	if !registryHasAPIConnectionTool(reg, []string{"infisical_admin__get_secrets"}) {
		t.Error("expected api-connection tool to be detected when it is registered and offered")
	}
	if registryHasAPIConnectionTool(reg, []string{"schedule_wakeup"}) {
		t.Error("did not expect detection for a name that is not a registered api-connection tool")
	}

	// The shared hint must state the query-object rule the whole feature exists to
	// teach, so cloud and local first prompts both prevent the top-level 502.
	if !strings.Contains(APIConnectionArgHint, "`query` object") {
		t.Errorf("APIConnectionArgHint must state the query-object rule; got %q", APIConnectionArgHint)
	}
}

// FIR-2441 fix-list #5: the compat tool loop has only the resolved []Tool (no
// Registry handle), so it keys on the concrete type to decide whether to append
// the connection guidance.
func TestToolsHaveAPIConnectionTool(t *testing.T) {
	if toolsHaveAPIConnectionTool(nil) {
		t.Fatal("empty tool slice must report no api-connection tool")
	}
	api := &APIConnectionTool{toolName: "infisical_admin__get_secrets", connName: "infisical-admin", method: "GET", path: "/secrets"}
	if !toolsHaveAPIConnectionTool([]Tool{api}) {
		t.Error("expected api-connection tool to be detected in the slice")
	}
}

// FIR-2441 fix-list #5: ConnectionGuidance is the full first-prompt guidance and
// MUST embed the argument-shape rule so cloud and local first prompts teach the
// same `query`-object contract from one source of truth.
func TestConnectionGuidanceEmbedsArgHint(t *testing.T) {
	if !strings.Contains(ConnectionGuidance, APIConnectionArgHint) {
		t.Error("ConnectionGuidance must embed APIConnectionArgHint so the two surfaces never drift")
	}
	if !strings.Contains(ConnectionGuidance, "connection") {
		t.Errorf("ConnectionGuidance must explain what a connection is; got %q", ConnectionGuidance)
	}
}

// FIR-2441 fix-list #5: the LIVE cloud compat tool loop must render the full
// connection guidance in the system prompt when an api-connection tool is
// offered — before this it shipped only a bare tool-name list, so a cloud agent
// never saw the `query`-object rule the local claim brief already teaches.
func TestWithRegistryToolUsageHintIncludesConnectionGuidance(t *testing.T) {
	sys := GatewayMessage{Role: "system", Content: "You are an agent."}
	api := &APIConnectionTool{toolName: "infisical_admin__get_secrets", connName: "infisical-admin", method: "GET", path: "/secrets"}

	out := withRegistryToolUsageHint(context.Background(), []GatewayMessage{sys}, []Tool{api})
	if len(out) == 0 || out[0].Role != "system" {
		t.Fatal("expected the system message to be preserved")
	}
	if !strings.Contains(out[0].Content, ConnectionGuidance) {
		t.Errorf("compat loop system prompt must include ConnectionGuidance when an api-connection tool is offered; got %q", out[0].Content)
	}
}

func TestAPIToolDescriptionIncludesSummary(t *testing.T) {
	conns := []connections.Connection{
		{
			Name: "registry", DisplayName: "Firtal Data Registry", Type: connections.TypeAPI,
			URL: "http://registry.internal:3000/api/registry/v1", Enabled: true,
			EndpointPermissions: []connections.EndpointPermission{
				{Path: "/data-sources/9be2/execute", Methods: []string{"POST"}, Summary: "Execute data source: Orders"},
				{Path: "/manifest", Methods: []string{"GET"}},
			},
		},
	}
	tools := buildAPIConnectionTools(conns, nil, nil)
	byName := map[string]string{}
	for _, tl := range tools {
		byName[tl.Name()] = tl.Description()
	}
	labeled := byName["registry__post_data_sources_9be2_execute"]
	if !strings.HasPrefix(labeled, "Execute data source: Orders. ") {
		t.Errorf("expected summary to lead the description, got %q", labeled)
	}
	if !strings.Contains(labeled, "POST /data-sources/9be2/execute") {
		t.Errorf("expected method+path retained in description, got %q", labeled)
	}
	plain := byName["registry__get_manifest"]
	if !strings.HasPrefix(plain, "Call the Firtal Data Registry API: GET /manifest") {
		t.Errorf("unexpected unlabeled description %q", plain)
	}
}

// FIR-2668: an on_behalf_of-enabled connection stamps the calling agent as the
// X-On-Behalf-Of delegation header, and an agent-delegated call never rides the
// triggering human's session exchange (the shared key + header is the whole
// contract). No agent on the context → no header, unchanged dispatch.
func TestAPIConnectionToolCallOnBehalfOfAgent(t *testing.T) {
	var gotHeader, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-On-Behalf-Of")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	tool := &APIConnectionTool{
		toolName: "t", connName: "c", method: "GET", path: "/rows",
		baseURL: srv.URL,
		auth: connections.AuthConfig{
			BearerToken: "shared",
			OnBehalfOf:  &connections.OnBehalfOfConfig{Enabled: true},
			// Session exchange enabled with NO exchanger wired: if the agent
			// delegation below did not take precedence, the triggering-member
			// path would fail closed and the call would error.
			SessionExchange: &connections.SessionExchangeConfig{Enabled: true},
		},
		client: srv.Client(),
	}

	ctx := WithConnectionAgent(context.Background(), "0ec120c8-d899-408f-ac1b-143f888bdc57")
	ctx = WithConnectionTriggerMember(ctx, "d7a6fa72-e68d-48ca-86be-2ab4313ecf44")
	if _, err := tool.Call(ctx, nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotHeader != "agent:0ec120c8-d899-408f-ac1b-143f888bdc57" {
		t.Errorf("X-On-Behalf-Of = %q", gotHeader)
	}
	if gotAuth != "Bearer shared" {
		t.Errorf("agent delegation must dispatch on the shared key, got auth %q", gotAuth)
	}
}

func TestAPIConnectionToolCallOnBehalfOfNoAgent(t *testing.T) {
	var gotHeader string
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-On-Behalf-Of")
		_, sawHeader = r.Header["X-On-Behalf-Of"]
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	tool := &APIConnectionTool{
		toolName: "t", connName: "c", method: "GET", path: "/rows",
		baseURL: srv.URL,
		auth: connections.AuthConfig{
			BearerToken: "shared",
			OnBehalfOf:  &connections.OnBehalfOfConfig{Enabled: true},
		},
		client: srv.Client(),
	}
	// No agent on the context (system/human surface): header must be absent.
	if _, err := tool.Call(context.Background(), nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if sawHeader {
		t.Errorf("X-On-Behalf-Of must not be sent without an agent caller, got %q", gotHeader)
	}
}

func TestAPIConnectionToolCallOnBehalfOfDisabled(t *testing.T) {
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawHeader = r.Header["X-On-Behalf-Of"]
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	tool := &APIConnectionTool{
		toolName: "t", connName: "c", method: "GET", path: "/rows",
		baseURL: srv.URL,
		auth:    connections.AuthConfig{BearerToken: "shared"},
		client:  srv.Client(),
	}
	ctx := WithConnectionAgent(context.Background(), "0ec120c8-d899-408f-ac1b-143f888bdc57")
	if _, err := tool.Call(ctx, nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if sawHeader {
		t.Error("X-On-Behalf-Of must not be sent when on_behalf_of is not enabled")
	}
}
