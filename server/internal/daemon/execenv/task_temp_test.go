package execenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeTaskTempDir creates a temp dir the way ensureTaskTempDir does, optionally
// without the execution lock so a pre-lock daemon's leftovers can be modelled.
func makeTaskTempDir(t *testing.T, base, suffix string, withLock bool) string {
	t.Helper()
	dir := filepath.Join(base, TaskTempDirPrefix+suffix)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create task temp dir: %v", err)
	}
	if withLock {
		lock, err := LockTaskTempDir(dir)
		if err != nil {
			t.Fatalf("LockTaskTempDir(): %v", err)
		}
		ReleaseTaskTempLock(lock)
	}
	return dir
}

// TestPruneTaskTempDirsHoldsOffLiveDirThenReclaimsIt is the core contract: a
// directory whose owner still holds the lock is never removed, and the same
// sweep reclaims it as soon as that lock is gone.
//
// The lock here is held by THIS process, which is the case an in-memory active
// set would be needed for and the case the lock covers for free: flock and
// LockFileEx are held by an open file description, not by a process, so the
// sweep's own probe loses to it exactly as another daemon's would.
func TestPruneTaskTempDirsHoldsOffLiveDirThenReclaimsIt(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, TaskTempDirPrefix+"live")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create task temp dir: %v", err)
	}
	lock, err := LockTaskTempDir(dir)
	if err != nil {
		t.Fatalf("LockTaskTempDir(): %v", err)
	}

	// legacyTTL 0 so nothing here can be reclaimed on age: the lock is the only
	// thing under test. `now` is far in the future for the same reason.
	future := time.Now().Add(365 * 24 * time.Hour)
	if removed, _ := PruneTaskTempDirs(base, 0, future, testLogger()); removed != 0 {
		t.Fatalf("prune removed %d dirs while the lock was held, want 0", removed)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("live task temp dir was removed: %v", err)
	}

	ReleaseTaskTempLock(lock)

	removed, _ := PruneTaskTempDirs(base, 0, future, testLogger())
	if removed != 1 {
		t.Fatalf("prune removed %d dirs after the lock was released, want 1", removed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("task temp dir still present after prune: %v", err)
	}
}

