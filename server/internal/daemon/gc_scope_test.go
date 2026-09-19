package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// Cross-process lifecycle safety (GH #8280 Blocker A).
//
// Two daemon PROCESSES now share one work-state scope, so the in-process guard
// maps cannot be the boundary that protects a live tree. These tests model the
// two processes with two independent Daemon guard states - never two goroutines
// on one Daemon - sharing only the scope directory on disk, and assert the
// invariant: while any daemon in the scope is using an env root or a persistent
// store, no other daemon may reclaim or mutate it.

// newScopeGuardDaemon builds a Daemon with its own guard state, pointed at one
// scope lock directory and one workspaces root. Two of these are two processes:
// nothing is shared but the files on disk.
func newScopeGuardDaemon(t *testing.T, lockDir, workspacesRoot string) *Daemon {
	t.Helper()
	d := &Daemon{
		cfg: Config{
			WorkspacesRoot:     workspacesRoot,
			GCArtifactPatterns: DefaultGCArtifactPatterns,
		},
		logger:            quietTaskLog(),
		scopeLocks:        execenv.NewScopeLocks(lockDir),
		scopeClaimTimeout: 250 * time.Millisecond,
		activeEnvRoots:    map[string]int{},
		deletingEnvRoots:  map[string]bool{},
		activeStores:      map[string]int{},
		deletingStores:    map[string]bool{},
	}
	d.activeEnvRootsCond = sync.NewCond(&d.activeEnvRootsMu)
	d.activeStoresCond = sync.NewCond(&d.activeStoresMu)
	return d
}

func scopeLockDirForTest(t *testing.T, key string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), workStateRootDirName, key, workStateLockDirName)
}

// envRootTestTask is the (workspaces root, workspace, task) triple the env-root
// tests claim paths for.
func envRootTestTask(t *testing.T) (execenv.RootDirParams, string) {
	t.Helper()
	params := execenv.RootDirParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "11111111-1111-1111-1111-111111111111",
		TaskID:         "22222222-2222-2222-2222-222222222222",
	}
	root, err := execenv.ResolveRootDir(params)
	if err != nil {
		t.Fatalf("resolve env root: %v", err)
	}
	return params, root
}

// startTaskOnEnvRoot mirrors what runTask does, in the order it does it: the
// stable scope-level use claim first, then the env root own .task_lock.
func startTaskOnEnvRoot(t *testing.T, d *Daemon, params execenv.RootDirParams, root string) (*execenv.EnvRootClaim, func()) {
	t.Helper()
	releaseScope, err := d.holdEnvRootForTask(root)
	if err != nil {
		t.Fatalf("scope claim for %s: %v", root, err)
	}
	claim, err := execenv.ClaimEnvRoot(params)
	if err != nil {
		releaseScope()
		t.Fatalf("env root claim for %s: %v", root, err)
	}
	return claim, releaseScope
}

// TestGCScope_EnvRootActiveInAnotherDaemon is case A: a task running in one
// daemon owns its env root through the stable scope claim and its .task_lock, so
// a GC in another daemon of the same scope must skip every mutation of that root
// — including the artifact path, which shares the reservation, and the full
// removal, which runs the production mutation under the claim.
func TestGCScope_EnvRootActiveInAnotherDaemon(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-a")
	params, root := envRootTestTask(t)
	daemonA := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)
	daemonB := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)

	claim, releaseScope := startTaskOnEnvRoot(t, daemonB, params, root)
	defer releaseScope()
	defer claim.Release()
	artifact := filepath.Join(root, "workdir", "node_modules", "pkg", "index.js")
	mustWriteFile(t, artifact, "module.exports = 1")
	mustWriteFile(t, filepath.Join(root, "workdir", "main.go"), "package main")

	if _, ok := daemonA.reserveEnvRootForGC(root); ok {
		t.Fatal("GC took the env root of a task running in another daemon")
	}
	stats := &gcStats{byPattern: map[string]int{}}
	if cleaned := daemonA.applyGCAction(root, gcActionCleanArtifacts, stats); cleaned != 0 {
		t.Fatalf("artifact cleanup mutated a live env root (%d dirs)", cleaned)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact cleanup removed content from a live env root: %v", err)
	}
	if cleaned := daemonA.applyGCAction(root, gcActionClean, stats); cleaned != 0 {
		t.Fatalf("full cleanup removed a live env root (%d dirs)", cleaned)
	}
	if _, err := os.Stat(filepath.Join(root, "workdir", "main.go")); err != nil {
		t.Fatalf("a live env root was removed: %v", err)
	}

	// B finishes its task; now A may do the cleanup it was asked for, and the
	// production path does it under the claim it takes internally.
	claim.Release()
	releaseScope()
	if cleaned := daemonA.applyGCAction(root, gcActionClean, stats); cleaned != 1 {
		t.Fatalf("eligible cleanup did not remove the released env root (%d dirs)", cleaned)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("env root survived an eligible cleanup (err = %v)", err)
	}
}

