package traceupload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// mockUploader is a test double for Uploader.
type mockUploader struct {
	mu       sync.Mutex
	calls    int
	bodies   [][]byte
	sidecars []Sidecar
	err      error // returned on every call when set
}

func (m *mockUploader) Upload(_ context.Context, s Sidecar, jsonl []byte) (UploadResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	cp := make([]byte, len(jsonl))
	copy(cp, jsonl)
	m.bodies = append(m.bodies, cp)
	m.sidecars = append(m.sidecars, s)
	if m.err != nil {
		return UploadResult{}, m.err
	}
	return UploadResult{Status: "ingested", EventCount: 1}, nil
}

func (m *mockUploader) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// testManager builds a Manager with an in-memory ledger and a controllable clock.
func testManager(t *testing.T, up Uploader) (*Manager, *Ledger) {
	t.Helper()
	led, _ := LoadLedger(filepath.Join(t.TempDir(), "ledger.json"))
	cfg := Config{
		Enabled:                   true,
		MaxConcurrentPerWorkspace: 2,
		MaxAge:                    DefaultMaxAge,
		Backoff:                   DefaultBackoff,
	}
	m := newManagerWith(cfg, led, up, nil)
	return m, led
}

func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// CP2: task-close → enqueue → upload → ledger marked ingested, transcript sent.
func TestContinuousSendMarksIngested(t *testing.T) {
	up := &mockUploader{}
	m, led := testManager(t, up)
	path := writeTranscript(t, `{"type":"assistant"}`)
	s := Sidecar{Runtime: "claude", TaskID: "t1", SessionID: "s1", WorkspaceID: "ws"}

	if err := m.Enqueue(s, path); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := led.Get("t1|s1"); got == nil || got.Status != StatusPending {
		t.Fatalf("expected pending after enqueue: %+v", got)
	}
	if out := m.processOnce(context.Background(), "t1|s1"); out != outcomeIngested {
		t.Fatalf("processOnce outcome=%v", out)
	}
	if up.callCount() != 1 {
		t.Fatalf("uploader called %d times", up.callCount())
	}
	if led.Get("t1|s1").Status != StatusIngested {
		t.Fatal("ledger not marked ingested")
	}
	if m.Metrics().Success != 1 {
		t.Fatalf("success metric = %d", m.Metrics().Success)
	}
}

// CP3: boot-sweep — a ledger pre-populated with pending entries (simulating a
// prior daemon that closed tasks but never confirmed ingest) re-sends them.
func TestBootSweepResendsPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	transcript := writeTranscript(t, `{"x":1}`)

	// Prior daemon run: record two pending entries for two different tasks
	// each with its own session_id (the realistic case — boot-sweep covers
	// distinct un-sent transcripts), then "crash" (drop the ledger).
	prior, _ := LoadLedger(path)
	_, _ = prior.Upsert(Sidecar{Runtime: "claude", TaskID: "a", SessionID: "s-a", WorkspaceID: "ws"}, transcript, "h", 1, time.Now())
	_, _ = prior.Upsert(Sidecar{Runtime: "codex", TaskID: "b", SessionID: "s-b", WorkspaceID: "ws"}, transcript, "h", 1, time.Now())

	// New daemon boots: load the same ledger file.
	up := &mockUploader{}
	led, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Enabled: true, MaxConcurrentPerWorkspace: 2, MaxAge: DefaultMaxAge, Backoff: DefaultBackoff}
	m := newManagerWith(cfg, led, up, nil)

	pending := led.Pending()
	if len(pending) != 2 {
		t.Fatalf("boot ledger should have 2 pending, got %d", len(pending))
	}
	for _, e := range pending {
		m.processOnce(context.Background(), e.Key)
	}
	if up.callCount() != 2 {
		t.Fatalf("boot-sweep sent %d, want 2", up.callCount())
	}
	for _, k := range []string{"a|s-a", "b|s-b"} {
		if led.Get(k).Status != StatusIngested {
			t.Errorf("%s not ingested after sweep", k)
		}
	}
}

