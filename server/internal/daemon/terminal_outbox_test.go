package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTerminalOutboxPutIsDurableAndIdempotent(t *testing.T) {
	outbox := &terminalOutbox{dir: filepath.Join(t.TempDir(), "outbox")}
	report := terminalTaskReport{
		kind:                  terminalTaskReportComplete,
		taskID:                "task-1",
		output:                "done",
		branchName:            "agent/task-1",
		sessionID:             "session-1",
		workDir:               "/tmp/task-1",
		durableWorkDir:        "/work/task-1",
		sessionRolloutMissing: true,
		retiredSessionID:      "session-old",
	}

	first, err := outbox.put(report)
	if err != nil {
		t.Fatalf("put terminal report: %v", err)
	}
	second, err := outbox.put(report)
	if err != nil {
		t.Fatalf("put duplicate terminal report: %v", err)
	}
	if first != second {
		t.Fatalf("duplicate report paths differ: %q vs %q", first, second)
	}

	entries, err := outbox.list()
	if err != nil {
		t.Fatalf("list terminal outbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("outbox entries = %d, want 1", len(entries))
	}
	if entries[0].err != nil {
		t.Fatalf("decode terminal outbox: %v", entries[0].err)
	}
	if got := entries[0].report; got != report {
		t.Fatalf("round-trip report = %#v, want %#v", got, report)
	}
}

func TestReportTaskResultUnauthorizedCompletePersistsAndReplaysAfterRestart(t *testing.T) {
	outboxDir := filepath.Join(t.TempDir(), "terminal-outbox")
	var completeCalls, failCalls atomic.Int32
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(rejecting.Close)

	d := New(Config{ServerBaseURL: rejecting.URL, WorkspacesRoot: t.TempDir(), TerminalOutboxDir: outboxDir}, slog.Default())
	d.reportTaskResult(context.Background(), "task-auth", TaskResult{
		Status:     "completed",
		Comment:    "real result",
		BranchName: "agent/task-auth",
		SessionID:  "session-auth",
	}, slog.Default())

	if got := completeCalls.Load(); got != 1 {
		t.Fatalf("complete calls = %d, want 1", got)
	}
	if got := failCalls.Load(); got != 0 {
		t.Fatalf("401 must preserve /complete rather than downgrade to /fail; fail calls = %d", got)
	}
	if !d.authRejected.Load() {
		t.Fatal("shared-PAT 401 did not invalidate daemon authentication")
	}
	entries, err := d.terminalOutbox.list()
	if err != nil {
		t.Fatalf("list pending reports: %v", err)
	}
	if len(entries) != 1 || entries[0].err != nil {
		t.Fatalf("pending reports = %#v, want one readable report", entries)
	}
	if entries[0].report.kind != terminalTaskReportComplete || entries[0].report.output != "real result" {
		t.Fatalf("pending report = %#v, want original completion", entries[0].report)
	}

	var replayBody map[string]any
	recovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/complete") {
			t.Errorf("replay path = %q, want /complete", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&replayBody); err != nil {
			t.Errorf("decode replay: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(recovered.Close)

	restarted := New(Config{ServerBaseURL: recovered.URL, WorkspacesRoot: t.TempDir()}, slog.Default())
	restarted.terminalOutbox = d.terminalOutbox
	restarted.replayTerminalOutbox(context.Background())
	if replayBody["output"] != "real result" || replayBody["branch_name"] != "agent/task-auth" {
		t.Fatalf("replayed body = %#v", replayBody)
	}
	entries, err = restarted.terminalOutbox.list()
	if err != nil {
		t.Fatalf("list replayed outbox: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("acknowledged outbox entries = %d, want 0", len(entries))
	}
}

func TestFailedTerminalReportPersistsAfterTransientExhaustion(t *testing.T) {
	defer noSleepRetry(t)()
	previous := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{time.Nanosecond}
	t.Cleanup(func() { defaultTerminalRetrySchedule = previous })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	d := New(Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), TerminalOutboxDir: t.TempDir()}, slog.Default())
	d.reportTaskResult(context.Background(), "task-failed", TaskResult{
		Status:        "blocked",
		Comment:       "provider unavailable",
		FailureReason: "agent_error.provider_unavailable",
	}, slog.Default())

	entries, err := d.terminalOutbox.list()
	if err != nil {
		t.Fatalf("list pending reports: %v", err)
	}
	if len(entries) != 1 || entries[0].err != nil {
		t.Fatalf("pending reports = %#v, want one readable report", entries)
	}
	if entries[0].report.kind != terminalTaskReportFail {
		t.Fatalf("pending report kind = %v, want fail", entries[0].report.kind)
	}
}

func TestFallbackFailurePersistsWhenCompleteIsRejected(t *testing.T) {
	defer noSleepRetry(t)()
	previous := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{time.Nanosecond}
	t.Cleanup(func() { defaultTerminalRetrySchedule = previous })

	var completeCalls, failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := New(Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), TerminalOutboxDir: t.TempDir()}, slog.Default())
	d.reportTaskResult(context.Background(), "task-fallback", TaskResult{
		Status:  "completed",
		Comment: "agent succeeded",
	}, slog.Default())

	if completeCalls.Load() != 1 || failCalls.Load() != 2 {
		t.Fatalf("callback calls complete=%d fail=%d, want 1 and 2", completeCalls.Load(), failCalls.Load())
	}
	entries, err := d.terminalOutbox.list()
	if err != nil {
		t.Fatalf("list pending reports: %v", err)
	}
	if len(entries) != 1 || entries[0].err != nil {
		t.Fatalf("pending reports = %#v, want one readable fallback", entries)
	}
	if entries[0].report.kind != terminalTaskReportFail || !strings.Contains(entries[0].report.errorMessage, "complete task failed") {
		t.Fatalf("pending report = %#v, want fallback failure", entries[0].report)
	}
}

