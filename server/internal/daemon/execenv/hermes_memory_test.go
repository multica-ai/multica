package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHermesMemoryProfileSegment covers the store segment derived from a
// resolved source home: named profiles and the default home stay readable (an
// operator has to be able to find the directory), an out-of-tree custom home
// falls back to a hash of its path.
func TestHermesMemoryProfileSegment(t *testing.T) {
	t.Parallel()

	if got := hermesMemoryProfileSegment(""); got != "default" {
		t.Fatalf("empty source home segment = %q, want default", got)
	}
	if got := hermesMemoryProfileSegment(platformDefaultHermesHome()); got != "default" {
		t.Fatalf("platform default segment = %q, want default", got)
	}
	// A profile of the native root keeps its bare, readable name.
	native := filepath.Join(platformDefaultHermesHome(), "profiles", "research")
	if got := hermesMemoryProfileSegment(native); got != "research" {
		t.Fatalf("native named profile segment = %q, want research", got)
	}
	// Same profile name under a different root must not share a memory line.
	root := t.TempDir()
	foreign := hermesMemoryProfileSegment(filepath.Join(root, "profiles", "research"))
	if foreign == "research" {
		t.Fatalf("foreign-root profile collided with the native one on %q", foreign)
	}
	if !strings.HasPrefix(foreign, "research_") {
		t.Fatalf("foreign-root profile segment = %q, want a research_<hash> form", foreign)
	}

	custom := hermesMemoryProfileSegment(filepath.Join(root, "custom-home"))
	if !strings.HasPrefix(custom, "h_") {
		t.Fatalf("custom home segment = %q, want an h_ hash", custom)
	}
	// Same basename in a different location must not collide.
	other := hermesMemoryProfileSegment(filepath.Join(root, "nested", "custom-home"))
	if custom == other {
		t.Fatalf("distinct custom homes collided on %q", custom)
	}
}

// TestHermesMemoryStorePathLayout pins the on-disk layout the documented
// one-off import depends on: <profile dir>/hermes-state/<agent>/<profile>.
func TestHermesMemoryStorePathLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agent := "11111111-2222-3333-4444-555555555555"
	got := HermesMemoryStorePath("", agent, filepath.Join(platformDefaultHermesHome(), "profiles", "research"))
	want := filepath.Join(home, ".multica", hermesMemoryStoreRoot, agent, "research")
	if got != want {
		t.Fatalf("store path = %q, want %q", got, want)
	}
}

// TestHermesMemoryStorePathDisabled covers the two ways memory stays
// task-local: no agent to key on, and the operator rollback switch.
func TestHermesMemoryStorePathDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := HermesMemoryStorePath("", "", ""); got != "" {
		t.Fatalf("store path without an agent = %q, want empty", got)
	}

	t.Setenv(MulticaHermesTaskMemoryEnv, "1")
	if got := HermesMemoryStorePath("", "agent-1", ""); got != "" {
		t.Fatalf("store path with the rollback switch on = %q, want empty", got)
	}
}