// TestGCScope_DeletionBlocksTaskStart is case B: once a GC owns the deletion for
// an env-root path, no task may begin using or recreating it until that deletion
// finishes.
func TestGCScope_DeletionBlocksTaskStart(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-b2")
	params, root := envRootTestTask(t)
	daemonA := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)
	daemonB := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)
	mustWriteFile(t, filepath.Join(root, "workdir", "main.go"), "package main")

	commitA, ok := daemonA.reserveEnvRootForGC(root)
	if !ok {
		t.Fatal("daemon A could not take the deletion claim for an unowned env root")
	}
	if _, err := daemonB.holdEnvRootForTask(root); err == nil {
		t.Fatal("a task started on an env root another daemon is deleting")
	}
	if _, err := os.Stat(filepath.Join(root, "workdir", "main.go")); err != nil {
		t.Fatalf("the pending deletion already mutated the root: %v", err)
	}

	commitA()
	releaseScope, err := daemonB.holdEnvRootForTask(root)
	if err != nil {
		t.Fatalf("a task was still refused after the deletion finished: %v", err)
	}
	releaseScope()
}

// TestGCScope_EnvRootRecreationDoesNotBypassExclusion is case C, the inode race
// this revision exists to close. A deletion claim is held while the env root (and
// with it .task_lock) is removed; another daemon then recreates the directory and
// a brand-new .task_lock inode. Because the authoritative claim is keyed on the
// PATH and lives outside the tree, the new inode buys the second daemon nothing.
func TestGCScope_EnvRootRecreationDoesNotBypassExclusion(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-c2")
	params, root := envRootTestTask(t)
	daemonA := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)
	daemonB := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)

	mustWriteFile(t, filepath.Join(root, "workdir", "main.go"), "package main")
	commitA, ok := daemonA.reserveEnvRootForGC(root)
	if !ok {
		t.Fatal("daemon A could not take the deletion claim")
	}
	// A removal in flight: the tree, and the old lock inode with it, are gone.
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("simulate removal: %v", err)
	}
	// B recreates the root and a fresh .task_lock at the same path.
	mustWriteFile(t, filepath.Join(root, ".task_lock"), "")

	if _, err := daemonB.holdEnvRootForTask(root); err == nil {
		t.Fatal("recreating the env root and its lock file bypassed the scope-level deletion claim")
	}

	// The in-tree lock alone is not what protected the deletion: on the recreated
	// inode it is taken freely, which is exactly why the scope claim is the
	// authority.
	legacyClaim, err := execenv.ClaimEnvRoot(params)
	if err != nil {
		t.Fatalf("recreated .task_lock should be claimable: %v", err)
	}
	legacyClaim.Release()

	commitA()
	releaseScope, err := daemonB.holdEnvRootForTask(root)
	if err != nil {
		t.Fatalf("a task was still refused after the deletion finished: %v", err)
	}
	releaseScope()
}

