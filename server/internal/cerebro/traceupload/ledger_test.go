package traceupload

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sc(task, sess string) Sidecar {
	return Sidecar{Runtime: "claude", TaskID: task, SessionID: sess, WorkspaceID: "ws"}
}

func TestLedgerUpsertAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	l, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := l.Upsert(sc("t1", "s1"), "/tmp/a.jsonl", "hash1", 123, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusPending || e.Attempts != 0 {
		t.Fatalf("unexpected entry: %+v", e)
	}

	// Reload from disk — entry survives a restart (crash-safety).
	l2, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	got := l2.Get("t1|s1")
	if got == nil {
		t.Fatal("entry missing after reload")
	}
	if got.ContentSHA256 != "hash1" || got.ContentSize != 123 || got.FilePath != "/tmp/a.jsonl" {
		t.Errorf("entry not persisted faithfully: %+v", got)
	}
}

func TestLedgerStatusTransitions(t *testing.T) {
	now := time.Now()
	l, _ := LoadLedger("")
	_, _ = l.Upsert(sc("t", "s"), "/f", "h", 1, now)

	if err := l.MarkFailed("t|s", now, "boom"); err != nil {
		t.Fatal(err)
	}
	e := l.Get("t|s")
	if e.Attempts != 1 || e.LastError != "boom" || e.Status != StatusPending {
		t.Fatalf("after fail: %+v", e)
	}

	if err := l.MarkIngested("t|s", now); err != nil {
		t.Fatal(err)
	}
	if l.Get("t|s").Status != StatusIngested {
		t.Fatal("expected ingested")
	}
}

// A duplicate task-close must never resurrect a settled (ingested) entry.
func TestLedgerUpsertDoesNotResurrectSettled(t *testing.T) {
	now := time.Now()
	l, _ := LoadLedger("")
	_, _ = l.Upsert(sc("t", "s"), "/f", "h", 1, now)
	_ = l.MarkIngested("t|s", now)

	e, _ := l.Upsert(sc("t", "s"), "/f2", "h2", 2, now)
	if e.Status != StatusIngested {
		t.Fatalf("settled entry changed status: %+v", e)
	}
}

func TestLedgerPendingOrdered(t *testing.T) {
	l, _ := LoadLedger("")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = l.Upsert(sc("b", "s"), "/f", "h", 1, t0.Add(2*time.Hour))
	_, _ = l.Upsert(sc("a", "s"), "/f", "h", 1, t0.Add(1*time.Hour))
	_, _ = l.Upsert(sc("c", "s"), "/f", "h", 1, t0.Add(3*time.Hour))
	_ = l.MarkIngested("c|s", time.Now())

	pending := l.Pending()
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	// Oldest first.
	if pending[0].Key != "a|s" || pending[1].Key != "b|s" {
		t.Errorf("pending order wrong: %s, %s", pending[0].Key, pending[1].Key)
	}
}

// Session watermarks survive a daemon restart so resume-delta tracking still
// works after the process is killed between tasks A and B.
func TestSessionStatePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	l, _ := LoadLedger(path)
	if err := l.MarkSessionUploaded("sess-1", 4096, "abc123", now); err != nil {
		t.Fatal(err)
	}

	// Daemon restarts → reload the file.
	l2, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	got := l2.SessionState("sess-1")
	if got == nil {
		t.Fatal("session state missing after reload")
	}
	if got.LastUploadedSize != 4096 || got.LastUploadedSHA256 != "abc123" {
		t.Errorf("session state not faithful: %+v", got)
	}
}

// The watermark only moves forward — a smaller follow-up upload (e.g. a fresh
// codex rollout under a reused session_id) must not regress what we already
// know was delivered.
func TestSessionStateMonotonicWatermark(t *testing.T) {
	now := time.Now()
	l, _ := LoadLedger("")
	_ = l.MarkSessionUploaded("sess", 1000, "hashA", now)
	_ = l.MarkSessionUploaded("sess", 200, "hashB", now)
	got := l.SessionState("sess")
	if got.LastUploadedSize != 1000 || got.LastUploadedSHA256 != "hashA" {
		t.Errorf("watermark regressed: %+v", got)
	}
}

// LoadLedger must still accept the legacy bare-array file shape so an upgrade
// does not strand a daemon that already had pending entries on disk.
func TestLoadLedgerAcceptsLegacyArrayShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")
	legacy := `[{"key":"t|s","sidecar":{"Runtime":"claude","TaskID":"t","SessionID":"s","WorkspaceID":"ws"},"file_path":"/x","status":"pending","first_seen":"2026-05-29T12:00:00Z"}]`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("legacy load: %v", err)
	}
	if e := l.Get("t|s"); e == nil || e.Status != StatusPending {
		t.Fatalf("legacy entry not loaded: %+v", e)
	}
}

func TestLoadLedgerMissingFileIsEmpty(t *testing.T) {
	l, err := LoadLedger(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(l.Pending()) != 0 {
		t.Fatal("expected empty ledger")
	}
}