// TestPruneTaskTempDirsReclaimsUnlockedDirImmediately pins that age plays no
// part once liveness is answerable: a directory released seconds ago is gone on
// the next cycle, no TTL wait. This is what makes the end-of-task RemoveAll
// failing (the Windows sharing violation in #7364) cost nothing.
func TestPruneTaskTempDirsReclaimsUnlockedDirImmediately(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "released", true)
	payload := filepath.Join(dir, "node-compile-cache")
	if err := os.WriteFile(payload, make([]byte, 4096), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	removed, bytesFreed := PruneTaskTempDirs(base, 0, time.Now(), testLogger())
	if removed != 1 {
		t.Fatalf("prune removed %d dirs, want 1", removed)
	}
	if bytesFreed < 4096 {
		t.Fatalf("prune reported %d bytes freed, want at least 4096", bytesFreed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("task temp dir still present after prune: %v", err)
	}
}

// TestPruneTaskTempDirsLegacyDirsFallBackToTTL covers directories left by a
// daemon predating the lock: nothing recorded liveness for them, so age is the
// only signal available. They carry task content — that is both what makes them
// worth reclaiming and what distinguishes them from a directory mid-publication
// (see TestPruneTaskTempDirsLegacyBranchNeedsContent).
func TestPruneTaskTempDirsLegacyDirsFallBackToTTL(t *testing.T) {
	const legacyTTL = 72 * time.Hour

	legacyDir := func(t *testing.T, base string) string {
		t.Helper()
		dir := makeTaskTempDir(t, base, "legacy", false)
		if err := os.WriteFile(filepath.Join(dir, "payload"), make([]byte, 1024), 0o600); err != nil {
			t.Fatalf("write payload: %v", err)
		}
		return dir
	}

	t.Run("young legacy dir is kept", func(t *testing.T) {
		base := t.TempDir()
		dir := legacyDir(t, base)
		if removed, _ := PruneTaskTempDirs(base, legacyTTL, time.Now(), testLogger()); removed != 0 {
			t.Fatalf("prune removed %d young legacy dirs, want 0", removed)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("young legacy dir was removed: %v", err)
		}
	})

	t.Run("legacy dir past the TTL is reclaimed", func(t *testing.T) {
		base := t.TempDir()
		dir := legacyDir(t, base)
		if removed, _ := PruneTaskTempDirs(base, legacyTTL, time.Now().Add(legacyTTL+time.Hour), testLogger()); removed != 1 {
			t.Fatalf("prune removed %d expired legacy dirs, want 1", removed)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("expired legacy dir still present: %v", err)
		}
	})

	t.Run("zero TTL disables the legacy branch", func(t *testing.T) {
		base := t.TempDir()
		dir := legacyDir(t, base)
		if removed, _ := PruneTaskTempDirs(base, 0, time.Now().Add(10*365*24*time.Hour), testLogger()); removed != 0 {
			t.Fatalf("prune removed %d legacy dirs with the TTL disabled, want 0", removed)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("legacy dir was removed with the TTL disabled: %v", err)
		}
	})
}

// TestPruneTaskTempDirsOnlyTouchesOwnDirs guards the blast radius: the temp base
// is usually a shared /tmp, so anything without our prefix — and the base
// itself — has to survive a sweep that is otherwise willing to delete on age.
func TestPruneTaskTempDirsOnlyTouchesOwnDirs(t *testing.T) {
	base := t.TempDir()
	foreignDir := filepath.Join(base, "someone-elses-dir")
	if err := os.Mkdir(foreignDir, 0o700); err != nil {
		t.Fatalf("create foreign dir: %v", err)
	}
	foreignFile := filepath.Join(base, "unrelated.sock")
	if err := os.WriteFile(foreignFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write foreign file: %v", err)
	}
	// A FILE carrying our prefix is not one of our directories either.
	prefixedFile := filepath.Join(base, TaskTempDirPrefix+"not-a-dir")
	if err := os.WriteFile(prefixedFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write prefixed file: %v", err)
	}

	removed, _ := PruneTaskTempDirs(base, time.Hour, time.Now().Add(10*365*24*time.Hour), testLogger())
	if removed != 0 {
		t.Fatalf("prune removed %d entries, want 0", removed)
	}
	for _, path := range []string{base, foreignDir, foreignFile, prefixedFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("prune touched %s: %v", path, err)
		}
	}
}

// TestPruneTaskTempDirsMissingBaseIsNotAnError: the base may not exist yet on a
// daemon that has never run a task, and a GC cycle must not care.
func TestPruneTaskTempDirsMissingBaseIsNotAnError(t *testing.T) {
	removed, bytesFreed := PruneTaskTempDirs(filepath.Join(t.TempDir(), "nope"), time.Hour, time.Now(), testLogger())
	if removed != 0 || bytesFreed != 0 {
		t.Fatalf("prune over a missing base = (%d, %d), want (0, 0)", removed, bytesFreed)
	}
}

// TestLockTaskTempDirPublishesAnAlreadyHeldMarker pins the publication order:
// when LockTaskTempDir returns, .task_lock exists AND is held, with no claim
// file left behind. If the marker were created first and locked second, a sweep
// landing in between would lock it, read the owner as dead, and delete the
// directory a task is about to start using.
func TestLockTaskTempDirPublishesAnAlreadyHeldMarker(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, TaskTempDirPrefix+"publishing")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create task temp dir: %v", err)
	}
	lock, err := LockTaskTempDir(dir)
	if err != nil {
		t.Fatalf("LockTaskTempDir(): %v", err)
	}
	t.Cleanup(func() { ReleaseTaskTempLock(lock) })

	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); err != nil {
		t.Fatalf("marker not published: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, taskTempLockClaimFile)); !os.IsNotExist(err) {
		t.Fatalf("claim file left behind, stat err = %v", err)
	}
	if got := classifyTaskTempDir(dir, testLogger()); got != taskTempInUse {
		t.Fatalf("classify = %v, want taskTempInUse", got)
	}
}

