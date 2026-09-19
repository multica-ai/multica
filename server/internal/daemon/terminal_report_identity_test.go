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

// A task id alone is not a terminal report's identity: the server may reclaim a
// task, and the later claim produces its own terminal result. These tests pin
// the queue's identity semantics — (task id, claim generation) — so an
// unsettled report from an older claim can never collide with, block, or delete
// the reclaim's own result.

func generationReport(taskID string, generation time.Time, output string) terminalTaskReport {
	return terminalTaskReport{
		kind:            terminalTaskReportComplete,
		taskID:          taskID,
		output:          output,
		claimGeneration: claimGeneration{dispatchedAt: generation.UTC()},
	}
}

func newIdentityTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	d := New(Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-identity",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		return errors.New("server unavailable")
	}
	return d
}

// TestTerminalReportQueueKeepsEveryClaimGeneration is the primary blocker: two
// generations of one task must be independently persistable.
func TestTerminalReportQueueKeepsEveryClaimGeneration(t *testing.T) {
	d := newIdentityTestDaemon(t)
	generationA := testClaimGeneration()
	generationB := generationA.Add(3 * time.Minute)
	reportA := generationReport("task-reclaimed", generationA, "result of claim A")
	reportB := generationReport("task-reclaimed", generationB, "result of claim B")

	if err := d.terminalReports.enqueue(reportA); err != nil {
		t.Fatalf("enqueue generation A: %v", err)
	}
	if err := d.terminalReports.enqueue(reportB); err != nil {
		t.Fatalf("enqueue generation B: %v", err)
	}

	items, err := d.terminalReports.list()
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("queued records = %d, want 2 (one per generation)", len(items))
	}
	if items[0].fileName == items[1].fileName {
		t.Fatalf("both generations share the file name %s", items[0].fileName)
	}
	byGeneration := map[string]terminalTaskReport{}
	for _, item := range items {
		byGeneration[item.report.claimGeneration.String()] = item.report
	}
	if got := byGeneration[generationA.Format(time.RFC3339Nano)]; got.output != reportA.output {
		t.Fatalf("generation A round-tripped as %+v, want %+v", got, reportA)
	}
	if got := byGeneration[generationB.Format(time.RFC3339Nano)]; got.output != reportB.output {
		t.Fatalf("generation B round-tripped as %+v, want %+v", got, reportB)
	}

	// Same identity, same payload: nothing to do.
	if err := d.terminalReports.enqueue(reportA); err != nil {
		t.Fatalf("re-enqueue of an identical report failed: %v", err)
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 2 {
		t.Fatalf("queue holds %d records (%v) after an idempotent enqueue, want 2", len(items), err)
	}

	// Same identity, different payload: one claim cannot change its outcome.
	conflict := reportA
	conflict.output = "a different outcome for the same claim"
	if err := d.terminalReports.enqueue(conflict); err == nil || !strings.Contains(err.Error(), "conflicts with the original") {
		t.Fatalf("conflicting enqueue error = %v, want original-payload conflict", err)
	}
}