// TestGCScope_LegacyTaskWithoutScopeClaimIsProtected is case D: a task started by
// an older daemon holds only .task_lock. The new GC must still refuse to delete
// that env root, or a rolling upgrade would delete a running task directory.
func TestGCScope_LegacyTaskWithoutScopeClaimIsProtected(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-d2")
	params, root := envRootTestTask(t)
	daemonA := newScopeGuardDaemon(t, lockDir, params.WorkspacesRoot)

	legacyClaim, err := execenv.ClaimEnvRoot(params)
	if err != nil {
		t.Fatalf("legacy env root claim: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "workdir", "main.go"), "package main")
	if _, ok := daemonA.reserveEnvRootForGC(root); ok {
		t.Fatal("GC took an env root an old daemon task still holds through .task_lock")
	}
	stats := &gcStats{byPattern: map[string]int{}}
	if cleaned := daemonA.applyGCAction(root, gcActionClean, stats); cleaned != 0 {
		t.Fatalf("cleanup removed an env root held by an old daemon task (%d dirs)", cleaned)
	}

	legacyClaim.Release()
	commit, ok := daemonA.reserveEnvRootForGC(root)
	if !ok {
		t.Fatal("GC still refused an env root no execution holds")
	}
	commit()
}

// TestGCScope_CodexStoreActiveInAnotherDaemon is case B.
func TestGCScope_CodexStoreActiveInAnotherDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	lockDir := scopeLockDirForTest(t, "key-b")
	daemonA := newScopeGuardDaemon(t, lockDir, "")
	daemonB := newScopeGuardDaemon(t, lockDir, "")

	const namespace = "w_scopetest"
	store := execenv.CodexSessionStorePath(namespace, execenv.TaskContextForEnv{AgentID: "agent-1", IssueID: "issue-1"})
	rollout := filepath.Join(store, "sessions", "rollout.jsonl")
	mustWriteFile(t, rollout, "{}")
	ageWorkStateTree(t, store, time.Now().Add(-30*24*time.Hour))

	holder, err := daemonB.holdStoreForTask(store)
	if err != nil {
		t.Fatalf("task store claim in daemon B: %v", err)
	}
	if removed, _ := execenv.PruneCodexSessionStores(namespace, 14*24*time.Hour, time.Now(), daemonA.reserveStoreForDeletion, quietTaskLog()); removed != 0 {
		t.Fatalf("GC reclaimed a store a task is using in another daemon (removed=%d)", removed)
	}
	if _, err := os.Stat(rollout); err != nil {
		t.Fatalf("store was mutated while another daemon held it: %v", err)
	}

	holder()
	if removed, _ := execenv.PruneCodexSessionStores(namespace, 14*24*time.Hour, time.Now(), daemonA.reserveStoreForDeletion, quietTaskLog()); removed != 1 {
		t.Fatalf("GC refused the released store (removed=%d)", removed)
	}
}

// TestGCScope_HermesMemoryStoreKeepsConcurrentUsers is case C, and the one that
// must not regress into serialisation: two tasks in two daemons share one
// agent memory store by design (last-writer-wins), while the GC still cannot
// reclaim it until both are done.
func TestGCScope_HermesMemoryStoreKeepsConcurrentUsers(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-c")
	daemonA := newScopeGuardDaemon(t, lockDir, "")
	daemonB := newScopeGuardDaemon(t, lockDir, "")
	daemonC := newScopeGuardDaemon(t, lockDir, "")

	stateRoot := t.TempDir()
	store := execenv.HermesMemoryStorePath(stateRoot, "agent-1", "")
	mustWriteFile(t, filepath.Join(store, "MEMORY.md"), "remembered")
	ageWorkStateTree(t, store, time.Now().Add(-100*24*time.Hour))

	releaseB, err := daemonB.holdStoreForTask(store)
	if err != nil {
		t.Fatalf("first task could not use the memory store: %v", err)
	}
	releaseC, err := daemonC.holdStoreForTask(store)
	if err != nil {
		t.Fatalf("a second legitimate task was serialised out of the memory store: %v", err)
	}

	if removed, _ := execenv.PruneHermesMemoryStores(stateRoot, 90*24*time.Hour, time.Now(), daemonA.reserveStoreForDeletion, quietTaskLog()); removed != 0 {
		t.Fatalf("GC reclaimed a memory store with live tasks (removed=%d)", removed)
	}

	releaseB()
	releaseC()
	if removed, _ := execenv.PruneHermesMemoryStores(stateRoot, 90*24*time.Hour, time.Now(), daemonA.reserveStoreForDeletion, quietTaskLog()); removed != 1 {
		t.Fatalf("GC refused the released memory store (removed=%d)", removed)
	}
}

// TestGCScope_HermesSessionStoreActiveInAnotherDaemon is case D.
func TestGCScope_HermesSessionStoreActiveInAnotherDaemon(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-d")
	daemonA := newScopeGuardDaemon(t, lockDir, "")
	daemonB := newScopeGuardDaemon(t, lockDir, "")

	stateRoot := t.TempDir()
	task := execenv.TaskContextForEnv{AgentID: "agent-1", IssueID: "issue-1"}
	store := execenv.HermesSessionStorePath(stateRoot, "agent-1", "", task)
	mustWriteFile(t, filepath.Join(store, "state.db"), "transcript")
	ageWorkStateTree(t, store, time.Now().Add(-30*24*time.Hour))

	holder, err := daemonB.holdStoreForTask(store)
	if err != nil {
		t.Fatalf("session store claim in daemon B: %v", err)
	}
	if removed, _ := execenv.PruneHermesSessionStores(stateRoot, 14*24*time.Hour, time.Now(), daemonA.reserveStoreForDeletion, quietTaskLog()); removed != 0 {
		t.Fatalf("GC reclaimed a session store a conversation is using elsewhere (removed=%d)", removed)
	}

	holder()
	if removed, _ := execenv.PruneHermesSessionStores(stateRoot, 14*24*time.Hour, time.Now(), daemonA.reserveStoreForDeletion, quietTaskLog()); removed != 1 {
		t.Fatalf("GC refused the released session store (removed=%d)", removed)
	}
}

// TestGCScope_DeletionStartRace is case E, both orderings. The point is that a
// task never mounts a store another daemon has begun deleting, and a GC never
// claims a store another daemon is already using.
func TestGCScope_DeletionStartRace(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "key-e")
	daemonA := newScopeGuardDaemon(t, lockDir, "")
	daemonB := newScopeGuardDaemon(t, lockDir, "")

	store := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}

	// A owns the deletion first: B must fail safe rather than mount halfway.
	commitA, ok := daemonA.reserveStoreForDeletion(store)
	if !ok {
		t.Fatal("daemon A could not claim an unowned store")
	}
	if _, err := daemonB.holdStoreForTask(store); err == nil {
		t.Fatal("a task mounted a store another daemon is deleting")
	}
	commitA()
	released, err := daemonB.holdStoreForTask(store)
	if err != nil {
		t.Fatalf("a task was still refused after the deletion finished: %v", err)
	}
	released()

	// B owns the store first: A must skip.
	holder, err := daemonB.holdStoreForTask(store)
	if err != nil {
		t.Fatalf("task claim: %v", err)
	}
	if _, ok := daemonA.reserveStoreForDeletion(store); ok {
		t.Fatal("GC claimed a store a task is using in another daemon")
	}
	holder()
	commitA, ok = daemonA.reserveStoreForDeletion(store)
	if !ok {
		t.Fatal("GC refused a store no task holds any more")
	}
	commitA()
}