func TestExplicitTokenUnauthorizedDoesNotInvalidateDaemonAuthentication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	client.SetToken("pat-daemon")
	var rejected atomic.Int32
	client.SetUnauthorizedHandler(func(error) { rejected.Add(1) })

	if err := client.getJSONWithToken(context.Background(), "/task-scoped", "mat-task", nil); !isUnauthorizedError(err) {
		t.Fatalf("explicit-token GET error = %v, want 401", err)
	}
	if err := client.postJSONWithToken(context.Background(), "/task-scoped", "mat-task", map[string]any{}, nil); !isUnauthorizedError(err) {
		t.Fatalf("explicit-token POST error = %v, want 401", err)
	}
	if got := rejected.Load(); got != 0 {
		t.Fatalf("explicit-token 401 invalidated daemon auth %d times", got)
	}

	if err := client.getJSON(context.Background(), "/control-plane", nil); !isUnauthorizedError(err) {
		t.Fatalf("shared-PAT GET error = %v, want 401", err)
	}
	if got := rejected.Load(); got != 1 {
		t.Fatalf("shared-PAT 401 invalidations = %d, want 1", got)
	}
}

func TestSharedPATUnauthorizedClosesAuthenticatedWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	connected := make(chan struct{})
	peerClosed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/reject" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		close(connected)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(peerClosed)
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	var logs bytes.Buffer
	d := New(Config{
		ServerBaseURL:     srv.URL,
		WorkspacesRoot:    t.TempDir(),
		HeartbeatInterval: time.Hour,
		Profile:           "staging",
	}, captureLogger(&logs))
	d.client.SetToken("pat-daemon")
	d.recordWSHeartbeatAck("runtime-1")

	errCh := make(chan error, 1)
	go func() {
		_, err := d.runTaskWakeupConnection(context.Background(), []string{"runtime-1"}, make(chan taskWakeup, 2), make(chan struct{}))
		errCh <- err
	}()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("websocket did not connect")
	}

	err := d.client.postJSON(context.Background(), "/reject", map[string]any{}, nil)
	if !isUnauthorizedError(err) {
		t.Fatalf("control-plane request error = %v, want 401", err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, errControlPlaneAuthRejected) {
			t.Fatalf("websocket exit error = %v, want auth rejected", err)
		}
	case <-time.After(time.Second):
		t.Fatal("websocket stayed connected after shared-PAT 401")
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("server did not observe websocket close")
	}
	if d.wsHeartbeatRecentlyAcked("runtime-1") {
		t.Fatal("stale websocket heartbeat ack survived auth rejection")
	}
	if got := logs.String(); !strings.Contains(got, "multica login --profile staging") {
		t.Fatalf("auth warning missing profile-specific re-login hint: %s", got)
	}
}