// TestPruneTaskTempDirsSpareDirMidClaim covers the window itself: a directory
// whose owner has taken the claim but not yet published it must survive a sweep
// under every legacy setting, including ones whose TTL the directory has
// already outlived. Age must play no part in what protects it.
func TestPruneTaskTempDirsSpareDirMidClaim(t *testing.T) {
	for _, legacyTTL := range []time.Duration{0, time.Nanosecond, time.Millisecond, time.Hour, 72 * time.Hour} {
		t.Run(legacyTTL.String(), func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, TaskTempDirPrefix+"claiming")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatalf("create task temp dir: %v", err)
			}
			// Exactly the on-disk state between claim and publish.
			claim, err := openLockFile(filepath.Join(dir, taskTempLockClaimFile))
			if err != nil {
				t.Fatalf("open claim: %v", err)
			}
			if ok, err := lockFileExclusiveNonBlocking(claim); err != nil || !ok {
				t.Fatalf("lock claim: ok=%v err=%v", ok, err)
			}
			t.Cleanup(func() { releaseLockFile(claim) })

			// now is pushed past the TTL on purpose: what protects this state
			// must not be that it is younger than the TTL, because legacyTTL
			// accepts any duration and an owner can stall between MkdirTemp
			// and the rename.
			now := time.Now().Add(legacyTTL + time.Second)
			if removed, _ := PruneTaskTempDirs(base, legacyTTL, now, testLogger()); removed != 0 {
				t.Fatalf("prune removed %d dirs mid-claim, want 0", removed)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("prune removed a directory whose owner was mid-claim: %v", err)
			}
		})
	}
}

// TestLockTaskTempDirRacesPruneCleanly is the concurrency regression: a sweep
// running flat out against tasks publishing their temp dirs must never hand a
// task a directory that has been deleted underneath it.
func TestLockTaskTempDirRacesPruneCleanly(t *testing.T) {
	base := t.TempDir()
	stop := make(chan struct{})
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		for {
			select {
			case <-stop:
				return
			default:
				PruneTaskTempDirs(base, 0, time.Now(), testLogger())
			}
		}
	}()

	for i := 0; i < 200; i++ {
		dir, err := os.MkdirTemp(base, TaskTempDirPrefix)
		if err != nil {
			t.Fatalf("MkdirTemp(): %v", err)
		}
		lock, err := LockTaskTempDir(dir)
		if err != nil {
			t.Fatalf("LockTaskTempDir() on iteration %d: %v", i, err)
		}
		// What the task does next: write into the TMPDIR it was handed.
		if err := os.WriteFile(filepath.Join(dir, "payload"), []byte("x"), 0o600); err != nil {
			t.Fatalf("task temp dir vanished under its owner on iteration %d: %v", i, err)
		}
		ReleaseTaskTempLock(lock)
		_ = RemoveTaskTempDir(dir)
	}

	close(stop)
	<-swept
}

