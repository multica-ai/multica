package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testOutboxDaemon(t *testing.T, baseURL string) *Daemon {
	t.Helper()
	return &Daemon{
		client:         NewClient(baseURL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		terminalOutbox: newTerminalOutbox(t.TempDir()),
	}
}

// noTerminalBackoff collapses the client's in-process retry schedule so these
// tests exercise the durable layer rather than sitting through ~124s of
// backoff. The schedule itself is covered by the client's own tests.
func noTerminalBackoff(t *testing.T) {
	t.Helper()
	original := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{}
	t.Cleanup(func() { defaultTerminalRetrySchedule = original })
}

func TestTerminalOutbox_PersistListRemove(t *testing.T) {
	o := newTerminalOutbox(t.TempDir())

	if got, err := o.list(); err != nil || len(got) != 0 {
		t.Fatalf("empty outbox: got %d reports, err %v", len(got), err)
	}

	report := terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-1",
		output: "the agent's answer", branchName: "fix/thing",
		sessionID: "sess-1", workDir: "/w", durableWorkDir: "/d",
		retiredSessionID: "old-sess", sessionRolloutMissing: true,
	}
	if err := o.persist(report); err != nil {
		t.Fatal(err)
	}

	got, err := o.list()
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d reports, err %v, want 1", len(got), err)
	}
	// Every field must survive the round trip: this payload is the agent's only
	// surviving output once the in-memory result is discarded.
	if rt := got[0].report(); rt != report {
		t.Fatalf("round trip lost data:\n got %+v\nwant %+v", rt, report)
	}

	if err := o.remove("task-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := o.list(); err != nil || len(got) != 0 {
		t.Fatalf("after remove: got %d reports, err %v", len(got), err)
	}
	// Removing an absent entry is normal — success and drain both call it.
	if err := o.remove("task-1"); err != nil {
		t.Fatalf("removing a missing report should be a no-op, got %v", err)
	}
}

// One task has exactly one terminal outcome, so a later report replaces an
// earlier one rather than queueing behind it.
func TestTerminalOutbox_LaterReportReplacesEarlier(t *testing.T) {
	o := newTerminalOutbox(t.TempDir())
	if err := o.persist(terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task-1", output: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := o.persist(terminalTaskReport{kind: terminalTaskReportFail, taskID: "task-1", errorMessage: "second"}); err != nil {
		t.Fatal(err)
	}
	got, err := o.list()
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d reports, err %v, want 1", len(got), err)
	}
	if got[0].Kind != terminalTaskReportFail || got[0].ErrorMessage != "second" {
		t.Fatalf("got %+v, want the later fail report", got[0])
	}
}

func TestTerminalOutbox_SkipsUnreadableEntries(t *testing.T) {
	dir := t.TempDir()
	o := newTerminalOutbox(dir)
	if err := o.persist(terminalTaskReport{kind: terminalTaskReportComplete, taskID: "good"}); err != nil {
		t.Fatal(err)
	}
	// A truncated file from a crash mid-write must not block the queue.
	if err := os.WriteFile(filepath.Join(o.dir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := o.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TaskID != "good" {
		t.Fatalf("got %+v, want only the readable report", got)
	}
	if _, err := os.Stat(filepath.Join(o.dir, "corrupt.json")); !os.IsNotExist(err) {
		t.Fatal("an unparseable entry should be discarded, not retried forever")
	}
}

func TestTerminalOutbox_RejectsUnsafeTaskID(t *testing.T) {
	o := newTerminalOutbox(t.TempDir())
	for _, id := range []string{"", "  ", "..", ".", "../escape", `sub\escape`, "a/b"} {
		if err := o.persist(terminalTaskReport{kind: terminalTaskReportComplete, taskID: id}); err == nil {
			t.Fatalf("persist(%q) should be refused as a filename", id)
		}
	}
}

// TestReportTerminalTask_QueuesTransientFailure is the GH #8221 shape: the run
// finished, the callback could not be delivered, and before this the report was
// dropped in memory and the row stayed 'running' forever.
func TestReportTerminalTask_QueuesTransientFailure(t *testing.T) {
	noTerminalBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d := testOutboxDaemon(t, srv.URL)
	err := d.reportTerminalTask(context.Background(), terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-1", output: "finished work",
	})
	if err == nil {
		t.Fatal("expected the delivery failure to be reported to the caller")
	}

	queued, listErr := d.terminalOutbox.list()
	if listErr != nil || len(queued) != 1 {
		t.Fatalf("got %d queued reports, err %v, want 1", len(queued), listErr)
	}
	if queued[0].Output != "finished work" {
		t.Fatalf("queued output = %q, want the agent's result preserved", queued[0].Output)
	}
}

// A 4xx is the server refusing this report on its merits. Replaying it would
// only reproduce the refusal, and the caller already downgrades a rejected
// completion to a fail report that gets its own durability.
func TestReportTerminalTask_DoesNotQueuePermanentRejection(t *testing.T) {
	noTerminalBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadRequest)
	}))
	defer srv.Close()

	d := testOutboxDaemon(t, srv.URL)
	if err := d.reportTerminalTask(context.Background(), terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-1",
	}); err == nil {
		t.Fatal("expected the rejection to reach the caller")
	}
	queued, err := d.terminalOutbox.list()
	if err != nil || len(queued) != 0 {
		t.Fatalf("got %d queued reports, err %v, want 0", len(queued), err)
	}
}

