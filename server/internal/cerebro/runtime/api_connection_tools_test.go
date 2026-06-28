package runtime

import (
	"context"
	"encoding/json"
	"io"
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
	tools := buildAPIConnectionTools(conns, nil)
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
