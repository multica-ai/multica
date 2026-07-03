package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
)

// Repro for FIR-2441 field report: connection-tools calls to endpoints with
// >=4 path segments (e.g. /v1/credentials/oauth/status, /api/v3/secrets/raw,
// /v1/vaults/{vault_name}/services) fail while shallower endpoints work.
func TestAPIConnectionToolCallDeepPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"path":"` + r.URL.Path + `"}`))
	}))
	defer upstream.Close()

	cases := []struct {
		name string
		path string
		args map[string]any
	}{
		{"three-segments", "/v1/users/invites", map[string]any{}},
		{"four-segments-plain", "/v1/credentials/oauth/status", map[string]any{}},
		{"four-segments-pathparam", "/v1/vaults/{vault_name}/services", map[string]any{"vault_name": "bigquery"}},
		{"four-segments-query", "/api/v3/secrets/raw", map[string]any{"query": map[string]any{"workspaceId": "w", "environment": "prod", "secretPath": "/AgentVault"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := &APIConnectionTool{
				connName: "agentvault",
				baseURL:  upstream.URL,
				method:   http.MethodGet,
				path:     tc.path,
				auth:     connections.AuthConfig{BearerToken: "test-token-1234"},
			}
			out, err := tool.Call(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("Call failed: %v", err)
			}
			if !strings.Contains(out, `"ok":true`) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}
