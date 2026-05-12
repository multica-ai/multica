package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// newTestOutbox creates an Outbox configured for fast test execution.
func newTestOutbox(dir, serverURL string) *Outbox {
	deadDir := filepath.Join(dir, "dead")
	os.MkdirAll(deadDir, 0o700)
	return &Outbox{
		dir:        dir,
		deadDir:    deadDir,
		client:     NewClient(serverURL),
		logger:     newTestLogger(),
		minBackoff: 10 * time.Millisecond,
		maxBackoff: 100 * time.Millisecond,
		maxRetries: 5,
		replayPoll: 50 * time.Millisecond,
		inflight:   make(map[string]struct{}),
		wakeup:     make(chan struct{}, 1),
	}
}

func TestOutbox_EnqueueComplete_DeliversImmediately(t *testing.T) {
	t.Parallel()

	delivered := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/tasks/task-1/complete" && r.Method == "POST" {
			delivered = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	o := newTestOutbox(t.TempDir(), srv.URL)

	err := o.EnqueueComplete(context.Background(), "task-1", "all done", "", "", "")
	if err != nil {
		t.Fatalf("EnqueueComplete: %v", err)
	}
	if !delivered {
		t.Fatal("expected immediate delivery on first attempt")
	}

	entries, err := o.loadAll()
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after success, got %d", len(entries))
	}
}

func TestOutbox_EnqueueComplete_RetriesOn500AndEventuallySucceeds(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	o := newTestOutbox(t.TempDir(), srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.Run(ctx)

	err := o.EnqueueComplete(context.Background(), "task-1", "all done", "", "", "")
	if err != nil {
		t.Logf("immediate delivery failed (expected): %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		entries, loadErr := o.loadAll()
		if loadErr != nil {
			t.Fatalf("loadAll: %v", loadErr)
		}
		if len(entries) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for retry to succeed, %d entries remaining, attempts=%d",
				len(entries), attempts.Load())
		case <-time.After(50 * time.Millisecond):
		}
	}

	if n := attempts.Load(); n < 3 {
		t.Errorf("expected at least 3 attempts, got %d", n)
	}
}

func TestOutbox_EnqueueFail_DoesNotRetryOnTaskNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	o := newTestOutbox(t.TempDir(), srv.URL)

	err := o.EnqueueFail(context.Background(), "task-gone", "error msg", "", "", "agent_error")
	if err != nil {
		t.Logf("delivery returned error (expected for 404): %v", err)
	}

	entries, loadErr := o.loadAll()
	if loadErr != nil {
		t.Fatalf("loadAll: %v", loadErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after task-not-found, got %d", len(entries))
	}
}

