package daemon

import (
	"context"
	"encoding/json"
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
func TestClaimTasksWSFirst_LegacyFallbackPreservesPausedFence(t *testing.T) {
	var batchCalls, legacyCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim"):
			batchCalls.Add(1)
			http.Error(w, "404 page not found", http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/runtimes/rt-paused/tasks/claim"):
			legacyCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":null,"paused":true,"workspace_id":"ws-paused","generation":12,"action_id":"action-12","effective_at":"2026-08-07T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	result, err := d.ClaimTasksWSFirstWithControl(
		context.Background(),
		"daemon-x",
		[]string{"rt-paused"},
		1,
	)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirstWithControl: %v", err)
	}
	if len(result.Tasks) != 0 {
		t.Fatalf("legacy paused fallback returned tasks: %+v", result.Tasks)
	}
	if len(result.PausedWorkspaces) != 1 ||
		result.PausedWorkspaces[0].WorkspaceID != "ws-paused" ||
		result.PausedWorkspaces[0].Generation != 12 ||
		result.PausedWorkspaces[0].ActionID != "action-12" {
		t.Fatalf("paused workspaces = %+v, want ws-paused generation 12", result.PausedWorkspaces)
	}
	if batchCalls.Load() != 1 || legacyCalls.Load() != 1 {
		t.Fatalf("claim calls = batch %d legacy %d, want 1/1", batchCalls.Load(), legacyCalls.Load())
	}
}

func TestClaimTasksWSFirst_LegacyFallbackReturnsLaterRuntimeError(t *testing.T) {
	var runtimeCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim"):
				http.NotFound(w, r)
			case strings.Contains(r.URL.Path, "/runtimes/rt1/"):
				runtimeCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(
					`{"task":{"id":"legacy-task",` +
						`"runtime_id":"rt1",` +
						`"agent":{"name":"a"}}}`,
				))
			case strings.Contains(r.URL.Path, "/runtimes/rt2/"):
				runtimeCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error":"claim-intake control unavailable"}`))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer srv.Close()

	d := New(
		Config{
			ServerBaseURL:      srv.URL,
			MaxConcurrentTasks: 4,
		},
		slog.New(slog.NewTextHandler(noopWriter{}, nil)),
	)
	result, err := d.ClaimTasksWSFirstWithControl(
		context.Background(),
		"daemon-x",
		[]string{"rt1", "rt2"},
		2,
	)
	if err == nil {
		t.Fatalf("legacy partial result = %+v, want later runtime error", result)
	}
	if runtimeCalls.Load() != 2 {
		t.Fatalf("legacy runtime calls = %d, want 2", runtimeCalls.Load())
	}
}