// TestScopeLifecycle_ResumeAcrossDaemonsWithConcurrentGC is the end-to-end
// regression for the issue lifecycle: profile A creates a task and leaves a
// workdir plus a provider session behind, profile B starts independently, finds
// them through the persisted mapping, and a GC running in the same scope while B
// works cannot reclaim what B is using.
func TestScopeLifecycle_ResumeAcrossDaemonsWithConcurrentGC(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, testDesktop, testBackendA, "")

	// Profile A (default) is the first daemon here: it establishes the mapping and
	// creates a task, exactly as it would have before B ever existed.
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))
	profileA := resolveScope(t, testBackendA, "", "")
	task := execenv.TaskContextForEnv{AgentID: testAgentID, IssueID: testIssueID}
	workdir := filepath.Join(profileA.WorkspacesRoot, "0f0f0f0f-1111-2222-3333-444444444444", "0c0c0c0c", "workdir")
	mustWriteFile(t, filepath.Join(workdir, "checkout.txt"), "turn 1")
	storeA := execenv.CodexSessionStorePath(profileA.CodexNamespace, task)
	rollout := filepath.Join(storeA, "sessions", "rollout-2026-09-18T00-00-00-session.jsonl")
	mustWriteFile(t, rollout, "{}")

	// Profile B starts independently and resolves the persisted mapping.
	profileB := resolveScope(t, testBackendA, testDesktop, "")
	if profileB.CodexNamespace != profileA.CodexNamespace || profileB.WorkspacesRoot != profileA.WorkspacesRoot {
		t.Fatalf("profile B resolved %+v, want profile A scope %+v", profileB, profileA)
	}
	if _, err := os.Stat(rollout); err != nil {
		t.Fatalf("profile B cannot reach the provider session profile A created: %v", err)
	}
	rel, err := filepath.Rel(profileB.WorkspacesRoot, workdir)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("profile A workdir %q is outside profile B root %q", workdir, profileB.WorkspacesRoot)
	}

	// A GC from another daemon in the same scope runs while B is using the store.
	lockDir := filepath.Join(profileB.ScopeDir, workStateLockDirName)
	daemonB := newScopeGuardDaemon(t, lockDir, profileB.WorkspacesRoot)
	daemonGC := newScopeGuardDaemon(t, lockDir, profileB.WorkspacesRoot)
	ageWorkStateTree(t, storeA, time.Now().Add(-30*24*time.Hour))
	session, err := daemonB.holdStoreForTask(storeA)
	if err != nil {
		t.Fatalf("profile B could not take the store: %v", err)
	}
	if removed, _ := execenv.PruneCodexSessionStores(profileB.CodexNamespace, 14*24*time.Hour, time.Now(), daemonGC.reserveStoreForDeletion, quietTaskLog()); removed != 0 {
		t.Fatalf("GC reclaimed the state profile B is resuming (removed=%d)", removed)
	}

	// B finishes; the state is idle now, so a later GC reclaims it normally.
	session()
	if removed, _ := execenv.PruneCodexSessionStores(profileB.CodexNamespace, 14*24*time.Hour, time.Now(), daemonGC.reserveStoreForDeletion, quietTaskLog()); removed != 1 {
		t.Fatalf("GC refused the idle store after the resume finished (removed=%d)", removed)
	}
}

