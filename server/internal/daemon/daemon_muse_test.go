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

// TestProbeMuseReceptionistProtocolSkew: Codex round2 item 5.
// A version-mismatched receptionist must yield builtinProbeProtocolSkew
// (demotable), not builtinProbeUnavailable.
func TestProbeMuseReceptionistProtocolSkew(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"protocol_version": 999,
			"version":          "9.9",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("MUSE_ENDPOINT", srv.URL)
	t.Setenv("MUSE_TOKEN", "")

	d := newTestDaemon(t)
	_, _, verdict := d.probeMuseReceptionist(context.Background())
	if verdict != builtinProbeProtocolSkew {
		t.Errorf("verdict = %v, want builtinProbeProtocolSkew", verdict)
	}
}

// TestProbeMuseReceptionistOK: matching version yields builtinProbeOK.
func TestProbeMuseReceptionistOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"protocol_version": 1,
			"version":          "1.4.0",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("MUSE_ENDPOINT", srv.URL)
	t.Setenv("MUSE_TOKEN", "")

	d := newTestDaemon(t)
	d.agentVersions = make(map[string]string)
	version, _, verdict := d.probeMuseReceptionist(context.Background())
	if verdict != builtinProbeOK {
		t.Errorf("verdict = %v, want builtinProbeOK", verdict)
	}
	if version != "1.4.0" {
		t.Errorf("version = %q, want 1.4.0", version)
	}
}

// TestProbeMuseReceptionistSkewNeedsConfirmation: Codex round2 item 1.
// Protocol skew must go through confirmation (not exempt like below-minimum).
func TestProbeMuseReceptionistSkewNeedsConfirmation(t *testing.T) {
	if !builtinProbeNeedsConfirmation(builtinProbeProtocolSkew) {
		t.Error("builtinProbeProtocolSkew should need confirmation")
	}
	if builtinProbeNeedsConfirmation(builtinProbeBelowMinimum) {
		t.Error("builtinProbeBelowMinimum should NOT need confirmation")
	}
}

// Ensure the agent package is referenced (for ErrMuseProtocolSkew mapping).
var _ = agent.ErrMuseProtocolSkew

// TestProbeMuseReceptionistNoEndpoint: missing endpoint yields unavailable.
func TestProbeMuseReceptionistNoEndpoint(t *testing.T) {
	os.Unsetenv("MUSE_ENDPOINT")
	d := newTestDaemon(t)
	_, _, verdict := d.probeMuseReceptionist(context.Background())
	if verdict != builtinProbeUnavailable {
		t.Errorf("verdict = %v, want builtinProbeUnavailable", verdict)
	}
}
