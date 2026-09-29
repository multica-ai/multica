package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTerminalReportStoreRoundTripAndPermissions(t *testing.T) {
	store := newTerminalReportStore(Config{
		WorkspacesRoot: t.TempDir(),
		ServerBaseURL:  "https://api.example.test",
		Profile:        "work",
		DaemonID:       "daemon-1",
	})
	report := terminalTaskReport{
		kind:                  terminalTaskReportComplete,
		taskID:                "task-private",
		output:                "private final answer",
		branchName:            "agent/private",
		sessionID:             "session-private",
		workDir:               "/private/workdir",
		durableWorkDir:        "/private/project",
		sessionRolloutMissing: true,
		retiredSessionID:      "retired-private",
		warnings:              []string{"Temporary worktree cleanup is pending."},
	}
	if err := store.enqueue(report); err != nil {
		t.Fatalf("enqueue terminal report: %v", err)
	}
	items, err := store.list()
	if err != nil {
		t.Fatalf("list terminal reports: %v", err)
	}
	if len(items) != 1 || !reflect.DeepEqual(items[0].report, report) {
		t.Fatalf("round trip = %+v, want %+v", items, report)
	}
	if err := store.enqueue(report); err != nil {
		t.Fatalf("identical warning-bearing enqueue: %v", err)
	}
	warningConflict := report
	warningConflict.warnings = []string{"Different cleanup warning."}
	if err := store.enqueue(warningConflict); err == nil || !strings.Contains(err.Error(), "conflicts with the original") {
		t.Fatalf("changed warning enqueue error = %v, want original-payload conflict", err)
	}

	if runtime.GOOS != "windows" {
		if info, err := os.Stat(store.dir); err != nil {
			t.Fatalf("stat queue directory: %v", err)
		} else if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("queue directory mode = %o, want 700", got)
		}
		path := store.dir + string(os.PathSeparator) + items[0].fileName
		if info, err := os.Stat(path); err != nil {
			t.Fatalf("stat queue file: %v", err)
		} else if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("queue file mode = %o, want 600", got)
		}
	}

	conflict := report
	conflict.output = "replacement must not overwrite the original"
	if err := store.enqueue(conflict); err == nil || !strings.Contains(err.Error(), "conflicts with the original") {
		t.Fatalf("conflicting enqueue error = %v, want original-payload conflict", err)
	}
	items, err = store.list()
	if err != nil {
		t.Fatalf("list after conflict: %v", err)
	}
	if len(items) != 1 || items[0].report.output != report.output {
		t.Fatalf("conflicting enqueue changed original payload: %+v", items)
	}
}