// TestHoldTaskRepoActivityClaimsEveryTaskRepo covers the task side of the repo
// boundary: a run claims the scope-level activity for each bare repo it may check
// out, so heavy maintenance and eviction in any daemon of this work state take the
// exclusive side and skip.
func TestHoldTaskRepoActivityClaimsEveryTaskRepo(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "repo-activity")
	root := filepath.Join(t.TempDir(), ".repos")
	locks := execenv.NewScopeLocks(lockDir)
	d := newScopeGuardDaemon(t, lockDir, t.TempDir())
	d.repoCache = repocache.NewScoped(root, quietTaskLog(), locks)

	const repoURL = "https://example.com/org/repo.git"
	task := Task{
		WorkspaceID: "11111111-1111-1111-1111-111111111111",
		Repos:       []RepoData{{URL: repoURL}, {URL: repoURL}, {URL: "   "}},
	}
	bare := d.repoCache.BarePath(task.WorkspaceID, repoURL)
	if bare == "" {
		t.Fatal("BarePath returned an empty path")
	}

	release, err := d.holdTaskRepoActivity(task)
	if err != nil {
		t.Fatalf("hold task repo activity: %v", err)
	}
	claim, ok, err := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bare))
	if err != nil {
		t.Fatalf("probe activity claim: %v", err)
	}
	if ok {
		claim.Release()
		t.Fatal("the task holds no activity claim for the repo it may check out")
	}

	release()
	claim, ok, err = locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bare))
	if err != nil || !ok {
		t.Fatalf("activity claim still held after release: ok=%v err=%v", ok, err)
	}
	claim.Release()
}

// TestHoldTaskRepoActivityFailsClosedWhileMaintenanceOwnsTheRepo is the
// reverse-order case the previous best-effort helper could not survive: heavy
// maintenance in another daemon already owns the repository exclusively, so this
// task must not start at all - a warning would have let the agent and a
// concurrent prune share the object store.
func TestHoldTaskRepoActivityFailsClosedWhileMaintenanceOwnsTheRepo(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "repo-activity-order")
	root := filepath.Join(t.TempDir(), ".repos")
	locks := execenv.NewScopeLocks(lockDir)
	// Two daemons of one work state, sharing the cache root and the scope locks.
	maintenanceDaemon := newScopeGuardDaemon(t, lockDir, t.TempDir())
	maintenanceDaemon.repoCache = repocache.NewScoped(root, quietTaskLog(), locks)
	taskDaemon := newScopeGuardDaemon(t, lockDir, t.TempDir())
	taskDaemon.repoCache = repocache.NewScoped(root, quietTaskLog(), locks)
	_ = maintenanceDaemon

	const repoURL = "https://example.com/org/repo.git"
	task := Task{
		WorkspaceID: "11111111-1111-1111-1111-111111111111",
		Repos:       []RepoData{{URL: repoURL}},
	}
	bare := taskDaemon.repoCache.BarePath(task.WorkspaceID, repoURL)
	if bare == "" {
		t.Fatal("BarePath returned an empty path")
	}

	// The sibling daemon is running heavy maintenance: it owns the activity
	// target exclusively.
	maintenance, ok, err := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bare))
	if err != nil {
		t.Fatalf("maintenance activity claim: %v", err)
	}
	if !ok {
		t.Fatal("could not take the exclusive activity claim for the test")
	}

	release, err := taskDaemon.holdTaskRepoActivity(task)
	if err == nil {
		if release != nil {
			release()
		}
		t.Fatal("a task claimed repository activity while another daemon owned it exclusively")
	}
	if release != nil {
		t.Fatal("a failed activity claim still handed back a release")
	}
	if !errors.Is(err, execenv.ErrScopeLockTimeout) {
		t.Fatalf("err = %v, want the scope-lock timeout cause", err)
	}
	if !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("err = %v, want an explicit task-did-not-start failure", err)
	}

	maintenance.Release()
	release, err = taskDaemon.holdTaskRepoActivity(task)
	if err != nil {
		t.Fatalf("claim after maintenance released: %v", err)
	}
	if claim, ok, probeErr := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bare)); probeErr != nil {
		t.Fatalf("probe activity claim: %v", probeErr)
	} else if ok {
		claim.Release()
		t.Fatal("the task holds no shared activity claim after a successful acquire")
	}
	release()
	claim, ok, err := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bare))
	if err != nil || !ok {
		t.Fatalf("exclusive activity ownership after the task finished: ok=%v err=%v", ok, err)
	}
	claim.Release()
}

