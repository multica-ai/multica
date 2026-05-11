package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newStaleRuntimeTestDaemon(t *testing.T, serverURL string, hbInterval time.Duration) *Daemon {
	t.Helper()
	cfg := Config{ServerBaseURL: serverURL, HeartbeatInterval: hbInterval}
	return New(cfg, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
}

func TestDropStaleRuntime_RemovesFromIndexAndWorkspace(t *testing.T) {
	t.Parallel()

	d := newStaleRuntimeTestDaemon(t, "", 0)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-a", "rt-b"}, "", nil, nil)
	d.runtimeIndex["rt-a"] = Runtime{ID: "rt-a", Provider: "claude"}
	d.runtimeIndex["rt-b"] = Runtime{ID: "rt-b", Provider: "codex"}

	d.dropStaleRuntime("rt-a")

	if _, ok := d.runtimeIndex["rt-a"]; ok {
		t.Fatal("rt-a still in runtimeIndex after dropStaleRuntime")
	}
	ids := d.workspaces["ws-1"].runtimeIDs
	if len(ids) != 1 || ids[0] != "rt-b" {
		t.Fatalf("workspace runtimeIDs after drop: want [rt-b], got %v", ids)
	}
}

// TestDropStaleRuntime_EmptyWorkspaceIsRemoved ensures that when the last
// runtime in a workspace gets pruned, the workspaceState itself is dropped so
// the next syncWorkspacesFromAPI tick treats it as new and re-registers
// (otherwise the "already in d.workspaces, skip register" branch would leave
// the daemon stuck with zero runtimes forever).
func TestDropStaleRuntime_EmptyWorkspaceIsRemoved(t *testing.T) {
	t.Parallel()

	d := newStaleRuntimeTestDaemon(t, "", 0)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-only"}, "", nil, nil)
	d.runtimeIndex["rt-only"] = Runtime{ID: "rt-only", Provider: "claude"}

	d.dropStaleRuntime("rt-only")

	if _, ok := d.workspaces["ws-1"]; ok {
		t.Fatal("workspace still present after its last runtime was pruned; sync would skip re-register")
	}
}

func TestDropStaleRuntime_NotifiesRuntimeSet(t *testing.T) {
	t.Parallel()

	d := newStaleRuntimeTestDaemon(t, "", 0)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-a"}, "", nil, nil)
	d.runtimeIndex["rt-a"] = Runtime{ID: "rt-a", Provider: "claude"}

	ch, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	d.dropStaleRuntime("rt-a")

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("dropStaleRuntime did not nudge runtime-set subscribers")
	}
}

func TestDropStaleRuntime_Idempotent(t *testing.T) {
	t.Parallel()

	d := newStaleRuntimeTestDaemon(t, "", 0)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-a"}, "", nil, nil)
	d.runtimeIndex["rt-a"] = Runtime{ID: "rt-a", Provider: "claude"}

	d.dropStaleRuntime("rt-a")
	// Second call against an already-gone ID must not panic or touch other state.
	d.dropStaleRuntime("rt-a")
	d.dropStaleRuntime("rt-never-existed")
}

func TestHeartbeatTick_RuntimeNotFound_PrunesAndStops(t *testing.T) {
	t.Parallel()

	var hbCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hbCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"runtime not found"}`))
	}))
	defer srv.Close()

	d := newStaleRuntimeTestDaemon(t, srv.URL, 0)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-a"}, "", nil, nil)
	d.runtimeIndex["rt-a"] = Runtime{ID: "rt-a", Provider: "claude"}

	d.runHeartbeatTick(context.Background(), "rt-a")

	if hbCalls.Load() != 1 {
		t.Fatalf("expected 1 heartbeat call, got %d", hbCalls.Load())
	}
	if _, ok := d.runtimeIndex["rt-a"]; ok {
		t.Fatal("rt-a not pruned after 404 'runtime not found'")
	}
	if _, ok := d.workspaces["ws-1"]; ok {
		t.Fatal("workspace not pruned after its sole runtime was pruned")
	}
}

// TestHeartbeatTick_GenericError_DoesNotPrune pins that the prune path is
// scoped to "runtime not found" — a generic 5xx or unrelated 404 must NOT
// silently delete state, otherwise a transient server bug would wipe a
// healthy daemon's runtime set.
func TestHeartbeatTick_GenericError_DoesNotPrune(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"upstream wobble"}`))
	}))
	defer srv.Close()

	d := newStaleRuntimeTestDaemon(t, srv.URL, 0)
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", []string{"rt-a"}, "", nil, nil)
	d.runtimeIndex["rt-a"] = Runtime{ID: "rt-a", Provider: "claude"}

	d.runHeartbeatTick(context.Background(), "rt-a")

	if _, ok := d.runtimeIndex["rt-a"]; !ok {
		t.Fatal("rt-a was pruned on a 500; only 'runtime not found' 404s should prune")
	}
	if _, ok := d.workspaces["ws-1"]; !ok {
		t.Fatal("workspace was pruned on a 500; only 'runtime not found' 404s should prune")
	}
}

func TestIsRuntimeNotFoundError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "404 with runtime not found body",
			err:  &requestError{Method: "POST", Path: "/api/daemon/heartbeat", StatusCode: 404, Body: `{"error":"runtime not found"}`},
			want: true,
		},
		{
			name: "404 with workspace not found body",
			err:  &requestError{Method: "POST", Path: "/api/daemon/heartbeat", StatusCode: 404, Body: `{"error":"workspace not found"}`},
			want: false,
		},
		{
			name: "500 with runtime not found body",
			err:  &requestError{Method: "POST", Path: "/api/daemon/heartbeat", StatusCode: 500, Body: "runtime not found"},
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "wrapped error",
			err:  &wrapErr{inner: &requestError{StatusCode: 404, Body: "runtime not found"}},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isRuntimeNotFoundError(tc.err)
			if got != tc.want {
				t.Fatalf("isRuntimeNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type wrapErr struct{ inner error }

func (w *wrapErr) Error() string { return "wrap: " + w.inner.Error() }
func (w *wrapErr) Unwrap() error { return w.inner }
