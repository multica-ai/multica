package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A generation-aware report must be settled only by a replica that enforces the
// generation fence. Heartbeat capability cannot prove that: heartbeat and
// terminal callbacks are independent requests, and behind a load balancer they
// can land on different replicas. The proof therefore has to be the request
// itself, which is why generation-aware reports use the versioned terminal route
// and never fall back to the legacy one.

// TestTerminalReportRoutingIsStructural pins the endpoint choice: a report with
// a claim generation can only go to the versioned route, and a legacy report can
// only go to the legacy route. Nothing else may influence it — in particular not
// the last heartbeat, which says nothing about the replica that will answer.
func TestTerminalReportRoutingIsStructural(t *testing.T) {
	tests := []struct {
		name            string
		taskID          string
		action          string
		generationAware bool
		want            string
	}{
		{name: "complete legacy", taskID: "t1", action: "complete", want: "/api/daemon/tasks/t1/complete"},
		{name: "fail legacy", taskID: "t1", action: "fail", want: "/api/daemon/tasks/t1/fail"},
		{name: "complete fenced", taskID: "t1", action: "complete", generationAware: true, want: "/api/daemon/v2/tasks/t1/complete"},
		{name: "fail fenced", taskID: "t1", action: "fail", generationAware: true, want: "/api/daemon/v2/tasks/t1/fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalTaskPath(tt.taskID, tt.action, tt.generationAware); got != tt.want {
				t.Fatalf("terminalTaskPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// mixedReplicaServer answers the fenced requests in the given order — an old
// replica has no such route — and every later one as a fence-capable replica.
// Responses are per attempt, so responses[0] is what attempt 1 sees.
func mixedReplicaServer(t *testing.T, responses ...int) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var fencedCalls, legacyCalls atomic.Int32
	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.Contains(req.URL.Path, "/api/daemon/v2/") {
			legacyCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		fencedCalls.Add(1)
		call := int(attempt.Add(1)) - 1
		if call < len(responses) {
			w.WriteHeader(responses[call])
			if responses[call] == http.StatusNotFound {
				_, _ = w.Write([]byte("404 page not found\n"))
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &fencedCalls, &legacyCalls
}

// TestTerminalReportReplaySurvivesMixedReplicaDeployment is the mixed-replica
// convergence test: the first attempt lands on an old replica and must leave the
// report exactly where it was; the second reaches a new replica and settles it.
func TestTerminalReportReplaySurvivesMixedReplicaDeployment(t *testing.T) {
	srv, fencedCalls, legacyCalls := mixedReplicaServer(t, http.StatusNotFound)
	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-mixed-replica",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := generationReport("task-mixed", testClaimGeneration(), "fenced answer")
	if err := d.terminalReports.enqueue(report); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Attempt 1: the old replica has no versioned route.
	pending, delivered := d.replayPendingTerminalReports(context.Background())
	if pending != 1 || delivered != 0 {
		t.Fatalf("first attempt pending=%d delivered=%d, want the report retained", pending, delivered)
	}
	if legacyCalls.Load() != 0 {
		t.Fatalf("legacy callbacks = %d, want 0: a fenced report must never fall back", legacyCalls.Load())
	}
	if got := fencedCalls.Load(); got != 1 {
		t.Fatalf("fenced requests after the old replica = %d, want exactly one attempt", got)
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 1 {
		t.Fatalf("queue after the old replica = %d records (%v), want the report", len(items), err)
	}
	entries, err := os.ReadDir(d.terminalReports.failedDir())
	if err == nil && len(entries) != 0 {
		t.Fatalf("failed queue = %v, want nothing: an unsupported route is not a rejection", entries)
	}
	// An unsupported route is not a rejection, so nothing may be compensated
	// either: the legacy route would have been hit for that.
	if got := legacyCalls.Load(); got != 0 {
		t.Fatalf("compensation or fallback requests = %d, want 0", got)
	}

	// Attempt 2: the new replica enforces the fence.
	pending, delivered = d.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 1 {
		t.Fatalf("second attempt pending=%d delivered=%d, want 0/1", pending, delivered)
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 0 {
		t.Fatalf("queue after the new replica = %d records (%v), want the report acknowledged", len(items), err)
	}
	if legacyCalls.Load() != 0 {
		t.Fatalf("legacy callbacks after convergence = %d, want 0", legacyCalls.Load())
	}
	if got := fencedCalls.Load(); got != 2 {
		t.Fatalf("fenced requests = %d, want one per attempt", got)
	}
}

func TestCodeLessJSON404StaysPendingUntilFencedReplicaIsAvailable(t *testing.T) {
	var fencedCalls, legacyCalls atomic.Int32
	var oldReplica atomic.Bool
	oldReplica.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.Contains(req.URL.Path, "/api/daemon/v2/") {
			legacyCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		fencedCalls.Add(1)
		if oldReplica.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"task not found"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-codeless-v2-404",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	base := time.Now().UTC()
	now := base
	d.terminalReportNow = func() time.Time { return now }
	report := generationReport("task-codeless-v2-404", testClaimGeneration(), "fenced answer")
	if err := d.terminalReports.enqueue(report); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	for _, elapsed := range []time.Duration{0, 6 * time.Minute, 12 * time.Minute} {
		now = base.Add(elapsed)
		pending, delivered := d.replayPendingTerminalReports(context.Background())
		if pending != 1 || delivered != 0 {
			t.Fatalf("old replica replay at %v = pending:%d delivered:%d, want 1/0", elapsed, pending, delivered)
		}
	}
	stats, err := d.terminalReports.stats()
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}
	if stats.PendingCount != 1 || stats.FailedCount != 0 {
		t.Fatalf("queue stats after code-less 404s = %+v, want one pending and none failed", stats)
	}
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 1 {
		t.Fatalf("pending reports = %d (%v), want one", len(items), err)
	}
	body, err := os.ReadFile(filepath.Join(d.terminalReports.dir, items[0].fileName))
	if err != nil {
		t.Fatalf("read pending report: %v", err)
	}
	record, err := decodePersistedTerminalReport(body)
	if err != nil {
		t.Fatalf("decode pending report: %v", err)
	}
	if record.PermanentRejectionCount != 0 || record.QuarantinedAt != nil {
		t.Fatalf("code-less 404 rejection metadata = %+v, want no rejection or quarantine", record)
	}
	if got := fencedCalls.Load(); got != 3 {
		t.Fatalf("fenced requests to old replica = %d, want 3", got)
	}
	if got := legacyCalls.Load(); got != 0 {
		t.Fatalf("legacy requests while v2 route is unsupported = %d, want 0", got)
	}

	oldReplica.Store(false)
	now = base.Add(13 * time.Minute)
	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 0 || delivered != 1 {
		t.Fatalf("supported replica replay = pending:%d delivered:%d, want 0/1", pending, delivered)
	}
	if got := legacyCalls.Load(); got != 0 {
		t.Fatalf("legacy fallback after v2 success = %d, want 0", got)
	}
}

// TestGenerationAwareForegroundReportNeverFallsBackToLegacy is the mandatory
// no-fallback test on first delivery: the fenced route answers 404 while the
// legacy route would happily accept the report. The daemon must count exactly
// one v2 request, zero legacy requests, and keep the report durable.
func TestGenerationAwareForegroundReportNeverFallsBackToLegacy(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		paths = append(paths, req.URL.Path)
		mu.Unlock()
		if strings.Contains(req.URL.Path, "/api/daemon/v2/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 page not found\n"))
			return
		}
		// The legacy route is still there on this replica and would accept the
		// report as an unfenced callback. Reaching it is the bug.
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-no-fallback",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := d.reportTerminalTask(context.Background(), generationReport("task-no-fallback", testClaimGeneration(), "answer"))
	if err == nil {
		t.Fatal("generation-aware report reported success against a replica without the fenced route")
	}

	mu.Lock()
	defer mu.Unlock()
	var fenced, legacy int
	for _, path := range paths {
		if strings.Contains(path, "/api/daemon/v2/") {
			fenced++
		} else {
			legacy++
		}
	}
	if fenced != 1 {
		t.Fatalf("fenced requests = %d, want exactly one (%v)", fenced, paths)
	}
	if legacy != 0 {
		t.Fatalf("legacy requests = %d, want 0 (%v): no fallback from the fenced route", legacy, paths)
	}
	if items, listErr := d.terminalReports.list(); listErr != nil || len(items) != 1 {
		t.Fatalf("queue = %d records (%v), want the report retained for a later replica", len(items), listErr)
	}
}

// TestRealTaskNotFoundOnFencedEndpointIsSemantic pins the other side of the
// classification: a structured task-not-found from a NEW server is a real
// absence, so it keeps the legacy terminal semantics instead of being read as a
// version mismatch.
func TestRealTaskNotFoundOnFencedEndpointIsSemantic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("{\"error\":\"task not found\",\"code\":\"" + protocol.DaemonTaskNotFoundCode + "\"}\n"))
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-v2-not-found",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := generationReport("task-v2-not-found", testClaimGeneration(), "answer")
	sentErr := d.sendTerminalTaskReport(context.Background(), report, nil)
	if sentErr == nil {
		t.Fatal("fenced callback against a missing task reported success")
	}
	if !isTaskNotFoundError(sentErr) {
		t.Fatalf("structured task-not-found was not classified as a semantic absence: %v", sentErr)
	}
	if isFencedTerminalEndpointUnsupported(sentErr) {
		t.Fatalf("structured task-not-found was misread as a missing route: %v", sentErr)
	}
	// And the plain route-level failure is the opposite classification.
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found\n"))
	}))
	t.Cleanup(plainSrv.Close)
	plainClient := NewClient(plainSrv.URL)
	err := plainClient.CompleteTask(context.Background(), report.taskID, report.output, "", "", "", false, "", "", report.claimGeneration.dispatchedAt)
	if err == nil {
		t.Fatal("plain 404 from the fenced route reported success")
	}
	if !isFencedTerminalEndpointUnsupported(err) {
		t.Fatalf("plain 404 on the fenced route was not classified as a version mismatch: %v", err)
	}
	if isTaskNotFoundError(err) {
		t.Fatalf("plain 404 was misread as a semantic task-not-found: %v", err)
	}
}