// TestHoldTaskRepoActivityReleasesPartialClaims covers multi-repo atomicity: a
// task naming several repositories must not start with only some of them
// protected, and the claims it did take must not leak when a later one fails.
func TestHoldTaskRepoActivityReleasesPartialClaims(t *testing.T) {
	lockDir := scopeLockDirForTest(t, "repo-activity-partial")
	root := filepath.Join(t.TempDir(), ".repos")
	locks := execenv.NewScopeLocks(lockDir)
	blocker := newScopeGuardDaemon(t, lockDir, t.TempDir())
	blocker.repoCache = repocache.NewScoped(root, quietTaskLog(), locks)
	taskDaemon := newScopeGuardDaemon(t, lockDir, t.TempDir())
	taskDaemon.repoCache = repocache.NewScoped(root, quietTaskLog(), locks)
	_ = blocker

	const (
		repoA = "https://example.com/org/repo-a.git"
		repoB = "https://example.com/org/repo-b.git"
	)
	task := Task{
		WorkspaceID: "11111111-1111-1111-1111-111111111111",
		// repo-b twice: duplicate bare paths must still mean one claim.
		Repos: []RepoData{{URL: repoA}, {URL: repoB}, {URL: repoB}},
	}
	bareA := taskDaemon.repoCache.BarePath(task.WorkspaceID, repoA)
	bareB := taskDaemon.repoCache.BarePath(task.WorkspaceID, repoB)
	if bareA == "" || bareB == "" {
		t.Fatal("BarePath returned an empty path")
	}

	held, ok, err := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bareB))
	if err != nil || !ok {
		t.Fatalf("block repo B: ok=%v err=%v", ok, err)
	}

	release, err := taskDaemon.holdTaskRepoActivity(task)
	if err == nil {
		if release != nil {
			release()
		}
		t.Fatal("a task started with one of its repositories owned exclusively elsewhere")
	}
	if release != nil {
		t.Fatal("a failed multi-repo claim still handed back a release")
	}
	if !strings.Contains(err.Error(), filepath.Base(bareB)) {
		t.Fatalf("err = %v, want the blocked repository named", err)
	}

	// The claim taken for repo A before B failed must already be released.
	claimA, ok, err := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bareA))
	if err != nil || !ok {
		t.Fatalf("repo A activity claim leaked from the failed attempt: ok=%v err=%v", ok, err)
	}
	claimA.Release()

	// With B free the whole set claims, and releasing it frees both repos.
	held.Release()
	release, err = taskDaemon.holdTaskRepoActivity(task)
	if err != nil {
		t.Fatalf("claim with both repos free: %v", err)
	}
	release()
	for _, bare := range []string{bareA, bareB} {
		claim, ok, err := locks.TryAcquireTargetDelete(execenv.RepoActivityTarget(bare))
		if err != nil || !ok {
			t.Fatalf("activity claim still held for %s after release: ok=%v err=%v", filepath.Base(bare), ok, err)
		}
		claim.Release()
	}
}
