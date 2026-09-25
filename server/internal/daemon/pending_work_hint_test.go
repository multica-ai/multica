package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// pendingWorkHintDaemon builds a Daemon that knows exactly one runtime and
// talks to an httptest server, so the hint path is exercised against the real
// daemon.Client instead of a mock. The per-runtime hint floor is disabled by
// default; TestHandlePendingWorkHint_ThrottlesRepeatHints restores it.
func pendingWorkHintDaemon(t *testing.T, handler http.HandlerFunc) (*Daemon, *int32) {
	t.Helper()
	withPendingWorkHintMinInterval(t, 0)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{
		cfg:                 Config{HeartbeatInterval: time.Hour},
		client:              NewClient(srv.URL),
		logger:              slog.Default(),
		runtimeIndex:        map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		pendingWorkInflight: make(map[string]struct{}),
		pendingWorkLastRun:  make(map[string]time.Time),
	}
	return d, &calls
}

func withPendingWorkHintMinInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := pendingWorkHintMinInterval
	pendingWorkHintMinInterval = d
	t.Cleanup(func() { pendingWorkHintMinInterval = prev })
}

// TestHandlePendingWorkHint_SendsImmediateHeartbeat is the core of MUL-5444:
// a server-pushed hint must produce a heartbeat right now instead of leaving the
// queued model-list request to wait for the next scheduled tick (up to a full
// HeartbeatInterval, 15s by default).
func TestHandlePendingWorkHint_SendsImmediateHeartbeat(t *testing.T) {
	var gotPath, gotRuntime string
	d, calls := pendingWorkHintDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			RuntimeID string `json:"runtime_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotRuntime = body.RuntimeID
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"runtime_id":"rt-1","status":"ok"}`))
	})

	d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 heartbeat, got %d", got)
	}
	if gotPath != "/api/daemon/heartbeat" {
		t.Fatalf("heartbeat path = %q", gotPath)
	}
	if gotRuntime != "rt-1" {
		t.Fatalf("heartbeat runtime_id = %q, want rt-1", gotRuntime)
	}
}

// TestHandlePendingWorkHint_IgnoresUnknownRuntime keeps a stale or
// cross-machine relay fanout from making this daemon heartbeat for a runtime it
// does not own.
func TestHandlePendingWorkHint_IgnoresUnknownRuntime(t *testing.T) {
	d, calls := pendingWorkHintDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"runtime_id":"other","status":"ok"}`))
	})

	d.handlePendingWorkHint("not-mine", protocol.PendingWorkKindModelList)
	d.handlePendingWorkHint("", protocol.PendingWorkKindModelList)

	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("expected no heartbeat for an unknown runtime, got %d", got)
	}
}

// TestHandlePendingWorkHint_CoalescesConcurrentHints pins the stampede guard:
// several UI surfaces (model picker, thinking level, service tier) each request
// the catalog for the same runtime within milliseconds, and one heartbeat
// already claims whatever is queued.
func TestHandlePendingWorkHint_CoalescesConcurrentHints(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{}, 1)
	d, calls := pendingWorkHintDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"runtime_id":"rt-1","status":"ok"}`))
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)
	}()

	// Wait until the first hint is actually inside the HTTP call, then fire the
	// siblings that must be dropped.
	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		close(release)
		wg.Wait()
		t.Fatal("first hint never reached the server")
	}
	for i := 0; i < 3; i++ {
		d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected concurrent hints to coalesce into 1 heartbeat, got %d", got)
	}

	// The guard must release afterwards, otherwise the runtime would ignore
	// every later hint for the lifetime of the process.
	d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("expected a later hint to heartbeat again, got %d total", got)
	}
}

// TestHandlePendingWorkHint_ThrottlesRepeatHints pins the amplification guard:
// the hint is triggered by any workspace member hitting the list-models
// endpoint, so back-to-back requests must not turn into back-to-back heartbeats.
func TestHandlePendingWorkHint_ThrottlesRepeatHints(t *testing.T) {
	d, calls := pendingWorkHintDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"runtime_id":"rt-1","status":"ok"}`))
	})
	withPendingWorkHintMinInterval(t, time.Minute)

	for i := 0; i < 5; i++ {
		d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)
	}

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected repeat hints inside the floor to collapse into 1 heartbeat, got %d", got)
	}

	// Past the floor, a hint is served again — the throttle is a floor, not a
	// one-shot latch.
	withPendingWorkHintMinInterval(t, time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("expected a hint past the floor to heartbeat, got %d total", got)
	}
}

// TestHandlePendingWorkHint_SurvivesHeartbeatFailure documents that a failed
// hint is a no-op: the scheduled heartbeat remains the correctness path, so the
// daemon must not panic or retry-storm here.
func TestHandlePendingWorkHint_SurvivesHeartbeatFailure(t *testing.T) {
	d, calls := pendingWorkHintDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	})

	d.handlePendingWorkHint("rt-1", protocol.PendingWorkKindModelList)

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt, got %d", got)
	}
}