// TestPruneTaskTempDirsSparesDirBeforeItsClaimExists covers the other half of
// the publication window — the instant between os.MkdirTemp and the claim file
// appearing, when the directory is simply empty. Nothing can be locked yet, so
// the only thing that can protect it is that it holds no task content.
func TestPruneTaskTempDirsSparesDirBeforeItsClaimExists(t *testing.T) {
	for _, legacyTTL := range []time.Duration{time.Nanosecond, time.Millisecond, 72 * time.Hour} {
		t.Run(legacyTTL.String(), func(t *testing.T) {
			base := t.TempDir()
			dir, err := os.MkdirTemp(base, TaskTempDirPrefix)
			if err != nil {
				t.Fatalf("MkdirTemp(): %v", err)
			}
			now := time.Now().Add(legacyTTL + time.Second)
			if removed, _ := PruneTaskTempDirs(base, legacyTTL, now, testLogger()); removed != 0 {
				t.Fatalf("prune removed %d dirs before their claim existed, want 0", removed)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("prune removed a directory whose owner had not claimed it yet: %v", err)
			}
		})
	}
}

// TestPruneTaskTempDirsLegacyBranchNeedsContent states the invariant the two
// tests above depend on, so that relaxing it fails here rather than in a race:
// the age branch reclaims a leftover holding task content, and nothing else.
func TestPruneTaskTempDirsLegacyBranchNeedsContent(t *testing.T) {
	base := t.TempDir()
	empty := makeTaskTempDir(t, base, "empty-legacy", false)
	withContent := makeTaskTempDir(t, base, "used-legacy", false)
	if err := os.WriteFile(filepath.Join(withContent, "payload"), make([]byte, 1024), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	removed, _ := PruneTaskTempDirs(base, time.Hour, time.Now().Add(48*time.Hour), testLogger())
	if removed != 1 {
		t.Fatalf("prune removed %d dirs, want 1 (only the one holding content)", removed)
	}
	if _, err := os.Stat(withContent); !os.IsNotExist(err) {
		t.Fatalf("expired legacy dir with content survived: %v", err)
	}
	if _, err := os.Stat(empty); err != nil {
		t.Fatalf("empty legacy dir was reclaimed on age: %v", err)
	}
}

// TestPruneTaskTempDirsRechecksLockBeforeRemoving is the TOCTOU regression: the
// sweep's legacy decision is several separate observations, and a directory can
// finish publishing in the middle of them. The barrier below reproduces exactly
// that interleaving — sweep classifies the directory as legacy, then the owner
// publishes its marker and the task writes — and the sweep must notice before
// it removes anything.
//
// Age deliberately cannot save this one: `now` is far past the TTL, and the
// write inside the barrier would normally refresh the directory's mtime, which
// is precisely the implicit timing crutch this pins against.
func TestPruneTaskTempDirsRechecksLockBeforeRemoving(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "publishing-mid-sweep", false)
	// Content at the top of the sweep, so the legacy branch's content and age
	// gates both pass and it commits to removing this directory.
	if err := os.WriteFile(filepath.Join(dir, "payload"), make([]byte, 1024), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	var lock *os.File
	barrierRuns := 0
	taskTempSweepBeforeRecheck = func(swept string) {
		if swept != dir {
			return
		}
		barrierRuns++
		// The owner wins the race: it publishes, and its task starts writing.
		var err error
		if lock, err = LockTaskTempDir(dir); err != nil {
			t.Errorf("LockTaskTempDir() at the barrier: %v", err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "task-output"), []byte("live"), 0o600); err != nil {
			t.Errorf("task write at the barrier: %v", err)
		}
	}
	t.Cleanup(func() {
		taskTempSweepBeforeRecheck = nil
		ReleaseTaskTempLock(lock)
	})

	removed, _ := PruneTaskTempDirs(base, time.Hour, time.Now().Add(72*time.Hour), testLogger())
	if barrierRuns != 1 {
		t.Fatalf("barrier ran %d times, want 1 — the sweep never reached the removal decision", barrierRuns)
	}
	if removed != 0 {
		t.Fatalf("sweep removed %d dirs after the owner published mid-decision, want 0", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "task-output")); err != nil {
		t.Fatalf("sweep destroyed a live task's temp dir: %v", err)
	}
	if got := classifyTaskTempDir(dir, testLogger()); got != taskTempInUse {
		t.Fatalf("classify = %v, want taskTempInUse", got)
	}
}
