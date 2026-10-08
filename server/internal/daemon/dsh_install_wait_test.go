package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// deregisterRecorder captures the offline reasons a daemon sends, keyed by
// runtime id, across every deregister call.
type deregisterRecorder struct {
	mu      sync.Mutex
	calls   int
	reasons map[string]RuntimeOfflineReason
}

func newDeregisterRecorder(t *testing.T) (*deregisterRecorder, *Client) {
	t.Helper()
	rec := &deregisterRecorder{reasons: map[string]RuntimeOfflineReason{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RuntimeIDs     []string                        `json:"runtime_ids"`
			OfflineReasons map[string]RuntimeOfflineReason `json:"offline_reasons"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode deregister body: %v", err)
		}
		rec.mu.Lock()
		rec.calls++
		for id, reason := range body.OfflineReasons {
			rec.reasons[id] = reason
		}
		rec.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return rec, NewClient(srv.URL)
}

func (r *deregisterRecorder) reasonFor(id string) (RuntimeOfflineReason, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reason, ok := r.reasons[id]
	return reason, ok
}

// `installing` is a promise that this offline runtime comes back by itself, and
// the server queues assignments and @mentions behind it instead of refusing
// them (MUL-6164). The row carrying it is written exactly once, by the demotion
// — which also drops the runtime from the index, so no later round has a
// runtime to condemn and no later deregistration is sent.
//
// So an install that fails after that point has to take the promise back
// itself. Left alone, every trigger for that agent queues forever behind an
// install that already gave up, which is precisely the failure the structured
// reason exists to prevent.
func TestWithdrawDshInstallWait_FailedInstallTakesTheWaitBack(t *testing.T) {
	rec, client := newDeregisterRecorder(t)
	d := &Daemon{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		client:       client,
		workspaces:   map[string]*workspaceState{"ws-1": {}},
		runtimeIndex: map[string]Runtime{},
	}
	d.dshInstallWaits = []dshInstallWait{{
		workspaceID: "ws-1",
		runtimeID:   "rt-dsh",
		reason: RuntimeOfflineReason{
			Code:       RuntimeOfflineCodeDshProfile,
			Detail:     dshProfileInstallStartedReason,
			Installing: true,
		},
	}}

	d.withdrawDshInstallWait(context.Background())

	got, ok := rec.reasonFor("rt-dsh")
	if !ok {
		t.Fatal("no corrected reason was sent; the runtime row still claims an install is running")
	}
	if got.Installing {
		t.Error("the corrected reason still says installing, so the server keeps queueing")
	}
	if got.Code != RuntimeOfflineCodeDshProfile {
		t.Errorf("code = %q, want it unchanged at %q", got.Code, RuntimeOfflineCodeDshProfile)
	}
	if got.Detail == dshProfileInstallStartedReason || got.Detail == "" {
		t.Errorf("detail = %q, want it to say the install ended", got.Detail)
	}

	// Drained, not replayed: a second call must not re-send a correction the
	// server already has, and must not undo a runtime that has since recovered.
	d.withdrawDshInstallWait(context.Background())
	rec.mu.Lock()
	calls := rec.calls
	rec.mu.Unlock()
	if calls != 1 {
		t.Errorf("deregister calls = %d, want 1", calls)
	}
}

// A runtime the daemon tracks again has been re-registered since the demotion,
// and its row already says online. Correcting it would knock a recovered
// runtime back offline on the strength of an older decision — the same
// re-check every other deregistration path makes.
func TestWithdrawDshInstallWait_SkipsARuntimeThatCameBack(t *testing.T) {
	rec, client := newDeregisterRecorder(t)
	d := &Daemon{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		client:       client,
		workspaces:   map[string]*workspaceState{"ws-1": {runtimeIDs: []string{"rt-dsh"}}},
		runtimeIndex: map[string]Runtime{"rt-dsh": {ID: "rt-dsh", Provider: "dsh"}},
	}
	d.dshInstallWaits = []dshInstallWait{{
		workspaceID: "ws-1",
		runtimeID:   "rt-dsh",
		reason:      RuntimeOfflineReason{Code: RuntimeOfflineCodeDshProfile, Installing: true},
	}}

	d.withdrawDshInstallWait(context.Background())

	rec.mu.Lock()
	calls := rec.calls
	rec.mu.Unlock()
	if calls != 0 {
		t.Fatalf("deregister calls = %d, want 0: the runtime is tracked again and its row says online", calls)
	}
}
