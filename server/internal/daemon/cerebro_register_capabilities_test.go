package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRegisterRuntimesForWorkspace_SendsCapabilities pins MUL-4634: the tool
// inventory has to travel with the registration itself.
//
// The server persists runtimes[].capabilities on /api/daemon/register, but the
// daemon used to send that snapshot only on the HTTP heartbeat — which
// runHeartbeatTick skips for every runtime whose WebSocket heartbeat is fresh.
// On sara.local that meant 438 WS-suppressed ticks against a single HTTP tick
// after a restart, so a probe added in a new release never reached the server
// and cerebroEffectiveToolsForClaim rejected every claim with HTTP 500.
func TestRegisterRuntimesForWorkspace_SendsCapabilities(t *testing.T) {
	t.Cleanup(stubAgentVersion(t))

	type registerRuntime struct {
		Name         string         `json:"name"`
		Type         string         `json:"type"`
		Capabilities map[string]any `json:"capabilities"`
	}
	var captured []registerRuntime

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/register":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Runtimes []registerRuntime `json:"runtimes"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("decode register body: %v", err)
			}
			captured = req.Runtimes
			_ = json.NewEncoder(w).Encode(map[string]any{
				"runtimes": []map[string]any{{"id": "rt-1", "provider": "codex"}},
			})
		default:
			// Custom runtime profiles are irrelevant here; 404 is the
			// documented best-effort path.
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	d := freshDaemon(srv.URL)
	// codex carries a curated static inventory and has no probe, so the test
	// asserts the transport without shelling out to any provider CLI.
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: "codex"}}

	if _, _, err := d.registerRuntimesForWorkspace(context.Background(), "ws-1"); err != nil {
		t.Fatalf("registerRuntimesForWorkspace: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("registered runtimes = %d, want 1", len(captured))
	}
	caps := captured[0].Capabilities
	if caps == nil {
		t.Fatalf("runtime %q registered without capabilities; the server has nothing to persist", captured[0].Type)
	}
	tools, _ := caps["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("capabilities.tools is empty: %v", caps)
	}
	if _, ok := caps["discovery_method"]; !ok {
		t.Fatalf("capabilities carries no discovery_method: %v", caps)
	}
}
