package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestProbeMuseAppReceptionistProtocolSkew: Codex round2 item 5.
// A version-mismatched receptionist must yield builtinProbeProtocolSkew
// (demotable), not builtinProbeUnavailable.
func TestProbeMuseAppReceptionistProtocolSkew(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"protocol_version": 999,
			"version":          "9.9",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("MULTICA_MUSEAPP_ENDPOINT", srv.URL)
	t.Setenv("MULTICA_MUSEAPP_TOKEN", "")

	d := newTestDaemon(t)
	_, _, verdict := d.probeMuseAppReceptionist(context.Background())
	if verdict != builtinProbeProtocolSkew {
		t.Errorf("verdict = %v, want builtinProbeProtocolSkew", verdict)
	}
}

// TestProbeMuseAppReceptionistOK: matching version yields builtinProbeOK.
func TestProbeMuseAppReceptionistOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"protocol_version": 1,
			"version":          "1.4.0",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("MULTICA_MUSEAPP_ENDPOINT", srv.URL)
	t.Setenv("MULTICA_MUSEAPP_TOKEN", "")

	d := newTestDaemon(t)
	d.agentVersions = make(map[string]string)
	version, _, verdict := d.probeMuseAppReceptionist(context.Background())
	if verdict != builtinProbeOK {
		t.Errorf("verdict = %v, want builtinProbeOK", verdict)
	}
	if version != "1.4.0" {
		t.Errorf("version = %q, want 1.4.0", version)
	}
}

// TestProbeMuseAppReceptionistSkewNeedsConfirmation: Codex round2 item 1.
// Protocol skew must go through confirmation (not exempt like below-minimum).
func TestProbeMuseAppReceptionistSkewNeedsConfirmation(t *testing.T) {
	if !builtinProbeNeedsConfirmation(builtinProbeProtocolSkew) {
		t.Error("builtinProbeProtocolSkew should need confirmation")
	}
	if builtinProbeNeedsConfirmation(builtinProbeBelowMinimum) {
		t.Error("builtinProbeBelowMinimum should NOT need confirmation")
	}
}

// Ensure the agent package is referenced (for ErrMuseAppProtocolSkew mapping).
var _ = agent.ErrMuseAppProtocolSkew

// TestProbeMuseAppReceptionistNoEndpoint: missing endpoint yields unavailable.
func TestProbeMuseAppReceptionistNoEndpoint(t *testing.T) {
	os.Unsetenv("MULTICA_MUSEAPP_ENDPOINT")
	d := newTestDaemon(t)
	_, _, verdict := d.probeMuseAppReceptionist(context.Background())
	if verdict != builtinProbeUnavailable {
		t.Errorf("verdict = %v, want builtinProbeUnavailable", verdict)
	}
}
