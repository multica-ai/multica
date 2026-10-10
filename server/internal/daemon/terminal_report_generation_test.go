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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The claim generation is the server-issued dispatched_at of the claim that
// produced a terminal result. These tests cover the daemon half of the fence:
// it has to survive the durable queue, reach the wire unchanged, and never be
// invented for a record that does not have one.

func testClaimGeneration() time.Time {
	// Sub-second precision on purpose: the queue must round-trip exactly what
	// the server issued, or the server-side CAS will not recognize its own claim.
	return time.Date(2026, time.September, 19, 4, 43, 58, 123456000, time.UTC)
}

// claimGenerationReport is one terminal report fenced to a known generation.
func claimGenerationReport(taskID string) terminalTaskReport {
	return terminalTaskReport{
		kind:            terminalTaskReportComplete,
		taskID:          taskID,
		claimGeneration: claimGeneration{dispatchedAt: testClaimGeneration()},
		output:          "the original answer",
		branchName:      "agent/fenced",
		sessionID:       "session-fenced",
		workDir:         "/tmp/fenced",
		durableWorkDir:  "/tmp/project",
	}
}

func TestTerminalReportQueuePersistsClaimGeneration(t *testing.T) {
	store := newTerminalReportStore(Config{
		WorkspacesRoot: t.TempDir(),
		ServerBaseURL:  "https://api.example.test",
		DaemonID:       "daemon-generation",
	})
	report := claimGenerationReport("task-generation")
	if err := store.enqueue(report); err != nil {
		t.Fatalf("enqueue terminal report: %v", err)
	}

	items, err := store.list()
	if err != nil {
		t.Fatalf("list terminal reports: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queued reports = %d, want 1", len(items))
	}
	if items[0].report != report {
		t.Fatalf("round-tripped report = %+v, want %+v", items[0].report, report)
	}
	if !items[0].report.claimGeneration.dispatchedAt.Equal(report.claimGeneration.dispatchedAt) {
		t.Fatalf("round-tripped generation = %s, want %s", items[0].report.claimGeneration.dispatchedAt, report.claimGeneration.dispatchedAt)
	}

	// The generation has to be on disk, not just in memory: replay after a
	// restart reads only the file.
	body, err := os.ReadFile(filepath.Join(store.dir, items[0].fileName))
	if err != nil {
		t.Fatalf("read queued record: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode queued record: %v", err)
	}
	raw, ok := decoded["claim_dispatched_at"].(string)
	if !ok {
		t.Fatalf("queued record has no claim_dispatched_at: %s", body)
	}
	stored, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || !stored.Equal(report.claimGeneration.dispatchedAt) {
		t.Fatalf("persisted claim_dispatched_at = %q, want %s", raw, report.claimGeneration.dispatchedAt)
	}

}