func TestTerminalReportStoreRecoversFlushedTempFileAfterCrash(t *testing.T) {
	store := newTerminalReportStore(Config{
		WorkspacesRoot: t.TempDir(),
		ServerBaseURL:  "https://api.example.test",
		DaemonID:       "daemon-crash",
	})
	if err := store.ensureDir(); err != nil {
		t.Fatalf("prepare store: %v", err)
	}
	report := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task-crash", output: "durable answer"}
	record, err := persistedTerminalReport(report, time.Now())
	if err != nil {
		t.Fatalf("build persisted report: %v", err)
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	targetName := terminalReportFileName(report.taskID)
	temp, err := os.CreateTemp(store.dir, "."+strings.TrimSuffix(targetName, ".json")+"-*.tmp")
	if err != nil {
		t.Fatalf("create interrupted temp: %v", err)
	}
	tempName := temp.Name()
	if err := temp.Chmod(0o600); err != nil {
		t.Fatalf("chmod interrupted temp: %v", err)
	}
	if _, err := temp.Write(body); err != nil {
		t.Fatalf("write interrupted temp: %v", err)
	}
	if err := temp.Sync(); err != nil {
		t.Fatalf("sync interrupted temp: %v", err)
	}
	if err := temp.Close(); err != nil {
		t.Fatalf("close interrupted temp: %v", err)
	}

	items, err := store.list()
	if err != nil {
		t.Fatalf("recover interrupted report: %v", err)
	}
	if len(items) != 1 || !reflect.DeepEqual(items[0].report, report) {
		t.Fatalf("recovered reports = %+v, want %+v", items, report)
	}
	if _, err := os.Stat(tempName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted temp still exists after recovery: %v", err)
	}
}

func TestTerminalReportReplaysAfterClientRetryWindow(t *testing.T) {
	defer noSleepRetry(t)()
	previousSchedule := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{time.Nanosecond, time.Nanosecond}
	t.Cleanup(func() { defaultTerminalRetrySchedule = previousSchedule })

	var online atomic.Bool
	var calls atomic.Int32
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/complete") {
			t.Errorf("unexpected request path %q", req.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		calls.Add(1)
		if !online.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-retry",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := terminalTaskReport{
		kind:           terminalTaskReportComplete,
		taskID:         "task-retry-window",
		warnings:       []string{"Temporary worktree cleanup is pending."},
		output:         "the original answer",
		branchName:     "agent/recovered",
		sessionID:      "session-1",
		workDir:        "/tmp/work",
		durableWorkDir: "/tmp/project",
	}
	if err := d.reportTerminalTask(context.Background(), report); err == nil {
		t.Fatal("terminal report unexpectedly succeeded while server was offline")
	}
	if got, want := calls.Load(), int32(len(defaultTerminalRetrySchedule)+1); got != want {
		t.Fatalf("initial callback attempts = %d, want exhausted window of %d", got, want)
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 1 {
		t.Fatalf("pending reports after exhausted retries = %d, %v; want 1", len(items), err)
	}

	online.Store(true)
	d = New(d.cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	pending, delivered := d.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 1 {
		t.Fatalf("replay result pending=%d delivered=%d, want 0/1", pending, delivered)
	}
	if got := calls.Load(); got != int32(len(defaultTerminalRetrySchedule)+2) {
		t.Fatalf("calls after recovery = %d, want one replay after retry exhaustion", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, body := range bodies {
		if !reflect.DeepEqual(body["warnings"], []any{report.warnings[0]}) {
			t.Fatalf("attempt %d warnings = %#v, want original warnings", i+1, body["warnings"])
		}
		if body["output"] != report.output || body["branch_name"] != report.branchName || body["durable_work_dir"] != report.durableWorkDir {
			t.Fatalf("attempt %d payload = %#v, want original report", i+1, body)
		}
	}
}

func TestTerminalReportReplaysAfterDaemonRestart(t *testing.T) {
	cfg := Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		Profile:        "restart",
		DaemonID:       "daemon-restart",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	report := terminalTaskReport{
		kind:          terminalTaskReportFail,
		taskID:        "task-restart",
		errorMessage:  "provider failed after doing useful work",
		branchName:    "agent/partial-work",
		failureReason: "agent_error.process_failure",
	}

	beforeRestart := New(cfg, logger)
	beforeRestart.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		return errors.New("network unavailable")
	}
	if err := beforeRestart.reportTerminalTask(context.Background(), report); err == nil {
		t.Fatal("terminal report unexpectedly succeeded before restart")
	}

	afterRestart := New(cfg, logger)
	var replayed terminalTaskReport
	afterRestart.terminalReportSend = func(_ context.Context, got terminalTaskReport, schedule []time.Duration) error {
		if schedule != nil {
			t.Fatalf("replay schedule = %v, want one HTTP attempt", schedule)
		}
		replayed = got
		return nil
	}
	pending, delivered := afterRestart.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 1 {
		t.Fatalf("restart replay pending=%d delivered=%d, want 0/1", pending, delivered)
	}
	if !reflect.DeepEqual(replayed, report) {
		t.Fatalf("restart replay = %+v, want %+v", replayed, report)
	}
}

func TestTerminalReportEnqueueFailureStillAttemptsHTTP(t *testing.T) {
	cfg := Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-read-only-queue",
	}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(filepath.Dir(d.terminalReports.dir), 0o700); err != nil {
		t.Fatalf("create queue parent: %v", err)
	}
	// A regular file where the namespace directory must be deterministically
	// exercises an unwritable/unusable outbox even when tests run as root.
	if err := os.WriteFile(d.terminalReports.dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create invalid queue path: %v", err)
	}

	var calls atomic.Int32
	d.terminalReportSend = func(_ context.Context, got terminalTaskReport, _ []time.Duration) error {
		calls.Add(1)
		if got.taskID != "task-online" || got.output != "deliver me" {
			t.Fatalf("direct report = %+v", got)
		}
		return nil
	}
	if err := d.reportTerminalTask(context.Background(), terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-online", output: "deliver me",
	}); err != nil {
		t.Fatalf("online delivery failed because enqueue failed: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("HTTP attempts = %d, want 1 despite enqueue failure", got)
	}
}

func TestTerminalReportPermanentRejectionQuarantinesOriginalAndStopsReplay(t *testing.T) {
	d := New(Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-quarantine",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	base := time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC)
	now := base
	d.terminalReportNow = func() time.Time { return now }
	report := terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-rejected", output: "original successful answer",
		branchName: "agent/original", sessionID: "session-original",
	}
	var completeCalls, fallbackCalls atomic.Int32
	d.terminalReportSend = func(_ context.Context, got terminalTaskReport, _ []time.Duration) error {
		switch got.kind {
		case terminalTaskReportComplete:
			completeCalls.Add(1)
			return &requestError{Method: http.MethodPost, Path: "/complete", StatusCode: http.StatusForbidden, Body: "forbidden"}
		case terminalTaskReportFail:
			fallbackCalls.Add(1)
			return nil
		default:
			t.Fatalf("unexpected terminal report kind %d", got.kind)
			return nil
		}
	}

	if err := d.reportTerminalTask(context.Background(), report); err == nil {
		t.Fatal("permanently rejected completion unexpectedly succeeded")
	}
	now = base.Add(5 * time.Minute)
	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 1 || delivered != 0 {
		t.Fatalf("second rejection replay = pending:%d delivered:%d, want 1/0", pending, delivered)
	}
	now = base.Add(terminalReportPermanentRejectionAge)
	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 0 || delivered != 0 {
		t.Fatalf("quarantine replay = pending:%d delivered:%d, want 0/0", pending, delivered)
	}
	if got := completeCalls.Load(); got != terminalReportPermanentRejectionLimit {
		t.Fatalf("completion attempts = %d, want %d", got, terminalReportPermanentRejectionLimit)
	}
	if got := fallbackCalls.Load(); got != 1 {
		t.Fatalf("failure compensation attempts = %d, want 1", got)
	}

	stats, err := d.terminalReports.stats()
	if err != nil {
		t.Fatalf("terminal report stats: %v", err)
	}
	if stats.PendingCount != 0 || stats.FailedCount != 1 || stats.FailedBytes == 0 {
		t.Fatalf("queue stats = %+v, want one non-empty failed record", stats)
	}
	body, err := os.ReadFile(filepath.Join(d.terminalReports.failedDir(), terminalReportFileName(report.taskID)))
	if err != nil {
		t.Fatalf("read failed terminal report: %v", err)
	}
	record, err := decodePersistedTerminalReport(body)
	if err != nil {
		t.Fatalf("decode failed terminal report: %v", err)
	}
	got, err := record.terminalReport()
	if err != nil {
		t.Fatalf("validate failed terminal report: %v", err)
	}
	if !reflect.DeepEqual(got, report) {
		t.Fatalf("quarantined payload = %+v, want original %+v", got, report)
	}
	if record.PermanentRejectionCount != terminalReportPermanentRejectionLimit || record.QuarantinedAt == nil {
		t.Fatalf("quarantine metadata = %+v", record)
	}
	// failed/ is not part of list(), so another replay pass cannot hot-loop it.
	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 0 || delivered != 0 {
		t.Fatalf("post-quarantine replay = pending:%d delivered:%d, want 0/0", pending, delivered)
	}
}

func TestTerminalReportPermanentRejectionClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"bad request", &requestError{StatusCode: http.StatusBadRequest}, true},
		{"forbidden", &requestError{StatusCode: http.StatusForbidden}, true},
		{"semantic missing task", &requestError{StatusCode: http.StatusNotFound, Body: "task not found"}, true},
		{"generic missing route", &requestError{StatusCode: http.StatusNotFound, Body: "not found"}, false},
		{"expired auth", &requestError{StatusCode: http.StatusUnauthorized}, false},
		{"rate limited", &requestError{StatusCode: http.StatusTooManyRequests}, false},
		{"conflict", &requestError{StatusCode: http.StatusConflict}, false},
		{"server failure", &requestError{StatusCode: http.StatusBadGateway}, false},
		{"transport", errors.New("connection reset"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, got := terminalReportPermanentRejection(tc.err)
			if got != tc.want {
				t.Fatalf("classification = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestTerminalReportForegroundAndReplayDoNotSendConcurrently(t *testing.T) {
	d := New(Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-in-flight",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task-race", output: "once"}
	if err := d.terminalReports.enqueue(report); err != nil {
		t.Fatalf("seed pending report: %v", err)
	}

	started := make(chan struct{})
	releaseSend := make(chan struct{})
	var calls atomic.Int32
	d.terminalReportSend = func(_ context.Context, _ terminalTaskReport, _ []time.Duration) error {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-releaseSend
		return nil
	}
	foregroundDone := make(chan error, 1)
	go func() { foregroundDone <- d.reportTerminalTask(context.Background(), report) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("foreground terminal send did not start")
	}

	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 1 || delivered != 0 {
		t.Fatalf("racing replay = pending:%d delivered:%d, want 1/0", pending, delivered)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent terminal sends = %d, want exactly one in flight", got)
	}
	close(releaseSend)
	if err := <-foregroundDone; err != nil {
		t.Fatalf("foreground terminal report: %v", err)
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 0 {
		t.Fatalf("pending reports after foreground ack = %d, %v", len(items), err)
	}
}

func TestTerminalReportCorruptRecordRemainsVisibleAcrossReplayPasses(t *testing.T) {
	d := New(Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-corrupt",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := d.terminalReports.ensureDir(); err != nil {
		t.Fatalf("prepare terminal report queue: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d.terminalReports.dir, "corrupt.json"), []byte("private-corrupt-payload"), 0o600); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}
	for pass := 1; pass <= 2; pass++ {
		if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 1 || delivered != 0 {
			t.Fatalf("replay pass %d = pending:%d delivered:%d, want 1/0", pass, pending, delivered)
		}
	}
	stats, err := d.terminalReports.stats()
	if err != nil {
		t.Fatalf("terminal report stats: %v", err)
	}
	if stats.PendingCount != 1 || stats.PendingBytes == 0 {
		t.Fatalf("corrupt record stats = %+v", stats)
	}
}

func TestTerminalReportFutureVersionIsRetainedAcrossDowngrade(t *testing.T) {
	store := newTerminalReportStore(Config{
		ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "older-daemon",
	})
	if err := store.ensureDir(); err != nil {
		t.Fatalf("prepare terminal report queue: %v", err)
	}
	report := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "future-task", output: "future payload"}
	record, err := persistedTerminalReport(report, time.Now())
	if err != nil {
		t.Fatalf("build terminal report: %v", err)
	}
	record.Version = terminalReportRecordVersion + 1
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal future report: %v", err)
	}
	path := filepath.Join(store.dir, terminalReportFileName(report.taskID))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write future report: %v", err)
	}
	for pass := 1; pass <= 2; pass++ {
		items, listErr := store.list()
		if listErr == nil || !strings.Contains(listErr.Error(), "unsupported terminal report version") {
			t.Fatalf("list pass %d error = %v, want unsupported-version warning", pass, listErr)
		}
		if len(items) != 0 {
			t.Fatalf("list pass %d replayed future-version record: %+v", pass, items)
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("future-version record removed on pass %d: %v", pass, readErr)
		}
		if string(got) != string(body) {
			t.Fatalf("future-version record changed on pass %d", pass)
		}
	}
}

func TestTerminalReportFindsOtherNamespacesWithoutAdoptingThem(t *testing.T) {
	cfg := Config{ServerBaseURL: "https://api.example.test", WorkspacesRoot: t.TempDir(), DaemonID: "current"}
	store := newTerminalReportStore(cfg)
	otherDir := filepath.Join(store.root, "different-identity")
	if err := os.MkdirAll(filepath.Join(otherDir, "failed"), 0o700); err != nil {
		t.Fatalf("create other namespace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "old.json"), []byte("pending"), 0o600); err != nil {
		t.Fatalf("write other pending record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "failed", "old.json"), []byte("failed"), 0o600); err != nil {
		t.Fatalf("write other failed record: %v", err)
	}
	namespaces, err := store.otherNamespaceStats()
	if err != nil {
		t.Fatalf("scan namespaces: %v", err)
	}
	if len(namespaces) != 1 || namespaces[0].name != "different-identity" ||
		namespaces[0].stats.PendingCount != 1 || namespaces[0].stats.FailedCount != 1 {
		t.Fatalf("other namespace stats = %+v", namespaces)
	}
	if items, err := store.list(); err != nil || len(items) != 0 {
		t.Fatalf("current namespace adopted other reports: %d, %v", len(items), err)
	}
}

func TestRunBatchPollerReleasesSlotAfterTerminalRetryExhaustion(t *testing.T) {
	defer noSleepRetry(t)()
	previousSchedule := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{time.Nanosecond, time.Nanosecond}
	t.Cleanup(func() { defaultTerminalRetrySchedule = previousSchedule })

	var completeAttempts atomic.Int32
	var secondServed atomic.Bool
	secondStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(req.URL.Path, "/api/daemon/tasks/claim"):
			switch {
			case completeAttempts.Load() == 0:
				_, _ = w.Write([]byte(`{"tasks":[{"id":"t1","runtime_id":"rt-1","issue_id":"i1"}]}`))
			case secondServed.CompareAndSwap(false, true):
				_, _ = w.Write([]byte(`{"tasks":[{"id":"t2","runtime_id":"rt-1","issue_id":"i2"}]}`))
			default:
				_, _ = w.Write([]byte(`{"tasks":[]}`))
			}
		case strings.HasSuffix(req.URL.Path, "/tasks/t1/complete"):
			completeAttempts.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		case strings.HasSuffix(req.URL.Path, "/tasks/t2/complete"):
			w.WriteHeader(http.StatusOK)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:      srv.URL,
		WorkspacesRoot:     t.TempDir(),
		DaemonID:           "daemon-slot",
		PollInterval:       time.Hour,
		MaxConcurrentTasks: 1,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1"}}
	d.runtimeIndex["rt-1"] = Runtime{ID: "rt-1"}
	d.cancelPollInterval = time.Hour
	d.taskSlotWait = 20 * time.Millisecond
	d.runner = taskRunnerFunc(func(_ context.Context, task Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		if task.ID == "t2" {
			close(secondStarted)
		}
		return TaskResult{Status: "completed"}, nil
	})

	sem := newTaskSlotSemaphore(1)
	wakeup := make(chan struct{}, 1)
	var taskWG sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		d.runBatchPoller(ctx, ctx, sem, wakeup, &taskWG)
	}()

	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		cancel()
		<-pollDone
		t.Fatalf("second task did not start after first report exhausted retries; attempts=%d", completeAttempts.Load())
	}
	if got, want := completeAttempts.Load(), int32(len(defaultTerminalRetrySchedule)+1); got != want {
		t.Fatalf("first terminal callback attempts = %d, want %d", got, want)
	}
	cancel()
	<-pollDone
	taskWG.Wait()
	items, err := d.terminalReports.list()
	if err != nil {
		t.Fatalf("list pending reports: %v", err)
	}
	if len(items) != 1 || items[0].report.taskID != "t1" {
		t.Fatalf("pending reports = %+v, want only exhausted task t1", items)
	}
}
