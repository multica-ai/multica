package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnqueuePendingResult_PersistsToDisk verifies that enqueuePendingResult
// correctly creates a valid JSON task record on disk and non-blockingly notifies
// the retrier loop.
func TestEnqueuePendingResult_PersistsToDisk(t *testing.T) {
	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:                Config{WorkspacesRoot: wsRoot},
		logger:             captureLogger(&buf),
		pendingQueueNotify: make(chan struct{}, 1),
	}

	res := TaskResult{
		Status:     "completed",
		Comment:    "done output",
		BranchName: "feat/test",
		SessionID:  "sess_123",
		WorkDir:    "/work",
	}

	d.enqueuePendingResult("task_abc", res)

	targetFile := filepath.Join(wsRoot, ".pending_results", "task_abc.json")
	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("expected pending record file on disk, got error: %v", err)
	}

	var rec pendingTaskRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("failed to decode pending record JSON: %v", err)
	}

	if rec.TaskID != "task_abc" || rec.Result.Comment != "done output" {
		t.Fatalf("unexpected record content: %+v", rec)
	}

	select {
	case <-d.pendingQueueNotify:
	default:
		t.Fatal("expected pendingQueueNotify channel to be signaled")
	}
}

// TestFlushPendingQueue_RetriesAndRemovesOnSuccess verifies that flushing the
// queue sends buffered task results to the server and permanently deletes the disk
// record upon a successful 200 OK response.
func TestFlushPendingQueue_RetriesAndRemovesOnSuccess(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/daemon/tasks/task_abc/complete" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:    Config{WorkspacesRoot: wsRoot},
		client: NewClient(srv.URL),
		logger: captureLogger(&buf),
	}

	res := TaskResult{Status: "completed", Comment: "done"}
	d.enqueuePendingResult("task_abc", res)

	d.flushPendingQueue(context.Background())

	if called.Load() == 0 {
		t.Fatal("expected server to be called during flush")
	}

	targetFile := filepath.Join(wsRoot, ".pending_results", "task_abc.json")
	if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
		t.Fatalf("expected pending file to be deleted after successful flush, stat error: %v", err)
	}
}

// TestFlushPendingQueue_KeepsFileOnTransientError verifies that transient server
// errors (e.g. 503 Service Unavailable) preserve the buffered task record on disk
// so it can be retried on a subsequent tick.
func TestFlushPendingQueue_KeepsFileOnTransientError(t *testing.T) {
	origSchedule := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = origSchedule[:1] // use single short retry
	defer func() { defaultTerminalRetrySchedule = origSchedule }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"gateway timeout"}`))
	}))
	t.Cleanup(srv.Close)

	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:    Config{WorkspacesRoot: wsRoot},
		client: NewClient(srv.URL),
		logger: captureLogger(&buf),
	}

	res := TaskResult{Status: "completed", Comment: "done"}
	d.enqueuePendingResult("task_abc", res)

	d.flushPendingQueue(context.Background())

	targetFile := filepath.Join(wsRoot, ".pending_results", "task_abc.json")
	if _, err := os.Stat(targetFile); err != nil {
		t.Fatalf("expected pending file to remain on disk after transient 503 error, got: %v", err)
	}
}

// TestFlushPendingQueue_DiscardsFileOnPermanentError verifies that permanent
// client errors (e.g. 400 Bad Request or 404 Task Not Found) permanently discard
// the disk record to prevent infinite retry loops.
func TestFlushPendingQueue_DiscardsFileOnPermanentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad output format"}`))
	}))
	t.Cleanup(srv.Close)

	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:    Config{WorkspacesRoot: wsRoot},
		client: NewClient(srv.URL),
		logger: captureLogger(&buf),
	}

	res := TaskResult{Status: "completed", Comment: "done"}
	d.enqueuePendingResult("task_abc", res)

	d.flushPendingQueue(context.Background())

	targetFile := filepath.Join(wsRoot, ".pending_results", "task_abc.json")
	if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
		t.Fatalf("expected pending file to be deleted after permanent 400 error, got: %v", err)
	}
}

