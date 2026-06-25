package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
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