// CP4: an upload failure does not fail the task — Enqueue returns nil and the
// transcript stays unmarked (pending) for a later sweep.
func TestUploadFailureLeavesEntryPending(t *testing.T) {
	up := &mockUploader{err: errors.New("endpoint down")}
	m, led := testManager(t, up)
	path := writeTranscript(t, `{"x":1}`)
	s := Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s", WorkspaceID: "ws"}

	if err := m.Enqueue(s, path); err != nil {
		t.Fatalf("Enqueue must not error on a task that will fail upload: %v", err)
	}
	if out := m.processOnce(context.Background(), "t|s"); out != outcomeRetry {
		t.Fatalf("outcome=%v want retry", out)
	}
	e := led.Get("t|s")
	if e.Status != StatusPending {
		t.Fatalf("entry should remain pending (unmarked), got %s", e.Status)
	}
	if e.Attempts != 1 {
		t.Fatalf("attempts=%d want 1", e.Attempts)
	}
	if m.Metrics().Failed != 1 {
		t.Fatalf("failed metric=%d", m.Metrics().Failed)
	}
}

// CP5: repeated failures retry with backoff; after MaxAge the entry becomes a
// dead letter and the metric increments.
func TestRetryBackoffAndDeadLetter(t *testing.T) {
	up := &mockUploader{err: errors.New("still down")}
	m, led := testManager(t, up)
	path := writeTranscript(t, `{"x":1}`)
	s := Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s", WorkspaceID: "ws"}

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }
	_ = m.Enqueue(s, path)

	// A few failing attempts within the window → stays pending, attempts climb.
	for i := 1; i <= 3; i++ {
		if out := m.processOnce(context.Background(), "t|s"); out != outcomeRetry {
			t.Fatalf("attempt %d outcome=%v want retry", i, out)
		}
		if led.Get("t|s").Attempts != i {
			t.Fatalf("attempts=%d want %d", led.Get("t|s").Attempts, i)
		}
	}

	// Backoff schedule: 1s, 5s, 30s, then capped at 30s.
	for attempts, want := range map[int]time.Duration{1: time.Second, 2: 5 * time.Second, 3: 30 * time.Second, 9: 30 * time.Second} {
		if got := m.backoffFor(attempts); got != want {
			t.Errorf("backoffFor(%d)=%v want %v", attempts, got, want)
		}
	}

	// Jump past MaxAge → dead letter.
	m.now = func() time.Time { return base.Add(DefaultMaxAge + time.Hour) }
	if out := m.processOnce(context.Background(), "t|s"); out != outcomeDeadLetter {
		t.Fatalf("outcome=%v want dead_letter", out)
	}
	if led.Get("t|s").Status != StatusDeadLetter {
		t.Fatal("entry not parked as dead_letter")
	}
	if m.Metrics().DeadLetter != 1 {
		t.Fatalf("dead_letter metric=%d", m.Metrics().DeadLetter)
	}
	// Dead letters are terminal — not re-sent.
	if out := m.processOnce(context.Background(), "t|s"); out != outcomeSkip {
		t.Fatalf("dead letter re-processed: %v", out)
	}
}

// CP6: with the flag off NewManager returns a nil Manager and every call is a
// no-op — not a single upload happens.
func TestFlagOffNoManagerNoCalls(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: false}, nil)
	if err != nil {
		t.Fatalf("disabled NewManager should not error: %v", err)
	}
	if mgr != nil {
		t.Fatal("expected nil manager when flag off")
	}
	// nil-safe methods.
	if err := mgr.Enqueue(Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s"}, "/whatever"); err != nil {
		t.Fatalf("nil Enqueue: %v", err)
	}
	mgr.Start(context.Background()) // must not panic
	if snap := mgr.Metrics(); snap.Success != 0 || snap.Failed != 0 {
		t.Fatal("nil metrics non-zero")
	}
}