func TestClaimTasksWSFirst_StructuredNotFoundDoesNotEnableLegacyFallback(t *testing.T) {
	var batchCalls, legacyCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim"):
			batchCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"daemon not registered"}`))
		case strings.Contains(r.URL.Path, "/runtimes/"):
			legacyCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task":{"id":"legacy-task","runtime_id":"rt1","agent":{"name":"a"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	if _, err := d.ClaimTasksWSFirstWithControl(
		context.Background(),
		"daemon-x",
		[]string{"rt1"},
		1,
	); err == nil {
		t.Fatal("structured batch 404 succeeded; want original request error")
	}
	if d.batchClaimUnsupported.Load() {
		t.Fatal("structured batch 404 permanently enabled legacy fallback")
	}
	if batchCalls.Load() != 1 || legacyCalls.Load() != 0 {
		t.Fatalf("claim calls = batch %d legacy %d, want 1/0", batchCalls.Load(), legacyCalls.Load())
	}
}

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

// TestClaimTasksWSFirst_NoDoubleClaimOnDetach pins the Sol-Boy review fix: if
// the WS connection detaches while a tasks.claim RPC is in flight (its frame
// already sent), ClaimTasksWSFirst must NOT fall back to an HTTP claim in the
// same call — the WS claim may have committed server-side, and an immediate
// second HTTP claim would double-claim.
func TestClaimTasksWSFirst_NoDoubleClaimOnDetach(t *testing.T) {
	var httpClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	// Sender enqueues the frame and hands back its cancelable handle; we then
	// mark it written to simulate the writer having put it on the wire, so the
	// disconnect leaves a genuinely uncertain outcome (the server may commit).
	var mu sync.Mutex
	var item *wsOutbound
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		return item, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

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
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks on uncertain WS outcome, got %d", len(tasks))
	}
	if httpClaims.Load() != 0 {
		t.Fatalf("HTTP fallback claimed %d times after an uncertain WS claim; must be 0 to avoid double-claim", httpClaims.Load())
	}
}

// TestClaimTasksWSFirst_HTTPFallbackAfterUncertainCooldown covers #5404: a
// flaky WS can reconnect and advertise rpc-v1 before the next poll. After one
// uncertain sent-frame outcome, the daemon must not immediately HTTP fallback,
// but it must bypass WS once after the safety window so queued tasks cannot wedge
// behind repeated uncertain WS attempts.
func TestClaimTasksWSFirst_HTTPFallbackAfterUncertainCooldown(t *testing.T) {
	originalDelay := wsClaimUncertainFallbackDelay
	wsClaimUncertainFallbackDelay = 25 * time.Millisecond
	t.Cleanup(func() { wsClaimUncertainFallbackDelay = originalDelay })

	var httpClaims, wsClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	var mu sync.Mutex
	var item *wsOutbound
	frameQueued := make(chan struct{})
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		close(frameQueued)
		return item, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	done := make(chan struct{})
	var tasks []*Task
	var err error
	go func() {
		tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
		close(done)
	}()

	select {
	case <-frameQueued:
	case <-time.After(time.Second):
		t.Fatal("WS claim frame was not queued")
	}
	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	d.wsRPC.attach(nil)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ClaimTasksWSFirst did not return after detach")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks on uncertain WS outcome, got %d", len(tasks))
	}
	if httpClaims.Load() != 0 {
		t.Fatalf("HTTP fallback claimed immediately after uncertain WS outcome; calls=%d", httpClaims.Load())
	}

	reconnectGeneration := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		wsClaims.Add(1)
		return &wsOutbound{data: frame}, nil
	})
	d.wsRPC.markRPCV1Supported(reconnectGeneration)

	tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
	if err != nil {
		t.Fatalf("cooldown ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("cooldown claim returned %d tasks, want 0", len(tasks))
	}
	if httpClaims.Load() != 0 || wsClaims.Load() != 0 {
		t.Fatalf("cooldown attempted claims: http=%d ws=%d, want 0/0", httpClaims.Load(), wsClaims.Load())
	}

	time.Sleep(2 * wsClaimUncertainFallbackDelay)
	tasks, err = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2)
	if err != nil {
		t.Fatalf("post-cooldown ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "http-t" {
		t.Fatalf("tasks = %#v, want one HTTP fallback task", tasks)
	}
	if httpClaims.Load() != 1 {
		t.Fatalf("HTTP fallback claims = %d, want 1", httpClaims.Load())
	}
	if wsClaims.Load() != 0 {
		t.Fatalf("WS claims after uncertain cooldown = %d, want 0", wsClaims.Load())
	}
}

// TestClaimTasksWSFirst_ReconnectToOldServerSkipsWSRPC pins compatibility when
// a daemon moves from an rpc-v1 server to an older server. The replacement
// connection must not inherit the first connection's negotiated capability;
// old servers ignore unknown WS frames, so claims must use HTTP until a fresh
// heartbeat ack explicitly advertises server support.
func TestClaimTasksWSFirst_ReconnectToOldServerSkipsWSRPC(t *testing.T) {
	var httpClaims, wsClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"http-t","runtime_id":"rt1","agent":{"name":"a"}}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	previousGeneration := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		return &wsOutbound{data: frame}, nil
	})
	d.wsRPC.markRPCV1Supported(previousGeneration)
	d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		wsClaims.Add(1)
		return &wsOutbound{data: frame}, nil
	})

	tasks, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 1)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "http-t" {
		t.Fatalf("tasks = %#v, want HTTP task", tasks)
	}
	if wsClaims.Load() != 0 {
		t.Fatalf("WS claims = %d without rpc-v1 advertisement, want 0", wsClaims.Load())
	}
	if httpClaims.Load() != 1 {
		t.Fatalf("HTTP claims = %d, want 1", httpClaims.Load())
	}
}

// A successful WS status means the server may already have committed task
// ownership. If the response body is malformed, the daemon must treat the
// result as uncertain rather than immediately claiming more work over HTTP.
func TestClaimTasksWSFirst_MalformedSuccessfulWSResponseDoesNotFallback(t *testing.T) {
	originalDelay := wsClaimUncertainFallbackDelay
	wsClaimUncertainFallbackDelay = time.Minute
	t.Cleanup(func() { wsClaimUncertainFallbackDelay = originalDelay })

	var httpClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"unexpected-http-task"}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		var message protocol.Message
		if err := json.Unmarshal(frame, &message); err != nil {
			return nil, err
		}
		var request protocol.RPCRequestPayload
		if err := json.Unmarshal(message.Payload, &request); err != nil {
			return nil, err
		}
		go d.wsRPC.deliver(protocol.RPCResponsePayload{
			RequestID: request.RequestID,
			Status:    http.StatusOK,
			Body:      json.RawMessage(`{"tasks":`),
		})
		return &wsOutbound{data: frame}, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	result, err := d.ClaimTasksWSFirstWithControl(context.Background(), "daemon-x", []string{"rt1"}, 1)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirstWithControl: %v", err)
	}
	if len(result.Tasks) != 0 {
		t.Fatalf("tasks = %+v, want none for uncertain successful WS response", result.Tasks)
	}
	if httpClaims.Load() != 0 {
		t.Fatalf("malformed successful WS response triggered %d HTTP claims", httpClaims.Load())
	}
	if d.wsClaimHTTPFallbackAfter.Load() == 0 {
		t.Fatal("malformed successful WS response did not arm uncertain-outcome cooldown")
	}
}

// A paused workspace is an authoritative successful claim result. The WS-first
// path must not turn that result into HTTP or legacy fallback traffic.
func TestClaimTasksWSFirst_PausedResponseDoesNotFallback(t *testing.T) {
	var httpClaims atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/claim") {
			httpClaims.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[{"id":"unexpected-http-task"}]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		var message protocol.Message
		if err := json.Unmarshal(frame, &message); err != nil {
			return nil, err
		}
		var request protocol.RPCRequestPayload
		if err := json.Unmarshal(message.Payload, &request); err != nil {
			return nil, err
		}
		go d.wsRPC.deliver(protocol.RPCResponsePayload{
			RequestID: request.RequestID,
			Status:    http.StatusOK,
			Body:      json.RawMessage(`{"tasks":[],"paused_workspaces":[{"workspace_id":"ws-paused","state":"paused","generation":9,"action_id":"action-9","effective_at":"2026-08-07T00:00:00Z"}]}`),
		})
		return &wsOutbound{data: frame}, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	result, err := d.ClaimTasksWSFirstWithControl(context.Background(), "daemon-x", []string{"rt1"}, 1)
	if err != nil {
		t.Fatalf("ClaimTasksWSFirstWithControl: %v", err)
	}
	if len(result.Tasks) != 0 {
		t.Fatalf("tasks = %+v, want none while paused", result.Tasks)
	}
	if len(result.PausedWorkspaces) != 1 || result.PausedWorkspaces[0].WorkspaceID != "ws-paused" || result.PausedWorkspaces[0].Generation != 9 || result.PausedWorkspaces[0].ActionID != "action-9" {
		t.Fatalf("paused workspaces = %+v, want authoritative ws-paused generation 9", result.PausedWorkspaces)
	}
	if httpClaims.Load() != 0 {
		t.Fatalf("paused WS response triggered %d HTTP claims", httpClaims.Load())
	}
}
