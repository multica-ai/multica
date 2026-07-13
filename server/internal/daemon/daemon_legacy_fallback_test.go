package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestClaimTasksWSFirst_LegacyFallbackWhenBatchRouteMissing pins the MUL-4257
// backward-compat fix: against a server that has no /api/daemon/tasks/claim
// route (returns 404), the daemon falls back to the legacy per-runtime
// POST /api/daemon/runtimes/{id}/tasks/claim loop, and remembers it so later
// polls skip the batch attempt.
func TestClaimTasksWSFirst_LegacyFallbackWhenBatchRouteMissing(t *testing.T) {
	var batchCalls, legacyCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/api/daemon/tasks/claim"):
			batchCalls.Add(1)
			http.Error(w, "404 page not found", http.StatusNotFound)
		case strings.HasSuffix(path, "/runtimes/rt1/tasks/claim"):
			legacyCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":{"id":"t1","runtime_id":"rt1","agent":{"name":"a"}}}`))
		case strings.HasSuffix(path, "/runtimes/rt2/tasks/claim"):
			legacyCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":{"id":"t2","runtime_id":"rt2","agent":{"name":"b"}}}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	tasks, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1", "rt2"}, 5)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("legacy fallback claimed %d tasks, want 2", len(tasks))
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		seen[task.RuntimeID] = true
	}
	if !seen["rt1"] || !seen["rt2"] {
		t.Fatalf("expected tasks for rt1 and rt2, got %v", seen)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch route called %d times, want exactly 1 (the initial probe)", batchCalls.Load())
	}
	if !d.batchClaimUnsupported.Load() {
		t.Fatal("expected batchClaimUnsupported to be set after a 404")
	}

	// Second poll must skip the batch route entirely and go straight to legacy.
	if _, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1", "rt2"}, 5); err != nil {
		t.Fatalf("second ClaimTasksWSFirst: %v", err)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch route retried after being marked unsupported; calls=%d", batchCalls.Load())
	}
}