// Flag on but endpoint/key missing is a clear configuration error, not a panic.
func TestFlagOnMissingEndpointErrors(t *testing.T) {
	if _, err := NewManager(Config{Enabled: true, APIKey: "k"}, nil); err == nil {
		t.Error("expected error when endpoint missing")
	}
	if _, err := NewManager(Config{Enabled: true, Endpoint: "https://x"}, nil); err == nil {
		t.Error("expected error when api key missing")
	}
}

// An incomplete sidecar (can never be ingested) is rejected at enqueue, not
// silently sent with null identity.
func TestEnqueueRejectsInvalidSidecar(t *testing.T) {
	up := &mockUploader{}
	m, _ := testManager(t, up)
	path := writeTranscript(t, `{}`)
	err := m.Enqueue(Sidecar{Runtime: "claude", TaskID: "t" /* no session */}, path)
	if err == nil {
		t.Fatal("expected error for invalid sidecar")
	}
	if up.callCount() != 0 {
		t.Fatal("invalid sidecar must not upload")
	}
}

// Claude `--resume` appends to the same <sessionID>.jsonl. Without delta
// tracking, the second task would re-ship the bytes the first task already
// uploaded — duplicating events under a different task_id. This test asserts
// that the second upload contains ONLY the suffix the second task added.
func TestClaudeResumeUploadsOnlyDelta(t *testing.T) {
	up := &mockUploader{}
	m, led := testManager(t, up)

	// One transcript file, two tasks resuming the same session.
	path := filepath.Join(t.TempDir(), "session.jsonl")
	firstPayload := `{"type":"assistant","text":"task A event 1"}` + "\n"
	if err := os.WriteFile(path, []byte(firstPayload), 0o644); err != nil {
		t.Fatal(err)
	}
	sA := Sidecar{Runtime: "claude", TaskID: "task-A", SessionID: "sess-1", WorkspaceID: "ws"}
	if err := m.Enqueue(sA, path); err != nil {
		t.Fatalf("enqueue A: %v", err)
	}
	if out := m.processOnce(context.Background(), sA.Key()); out != outcomeIngested {
		t.Fatalf("task A outcome=%v", out)
	}

	// Task B resumes the same session: claude appends to the same file.
	secondPayload := `{"type":"assistant","text":"task B event 1"}` + "\n"
	if err := os.WriteFile(path, []byte(firstPayload+secondPayload), 0o644); err != nil {
		t.Fatal(err)
	}
	sB := Sidecar{Runtime: "claude", TaskID: "task-B", SessionID: "sess-1", WorkspaceID: "ws"}
	if err := m.Enqueue(sB, path); err != nil {
		t.Fatalf("enqueue B: %v", err)
	}
	if out := m.processOnce(context.Background(), sB.Key()); out != outcomeIngested {
		t.Fatalf("task B outcome=%v", out)
	}

	if up.callCount() != 2 {
		t.Fatalf("uploader called %d times, want 2", up.callCount())
	}
	// Body 0 was the full first payload; body 1 must be ONLY the suffix the
	// second task added — not the prior bytes.
	if string(up.bodies[0]) != firstPayload {
		t.Errorf("task A body = %q, want %q", up.bodies[0], firstPayload)
	}
	if string(up.bodies[1]) != secondPayload {
		t.Errorf("task B body = %q, want delta-only %q", up.bodies[1], secondPayload)
	}
	// Both ledger rows settle as ingested under their own (task_id|session_id).
	if led.Get("task-A|sess-1").Status != StatusIngested {
		t.Error("task A not ingested")
	}
	if led.Get("task-B|sess-1").Status != StatusIngested {
		t.Error("task B not ingested")
	}
	// Session watermark advanced to full file size.
	ss := led.SessionState("sess-1")
	if ss == nil || ss.LastUploadedSize != int64(len(firstPayload)+len(secondPayload)) {
		t.Errorf("session watermark = %+v, want size %d", ss, len(firstPayload)+len(secondPayload))
	}
}

