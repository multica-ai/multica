package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestServerCompatibilityGate covers version mismatch at startup: a server that
// reports a version below MinServerVersion fails loudly; a compatible or
// unreported version does not.
func TestServerCompatibilityGate(t *testing.T) {
	newDaemon := func() *Daemon {
		return &Daemon{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			cfg:    Config{CLIVersion: "0.4.2"},
		}
	}

	t.Run("too old fails loudly", func(t *testing.T) {
		d := newDaemon()
		d.recordServerVersion("0.3.22")
		err := d.checkServerCompatibility()
		if err == nil {
			t.Fatal("a 0.3.22 server must fail the compatibility gate")
		}
		for _, want := range []string{"incompatible", "0.3.22"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error missing %q: %v", want, err)
			}
		}
	})

	t.Run("compatible passes", func(t *testing.T) {
		d := newDaemon()
		d.recordServerVersion("0.4.2")
		if err := d.checkServerCompatibility(); err != nil {
			t.Fatalf("a matching server must pass: %v", err)
		}
	})

	t.Run("unreported version warns but proceeds", func(t *testing.T) {
		d := newDaemon()
		// No recordServerVersion call: server never reported a version.
		if err := d.checkServerCompatibility(); err != nil {
			t.Fatalf("an unreported server version must not fail startup: %v", err)
		}
	})

	t.Run("empty does not clobber a known version", func(t *testing.T) {
		d := newDaemon()
		d.recordServerVersion("0.4.2")
		d.recordServerVersion("") // e.g. a later response from an older backend
		if got := d.serverVersionSeen(); got != "0.4.2" {
			t.Fatalf("empty must not clobber a known version, got %q", got)
		}
	})
}

// TestPrepareLeaseExtender_UnsupportedRouteStopsLoudly covers the version-skew
// backstop: when /prepare-lease 404s because the route is missing on an
// un-upgraded server, the extender logs once and STOPS instead of Warn-looping
// forever.
func TestPrepareLeaseExtender_UnsupportedRouteStopsLoudly(t *testing.T) {
	oldRefresh := taskPrepareLeaseRefresh
	oldTimeout := taskPrepareLeaseTimeout
	taskPrepareLeaseRefresh = 10 * time.Millisecond
	taskPrepareLeaseTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		taskPrepareLeaseRefresh = oldRefresh
		taskPrepareLeaseTimeout = oldTimeout
	})

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prepare-lease") {
			calls.Add(1)
			// Route missing on an old server: a bare 404, NOT "task not found".
			http.Error(w, "404 page not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client: NewClient(srv.URL),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	task := Task{ID: "task-1", RuntimeID: "rt-1"}
	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))

	stop := d.startTaskPrepareLeaseExtender(context.Background(), task, taskLog)
	t.Cleanup(stop)

	// Let many refresh intervals elapse. If the loop stopped after the first
	// 404 (correct), the server sees exactly one call; if it kept looping (the
	// bug), it would see ~30.
	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("prepare-lease extender should stop after the first unsupported-route 404; got %d calls", got)
	}
}

// TestPrepareLeaseExtender_Handler404KeepsRetrying guards the classifier's
// positive matching: a HANDLER-level 404 (JSON {"error": ...} body — a deleted
// row, or a transient DB error the server's access checks map to "not found")
// must NOT be mistaken for a missing route. Stopping the extender on one such
// blip would lapse the lease mid-task and let the server re-dispatch a second
// execution — the duplicate-writer class this change exists to prevent.
func TestPrepareLeaseExtender_Handler404KeepsRetrying(t *testing.T) {
	oldRefresh := taskPrepareLeaseRefresh
	oldTimeout := taskPrepareLeaseTimeout
	taskPrepareLeaseRefresh = 10 * time.Millisecond
	taskPrepareLeaseTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		taskPrepareLeaseRefresh = oldRefresh
		taskPrepareLeaseTimeout = oldTimeout
	})

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prepare-lease") {
			calls.Add(1)
			// Handler-shaped 404: what writeError produces for a gone row or a
			// transient access-check failure. NOT chi's plain catch-all.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client: NewClient(srv.URL),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	stop := d.startTaskPrepareLeaseExtender(context.Background(), Task{ID: "task-1", RuntimeID: "rt-1"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(stop)

	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got < 2 {
		t.Fatalf("a handler-level 404 must keep the loop retrying; got only %d calls", got)
	}
}

// TestPrepareLeaseExtender_TransientErrorKeepsRetrying guards that the backstop
// only fires on a missing route: an ordinary transient failure must NOT stop
// the loop.
func TestPrepareLeaseExtender_TransientErrorKeepsRetrying(t *testing.T) {
	oldRefresh := taskPrepareLeaseRefresh
	oldTimeout := taskPrepareLeaseTimeout
	taskPrepareLeaseRefresh = 10 * time.Millisecond
	taskPrepareLeaseTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		taskPrepareLeaseRefresh = oldRefresh
		taskPrepareLeaseTimeout = oldTimeout
	})

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prepare-lease") {
			calls.Add(1)
			http.Error(w, "boom", http.StatusInternalServerError) // transient 500
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client: NewClient(srv.URL),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	stop := d.startTaskPrepareLeaseExtender(context.Background(), Task{ID: "task-1", RuntimeID: "rt-1"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(stop)

	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got < 2 {
		t.Fatalf("a transient error must keep the loop retrying; got only %d calls", got)
	}
}
