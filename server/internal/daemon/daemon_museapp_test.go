package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

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
	t.Setenv("MULTICA_MUSEAPP_ENDPOINT", "")
	d := newTestDaemon(t)
	_, _, verdict := d.probeMuseAppReceptionist(context.Background())
	if verdict != builtinProbeUnavailable {
		t.Errorf("verdict = %v, want builtinProbeUnavailable", verdict)
	}
}

func TestMuseAppCleanupUnconfirmedOutranksCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/execute" {
			_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "remote-worker", "protocol_version": 1})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_MUSEAPP_ENDPOINT", srv.URL)
	t.Setenv("MULTICA_MUSEAPP_TOKEN", "")
	backend, err := agent.New("museapp", agent.Config{})
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDaemon(t)
	d.client = NewClient(srv.URL)
	d.cfg.AgentIdleWatchdog = 0
	result, _, err := d.executeAndDrain(ctx, cancelMuseAppAfterExecute{Backend: backend, cancel: cancel}, "edit the shared workdir", agent.ExecOptions{}, slog.Default(), "task", "", new(atomic.Int32))
	if err != nil || result.Status != "cleanup_unconfirmed" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if d.ShutdownCleanupConfirmed() {
		t.Fatal("daemon forgot the active remote worker")
	}
	ack := taskCancelAcknowledgement(TaskResult{FailureReason: "cleanup_unconfirmed", Comment: result.Error}, nil, "museapp")
	if ack.ProcessCleanupConfirmed == nil || *ack.ProcessCleanupConfirmed {
		t.Fatal("cancel acknowledgement discarded unconfirmed remote cleanup")
	}
}

type cancelMuseAppAfterExecute struct {
	agent.Backend
	cancel context.CancelFunc
}

func (b cancelMuseAppAfterExecute) Execute(ctx context.Context, prompt string, opts agent.ExecOptions) (*agent.Session, error) {
	session, err := b.Backend.Execute(ctx, prompt, opts)
	b.cancel()
	return session, err
}

func TestMuseAppStartRequiresServerCleanupCapability(t *testing.T) {
	for _, mode := range []bool{false, true} {
		for _, acknowledged := range []bool{false, true} {
			task := startTestClaim()
			task.StartClaimSupported = mode
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := map[string]string{}
				if acknowledged {
					response["process_cleanup_capability"] = protocol.DaemonCapabilityMuseAppProcessCleanupV1
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			_, err := NewClient(srv.URL).StartTask(context.Background(), task, protocol.DaemonCapabilityMuseAppProcessCleanupV1)
			srv.Close()
			if (err == nil) != acknowledged {
				t.Fatalf("claim=%v acknowledged=%v error=%v", mode, acknowledged, err)
			}
		}
	}
}
