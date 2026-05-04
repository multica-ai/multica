package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func withFastUpdateReportBackoffs(t *testing.T) {
	t.Helper()
	prev := updateReportBackoffs
	updateReportBackoffs = []time.Duration{0, 0, 0, 0}
	t.Cleanup(func() { updateReportBackoffs = prev })
}

func updateReportDaemon(t *testing.T, handler http.HandlerFunc) (*Daemon, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Daemon{
		client: NewClient(srv.URL),
		logger: slog.Default(),
	}, &calls
}

func TestReportUpdateResult_RetriesOn500AndEventuallySucceeds(t *testing.T) {
	withFastUpdateReportBackoffs(t)

	var hits int32
	d, calls := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n <= 2 {
			http.Error(w, "{}", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportUpdateResult(context.Background(), "rt-1", "upd-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
}

func TestReportUpdateResult_DoesNotRetryOn4xx(t *testing.T) {
	withFastUpdateReportBackoffs(t)

	d, calls := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"update not found"}`, http.StatusNotFound)
	})

	d.reportUpdateResult(context.Background(), "rt-1", "upd-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt (4xx is terminal), got %d", got)
	}
}

func TestReportUpdateResult_GivesUpAfterAllAttemptsFail(t *testing.T) {
	withFastUpdateReportBackoffs(t)

	d, calls := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusInternalServerError)
	})

	d.reportUpdateResult(context.Background(), "rt-1", "upd-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); int(got) != len(updateReportBackoffs) {
		t.Fatalf("expected %d attempts, got %d", len(updateReportBackoffs), got)
	}
}

func TestReportUpdateResult_AbortsOnContextCancel(t *testing.T) {
	prev := updateReportBackoffs
	updateReportBackoffs = []time.Duration{0, 200 * time.Millisecond}
	t.Cleanup(func() { updateReportBackoffs = prev })

	d, calls := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusInternalServerError)
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	d.reportUpdateResult(ctx, "rt-1", "upd-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt before cancel, got %d", got)
	}
}

func TestReportUpdateResult_SendsCorrectPath(t *testing.T) {
	withFastUpdateReportBackoffs(t)

	var path string
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportUpdateResult(context.Background(), "rt-a", "upd-a", map[string]any{"status": "completed"})

	if !strings.HasSuffix(path, "/api/daemon/runtimes/rt-a/update/upd-a/result") {
		t.Fatalf("update path = %q", path)
	}
}