// A resumed task that produced ZERO new events must settle without a redundant
// network call (the prior task already shipped every byte of the transcript).
func TestClaudeResumeNoNewEventsSkipsUpload(t *testing.T) {
	up := &mockUploader{}
	m, led := testManager(t, up)

	path := filepath.Join(t.TempDir(), "session.jsonl")
	payload := `{"type":"assistant"}` + "\n"
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	sA := Sidecar{Runtime: "claude", TaskID: "task-A", SessionID: "sess-noop", WorkspaceID: "ws"}
	_ = m.Enqueue(sA, path)
	_ = m.processOnce(context.Background(), sA.Key())

	// Task B closes with no new transcript bytes.
	sB := Sidecar{Runtime: "claude", TaskID: "task-B", SessionID: "sess-noop", WorkspaceID: "ws"}
	_ = m.Enqueue(sB, path)
	if out := m.processOnce(context.Background(), sB.Key()); out != outcomeIngested {
		t.Fatalf("task B outcome=%v want ingested", out)
	}
	if up.callCount() != 1 {
		t.Fatalf("uploader called %d times, want 1 (B should skip)", up.callCount())
	}
	if led.Get("task-B|sess-noop").Status != StatusIngested {
		t.Error("task B not ingested in ledger")
	}
}

// If the transcript was truncated/rewritten between tasks, the prefix hash
// will diverge and the uploader falls back to a full upload (better to
// re-deliver than to send misaligned suffix bytes).
func TestSessionPrefixDivergenceFallsBackToFullUpload(t *testing.T) {
	up := &mockUploader{}
	m, _ := testManager(t, up)

	path := filepath.Join(t.TempDir(), "session.jsonl")
	first := `{"type":"assistant","text":"original"}` + "\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	sA := Sidecar{Runtime: "claude", TaskID: "task-A", SessionID: "sess-rewrite", WorkspaceID: "ws"}
	_ = m.Enqueue(sA, path)
	_ = m.processOnce(context.Background(), sA.Key())

	// Same session id, but the file now starts with different bytes
	// (truncated and rewritten with new content of the same length, plus more).
	rewritten := `{"type":"assistant","text":"DIFFERENT"}` + "\n" +
		`{"type":"assistant","text":"new"}` + "\n"
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	sB := Sidecar{Runtime: "claude", TaskID: "task-B", SessionID: "sess-rewrite", WorkspaceID: "ws"}
	_ = m.Enqueue(sB, path)
	_ = m.processOnce(context.Background(), sB.Key())

	if up.callCount() != 2 {
		t.Fatalf("uploader called %d times, want 2", up.callCount())
	}
	// Task B should have shipped the whole rewritten file (full fallback),
	// not a misaligned suffix.
	if string(up.bodies[1]) != rewritten {
		t.Errorf("task B body = %q, want full rewritten content %q", up.bodies[1], rewritten)
	}
}

// Per-workspace rate limit caps concurrent uploads (F2.9).
func TestPerWorkspaceRateLimit(t *testing.T) {
	m, _ := testManager(t, &mockUploader{})
	m.cfg.MaxConcurrentPerWorkspace = 1
	ctx := context.Background()

	rel1 := m.acquire(ctx, "ws")
	if rel1 == nil {
		t.Fatal("first acquire failed")
	}
	// Second acquire for same workspace must block until release; verify via a
	// cancelled context returning nil.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if rel2 := m.acquire(cctx, "ws"); rel2 != nil {
		t.Fatal("expected blocked acquire to fail on cancelled ctx")
	}
	rel1()
	// Now a slot is free again.
	if rel3 := m.acquire(ctx, "ws"); rel3 == nil {
		t.Fatal("acquire after release failed")
	} else {
		rel3()
	}
}
