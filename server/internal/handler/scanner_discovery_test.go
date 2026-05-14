// CEREBRO-PATCH(scanner-discovery-test): persona integration changes.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
)

// TestScannerDiscovery_TokenGated locks in the auth contract for the
// scanner-discovery endpoint: no token = 401, wrong token = 401, correct
// token = 200, and when the env var is empty (token disabled) the endpoint
// returns 503 so the operator can't accidentally expose runtime topology.
func TestScannerDiscovery_TokenGated(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Build a fresh handler with the token configured so we don't mutate
	// the shared testHandler's config.
	queries := testHandler.Queries
	pool := testHandler.TxStarter
	hub := realtime.NewHub()
	bus := events.New()
	emailSvc := service.NewEmailService()
	cfg := Config{ScannerDiscoveryToken: "secret-scanner-token"}
	gatedHandler := New(queries, pool, hub, bus, emailSvc, nil, nil, nil, nil, cfg)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no auth header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized},
		{"non-bearer scheme", "Basic c2VjcmV0LXNjYW5uZXItdG9rZW4=", http.StatusUnauthorized},
		{"correct token", "Bearer secret-scanner-token", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/scanner-discovery/runtimes", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			gatedHandler.GetScannerDiscoveryRuntimes(w, req)
			if w.Code != tc.want {
				t.Fatalf("status: got %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}

	// Disabled when the token is empty: must 503 even with a valid-looking
	// Authorization header so an operator who hasn't configured the token
	// can't accidentally expose runtime topology.
	disabledCfg := Config{ScannerDiscoveryToken: ""}
	disabledHandler := New(queries, pool, hub, bus, emailSvc, nil, nil, nil, nil, disabledCfg)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/scanner-discovery/runtimes", nil)
	req.Header.Set("Authorization", "Bearer anything")
	disabledHandler.GetScannerDiscoveryRuntimes(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled endpoint: got %d, want 503", w.Code)
	}
}

// TestScannerDiscovery_ReturnsRuntimes verifies that the endpoint returns
// the cross-workspace runtime list with name, provider, and capabilities
// (tools + mcp_servers) extracted from the JSONB column. Persona's scanner
// keys off this exact shape.
func TestScannerDiscovery_ReturnsRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Seed a runtime with capabilities so we can assert the shape.
	caps := map[string]any{
		"providers":   []string{"claude"},
		"tools":       []string{"Bash", "Read", "Write"},
		"mcp_servers": []string{},
	}
	capsJSON, _ := json.Marshal(caps)
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, capabilities, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'claude', 'online', $3, '{}'::jsonb, $4::jsonb, now())
		RETURNING id
	`, testWorkspaceID, "scanner-disc-rt", "scanner discovery test", capsJSON).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)

	queries := testHandler.Queries
	pool := testHandler.TxStarter
	hub := realtime.NewHub()
	bus := events.New()
	emailSvc := service.NewEmailService()
	cfg := Config{ScannerDiscoveryToken: "secret-scanner-token"}
	gatedHandler := New(queries, pool, hub, bus, emailSvc, nil, nil, nil, nil, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/scanner-discovery/runtimes", nil)
	req.Header.Set("Authorization", "Bearer secret-scanner-token")
	gatedHandler.GetScannerDiscoveryRuntimes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		Runtimes []ScannerDiscoveryRuntime `json:"runtimes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var got *ScannerDiscoveryRuntime
	for i, rt := range resp.Runtimes {
		if rt.Name == "scanner-disc-rt" {
			got = &resp.Runtimes[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("seeded runtime not in response: %+v", resp.Runtimes)
	}
	if got.Provider != "claude" {
		t.Errorf("provider: got %q, want claude", got.Provider)
	}
	if len(got.Tools) != 3 || got.Tools[0] != "Bash" {
		t.Errorf("tools: got %v, want [Bash Read Write]", got.Tools)
	}
	// mcp_servers must be a JSON array (not null/missing) since persona's UI iterates.
	if got.MCPServers == nil {
		t.Errorf("mcp_servers: got nil, want []")
	}
}