// TestTerminalReportGenerationsSurviveRestartTogether keeps the durability
// guarantee for the reclaim: an older generation still queued must not prevent
// the newer one from being stored, or from being found after a restart.
func TestTerminalReportGenerationsSurviveRestartTogether(t *testing.T) {
	cfg := Config{
		ServerBaseURL:  "https://api.example.test",
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-identity-restart",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(cfg, logger)
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		return errors.New("server unavailable")
	}
	generationA := testClaimGeneration()
	generationB := generationA.Add(90 * time.Second)
	if err := d.reportTerminalTask(context.Background(), generationReport("task-reclaimed", generationA, "A")); err == nil {
		t.Fatal("generation A delivery unexpectedly succeeded")
	}
	if err := d.reportTerminalTask(context.Background(), generationReport("task-reclaimed", generationB, "B")); err == nil {
		t.Fatal("generation B delivery unexpectedly succeeded")
	}

	afterRestart := New(cfg, logger)
	var replayed []terminalTaskReport
	var mu sync.Mutex
	afterRestart.terminalReportSend = func(_ context.Context, report terminalTaskReport, schedule []time.Duration) error {
		if schedule != nil {
			t.Fatalf("replay schedule = %v, want one HTTP attempt", schedule)
		}
		mu.Lock()
		replayed = append(replayed, report)
		mu.Unlock()
		return nil
	}
	pending, delivered := afterRestart.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 2 {
		t.Fatalf("restart replay pending=%d delivered=%d, want 0/2", pending, delivered)
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[string]string{}
	for _, report := range replayed {
		seen[report.claimGeneration.String()] = report.output
	}
	if seen[generationA.Format(time.RFC3339Nano)] != "A" || seen[generationB.Format(time.RFC3339Nano)] != "B" {
		t.Fatalf("restart replayed %+v, want both generations with their own payloads", replayed)
	}
}

// TestTerminalReportInFlightDeliveryIsPerClaimGeneration is the second blocker:
// a replay of an older generation must not block the reclaim's foreground
// callback, while a duplicate of the same generation still must.
func TestTerminalReportInFlightDeliveryIsPerClaimGeneration(t *testing.T) {
	d := newIdentityTestDaemon(t)
	d.terminalReports = nil // deliver straight to the seam; the queue has its own tests
	generationA := testClaimGeneration()
	generationB := generationA.Add(time.Minute)

	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var delivered []terminalTaskReport
	sends := 0
	d.terminalReportSend = func(_ context.Context, report terminalTaskReport, _ []time.Duration) error {
		mu.Lock()
		sends++
		first := sends == 1
		delivered = append(delivered, report)
		mu.Unlock()
		if first {
			close(started)
			<-release
		}
		return nil
	}

	reportA := generationReport("task-blocked", generationA, "A")
	doneA := make(chan error, 1)
	go func() { doneA <- d.reportTerminalTask(context.Background(), reportA) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("generation A delivery did not start")
	}

	// A duplicate of the same identity is refused while A is in flight...
	duplicateDone := make(chan error, 1)
	go func() { duplicateDone <- d.reportTerminalTask(context.Background(), reportA) }()
	if err := <-duplicateDone; err == nil || !strings.Contains(err.Error(), "already being delivered") {
		t.Fatalf("duplicate in-flight report error = %v, want already-being-delivered", err)
	}

	// ...but the reclaim's own result is a different identity and must go out.
	reportB := generationReport("task-blocked", generationB, "B")
	doneB := make(chan error, 1)
	go func() { doneB <- d.reportTerminalTask(context.Background(), reportB) }()
	select {
	case err := <-doneB:
		if err != nil {
			t.Fatalf("generation B delivery failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("generation B delivery was blocked by generation A's in-flight send")
	}
	close(release)
	if err := <-doneA; err != nil {
		t.Fatalf("generation A delivery failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != 2 {
		t.Fatalf("delivered %d reports, want A and B", len(delivered))
	}
}

// TestTerminalReportAcknowledgeIsPerClaimGeneration: acknowledging one
// generation must never delete another's durable copy.
func TestTerminalReportAcknowledgeIsPerClaimGeneration(t *testing.T) {
	d := newIdentityTestDaemon(t)
	generationA := testClaimGeneration()
	generationB := generationA.Add(time.Minute)
	reportA := generationReport("task-ack", generationA, "A")
	reportB := generationReport("task-ack", generationB, "B")
	for _, report := range []terminalTaskReport{reportA, reportB} {
		if err := d.terminalReports.enqueue(report); err != nil {
			t.Fatalf("enqueue %s: %v", report.claimGeneration, err)
		}
	}

	itemA := pendingTerminalTaskReport{fileName: reportFileName(reportA), report: reportA}
	if err := d.terminalReports.acknowledge(itemA); err != nil {
		t.Fatalf("acknowledge generation A: %v", err)
	}
	items, err := d.terminalReports.list()
	if err != nil {
		t.Fatalf("list after acknowledging A: %v", err)
	}
	if len(items) != 1 || items[0].report.claimGeneration != reportB.claimGeneration {
		t.Fatalf("queue after acknowledging A = %+v, want generation B only", items)
	}

	itemB := pendingTerminalTaskReport{fileName: reportFileName(reportB), report: reportB}
	if err := d.terminalReports.acknowledge(itemB); err != nil {
		t.Fatalf("acknowledge generation B: %v", err)
	}
	if items, err := d.terminalReports.list(); err != nil || len(items) != 0 {
		t.Fatalf("queue after acknowledging both = %d records (%v), want 0", len(items), err)
	}
}

// TestTerminalReportSupersedeIsPerClaimGeneration: retiring a stale generation
// must leave the reclaim's own pending report exactly where it is, and its
// failed-queue slot must not collide with it.
func TestTerminalReportSupersedeIsPerClaimGeneration(t *testing.T) {
	var failCalls atomic.Int32
	var rejectEverything atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/complete"):
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["output"] == "B" && !rejectEverything.Load() {
				// The reclaim's own report is the current one.
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("{\"error\":\"stale\",\"code\":\"" + protocol.DaemonTaskClaimGenerationMismatchCode + "\"}\n"))
		default:
			t.Errorf("unexpected request path %q", req.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		DaemonID:       "daemon-supersede-identity",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	generationA := testClaimGeneration()
	generationB := generationA.Add(time.Minute)
	reportA := generationReport("task-superseded", generationA, "A")
	reportB := generationReport("task-superseded", generationB, "B")
	for _, report := range []terminalTaskReport{reportA, reportB} {
		if err := d.terminalReports.enqueue(report); err != nil {
			t.Fatalf("enqueue %s: %v", report.claimGeneration, err)
		}
	}

	// Generation B is the current claim, so it settles the row; generation A is
	// refused as stale and retired instead of being counted as delivered.
	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 0 || delivered != 1 {
		t.Fatalf("replay pending=%d delivered=%d, want 0/1", pending, delivered)
	}
	if got := failCalls.Load(); got != 0 {
		t.Fatalf("/fail calls = %d, want 0: a superseded report must not compensate", got)
	}
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 0 {
		t.Fatalf("pending queue = %d (%v), want 0", len(items), err)
	}
	entries, err := os.ReadDir(d.terminalReports.failedDir())
	if err != nil {
		t.Fatalf("read failed queue: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != reportFileName(reportA) {
		t.Fatalf("failed queue = %v, want only generation A under %s", entries, reportFileName(reportA))
	}
	body, err := os.ReadFile(filepath.Join(d.terminalReports.failedDir(), entries[0].Name()))
	if err != nil {
		t.Fatalf("read superseded record: %v", err)
	}
	var record persistedTerminalTaskReport
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatalf("decode superseded record: %v", err)
	}
	if record.SupersededAt == nil || record.ClaimDispatchedAt == nil || !record.ClaimDispatchedAt.Equal(generationA) {
		t.Fatalf("superseded record = %s, want generation A marked superseded", body)
	}

	// Both generations of one task must be able to sit in failed/ together: the
	// retirement path keys on the identity, not on the task id.
	rejectEverything.Store(true)
	if err := d.terminalReports.enqueue(reportB); err != nil {
		t.Fatalf("requeue generation B: %v", err)
	}
	if pending, delivered := d.replayPendingTerminalReports(context.Background()); pending != 0 || delivered != 0 {
		t.Fatalf("second replay pending=%d delivered=%d, want 0/0", pending, delivered)
	}
	entries, err = os.ReadDir(d.terminalReports.failedDir())
	if err != nil {
		t.Fatalf("read failed queue: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("failed queue holds %d records, want both generations", len(entries))
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, report := range []terminalTaskReport{reportA, reportB} {
		if !names[reportFileName(report)] {
			t.Fatalf("failed queue %v is missing %s", entries, reportFileName(report))
		}
	}
}

// TestTerminalReportRecordVersionGuardsDowngrade is the mandatory downgrade
// test. Generation affects replay safety, so a generation-aware record must be
// unreadable to a daemon that only knows the pre-fence layout: that daemon
// leaves it untouched instead of replaying it unfenced.
func TestTerminalReportRecordVersionGuardsDowngrade(t *testing.T) {
	d := newIdentityTestDaemon(t)
	report := generationReport("task-downgrade", testClaimGeneration(), "answer")
	if err := d.terminalReports.enqueue(report); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(d.terminalReports.dir, reportFileName(report)))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var record persistedTerminalTaskReport
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatalf("decode record: %v", err)
	}

	// A pre-fence reader accepted exactly one version. Pinning both halves of
	// that gate is the whole downgrade contract.
	if record.Version != terminalReportRecordVersion {
		t.Fatalf("record version = %d, want %d", record.Version, terminalReportRecordVersion)
	}
	legacyReaderAccepts := record.Version == legacyTerminalReportRecordVersion
	if legacyReaderAccepts {
		t.Fatal("an old daemon would accept this record as a replayable unfenced report")
	}

	// Clear the valid record so the pass below can only report on the
	// unsupported one.
	if err := d.terminalReports.acknowledge(pendingTerminalTaskReport{fileName: reportFileName(report), report: report}); err != nil {
		t.Fatalf("acknowledge the version-2 record: %v", err)
	}

	// Unsupported (unknown) versions are retained and surfaced, never replayed
	// and never deleted. A different task keeps this record away from the valid
	// one above, which is delivered on its own.
	future := generationReport("task-future-version", testClaimGeneration(), "future answer")
	futureName := reportFileName(future)
	futureBody, err := json.Marshal(persistedTerminalTaskReport{Version: 99, Kind: "complete", TaskID: future.taskID})
	if err != nil {
		t.Fatalf("encode future record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d.terminalReports.dir, futureName), futureBody, 0o600); err != nil {
		t.Fatalf("write future record: %v", err)
	}
	var sent atomic.Int32
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		sent.Add(1)
		return nil
	}
	pending, _ := d.replayPendingTerminalReports(context.Background())
	if sent.Load() != 0 {
		t.Fatalf("unsupported record was delivered %d times", sent.Load())
	}
	if pending == 0 {
		t.Fatal("unsupported record no longer keeps the queue pending")
	}
	if _, err := os.Stat(filepath.Join(d.terminalReports.dir, futureName)); err != nil {
		t.Fatalf("unsupported record was removed: %v", err)
	}
}

// TestTerminalReportMalformedV2IsRejectedNotDowngraded: a version-2 record
// without a generation is corrupt, and decoding it must not produce a report
// that would be delivered unfenced.
func TestTerminalReportMalformedV2IsRejectedNotDowngraded(t *testing.T) {
	d := newIdentityTestDaemon(t)
	malformed := persistedTerminalTaskReport{
		Version:   terminalReportRecordVersion,
		CreatedAt: time.Now().UTC(),
		Kind:      "complete",
		TaskID:    "task-malformed-v2",
		Output:    "no generation on a version-2 record",
	}
	body, err := json.Marshal(malformed)
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	if strings.Contains(string(body), "claim_dispatched_at") {
		t.Fatalf("malformed fixture unexpectedly carries a generation: %s", body)
	}
	name := legacyTerminalReportFileName(malformed.TaskID)
	if err := d.terminalReports.ensureDir(); err != nil {
		t.Fatalf("prepare queue: %v", err)
	}
	if err := writeTerminalReportRecord(d.terminalReports.dir, name, malformed); err != nil {
		t.Fatalf("write malformed record: %v", err)
	}

	var sent atomic.Int32
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error {
		sent.Add(1)
		return nil
	}
	pending, delivered := d.replayPendingTerminalReports(context.Background())
	if sent.Load() != 0 || delivered != 0 {
		t.Fatalf("malformed version-2 record was delivered (%d sends, %d delivered)", sent.Load(), delivered)
	}
	if pending == 0 {
		t.Fatal("malformed version-2 record no longer keeps the queue pending")
	}
	if _, err := os.Stat(filepath.Join(d.terminalReports.dir, name)); err != nil {
		t.Fatalf("malformed record was removed: %v", err)
	}
}