// TestPrepareHermesHomeMemoryStorePersistsAcrossTasks is the regression test for
// #6638: an agent with skills bound must keep its memory between tasks. Each
// task gets a fresh overlay home, so the memory can only survive if memories/
// resolves to the shared per-agent store.
func TestPrepareHermesHomeMemoryStorePersistsAcrossTasks(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "hermes-state", "agent-1", "default")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	firstTask := filepath.Join(t.TempDir(), "hermes-home")
	if err := prepareHermesHome(firstTask, sharedHome, false, skills, nil, store, testLogger()); err != nil {
		t.Fatalf("prepare first task: %v", err)
	}
	// Hermes writes memory back into <HERMES_HOME>/memories during the run.
	mustWrite(t, filepath.Join(firstTask, "memories", "MEMORY.md"), "prefers tabs")

	secondTask := filepath.Join(t.TempDir(), "hermes-home")
	if err := prepareHermesHome(secondTask, sharedHome, false, skills, nil, store, testLogger()); err != nil {
		t.Fatalf("prepare second task: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(secondTask, "memories", "MEMORY.md"))
	if err != nil {
		t.Fatalf("second task lost the agent's memory: %v", err)
	}
	if string(got) != "prefers tabs" {
		t.Fatalf("memory content = %q, want %q", got, "prefers tabs")
	}

	// The link must point at the store, not hold a copy — otherwise the next
	// task's writes would diverge from it.
	fi, err := os.Lstat(filepath.Join(secondTask, "memories"))
	if err != nil {
		t.Fatalf("lstat memories: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("memories is not a link (mode %v)", fi.Mode())
	}
}

// TestPrepareHermesHomeMemoryStoreIsolatesAgents guards the isolation promise
// the store makes: one agent's memory must never be visible to another.
func TestPrepareHermesHomeMemoryStoreIsolatesAgents(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	stateRoot := t.TempDir()
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	homeA := filepath.Join(t.TempDir(), "hermes-home")
	if err := prepareHermesHome(homeA, sharedHome, false, skills, nil, filepath.Join(stateRoot, "agent-a", "default"), testLogger()); err != nil {
		t.Fatalf("prepare agent A: %v", err)
	}
	mustWrite(t, filepath.Join(homeA, "memories", "MEMORY.md"), "agent A secret")

	homeB := filepath.Join(t.TempDir(), "hermes-home")
	if err := prepareHermesHome(homeB, sharedHome, false, skills, nil, filepath.Join(stateRoot, "agent-b", "default"), testLogger()); err != nil {
		t.Fatalf("prepare agent B: %v", err)
	}
	if _, err := os.Stat(filepath.Join(homeB, "memories", "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatalf("agent B can see agent A's memory (err = %v)", err)
	}
}

// TestPrepareHermesHomeMemoryStoreRollback verifies the operator switch: with no
// store the overlay keeps a plain task-local memories dir, i.e. the old
// behaviour, and never leaves a dangling link behind.
func TestPrepareHermesHomeMemoryStoreRollback(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", testLogger()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	fi, err := os.Lstat(filepath.Join(hermesHome, "memories"))
	if err != nil {
		t.Fatalf("lstat memories: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("memories should be a real dir without a store, got a link")
	}
	if !fi.IsDir() {
		t.Fatalf("memories is not a directory (mode %v)", fi.Mode())
	}
}

// TestPrepareHermesHomeRollbackDetachesExistingStoreLink is the regression test
// for the rollback switch on a reused workdir. The overlay still carries the
// link to the persistent store from the previous run, and MkdirAll would follow
// it and silently succeed — leaving the task writing to a store the daemon no
// longer marks active, and the GC free to reclaim it mid-task.
func TestPrepareHermesHomeRollbackDetachesExistingStoreLink(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "hermes-state", "agent-1", "default")
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	// Run once with the store mounted, as a pre-rollback task would.
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, store, testLogger()); err != nil {
		t.Fatalf("prepare with store: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "memories", "MEMORY.md"), "persistent memory")

	// Operator flips MULTICA_HERMES_TASK_MEMORY=1; the same overlay is reused.
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", testLogger()); err != nil {
		t.Fatalf("prepare after rollback: %v", err)
	}

	fi, err := os.Lstat(filepath.Join(hermesHome, "memories"))
	if err != nil {
		t.Fatalf("lstat memories: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("rollback left the store link in place; the task still writes to the persistent store")
	}
	if _, err := os.Stat(filepath.Join(hermesHome, "memories", "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatalf("rollback should give the task an empty memories dir (err = %v)", err)
	}
	// The store itself must survive, so flipping the switch back restores memory.
	if _, err := os.Stat(filepath.Join(store, "MEMORY.md")); err != nil {
		t.Fatalf("rollback destroyed the persistent store: %v", err)
	}
}

// TestPrepareHermesHomeRollbackKeepsTaskLocalMemories checks the other reuse
// case: with no store, an existing real memories dir is this task's own memory
// and must be preserved across reuse.
func TestPrepareHermesHomeRollbackKeepsTaskLocalMemories(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", testLogger()); err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "memories", "MEMORY.md"), "task memory")

	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", testLogger()); err != nil {
		t.Fatalf("reuse prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hermesHome, "memories", "MEMORY.md")); err != nil {
		t.Fatalf("reuse dropped the task's own memory: %v", err)
	}
}

// TestMigrateHermesTaskMemoriesFailureKeepsSource is the regression test for
// migration data loss: when an entry cannot be carried over, the source must
// survive and the overlay must fail, because the caller deletes the source
// directory as soon as migration reports success.
func TestMigrateHermesTaskMemoriesFailureKeepsSource(t *testing.T) {
	t.Parallel()
	taskDir := t.TempDir()
	storeDir := t.TempDir()

	mustWrite(t, filepath.Join(taskDir, "MEMORY.md"), "first")
	mustWrite(t, filepath.Join(taskDir, "USER.md"), "second")
	// Make one entry uncopyable the way a cross-filesystem move fails: a
	// directory whose contents cannot be read.
	blocked := filepath.Join(taskDir, "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatalf("create blocked dir: %v", err)
	}
	mustWrite(t, filepath.Join(blocked, "note.md"), "unreadable")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod blocked dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	err := migrateHermesTaskMemories(taskDir, storeDir, testLogger())
	if err == nil {
		t.Fatalf("migration reported success despite an unreadable entry; the caller would delete the source")
	}

	// Source intact.
	for _, name := range []string{"MEMORY.md", "USER.md"} {
		if _, statErr := os.Stat(filepath.Join(taskDir, name)); statErr != nil {
			t.Fatalf("source %s was lost on a failed migration: %v", name, statErr)
		}
	}
	// Store rolled back — a half-populated store would read as accumulated
	// memory on the next run and block the retry.
	left, readErr := os.ReadDir(storeDir)
	if readErr != nil {
		t.Fatalf("read store: %v", readErr)
	}
	if len(left) != 0 {
		t.Fatalf("failed migration left %d entries in the store, want 0", len(left))
	}
}

// TestMountHermesMemoriesUnreadableStoreKeepsSource covers the entry-point I/O
// error: an unreadable store must not read as "nothing to migrate", because the
// caller deletes the source directory the moment migration reports success.
func TestMountHermesMemoriesUnreadableStoreKeepsSource(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	hermesHome := t.TempDir()
	memories := filepath.Join(hermesHome, "memories")
	mustWrite(t, filepath.Join(memories, "MEMORY.md"), "irreplaceable")

	storeDir := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := os.Chmod(storeDir, 0o000); err != nil {
		t.Fatalf("chmod store: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(storeDir, 0o700) })

	if err := mountHermesMemories(hermesHome, storeDir, testLogger()); err == nil {
		t.Fatalf("mount reported success against an unreadable store; the source would be deleted")
	}
	if _, err := os.Stat(filepath.Join(memories, "MEMORY.md")); err != nil {
		t.Fatalf("source memory was lost after a failed mount: %v", err)
	}
}

// TestMigrateHermesTaskMemoriesRefusesUnsupportedEntries checks that an entry
// the migration will not copy fails closed instead of being skipped — a skip
// plus the caller's source deletion is a silent loss.
func TestMigrateHermesTaskMemoriesRefusesUnsupportedEntries(t *testing.T) {
	t.Parallel()
	taskDir := t.TempDir()
	storeDir := filepath.Join(t.TempDir(), "store")

	mustWrite(t, filepath.Join(taskDir, "MEMORY.md"), "regular")
	if err := os.Symlink(filepath.Join(taskDir, "MEMORY.md"), filepath.Join(taskDir, "LINK.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := migrateHermesTaskMemories(taskDir, storeDir, testLogger()); err == nil {
		t.Fatalf("migration skipped a symlink and reported success")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "MEMORY.md")); err != nil {
		t.Fatalf("source was lost on a refused migration: %v", err)
	}
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("refused migration left a store behind (err = %v)", err)
	}
	// A nested symlink must be refused too — copyDirTree would have skipped it.
	nested := t.TempDir()
	mustWrite(t, filepath.Join(nested, "notes", "note.md"), "regular")
	if err := os.Symlink(filepath.Join(nested, "notes", "note.md"), filepath.Join(nested, "notes", "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := migrateHermesTaskMemories(nested, filepath.Join(t.TempDir(), "store"), testLogger()); err == nil {
		t.Fatalf("migration skipped a nested symlink and reported success")
	}
}

// TestMigrateHermesTaskMemoriesConcurrentFirstWriterWins covers two tasks of the
// same agent migrating at once, which is reachable in the window right after a
// daemon upgrade. Exactly one store must be published, and it must be one task's
// complete tree — never a mix of both.
func TestMigrateHermesTaskMemoriesConcurrentFirstWriterWins(t *testing.T) {
	t.Parallel()
	storeDir := filepath.Join(t.TempDir(), "agent", "default")

	const tasks = 4
	sources := make([]string, tasks)
	for i := range sources {
		sources[i] = t.TempDir()
		// Same filenames, distinct contents: a store that interleaves the two
		// would be detectable as a mixed marker set.
		mustWrite(t, filepath.Join(sources[i], "MEMORY.md"), fmt.Sprintf("task-%d", i))
		mustWrite(t, filepath.Join(sources[i], "USER.md"), fmt.Sprintf("task-%d", i))
	}

	var wg sync.WaitGroup
	errs := make([]error, tasks)
	start := make(chan struct{})
	for i := range sources {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = migrateHermesTaskMemories(sources[i], storeDir, testLogger())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration %d failed: %v", i, err)
		}
	}

	memory, err := os.ReadFile(filepath.Join(storeDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("store is missing MEMORY.md after concurrent migration: %v", err)
	}
	user, err := os.ReadFile(filepath.Join(storeDir, "USER.md"))
	if err != nil {
		t.Fatalf("store is missing USER.md after concurrent migration: %v", err)
	}
	if string(memory) != string(user) {
		t.Fatalf("store interleaved two tasks: MEMORY.md=%q USER.md=%q", memory, user)
	}

	// Every source survives: a task that lost the race must not have had its
	// directory reported as migrated.
	for i, src := range sources {
		if _, err := os.Stat(filepath.Join(src, "MEMORY.md")); err != nil {
			t.Fatalf("source %d was consumed by the migration: %v", i, err)
		}
	}
	// No staging directories left behind next to the store.
	siblings, err := os.ReadDir(filepath.Dir(storeDir))
	if err != nil {
		t.Fatalf("read store parent: %v", err)
	}
	if len(siblings) != 1 {
		t.Fatalf("expected only the store next to itself, got %d entries", len(siblings))
	}
}

// TestPromoteHermesMemoryStagingClassifiesRemoveFailures covers the publish
// latch. Losing the race must be positively confirmed, never inferred from any
// os.Remove failure: the caller deletes the task's source directory on
// (false, nil), so a permission or I/O error there is a data-loss path.
func TestPromoteHermesMemoryStagingClassifiesRemoveFailures(t *testing.T) {
	t.Parallel()

	// A genuinely non-empty store means another task published first.
	t.Run("populated store loses the race", func(t *testing.T) {
		t.Parallel()
		parent := t.TempDir()
		storeDir := filepath.Join(parent, "default")
		mustWrite(t, filepath.Join(storeDir, "MEMORY.md"), "winner")
		staging := filepath.Join(parent, ".default.migrating-x")
		mustWrite(t, filepath.Join(staging, "MEMORY.md"), "loser")

		promoted, err := promoteHermesMemoryStaging(staging, storeDir)
		if err != nil {
			t.Fatalf("a populated store should be a lost race, not an error: %v", err)
		}
		if promoted {
			t.Fatalf("promote clobbered a store another task had published")
		}
		got, readErr := os.ReadFile(filepath.Join(storeDir, "MEMORY.md"))
		if readErr != nil || string(got) != "winner" {
			t.Fatalf("winner's store was modified: content=%q err=%v", got, readErr)
		}
	})

	// An empty store that cannot be removed is an I/O failure, not a race.
	t.Run("unremovable empty store fails closed", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		parent := t.TempDir()
		storeDir := filepath.Join(parent, "default")
		if err := os.MkdirAll(storeDir, 0o700); err != nil {
			t.Fatalf("create store: %v", err)
		}
		staging := filepath.Join(parent, ".default.migrating-x")
		mustWrite(t, filepath.Join(staging, "MEMORY.md"), "irreplaceable")

		// Removing a directory needs write permission on its parent.
		if err := os.Chmod(parent, 0o500); err != nil {
			t.Fatalf("chmod parent: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

		promoted, err := promoteHermesMemoryStaging(staging, storeDir)
		if promoted {
			t.Fatalf("promote reported success against an unremovable store")
		}
		if err == nil {
			t.Fatalf("a permission error was reported as a lost race; the caller would delete the source")
		}
	})
}

// TestMigrateHermesTaskMemoriesCopiesRatherThanMoves pins the cross-filesystem
// property: migration must not depend on rename, and must carry nested dirs.
func TestMigrateHermesTaskMemoriesCopiesRatherThanMoves(t *testing.T) {
	t.Parallel()
	taskDir := t.TempDir()
	storeDir := t.TempDir()

	mustWrite(t, filepath.Join(taskDir, "MEMORY.md"), "top level")
	mustWrite(t, filepath.Join(taskDir, "notes", "deep", "note.md"), "nested")

	if err := migrateHermesTaskMemories(taskDir, storeDir, testLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, rel := range []string{"MEMORY.md", filepath.Join("notes", "deep", "note.md")} {
		if _, err := os.Stat(filepath.Join(storeDir, rel)); err != nil {
			t.Fatalf("store is missing %s: %v", rel, err)
		}
		// Copy, not move: the source stays until the caller removes the dir.
		if _, err := os.Stat(filepath.Join(taskDir, rel)); err != nil {
			t.Fatalf("migration moved %s instead of copying it: %v", rel, err)
		}
	}
}

// TestPrepareHermesHomeMigratesTaskLocalMemories covers the upgrade path: a
// workdir reused from a pre-store daemon still holds a real memories/ dir, whose
// contents must move into the (empty) store rather than be dropped.
func TestPrepareHermesHomeMigratesTaskLocalMemories(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	// Simulate the old layout: a task-local memories dir with accumulated state.
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", testLogger()); err != nil {
		t.Fatalf("prepare pre-store overlay: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "memories", "MEMORY.md"), "carried over")

	store := filepath.Join(t.TempDir(), "hermes-state", "agent-1", "default")
	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, store, testLogger()); err != nil {
		t.Fatalf("prepare with store: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(store, "MEMORY.md"))
	if err != nil {
		t.Fatalf("task-local memory was not migrated into the store: %v", err)
	}
	if string(got) != "carried over" {
		t.Fatalf("migrated content = %q, want %q", got, "carried over")
	}
}

// TestPrepareHermesHomeMigrationKeepsExistingStore is the other half of the
// upgrade path: an agent that already accumulated memory must never have it
// overwritten by a stale task-local dir.
func TestPrepareHermesHomeMigrationKeepsExistingStore(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", testLogger()); err != nil {
		t.Fatalf("prepare pre-store overlay: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "memories", "MEMORY.md"), "stale task copy")

	store := filepath.Join(t.TempDir(), "hermes-state", "agent-1", "default")
	mustWrite(t, filepath.Join(store, "MEMORY.md"), "authoritative agent memory")

	if err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, store, testLogger()); err != nil {
		t.Fatalf("prepare with store: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(store, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read store memory: %v", err)
	}
	if string(got) != "authoritative agent memory" {
		t.Fatalf("store memory was overwritten by the task-local copy: %q", got)
	}
}

// TestPruneHermesMemoryStores covers the GC contract: stores idle past retention
// are reclaimed, recently-used ones are kept, and a store a live task holds is
// never removed.
func TestPruneHermesMemoryStores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := filepath.Join(home, ".multica", hermesMemoryStoreRoot)
	idle := filepath.Join(root, "agent-idle", "default")
	fresh := filepath.Join(root, "agent-fresh", "default")
	held := filepath.Join(root, "agent-held", "default")
	for _, dir := range []string{idle, fresh, held} {
		mustWrite(t, filepath.Join(dir, "MEMORY.md"), "remembered")
	}

	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)
	for _, dir := range []string{idle, held} {
		if err := os.Chtimes(filepath.Join(dir, "MEMORY.md"), old, old); err != nil {
			t.Fatalf("age store: %v", err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("age store dir: %v", err)
		}
	}

	reserve := func(storeDir string) (func(), bool) {
		if storeDir == held {
			return nil, false // a live task holds it
		}
		return func() {}, true
	}

	removed, freed := PruneHermesMemoryStores("", 14*24*time.Hour, now, reserve, testLogger())
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if freed <= 0 {
		t.Fatalf("bytesFreed = %d, want > 0", freed)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Fatalf("idle store survived the prune (err = %v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("recently used store was reclaimed: %v", err)
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("store held by a live task was reclaimed: %v", err)
	}
}

// TestPruneHermesMemoryStoresDisabled documents that retention <= 0 turns the
// pruner off entirely, matching the Codex store knob.
func TestPruneHermesMemoryStoresDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	store := filepath.Join(home, ".multica", hermesMemoryStoreRoot, "agent-1", "default")
	mustWrite(t, filepath.Join(store, "MEMORY.md"), "remembered")
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(store, old, old); err != nil {
		t.Fatalf("age store: %v", err)
	}

	if removed, _ := PruneHermesMemoryStores("", 0, time.Now(), nil, testLogger()); removed != 0 {
		t.Fatalf("removed = %d with pruning disabled, want 0", removed)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("store was reclaimed with pruning disabled: %v", err)
	}
}
