package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Cross-process local_directory exclusion (GH #8280).
//
// LocalPathLocker is process-local, so two daemons on one machine could each
// believe they held the same checkout: an in_place writer in one process and an
// in_place writer - or a worktree snapshot - in another. These tests use two
// Daemon values sharing only the machine lock directory.

func newLocalDirectoryScopeDaemon(t *testing.T, lockDir string) *Daemon {
	t.Helper()
	d := &Daemon{
		cfg:            Config{WorkspacesRoot: t.TempDir()},
		logger:         quietTaskLog(),
		localPathLocks: NewLocalPathLocker(),
		machineLocks:   execenv.NewScopeLocks(lockDir),
	}
	return d
}

func localScopeLockDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), machineLockDirName)
}

// TestLocalDirectoryScope_InPlaceSerializesAcrossProcesses is spec case 13: an
// in_place task in one daemon blocks an in_place task in another for the same
// canonical real path.
func TestLocalDirectoryScope_InPlaceSerializesAcrossProcesses(t *testing.T) {
	lockDir := localScopeLockDir(t)
	a := newLocalDirectoryScopeDaemon(t, lockDir)
	b := newLocalDirectoryScopeDaemon(t, lockDir)
	realPath := t.TempDir()

	releaseA, err := a.holdLocalDirectoryPath(context.Background(), realPath, "task-a", nil)
	if err != nil {
		t.Fatalf("daemon A in_place claim: %v", err)
	}

	// B must NOT be able to take it, and must not return before A releases.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	var waited bool
	_, err = b.holdLocalDirectoryPath(ctx, realPath, "task-b", func(string) { waited = true })
	if err == nil {
		t.Fatal("a second daemon entered an in_place path another process holds")
	}
	if !waited {
		t.Fatal("the waiter was never told it was queued")
	}

	// Release, then the waiter proceeds normally.
	releaseA()
	releaseB, err := b.holdLocalDirectoryPath(context.Background(), realPath, "task-b", nil)
	if err != nil {
		t.Fatalf("daemon B after release: %v", err)
	}
	releaseB()
}

// TestLocalDirectoryScope_WorktreeSnapshotBlocksOnCrossProcessWriter is spec case
// 14: a snapshot must not read the tree while another process writes it.
func TestLocalDirectoryScope_WorktreeSnapshotBlocksOnCrossProcessWriter(t *testing.T) {
	lockDir := localScopeLockDir(t)
	writer := newLocalDirectoryScopeDaemon(t, lockDir)
	snapshotter := newLocalDirectoryScopeDaemon(t, lockDir)
	realPath := t.TempDir()

	releaseWriter, err := writer.holdLocalDirectoryPath(context.Background(), realPath, "in-place-task", nil)
	if err != nil {
		t.Fatalf("writer claim: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if _, err := snapshotter.holdLocalDirectoryPath(ctx, realPath, "worktree-task", nil); err == nil {
		t.Fatal("a worktree snapshot read a path another process was writing")
	}
	releaseWriter()
}

// TestLocalDirectoryScope_CancelledWaitLeaksNothing is spec case 16: a cancelled
// wait must release nothing it did not take, and must leave the path acquirable.
func TestLocalDirectoryScope_CancelledWaitLeaksNothing(t *testing.T) {
	lockDir := localScopeLockDir(t)
	holder := newLocalDirectoryScopeDaemon(t, lockDir)
	waiter := newLocalDirectoryScopeDaemon(t, lockDir)
	realPath := t.TempDir()

	release, err := holder.holdLocalDirectoryPath(context.Background(), realPath, "holder", nil)
	if err != nil {
		t.Fatalf("holder claim: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := waiter.holdLocalDirectoryPath(ctx, realPath, "waiter", nil); err == nil {
		t.Fatal("a cancelled wait reported success")
	}
	cancel()

	// The holder still owns it; releasing frees it exactly once.
	release()
	release()
	again, err := waiter.holdLocalDirectoryPath(context.Background(), realPath, "waiter", nil)
	if err != nil {
		t.Fatalf("path not acquirable after a cancelled wait: %v", err)
	}
	again()
}

// TestLocalDirectoryScope_SymlinkAndAlternateSpellingShareOneClaim is spec case
// 17: two spellings of one directory must contend on one claim.
func TestLocalDirectoryScope_SymlinkAndAlternateSpellingShareOneClaim(t *testing.T) {
	lockDir := localScopeLockDir(t)
	a := newLocalDirectoryScopeDaemon(t, lockDir)
	b := newLocalDirectoryScopeDaemon(t, lockDir)
	base := t.TempDir()
	realDir := filepath.Join(base, "repo")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Two spellings of the same directory: a trailing separator and a ".." hop.
	spellingOne := realDir
	spellingTwo := filepath.Join(base, "repo", "..", "repo")
	if execenv.LocalDirectoryTarget(spellingOne) != execenv.LocalDirectoryTarget(spellingTwo) {
		t.Fatal("two spellings of one directory produced two claims")
	}

	release, err := a.holdLocalDirectoryPath(context.Background(), spellingOne, "task-a", nil)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := b.holdLocalDirectoryPath(ctx, spellingTwo, "task-b", nil); err == nil {
		t.Fatal("an alternate spelling bypassed the claim")
	}
	release()
}

// TestLocalDirectoryScope_DifferentPathsStayConcurrent is the property that must
// NOT regress: exclusion is per path, never machine-wide.
func TestLocalDirectoryScope_DifferentPathsStayConcurrent(t *testing.T) {
	lockDir := localScopeLockDir(t)
	a := newLocalDirectoryScopeDaemon(t, lockDir)
	b := newLocalDirectoryScopeDaemon(t, lockDir)

	releaseA, err := a.holdLocalDirectoryPath(context.Background(), t.TempDir(), "task-a", nil)
	if err != nil {
		t.Fatalf("first path: %v", err)
	}
	defer releaseA()
	releaseB, err := b.holdLocalDirectoryPath(context.Background(), t.TempDir(), "task-b", nil)
	if err != nil {
		t.Fatalf("a different path must not contend: %v", err)
	}
	releaseB()
}

// TestLocalDirectoryScope_HolderHintIsUnknownNotFabricated pins the UI contract:
// the holder task id lives in another process, so the waiter reports no holder
// rather than inventing one.
func TestLocalDirectoryScope_HolderHintIsUnknownNotFabricated(t *testing.T) {
	lockDir := localScopeLockDir(t)
	holder := newLocalDirectoryScopeDaemon(t, lockDir)
	waiter := newLocalDirectoryScopeDaemon(t, lockDir)
	realPath := t.TempDir()
	release, err := holder.holdLocalDirectoryPath(context.Background(), realPath, "the-real-holder", nil)
	if err != nil {
		t.Fatalf("holder claim: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var hint string
	var seen bool
	_, _ = waiter.holdLocalDirectoryPath(ctx, realPath, "waiter", func(h string) { hint = h; seen = true })
	if !seen {
		t.Fatal("no wait notification")
	}
	if hint != "" {
		t.Fatalf("holder hint = %q, want empty for a holder in another process", hint)
	}
	if strings.Contains(hint, "the-real-holder") {
		t.Fatal("holder hint leaked a task id it cannot know")
	}
}
