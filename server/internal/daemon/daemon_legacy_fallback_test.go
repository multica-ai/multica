package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
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
		case r.Method == http.MethodPut && strings.Contains(path, "/api/daemon/task-claim-attempts/"):
			http.Error(w, "404 page not found", http.StatusNotFound)
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

// TestClaimTasksWSFirst_ReplaysSameAttemptOnDetach pins MUL-4756: once the WS
// frame may have reached the server, the daemon falls back to HTTP with the
// same claim_attempt_id instead of either skipping forever or issuing a fresh
// capacity-consuming claim.
func TestClaimTasksWSFirst_ReplaysSameAttemptOnDetach(t *testing.T) {
	var httpClaims atomic.Int64
	var replayedAttempt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/task-claim-attempts/") {
			httpClaims.Add(1)
			replayedAttempt = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"claim_attempt_id":%q,"state":"ready","tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`, replayedAttempt)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	// Sender enqueues the frame and hands back its cancelable handle; we then
	// mark it written to simulate the writer having put it on the wire, so the
	// disconnect leaves a genuinely uncertain outcome (the server may commit).
	var mu sync.Mutex
	var item *wsOutbound
	var wsAttempt string
	d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		var err error
		wsAttempt, err = claimAttemptIDFromWSFrame(frame)
		if err != nil {
			return nil, err
		}
		item = &wsOutbound{data: frame}
		return item, nil
	})

	done := make(chan struct{})
	var tasks []*Task
	var err error
	go func() {
		tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond) // let Call send the frame and block on the response
	mu.Lock()
	item.beginWrite() // frame is now on the wire — cannot be un-sent
	mu.Unlock()
	d.wsRPC.attach(nil) // detach mid-flight (reconnect / teardown)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ClaimTasksWSFirst did not return after detach")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "http-t" {
		t.Fatalf("HTTP replay tasks = %#v, want http-t", tasks)
	}
	if httpClaims.Load() != 1 {
		t.Fatalf("HTTP replay called %d times, want 1", httpClaims.Load())
	}
	if replayedAttempt == "" || replayedAttempt != wsAttempt {
		t.Fatalf("attempt ids = ws:%q http:%q, want the same non-empty id", wsAttempt, replayedAttempt)
	}
}

func TestClaimTasksWSFirst_ReplaysSameAttemptOnTimeout(t *testing.T) {
	var replayedAttempt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/task-claim-attempts/") {
			replayedAttempt = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"claim_attempt_id":%q,"state":"ready","tasks":[]}`, replayedAttempt)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 2}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	// Keep the test fast while exercising the real timer path: the call timeout
	// is server budget + grace, so this produces a 20ms response deadline.
	d.wsRPC.grace = -batchClaimRequestTimeout + 20*time.Millisecond
	var wsAttempt string
	d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		var err error
		wsAttempt, err = claimAttemptIDFromWSFrame(frame)
		if err != nil {
			return nil, err
		}
		item := &wsOutbound{data: frame}
		item.beginWrite()
		return item, nil
	})

	if _, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 1); err != nil {
		t.Fatalf("timeout replay: %v", err)
	}
	if wsAttempt == "" || replayedAttempt != wsAttempt {
		t.Fatalf("attempt ids = ws:%q http:%q, want the same non-empty id", wsAttempt, replayedAttempt)
	}
}

func claimAttemptIDFromWSFrame(frame []byte) (string, error) {
	var msg protocol.Message
	if err := json.Unmarshal(frame, &msg); err != nil {
		return "", fmt.Errorf("decode WS message: %w", err)
	}
	var req protocol.RPCRequestPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return "", fmt.Errorf("decode WS RPC: %w", err)
	}
	return req.RequestID, nil
}

func TestClaimTasksWSFirst_RetainsAttemptAcrossHTTPFailure(t *testing.T) {
	originalSchedule := claimAttemptRetrySchedule
	claimAttemptRetrySchedule = nil
	t.Cleanup(func() { claimAttemptRetrySchedule = originalSchedule })

	var mu sync.Mutex
	var attemptIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/task-claim-attempts/") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
			return
		}
		attemptID := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		attemptIDs = append(attemptIDs, attemptID)
		call := len(attemptIDs)
		mu.Unlock()
		if call == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"claim_attempt_id":%q,"state":"ready","tasks":[]}`, attemptID)
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 2}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	if _, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 1); err == nil {
		t.Fatal("first HTTP failure unexpectedly succeeded")
	}
	if _, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 1); err != nil {
		t.Fatalf("second replay: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attemptIDs) != 2 || attemptIDs[0] == "" || attemptIDs[0] != attemptIDs[1] {
		t.Fatalf("HTTP attempts used ids %v, want the same non-empty id", attemptIDs)
	}
}