// TestTerminalReportLegacyRecordReplaysViaLegacyEndpoint pins the #8533
// compatibility contract: a version-1 record written before the fence keeps
// the legacy replay behavior — it is delivered through the unfenced legacy
// terminal endpoint, not retained forever. The generation fence applies only
// to version-2 records from fence-capable claims.
func TestTerminalReportLegacyRecordReplaysViaLegacyEndpoint(t *testing.T) {
	cfg := Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-legacy-record",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(cfg, logger)

	// A version-1 record written by a daemon that predates the fence: keyed by
	// task id alone, no claim generation, replayed with legacy semantics.
	legacy := persistedTerminalTaskReport{
		Version:   legacyTerminalReportRecordVersion,
		CreatedAt: time.Now().UTC(),
		Kind:      "complete",
		TaskID:    "task-legacy",
		Output:    "legacy answer",
	}
	name := legacyTerminalReportFileName(legacy.TaskID)
	if err := d.terminalReports.ensureDir(); err != nil {
		t.Fatalf("prepare queue: %v", err)
	}
	if err := writeTerminalReportRecord(d.terminalReports.dir, name, legacy); err != nil {
		t.Fatalf("write legacy record: %v", err)
	}

	var delivered atomic.Int32
	d.terminalReportSend = func(_ context.Context, report terminalTaskReport, _ []time.Duration) error {
		if !report.claimGeneration.dispatchedAt.IsZero() || report.claimGeneration.unreadable {
			return errors.New("legacy replay must carry no claim generation")
		}
		delivered.Add(1)
		return nil
	}
	pending, sent := d.replayPendingTerminalReports(context.Background())
	if sent != 1 || delivered.Load() != 1 {
		t.Fatalf("legacy replay: sent=%d delivered=%d, want 1/1 via the legacy endpoint", sent, delivered.Load())
	}
	if pending != 0 {
		t.Fatalf("pending = %d, want 0 after the legacy replay was acknowledged", pending)
	}

	if _, err := os.Stat(filepath.Join(d.terminalReports.dir, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy record still pending: %v", err)
	}
}

func TestTerminalReportStaleClaimGenerationIsRetiredWithoutCompensation(t *testing.T) {
	var failCalls atomic.Int32
	var completeCalls atomic.Int32
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		paths = append(paths, req.URL.Path)
		mu.Unlock()
		switch {
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("{\"error\":\"task claim generation is no longer current\",\"code\":\"" +
				protocol.DaemonTaskClaimGenerationMismatchCode + "\"}\n"))
		default:
			t.Errorf("unexpected request path %q", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-stale-generation",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	report := claimGenerationReport("task-stale")

	// The first delivery is refused as stale; the report must not be replayed.
	if err := d.reportTerminalTask(context.Background(), report); err == nil {
		t.Fatal("stale terminal report unexpectedly reported success")
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 0 {
		t.Fatalf("pending reports after a stale rejection = %d, %v; want 0", len(items), err)
	}
	if failCalls.Load() != 0 {
		t.Fatalf("/fail calls = %d, want 0: a stale completion must never fail the reclaim that owns the task",
			failCalls.Load())
	}
	// A generation-aware report is only ever sent to the versioned route.
	mu.Lock()
	for _, path := range paths {
		if !strings.Contains(path, "/api/daemon/v2/") {
			t.Fatalf("stale report used a non-fenced route: %v", paths)
		}
	}
	mu.Unlock()

	// The payload is retained for operators instead of being discarded.
	entries, err := os.ReadDir(d.terminalReports.failedDir())
	if err != nil {
		t.Fatalf("read failed queue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("failed queue entries = %d, want 1", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(d.terminalReports.failedDir(), entries[0].Name()))
	if err != nil {
		t.Fatalf("read failed record: %v", err)
	}
	var record persistedTerminalTaskReport
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatalf("decode failed record: %v", err)
	}
	if record.SupersededAt == nil {
		t.Fatalf("failed record was not marked superseded: %s", body)
	}
	if record.Output != report.output {
		t.Fatalf("failed record output = %q, want the original %q", record.Output, report.output)
	}

	// Later replay passes must stay quiet rather than retrying the stale report.
	pending, delivered := d.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 0 {
		t.Fatalf("replay after retirement pending=%d delivered=%d, want 0/0", pending, delivered)
	}
	// A conflict is not a transient failure, so the send itself does not retry
	// it either: one authoritative response is all the daemon needs.
	if got := completeCalls.Load(); got != 1 {
		t.Fatalf("complete attempts = %d, want exactly one", got)
	}
}

func TestTerminalReportOrdinaryConflictStaysPending(t *testing.T) {
	d := New(Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-ordinary-conflict",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		// A 409 without the fence code is an ordinary conflict: the server may
		// recover, so the report must stay queued instead of being retired.
		return &requestError{Method: http.MethodPost, Path: "/complete", StatusCode: http.StatusConflict, Body: "{\"error\":\"some other conflict\"}"}
	}
	if err := d.terminalReports.enqueue(claimGenerationReport("task-ordinary-conflict")); err != nil {
		t.Fatalf("enqueue report: %v", err)
	}
	pending, delivered := d.replayPendingTerminalReports(context.Background())
	if pending != 1 || delivered != 0 {
		t.Fatalf("ordinary conflict pending=%d delivered=%d, want 1/0", pending, delivered)
	}
	if _, err := os.Stat(filepath.Join(d.terminalReports.failedDir(), legacyTerminalReportFileName("task-ordinary-conflict"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary conflict moved the report to failed/: %v", err)
	}
}

func TestTerminalReportReplayUsesThePersistedClaimGeneration(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/complete") {
			// A restart replay must not ask the server what the current claim is:
			// the original generation is the only one this report may carry.
			t.Errorf("replay looked up %q", req.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode replay body: %v", err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-restart-generation",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	report := claimGenerationReport("task-restart-generation")

	beforeRestart := New(cfg, logger)
	beforeRestart.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		return errors.New("network unavailable")
	}
	if err := beforeRestart.reportTerminalTask(context.Background(), report); err == nil {
		t.Fatal("terminal report unexpectedly succeeded before restart")
	}

	afterRestart := New(cfg, logger)
	pending, delivered := afterRestart.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 1 {
		t.Fatalf("restart replay pending=%d delivered=%d, want 0/1", pending, delivered)
	}
	if len(bodies) != 1 {
		t.Fatalf("replay bodies = %d, want 1", len(bodies))
	}
	got, _ := bodies[0]["expected_dispatched_at"].(string)
	if want := report.claimGeneration.dispatchedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("replayed expected_dispatched_at = %q, want the persisted %q", got, want)
	}
	if got := bodies[0]["output"]; got != report.output {
		t.Fatalf("replayed output = %v, want %q", got, report.output)
	}
}

func TestClaimGenerationComesOnlyFromTheClaimPayload(t *testing.T) {
	generation := testClaimGeneration()
	formatted := generation.Format(time.RFC3339Nano)
	secondPrecision := generation.Truncate(time.Second).Format(time.RFC3339)

	tests := []struct {
		name           string
		fenced         bool
		raw            string
		want           time.Time
		wantUnreadable bool
	}{
		{name: "advertised nano precision", fenced: true, raw: formatted, want: generation},
		{name: "advertised second precision", fenced: true, raw: secondPrecision, want: generation.Truncate(time.Second)},
		// No capability is a property of the SERVER (it never promised to compare
		// the generation), and it keeps the legacy unfenced callback working —
		// whatever the timestamp looks like.
		{name: "not advertised, valid timestamp", raw: formatted},
		{name: "not advertised, absent"},
		{name: "not advertised, malformed", raw: "not a timestamp"},
		// An advertised contract with no readable generation is a protocol error,
		// never an old server: it must not be reported as an absent generation.
		{name: "advertised but missing", fenced: true, wantUnreadable: true},
		{name: "advertised but empty", fenced: true, raw: "", wantUnreadable: true},
		{name: "advertised but whitespace", fenced: true, raw: "  ", wantUnreadable: true},
		{name: "advertised but unparseable", fenced: true, raw: "not a timestamp", wantUnreadable: true},
		{name: "advertised but zero instant", fenced: true, raw: "0001-01-01T00:00:00Z", wantUnreadable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := claimGenerationForTask(Task{DispatchedAt: tt.raw, TerminalReportGenerationFenceV1: tt.fenced})
			if got.unreadable != tt.wantUnreadable {
				t.Fatalf("claimGenerationForTask() unreadable = %v, want %v", got.unreadable, tt.wantUnreadable)
			}
			if !got.dispatchedAt.Equal(tt.want) {
				t.Fatalf("claimGenerationForTask() = %s, want %s", got.dispatchedAt, tt.want)
			}
		})
	}
}

// TestTerminalReportRefusesAnUnreadableClaimGeneration pins the third case of
// the parser: a claim that ships a dispatched_at we cannot read is a protocol
// error, so no terminal callback may leave the daemon and no replayable record
// may be written. Degrading into an unfenced callback is the one outcome that
// would let a stale report settle a reclaim.
func TestTerminalReportRefusesAnUnreadableClaimGeneration(t *testing.T) {
	d := New(Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-unreadable-generation",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var sent atomic.Int32
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		sent.Add(1)
		return nil
	}
	report := terminalTaskReport{
		kind:            terminalTaskReportComplete,
		taskID:          "task-unreadable",
		output:          "must never be delivered unfenced",
		claimGeneration: claimGenerationForTask(Task{DispatchedAt: "yesterday", TerminalReportGenerationFenceV1: true}),
	}
	if err := d.reportTerminalTask(context.Background(), report); err == nil {
		t.Fatal("unreadable claim generation was reported as delivered")
	}
	if got := sent.Load(); got != 0 {
		t.Fatalf("terminal sends = %d, want 0", got)
	}
	items, err := d.terminalReports.list()
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("queue holds %d records, want 0", len(items))
	}
}

func TestTaskDispatchedAtMirrorsTheClaimPayload(t *testing.T) {
	// internal/daemon.Task is a hand-kept mirror of the server's claim response.
	// A missing or renamed JSON tag here would silently drop the capability or
	// the generation, and every callback of that claim would go back to the
	// unfenced path.
	var task Task
	if err := json.Unmarshal([]byte("{\"id\":\"task-1\",\"terminal_report_generation_fence_v1\":true,\"dispatched_at\":\"2026-09-19T04:43:58.123456Z\"}"), &task); err != nil {
		t.Fatalf("decode claim payload: %v", err)
	}
	want := time.Date(2026, time.September, 19, 4, 43, 58, 123456000, time.UTC)
	if got := claimGenerationForTask(task); got.unreadable || !got.dispatchedAt.Equal(want) {
		t.Fatalf("claim generation from the claim payload = %s (%v), want %s", got, got.unreadable, want)
	}

	// A server that predates the capability must leave the generation unknown
	// rather than inferring one from the timestamp it happens to send.
	var legacy Task
	if err := json.Unmarshal([]byte("{\"id\":\"task-2\",\"dispatched_at\":\"2026-09-19T04:43:58Z\"}"), &legacy); err != nil {
		t.Fatalf("decode legacy claim payload: %v", err)
	}
	if got := claimGenerationForTask(legacy); !got.dispatchedAt.IsZero() || got.unreadable {
		t.Fatalf("legacy claim payload produced a generation: %s", got)
	}
	if legacy.TerminalReportGenerationFenceV1 {
		t.Fatal("capability decoded as true from a payload that never advertised it")
	}
}

func TestTerminalReportEnqueueKeepsGenerationForEveryReportKind(t *testing.T) {
	// Both terminal kinds carry the generation; a fail report that lost it would
	// never be replayed and would be retained as unknown ownership instead.
	for _, kind := range []terminalTaskReportKind{terminalTaskReportComplete, terminalTaskReportFail} {
		store := newTerminalReportStore(Config{
			WorkspacesRoot: t.TempDir(),
			ServerBaseURL:  "https://api.example.test",
			DaemonID:       "daemon-kinds",
		})
		report := claimGenerationReport("task-kind")
		report.kind = kind
		report.errorMessage = "provider failed"
		if err := store.enqueue(report); err != nil {
			t.Fatalf("enqueue %d report: %v", kind, err)
		}
		items, err := store.list()
		if err != nil {
			t.Fatalf("list %d report: %v", kind, err)
		}
		if len(items) != 1 || !items[0].report.claimGeneration.dispatchedAt.Equal(report.claimGeneration.dispatchedAt) {
			t.Fatalf("kind %d lost its generation: %+v", kind, items)
		}
	}
}

// These two cases stay as the claim-negotiation proof: the timestamp alone is never
// fence support, and only an advertised capability makes a claim generation-aware.
// fenceCapableServer is an httptest server that records every terminal callback
// path and body it receives and answers 200.
func fenceCapableServer(t *testing.T) (*httptest.Server, func() []map[string]any, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode %s body: %v", req.URL.Path, err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		paths = append(paths, req.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
			mu.Lock()
			defer mu.Unlock()
			return append([]map[string]any(nil), bodies...)
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), paths...)
		}
}

