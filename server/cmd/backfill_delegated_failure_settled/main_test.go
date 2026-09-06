package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/delegatedrecoverybackfill"
)

func cursorFor(createdAt time.Time, id string) *delegatedrecoverybackfill.Cursor {
	return &delegatedrecoverybackfill.Cursor{CreatedAt: createdAt, ID: id}
}

// A bounded run (--max-batches, SIGTERM) is only resumable if the watermark
// outlives the process. Without this the next run restarts at the front of the
// index — which is exactly where the rows this walk deliberately never settles
// live, so repeated bounded runs would rescan the same prefix forever.
func TestCheckpointRoundTripsTheWatermark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if got, err := readCheckpoint(path); err != nil || got != nil {
		t.Fatalf("missing checkpoint = %v, %v; want nil, nil", got, err)
	}

	createdAt := time.Date(2026, 8, 31, 9, 30, 0, 123456000, time.UTC)
	want := cursorFor(createdAt, "6f1a0d4e-0000-4000-8000-000000000001")
	if err := writeCheckpoint(path, want); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}
	got, err := readCheckpoint(path)
	if err != nil {
		t.Fatalf("readCheckpoint: %v", err)
	}
	if got == nil || got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("checkpoint round trip = %+v, want %+v", got, want)
	}
}

// The file is replaced atomically, so a crash mid-write cannot leave a
// truncated watermark that resumes in the wrong place.
func TestCheckpointWriteLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	for i := range 3 {
		if err := writeCheckpoint(path, cursorFor(time.Unix(int64(i), 0).UTC(), "id")); err != nil {
			t.Fatalf("writeCheckpoint %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "checkpoint.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("checkpoint dir = %v, want only checkpoint.json", names)
	}
}

func TestResolveStartPrefersExplicitFlagsOverTheCheckpointFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	stale := cursorFor(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "stale-id")
	if err := writeCheckpoint(path, stale); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	got, err := resolveStart(path, "2026-08-31T09:30:00Z", "override-id")
	if err != nil {
		t.Fatalf("resolveStart: %v", err)
	}
	if got == nil || got.ID != "override-id" {
		t.Fatalf("resolveStart = %+v, want the explicit override to win", got)
	}

	got, err = resolveStart(path, "", "")
	if err != nil {
		t.Fatalf("resolveStart from file: %v", err)
	}
	if got == nil || got.ID != "stale-id" {
		t.Fatalf("resolveStart from file = %+v, want the persisted watermark", got)
	}

	if _, err := resolveStart("", "2026-08-31T09:30:00Z", ""); err == nil {
		t.Fatal("half an explicit cursor was accepted; it would silently resume from the wrong place")
	}
	if _, err := resolveStart("", "not-a-timestamp", "some-id"); err == nil {
		t.Fatal("an unparseable --after-created-at was accepted")
	}
}

func TestResolveStartWithoutAnySourceStartsFromTheBeginning(t *testing.T) {
	got, err := resolveStart("", "", "")
	if err != nil || got != nil {
		t.Fatalf("resolveStart = %v, %v; want nil, nil", got, err)
	}
}