func TestOutbox_EnqueueFail_RetriesOn500(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	o := newTestOutbox(t.TempDir(), srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go o.Run(ctx)

	err := o.EnqueueFail(context.Background(), "task-1", "error msg", "", "", "infrastructure_error")
	if err != nil {
		t.Logf("immediate delivery failed (expected): %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		entries, loadErr := o.loadAll()
		if loadErr != nil {
			t.Fatalf("loadAll: %v", loadErr)
		}
		if len(entries) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for retry to succeed, %d entries remaining", len(entries))
		case <-time.After(50 * time.Millisecond):
		}
	}

	if n := attempts.Load(); n < 3 {
		t.Errorf("expected at least 3 attempts, got %d", n)
	}
}

func TestOutbox_ExponentialBackoff(t *testing.T) {
	t.Parallel()

	o := &Outbox{
		minBackoff: 1 * time.Second,
		maxBackoff: 60 * time.Second,
	}

	// Attempt 1 (first retry) -> 1s
	// Attempt 2 -> 2s
	// Attempt 3 -> 4s
	// Attempt 4 -> 8s
	// Attempt 5 -> 16s
	// Attempt 6 -> 32s
	// Attempt 7 -> 60s (capped)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 60 * time.Second},
		{8, 60 * time.Second},
	}
	for _, tc := range tests {
		got := o.backoff(tc.attempt)
		if got != tc.want {
			t.Errorf("backoff(attempt=%d): got %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestOutbox_ReplayOnStartup(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	delivered := make(map[string]int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path
		if path == "/api/daemon/tasks/task-pending/complete" {
			delivered["task-pending"]++
			w.WriteHeader(http.StatusOK)
		} else if path == "/api/daemon/tasks/task-exhausted/complete" {
			delivered["task-exhausted"]++
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()

	// Pre-populate outbox entries (simulating a previous daemon run that crashed).
	err := os.WriteFile(filepath.Join(dir, "pending.json"), []byte(`{
		"id": "pending",
		"task_id": "task-pending",
		"result_type": "complete",
		"output": "done",
		"created_at": "2026-05-12T00:00:00Z",
		"last_attempt_at": "2026-05-12T00:00:00Z",
		"next_attempt_at": "2026-05-12T00:00:00Z",
		"attempts": 1,
		"max_attempts": 5
	}`), 0o600)
	if err != nil {
		t.Fatalf("write pending entry: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir, "exhausted.json"), []byte(`{
		"id": "exhausted",
		"task_id": "task-exhausted",
		"result_type": "complete",
		"output": "done",
		"created_at": "2026-05-12T00:00:00Z",
		"last_attempt_at": "2026-05-12T00:00:00Z",
		"next_attempt_at": "2026-05-12T00:00:00Z",
		"attempts": 5,
		"max_attempts": 5,
		"delivery_error": "always fails"
	}`), 0o600)
	if err != nil {
		t.Fatalf("write exhausted entry: %v", err)
	}

	o := newTestOutbox(dir, srv.URL)

	// Run replay directly (not goroutine, so we can observe the immediate effect).
	o.replayPending(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if delivered["task-pending"] != 1 {
		t.Errorf("expected task-pending to be delivered once, got %d", delivered["task-pending"])
	}
	if delivered["task-exhausted"] != 0 {
		t.Errorf("expected task-exhausted NOT to be delivered (attempts exhausted), got %d", delivered["task-exhausted"])
	}

	entries, err := o.loadAll()
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after replay, got %d", len(entries))
	}

	// Verify exhausted entry was moved to dead-letter.
	deadEntries, err := os.ReadDir(filepath.Join(dir, "dead"))
	if err != nil {
		t.Fatalf("read dead-letter dir: %v", err)
	}
	if len(deadEntries) != 1 {
		t.Fatalf("expected 1 dead-letter entry, got %d", len(deadEntries))
	}
}

func TestOutbox_PreservesPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/tasks/task-1/complete" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			captured = body
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	o := newTestOutbox(t.TempDir(), srv.URL)

	err := o.EnqueueComplete(context.Background(), "task-1", "the output", "agent/my-branch", "ses-123", "/tmp/work")
	if err != nil {
		t.Fatalf("EnqueueComplete: %v", err)
	}

	if captured["output"] != "the output" {
		t.Errorf("output: got %v", captured["output"])
	}
	if captured["branch_name"] != "agent/my-branch" {
		t.Errorf("branch_name: got %v", captured["branch_name"])
	}
	if captured["session_id"] != "ses-123" {
		t.Errorf("session_id: got %v", captured["session_id"])
	}
	if captured["work_dir"] != "/tmp/work" {
		t.Errorf("work_dir: got %v", captured["work_dir"])
	}
}

func TestFailTaskWithOutbox_UsesOutboxWhenAvailable(t *testing.T) {
	t.Parallel()

	delivered := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/tasks/task-1/fail" && r.Method == "POST" {
			delivered = true
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["failure_reason"] != "infrastructure_error" {
				t.Errorf("failure_reason: got %v, want infrastructure_error", body["failure_reason"])
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client: NewClient(srv.URL),
		outbox: newTestOutbox(t.TempDir(), srv.URL),
		logger: newTestLogger(),
	}

	d.failTaskWithOutbox(context.Background(), "task-1", "something went wrong", "", "", "infrastructure_error", newTestLogger())
	if !delivered {
		t.Fatal("expected outbox delivery")
	}
}

func TestFailTaskWithOutbox_FallsBackToDirectWhenNoOutbox(t *testing.T) {
	t.Parallel()

	delivered := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/daemon/tasks/task-1/fail" && r.Method == "POST" {
			delivered = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client: NewClient(srv.URL),
		outbox: nil,
		logger: newTestLogger(),
	}

	d.failTaskWithOutbox(context.Background(), "task-1", "error", "", "", "agent_error", newTestLogger())
	if !delivered {
		t.Fatal("expected direct HTTP delivery when outbox is nil")
	}
}

func TestOutbox_TruncatesOutput(t *testing.T) {
	longOutput := ""
	for i := 0; i < 5000; i++ {
		longOutput += "x"
	}
	truncated := truncateString(longOutput, outboxOutputMaxLen)
	if len(truncated) > outboxOutputMaxLen+3 {
		t.Errorf("expected truncated length <= %d, got %d", outboxOutputMaxLen+3, len(truncated))
	}
}

func TestOutbox_IdempotencyKeyPopulated(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	o := newTestOutbox(dir, srv.URL)

	// EnqueueComplete fails (500), so the entry stays on disk.
	err := o.EnqueueComplete(context.Background(), "task-1", "output", "", "", "")
	if err == nil {
		t.Fatal("expected delivery failure")
	}

	entries, loadErr := o.loadAll()
	if loadErr != nil {
		t.Fatalf("loadAll: %v", loadErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", len(entries))
	}

	if entries[0].IdempotencyKey == "" {
		t.Errorf("idempotency_key should be non-empty")
	}
}