// TestOldServerClaimWithDispatchedAtStaysLegacy is the version-skew regression.
// The payload is exactly what an older server sends today: a dispatched_at and
// no fence capability. It must stay legacy — no fenced callback, no durable

func TestOldServerClaimWithDispatchedAtStaysLegacy(t *testing.T) {
	for _, dispatchedAt := range []string{
		"2026-09-19T04:43:58Z",             // second precision, the old shape
		"2026-09-19T04:43:58.123456Z",      // even a nanosecond-looking value
		"2026-09-19T13:43:58.123456+09:00", // and another timezone
	} {
		t.Run(dispatchedAt, func(t *testing.T) {
			var task Task
			payload := `{"id":"task-old-server","dispatched_at":"` + dispatchedAt + `"}`
			if err := json.Unmarshal([]byte(payload), &task); err != nil {
				t.Fatalf("decode claim payload: %v", err)
			}
			if got := claimGenerationForTask(task); !got.dispatchedAt.IsZero() || got.unreadable {
				t.Fatalf("old-server claim produced generation %s (unreadable=%v), want legacy/absent", got, got.unreadable)
			}

			srv, sentBodies, sentPaths := fenceCapableServer(t)
			d := New(Config{
				ServerBaseURL:  srv.URL,
				WorkspacesRoot: t.TempDir(),
				DaemonID:       "daemon-old-server",
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			report := terminalTaskReport{
				kind: terminalTaskReportComplete, taskID: task.ID, output: "legacy answer",
				claimGeneration: claimGenerationForTask(task),
			}
			if err := d.reportTerminalTask(context.Background(), report); err != nil {
				t.Fatalf("legacy terminal report: %v", err)
			}

			bodies := sentBodies()
			if len(bodies) != 1 {
				t.Fatalf("terminal requests = %d, want the one legacy live callback", len(bodies))
			}
			if value, present := bodies[0]["expected_dispatched_at"]; present {
				t.Fatalf("legacy callback carried a fence (%v); an old server ignores it", value)
			}
			if got := sentPaths(); len(got) != 1 || got[0] != "/api/daemon/tasks/task-old-server/complete" {
				t.Fatalf("legacy callback paths = %v, want the legacy terminal route", got)
			}
			// The live callback succeeded, so its durable copy was acknowledged.
			if items, err := d.terminalReports.list(); err != nil || len(items) != 0 {
				t.Fatalf("durable queue = %d records (%v), want 0 after the acked live callback", len(items), err)
			}
			// Durability: a failed live callback must leave a version-1 record
			// that replays through the legacy endpoint (#8533 contract).
			d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
				return errors.New("network unavailable")
			}
			if err := d.reportTerminalTask(context.Background(), report); err == nil {
				t.Fatal("failed live callback unexpectedly reported success")
			}
			items, err := d.terminalReports.list()
			if err != nil || len(items) != 1 {
				t.Fatalf("durable queue = %d records (%v), want 1 version-1 record for the failed old-server report", len(items), err)
			}
			if !items[0].report.claimGeneration.dispatchedAt.IsZero() {
				t.Fatalf("old-server record carries a generation: %+v", items[0].report)
			}
			if want := legacyTerminalReportFileName(task.ID); items[0].fileName != want {
				t.Fatalf("old-server record file = %q, want legacy identity %q", items[0].fileName, want)
			}
			var replayed atomic.Int32
			d.terminalReportSend = func(_ context.Context, rereport terminalTaskReport, _ []time.Duration) error {
				if !rereport.claimGeneration.dispatchedAt.IsZero() {
					return errors.New("legacy replay must carry no claim generation")
				}
				replayed.Add(1)
				return nil
			}
			if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 0 || delivered != 1 {
				t.Fatalf("legacy replay pending=%d delivered=%d, want 0/1", pending, delivered)
			}
			if replayed.Load() != 1 {
				t.Fatalf("legacy replay deliveries = %d, want 1", replayed.Load())
			}
		})
	}
}
