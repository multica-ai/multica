package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type startTaskTransport func(*http.Request) (*http.Response, error)

func (f startTaskTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStartTaskBoundsResponseRead(t *testing.T) {
	const responseSize = 2 << 20
	body := strings.NewReader(strings.Repeat(" ", responseSize))
	client := NewClient("https://daemon.test")
	client.client.Transport = startTaskTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body)}, nil
	})
	_, err := client.StartTask(context.Background(), Task{ID: "task-1"})
	if !errors.Is(err, errInvalidResponseBody) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("StartTask error = %v, want response size error", err)
	}
	if read := responseSize - body.Len(); read > (1<<20)+1 {
		t.Fatalf("read %d bytes before rejecting oversized response", read)
	}
}

func TestStartTaskRetries(t *testing.T) {
	defer noSleepRetry(t)()
	for _, tc := range []struct {
		name      string
		failure   error
		status    int
		failures  int
		wantCalls int
		wantError bool
	}{
		{name: "EOF", failure: io.EOF, failures: 1, wantCalls: 2},
		{name: "unexpected EOF", failure: io.ErrUnexpectedEOF, failures: 2, wantCalls: 3},
		{name: "gateway outage", status: http.StatusBadGateway, failures: 1, wantCalls: 2},
		{name: "persistent outage", failure: io.ErrUnexpectedEOF, failures: 10, wantCalls: 3, wantError: true},
		{name: "unauthorized", status: http.StatusUnauthorized, failures: 10, wantCalls: 1, wantError: true},
		{name: "invalid task state", status: http.StatusBadRequest, failures: 10, wantCalls: 1, wantError: true},
		{name: "stale claim", status: http.StatusConflict, failures: 10, wantCalls: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := NewClient("https://daemon.test")
			client.client.Transport = startTaskTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/daemon/tasks/task-1/start" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				body, err := io.ReadAll(r.Body)
				var claim map[string]string
				if err != nil || json.Unmarshal(body, &claim) != nil || claim["runtime_id"] != "runtime-1" || claim["dispatched_at"] != "2026-09-17T09:00:00.123456Z" {
					t.Fatalf("start body = %q, error = %v", body, err)
				}
				status := http.StatusOK
				if calls <= tc.failures {
					if tc.failure != nil {
						return nil, tc.failure
					}
					status = tc.status
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			})
			_, err := client.StartTask(context.Background(), startTestClaim())
			if tc.status == http.StatusConflict && !errors.Is(err, errStartClaimRejected) {
				t.Fatalf("lost claim rejection: %v", err)
			}
			if (err != nil) != tc.wantError || calls != tc.wantCalls {
				t.Fatalf("StartTask error = %v, calls = %d; want error = %v, calls = %d", err, calls, tc.wantError, tc.wantCalls)
			}
		})
	}
}

func TestStartTaskCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previous := retrySleep
	t.Cleanup(func() { retrySleep = previous })
	retrySleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	calls := 0
	client := NewClient("https://daemon.test")
	client.client.Transport = startTaskTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, io.ErrUnexpectedEOF
	})
	_, err := client.StartTask(ctx, startTestClaim())
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) || calls != 1 {
		t.Fatalf("error = %v, context = %v, calls = %d; want cancelled backoff and one attempt", err, ctx.Err(), calls)
	}
}

func TestHandleTaskRejectedStartDoesNotFailNewOwner(t *testing.T) {
	var callbacks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") || strings.HasSuffix(r.URL.Path, "/complete") || strings.HasSuffix(r.URL.Path, "/cancel-ack") {
			callbacks.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &Daemon{
		client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces: make(map[string]*workspaceState), runtimeIndex: map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int), cancelPollInterval: time.Hour,
		cfg: Config{WorkspacesRoot: t.TempDir()},
	}
	var ran bool
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		ran = true
		return TaskResult{}, errStartClaimRejected
	})
	d.handleTask(context.Background(), Task{ID: "stale-task", WorkspaceID: "workspace", RuntimeID: "rt-1", Agent: &AgentData{Name: "test-agent"}}, 0)
	if !ran || callbacks.Load() != 0 {
		t.Fatalf("ran=%t terminal callbacks=%d", ran, callbacks.Load())
	}
}

func startTestClaim() Task {
	return Task{ID: "task-1", RuntimeID: "runtime-1", DispatchedAt: "2026-09-17T09:00:00.123456Z", StartClaimSupported: true}
}

func TestStartTaskLegacyServerDoesNotRetry(t *testing.T) {
	calls := 0
	client := NewClient("https://daemon.test")
	client.client.Transport = startTaskTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"capabilities":null}` {
			t.Fatalf("legacy body: %s", body)
		}
		return nil, io.ErrUnexpectedEOF
	})
	claim := startTestClaim()
	claim.StartClaimSupported = false
	if _, err := client.StartTask(context.Background(), claim); err == nil || calls != 1 {
		t.Fatalf("legacy start: err=%v calls=%d", err, calls)
	}
}

func TestStartTaskBudgetAndMissingClaim(t *testing.T) {
	client := NewClient("https://daemon.test")
	calls := 0
	client.client.Transport = startTaskTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > startTaskTimeout {
			t.Fatal("missing overall start budget")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	claim := startTestClaim()
	claim.DispatchedAt = ""
	if _, err := client.StartTask(context.Background(), claim); err == nil || calls != 0 {
		t.Fatalf("missing claim: %v, calls=%d", err, calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.StartTask(ctx, startTestClaim()); err == nil || calls != 1 {
		t.Fatalf("deadline: %v, calls=%d", err, calls)
	}
}