// A report that succeeds on a later in-process attempt must not leave a queued
// copy behind, or the drain would replay an outcome that already landed.
func TestReportTerminalTask_ClearsQueueOnSuccess(t *testing.T) {
	noTerminalBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := testOutboxDaemon(t, srv.URL)
	if err := d.terminalOutbox.persist(terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.reportTerminalTask(context.Background(), terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-1",
	}); err != nil {
		t.Fatal(err)
	}
	queued, err := d.terminalOutbox.list()
	if err != nil || len(queued) != 0 {
		t.Fatalf("got %d queued reports, err %v, want 0", len(queued), err)
	}
}

// TestDrainTerminalOutbox_ReplaysAfterRecovery is the end-to-end recovery: the
// report is queued while the server is unreachable, then delivered intact on
// the first poll after connectivity returns.
func TestDrainTerminalOutbox_ReplaysAfterRecovery(t *testing.T) {
	noTerminalBackoff(t)
	var down atomic.Bool
	down.Store(true)
	var delivered atomic.Int32
	var gotPath, gotOutput atomic.Value
	gotPath.Store("")
	gotOutput.Store("")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "partitioned", http.StatusServiceUnavailable)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		json.Unmarshal(body, &parsed)
		if out, ok := parsed["output"].(string); ok {
			gotOutput.Store(out)
		}
		gotPath.Store(r.URL.Path)
		delivered.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := testOutboxDaemon(t, srv.URL)
	ctx := context.Background()

	if err := d.reportTerminalTask(ctx, terminalTaskReport{
		kind: terminalTaskReportComplete, taskID: "task-1", output: "the deliverable",
	}); err == nil {
		t.Fatal("expected delivery to fail while the server is down")
	}

	// Draining during the outage must keep the report, not discard it.
	d.drainTerminalOutbox(ctx)
	if queued, _ := d.terminalOutbox.list(); len(queued) != 1 {
		t.Fatalf("got %d queued reports during outage, want 1 (it must not be dropped)", len(queued))
	}
	if delivered.Load() != 0 {
		t.Fatalf("delivered %d reports during the outage, want 0", delivered.Load())
	}

	down.Store(false)
	d.drainTerminalOutbox(ctx)

	if delivered.Load() != 1 {
		t.Fatalf("delivered %d reports after recovery, want 1", delivered.Load())
	}
	if got := gotPath.Load().(string); got != "/api/daemon/tasks/task-1/complete" {
		t.Fatalf("replayed to %q, want the complete callback", got)
	}
	if got := gotOutput.Load().(string); got != "the deliverable" {
		t.Fatalf("replayed output = %q, want the agent's result", got)
	}
	if queued, _ := d.terminalOutbox.list(); len(queued) != 0 {
		t.Fatalf("got %d queued reports after delivery, want 0", len(queued))
	}

	// A second drain must not re-send: the queue is the only thing preventing a
	// duplicate, since the server accepts replays idempotently and would not
	// complain.
	d.drainTerminalOutbox(ctx)
	if delivered.Load() != 1 {
		t.Fatalf("delivered %d reports after a second drain, want 1", delivered.Load())
	}
}

func TestDrainTerminalOutbox_DropsPermanentlyRejected(t *testing.T) {
	noTerminalBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone for good", http.StatusBadRequest)
	}))
	defer srv.Close()

	d := testOutboxDaemon(t, srv.URL)
	if err := d.terminalOutbox.persist(terminalTaskReport{kind: terminalTaskReportFail, taskID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	d.drainTerminalOutbox(context.Background())

	queued, err := d.terminalOutbox.list()
	if err != nil || len(queued) != 0 {
		t.Fatalf("got %d queued reports, err %v, want 0 — a refused report must not be retried forever", len(queued), err)
	}
}

func TestDrainTerminalOutbox_AbandonsExpiredReports(t *testing.T) {
	noTerminalBackoff(t)
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	d := testOutboxDaemon(t, srv.URL)
	stale := newPendingTerminalReport(terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task-1"})
	stale.QueuedAt = time.Now().Add(-terminalOutboxMaxAge - time.Hour)
	payload, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(d.terminalOutbox.dir, terminalOutboxDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.terminalOutbox.dir, "task-1.json"), payload, terminalOutboxFileMode); err != nil {
		t.Fatal(err)
	}

	d.drainTerminalOutbox(context.Background())

	if called.Load() != 0 {
		t.Fatalf("sent %d expired reports, want 0", called.Load())
	}
	if queued, _ := d.terminalOutbox.list(); len(queued) != 0 {
		t.Fatalf("got %d queued reports, want 0 — an expired report must not grow the queue forever", len(queued))
	}
}