// TestEnqueuePendingResult_Permissions verifies that the buffered record and
// directory are created with owner-only permissions (0700 dir, 0600 file).
func TestEnqueuePendingResult_Permissions(t *testing.T) {
	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:                Config{WorkspacesRoot: wsRoot},
		logger:             captureLogger(&buf),
		pendingQueueNotify: make(chan struct{}, 1),
	}

	res := TaskResult{Status: "completed", Comment: "secret data"}
	d.enqueuePendingResult("task_perm", res)

	dirPath := filepath.Join(wsRoot, ".pending_results")
	filePath := filepath.Join(dirPath, "task_perm.json")

	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("stat dir error: %v", err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat file error: %v", err)
	}

	if runtime.GOOS != "windows" {
		if dirInfo.Mode().Perm() != 0700 {
			t.Errorf("expected dir permissions 0700, got %v", dirInfo.Mode().Perm())
		}
		if fileInfo.Mode().Perm() != 0600 {
			t.Errorf("expected file permissions 0600, got %v", fileInfo.Mode().Perm())
		}
	}
}

// TestFlushPendingQueue_FailPath verifies that failed task terminal states
// are retried via FailTask and removed on success.
func TestFlushPendingQueue_FailPath(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/daemon/tasks/task_fail/fail" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode error: %v", err)
		}
		if reqBody["failure_reason"] != "process_failure" {
			t.Errorf("expected failure_reason process_failure, got %v", reqBody["failure_reason"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:    Config{WorkspacesRoot: wsRoot},
		client: NewClient(srv.URL),
		logger: captureLogger(&buf),
	}

	res := TaskResult{Status: "failed", Comment: "crash", FailureReason: "process_failure"}
	d.enqueuePendingResult("task_fail", res)

	d.flushPendingQueue(context.Background())

	if called.Load() == 0 {
		t.Fatal("expected server to be called during flush for failed task")
	}

	targetFile := filepath.Join(wsRoot, ".pending_results", "task_fail.json")
	if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
		t.Fatalf("expected pending file to be deleted after successful fail flush, got: %v", err)
	}
}

// TestPrunePendingQueue_RetentionAndTmp verifies cleanup of orphaned tmp files,
// expired records, and max count eviction.
func TestPrunePendingQueue_RetentionAndTmp(t *testing.T) {
	wsRoot := t.TempDir()
	dirPath := filepath.Join(wsRoot, ".pending_results")
	if err := os.MkdirAll(dirPath, 0700); err != nil {
		t.Fatalf("mkdir error: %v", err)
	}

	now := time.Now()
	oldTime := now.Add(-8 * 24 * time.Hour)
	oldTmpTime := now.Add(-2 * time.Hour)

	oldJson := filepath.Join(dirPath, "old.json")
	_ = os.WriteFile(oldJson, []byte("{}"), 0600)
	_ = os.Chtimes(oldJson, oldTime, oldTime)

	oldTmp := filepath.Join(dirPath, "orphaned.json.tmp")
	_ = os.WriteFile(oldTmp, []byte("{}"), 0600)
	_ = os.Chtimes(oldTmp, oldTmpTime, oldTmpTime)

	var buf bytes.Buffer
	d := &Daemon{
		cfg:    Config{WorkspacesRoot: wsRoot},
		logger: captureLogger(&buf),
	}

	d.prunePendingQueue()

	if _, err := os.Stat(oldJson); !os.IsNotExist(err) {
		t.Errorf("expected old.json to be pruned")
	}
	if _, err := os.Stat(oldTmp); !os.IsNotExist(err) {
		t.Errorf("expected orphaned.json.tmp to be pruned")
	}
}

// TestReportTaskResult_EnqueuesOnFailure verifies that failure terminal states
// (default branch of reportTaskResult) enqueue to disk on transient reporting error.
func TestReportTaskResult_EnqueuesOnFailure(t *testing.T) {
	origSchedule := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{time.Nanosecond}
	defer func() { defaultTerminalRetrySchedule = origSchedule }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"temporarily down"}`))
	}))
	t.Cleanup(srv.Close)

	wsRoot := t.TempDir()
	var buf bytes.Buffer
	d := &Daemon{
		cfg:                Config{WorkspacesRoot: wsRoot},
		client:             NewClient(srv.URL),
		logger:             captureLogger(&buf),
		pendingQueueNotify: make(chan struct{}, 1),
	}

	res := TaskResult{Status: "failed", Comment: "runtime crash"}
	d.reportTaskResult(context.Background(), "task_report_fail", res, d.logger)

	targetFile := filepath.Join(wsRoot, ".pending_results", "task_report_fail.json")
	if _, err := os.Stat(targetFile); err != nil {
		t.Fatalf("expected failed task result to be enqueued to disk on 503 error, got: %v", err)
	}
}
