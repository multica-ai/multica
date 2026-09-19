package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Work-state identity tests (GH #8280).
//
// The invariant under test is the whole fix: same machine + same normalized
// backend means one persistent work tree, whatever Multica profile is running;
// different backends never share one. Every test here stages its own HOME (and
// USERPROFILE, which is what os.UserHomeDir reads on Windows) so nothing reaches
// the real ~/.multica or ~/.codex of whoever runs the suite.

const (
	testBackendA = "https://same.example"
	testBackendB = "https://two.example"

	testAgentID = "11111111-2222-3333-4444-555555555555"
	testIssueID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testDesktop = "desktop-example"
)

// stageWorkStateHome points HOME and USERPROFILE at a fresh directory and clears
// every env knob the resolver reads.
func stageWorkStateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(workspacesRootEnv, "")
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return home
}

// writeProfileConfig records a backend (and optionally an explicit workspaces
// root) for a Multica profile, the same way `multica login` and `config set` do.
func writeProfileConfig(t *testing.T, profile, serverURL, workspacesRoot string) {
	t.Helper()
	cfg := cli.CLIConfig{ServerURL: serverURL}
	if workspacesRoot != "" {
		cfg.WorkspacesRoot = workspacesRoot
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		t.Fatalf("save profile %q config: %v", profile, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func profileDirFor(t *testing.T, profile string) string {
	t.Helper()
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		t.Fatalf("resolve profile dir %q: %v", profile, err)
	}
	return dir
}

// ageWorkStateTree sets the atime/mtime of every entry under root to ts. The GC
// reads a store's newest mtime as its last activity, so leaving one fresh file
// inside keeps the whole store alive.
func ageWorkStateTree(t *testing.T, root string, ts time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, ts, ts)
	})
	if err != nil {
		t.Fatalf("age %s: %v", root, err)
	}
}

// seedWorkspaceState gives a workspaces root the one thing that makes it
// authoritative: a workspace/task directory. The daemon own bookkeeping lives in
// dot directories and deliberately does not count.
func seedWorkspaceState(t *testing.T, root string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(root, "0f0f0f0f-1111-2222-3333-444444444444", "0a0a0a0a", "workdir", "main.go"), "package main")
}

func resolveScope(t *testing.T, baseURL, profile, explicitRoot string) WorkStateScope {
	t.Helper()
	scope, err := ResolveOrCreateWorkStateScope(WorkStateScopeParams{
		ServerBaseURL:          baseURL,
		Profile:                profile,
		ExplicitWorkspacesRoot: explicitRoot,
	})
	if err != nil {
		t.Fatalf("ResolveWorkStateScope(profile=%q): %v", profile, err)
	}
	return scope
}

// TestWorkStateKey_FollowsURLNormalization pins the key itself: fixed length,
// filesystem-safe, stable, and derived from the NORMALIZED base URL rather than
// from a second normalization of its own.
//
// Case folding belongs to NormalizeServerBaseURL, which folds exactly the two
// components that are case-insensitive by contract (scheme and host). The path
// is not one of them: https://host/TenantA and https://host/tenanta are two HTTP
// resources, so they must stay two work states rather than being collapsed by a
// blanket ToLower over the whole URL.
func TestWorkStateKey_FollowsURLNormalization(t *testing.T) {
	t.Parallel()

	key := WorkStateKey(testBackendA)
	if len(key) != 16 {
		t.Fatalf("key = %q (len %d), want a fixed 16-char digest", key, len(key))
	}
	for _, r := range key {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("key %q is not filesystem-safe hex", key)
		}
	}
	if key != WorkStateKey(testBackendA) {
		t.Fatal("key must be stable across calls")
	}
	if other := WorkStateKey(testBackendB); other == key {
		t.Fatalf("two backends share key %q", key)
	}

	// Scheme and host case reach one backend, so they must reach one key.
	upper, err := NormalizeServerBaseURL("HTTPS://Same.Example")
	if err != nil {
		t.Fatalf("normalize upper-case backend: %v", err)
	}
	lower, err := NormalizeServerBaseURL("https://same.example")
	if err != nil {
		t.Fatalf("normalize lower-case backend: %v", err)
	}
	if upper != lower {
		t.Fatalf("normalization disagrees on one backend: %q vs %q", upper, lower)
	}
	if WorkStateKey(upper) != WorkStateKey(lower) {
		t.Fatalf("two spellings of one backend got keys %q and %q", WorkStateKey(upper), WorkStateKey(lower))
	}

	// A path difference is a backend difference, and it must survive both the
	// normalizer and the key.
	tenantA, err := NormalizeServerBaseURL("https://example.com/TenantA")
	if err != nil {
		t.Fatalf("normalize TenantA: %v", err)
	}
	tenantLower, err := NormalizeServerBaseURL("https://example.com/tenanta")
	if err != nil {
		t.Fatalf("normalize tenanta: %v", err)
	}
	if tenantA == tenantLower {
		t.Fatal("normalization folded a case-significant path into one backend")
	}
	if WorkStateKey(tenantA) == WorkStateKey(tenantLower) {
		t.Fatal("two distinct backends share one work-state key")
	}
}

// TestWorkStateScope_SameBackendSharesOneTree is case A: two profiles on one
// machine aimed at one backend must resolve one work tree. That shared tree is
// what the CLI daemon and the Desktop daemon both need, and its absence is what
// lost every pre-existing conversation in #8280.
func TestWorkStateScope_SameBackendSharesOneTree(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, testDesktop, testBackendA, "")

	cliScope := resolveScope(t, testBackendA, "", "")
	desktopScope := resolveScope(t, testBackendA, testDesktop, "")

	if cliScope.Key != desktopScope.Key {
		t.Fatalf("keys differ: %q vs %q", cliScope.Key, desktopScope.Key)
	}
	if cliScope.WorkspacesRoot != desktopScope.WorkspacesRoot {
		t.Fatalf("workspaces roots differ: %q vs %q", cliScope.WorkspacesRoot, desktopScope.WorkspacesRoot)
	}
	if cliScope.StateRoot != desktopScope.StateRoot {
		t.Fatalf("provider state roots differ: %q vs %q", cliScope.StateRoot, desktopScope.StateRoot)
	}
	if cliScope.CodexNamespace != desktopScope.CodexNamespace {
		t.Fatalf("Codex namespaces differ: %q vs %q", cliScope.CodexNamespace, desktopScope.CodexNamespace)
	}

	// With no state anywhere the default layout is backend-scoped, and it does not
	// carry the profile name.
	wantRoot := filepath.Join(home, workspacesRootDirName+"_"+cliScope.Key)
	if cliScope.WorkspacesRoot != wantRoot {
		t.Fatalf("workspaces root = %q, want %q", cliScope.WorkspacesRoot, wantRoot)
	}
	if want := filepath.Join(home, ".multica", workStateRootDirName, cliScope.Key); cliScope.StateRoot != want {
		t.Fatalf("provider state root = %q, want %q", cliScope.StateRoot, want)
	}
	if want := execenv.CodexSessionNamespaceForWorkState(cliScope.Key); cliScope.CodexNamespace != want {
		t.Fatalf("Codex namespace = %q, want %q", cliScope.CodexNamespace, want)
	}
	if strings.Contains(cliScope.WorkspacesRoot, testDesktop) {
		t.Fatalf("resolved root %q still encodes a profile name", cliScope.WorkspacesRoot)
	}
}

// TestWorkStateScope_DifferentBackendsStaySeparate is case B: the converse, and
// the reason the scope is keyed on the backend rather than collapsed into one
// machine-wide root.
func TestWorkStateScope_DifferentBackendsStaySeparate(t *testing.T) {
	stageWorkStateHome(t)
	writeProfileConfig(t, "one", testBackendA, "")
	writeProfileConfig(t, "two", testBackendB, "")

	one := resolveScope(t, testBackendA, "one", "")
	two := resolveScope(t, testBackendB, "two", "")

	if one.Key == two.Key {
		t.Fatal("two backends resolved one key")
	}
	if one.WorkspacesRoot == two.WorkspacesRoot {
		t.Fatalf("two backends share workspaces root %q", one.WorkspacesRoot)
	}
	if one.StateRoot == two.StateRoot {
		t.Fatalf("two backends share provider state root %q", one.StateRoot)
	}
	if one.CodexNamespace == two.CodexNamespace {
		t.Fatalf("two backends share Codex namespace %q", one.CodexNamespace)
	}
}

// TestWorkStateScope_AdoptsExistingLegacyState is case D: a machine that already
// has profile-scoped state keeps serving it. A daemon upgrade that switched to a
// fresh namespace would turn every resumable conversation into a new one, which
// is exactly what this change must not do.
func TestWorkStateScope_AdoptsExistingLegacyState(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, testDesktop, testBackendA, "")

	// What the pre-change CLI daemon left behind for this backend:
	// ~/multica_workspaces, the "default" Codex namespace, and the Hermes stores
	// under the default profile directory.
	legacyRoot := filepath.Join(home, workspacesRootDirName)
	seedWorkspaceState(t, legacyRoot)
	legacyNamespace := execenv.CodexSessionNamespaceForProfile("")
	legacyStore := execenv.CodexSessionStorePath(legacyNamespace, execenv.TaskContextForEnv{AgentID: testAgentID, IssueID: testIssueID})
	mustWriteFile(t, filepath.Join(legacyStore, "sessions", "rollout-2026-09-01T00-00-00-abc.jsonl"), "{}")
	legacyProfileDir := profileDirFor(t, "")
	hermesStore := execenv.HermesMemoryStorePath(legacyProfileDir, testAgentID, "")
	mustWriteFile(t, filepath.Join(hermesStore, "MEMORY.md"), "prefers tabs")

	scope := resolveScope(t, testBackendA, testDesktop, "")

	if scope.WorkspacesRoot != legacyRoot {
		t.Fatalf("workspaces root = %q, want the adopted legacy root %q", scope.WorkspacesRoot, legacyRoot)
	}
	if scope.StateRoot != legacyProfileDir {
		t.Fatalf("provider state root = %q, want the adopted legacy root %q", scope.StateRoot, legacyProfileDir)
	}
	if scope.CodexNamespace != legacyNamespace {
		t.Fatalf("Codex namespace = %q, want the adopted legacy namespace %q", scope.CodexNamespace, legacyNamespace)
	}
}

// TestWorkStateScope_ConflictingLegacyStateFailsClosed is case E: two non-empty
// legacy trees claim one backend, which is the shape #8280 leaves behind on an
// affected machine. Picking one discards work the user can still see, and merging
// them is the "hope" behavior that lost state in the first place, so the resolver
// must refuse and leave both trees alone.
func TestWorkStateScope_ConflictingLegacyStateFailsClosed(t *testing.T) {
	type treeCase struct {
		name string
		seed func(t *testing.T, home string)
	}

	cases := []treeCase{
		{
			name: "workspaces root",
			seed: func(t *testing.T, home string) {
				seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))
				seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName+"_"+testDesktop))
			},
		},
		{
			name: "Codex session namespace",
			seed: func(t *testing.T, home string) {
				task := execenv.TaskContextForEnv{AgentID: testAgentID, IssueID: testIssueID}
				for _, profile := range []string{"", testDesktop} {
					store := execenv.CodexSessionStorePath(execenv.CodexSessionNamespaceForProfile(profile), task)
					mustWriteFile(t, filepath.Join(store, "sessions", "rollout.jsonl"), "{}")
				}
			},
		},
		{
			name: "provider state root",
			seed: func(t *testing.T, home string) {
				for _, profile := range []string{"", testDesktop} {
					store := execenv.HermesMemoryStorePath(profileDirFor(t, profile), testAgentID, "")
					mustWriteFile(t, filepath.Join(store, "MEMORY.md"), "remembered")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := stageWorkStateHome(t)
			writeProfileConfig(t, "", testBackendA, "")
			writeProfileConfig(t, testDesktop, testBackendA, "")
			tc.seed(t, home)

			_, err := ResolveOrCreateWorkStateScope(WorkStateScopeParams{
				ServerBaseURL: testBackendA,
				Profile:       testDesktop,
			})
			var conflict *WorkStateConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("err = %v, want a WorkStateConflictError for %s", err, tc.name)
			}
			if conflict.Tree != tc.name {
				t.Fatalf("conflict tree = %q, want %q", conflict.Tree, tc.name)
			}
			if len(conflict.Candidates) < 2 {
				t.Fatalf("conflict named %d candidates, want both trees: %v", len(conflict.Candidates), conflict.Candidates)
			}
			if msg := conflict.Error(); !strings.Contains(msg, "Nothing was moved or deleted") {
				t.Fatalf("conflict error %q does not promise the trees are preserved", msg)
			}

			// Both trees survive: refusing is the whole point.
			for _, candidate := range conflict.Candidates {
				if !filepath.IsAbs(candidate.path) {
					continue // a Codex candidate is a namespace name, checked below
				}
				if _, statErr := os.Stat(candidate.path); statErr != nil {
					t.Fatalf("conflicting tree %q was removed or moved: %v", candidate.path, statErr)
				}
			}
			if tc.name == "Codex session namespace" {
				for _, candidate := range conflict.Candidates {
					if !execenv.CodexSessionNamespaceHasState(candidate.path) {
						t.Fatalf("conflicting namespace %q lost its stores", candidate.path)
					}
				}
			}
		})
	}
}

// TestWorkStateScope_ExplicitRootIsShared is case F: an operator-selected root
// is the machine-wide canonical mapping for that backend, so it cannot leave two
// profiles serving one runtime from two trees.
func TestWorkStateScope_ExplicitRootIsShared(t *testing.T) {
	stageWorkStateHome(t)
	explicit := filepath.Join(t.TempDir(), "operator-root")
	writeProfileConfig(t, "", testBackendA, explicit)
	writeProfileConfig(t, testDesktop, testBackendA, "")

	cliScope := resolveScope(t, testBackendA, "", "")
	desktopScope := resolveScope(t, testBackendA, testDesktop, "")

	if cliScope.WorkspacesRoot != explicit || desktopScope.WorkspacesRoot != explicit {
		t.Fatalf("explicit root not shared: default=%q desktop=%q want %q",
			cliScope.WorkspacesRoot, desktopScope.WorkspacesRoot, explicit)
	}
}

// TestWorkStateScope_ConflictingExplicitRootsAreRejected is the other half of
// case F: profiles that explicitly select different trees must fail startup
// rather than silently split one runtime.
func TestWorkStateScope_ConflictingExplicitRootsAreRejected(t *testing.T) {
	stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, filepath.Join(t.TempDir(), "root-a"))
	writeProfileConfig(t, testDesktop, testBackendA, filepath.Join(t.TempDir(), "root-b"))

	_, err := ResolveOrCreateWorkStateScope(WorkStateScopeParams{
		ServerBaseURL: testBackendA,
		Profile:       testDesktop,
	})
	var conflict *WorkStateConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a WorkStateConflictError", err)
	}
	if conflict.Tree != "workspaces root" {
		t.Fatalf("conflict tree = %q, want the workspaces root", conflict.Tree)
	}

	// A per-process override is a third selection, and conflicts just the same.
	_, err = ResolveOrCreateWorkStateScope(WorkStateScopeParams{
		ServerBaseURL:          testBackendA,
		Profile:                testDesktop,
		ExplicitWorkspacesRoot: filepath.Join(t.TempDir(), "root-c"),
	})
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a WorkStateConflictError for the flag override", err)
	}
}

// TestWorkStateScope_ResumeContinuityAcrossProfiles is case G at the closest
// level to the real flow: turn 1 runs under the default profile and leaves a
// workdir plus a Codex rollout behind, and turn 2 under the Desktop profile
// reaches that same state through the paths turn 2 resolves. Nothing is copied
// between the turns, so what this asserts is the underlying tree, not a fixture.
func TestWorkStateScope_ResumeContinuityAcrossProfiles(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, testDesktop, testBackendA, "")
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))

	task := execenv.TaskContextForEnv{AgentID: testAgentID, IssueID: testIssueID}
	turnOne := resolveScope(t, testBackendA, "", "")

	// Turn 1 persists: a workdir under the resolved root and the conversation
	// rollout in the resolved store.
	workdir := filepath.Join(turnOne.WorkspacesRoot, "0f0f0f0f-1111-2222-3333-444444444444", "0b0b0b0b", "workdir")
	mustWriteFile(t, filepath.Join(workdir, "checkout.txt"), "turn 1")
	store := execenv.CodexSessionStorePath(turnOne.CodexNamespace, task)
	rollout := filepath.Join(store, "sessions", "rollout-2026-09-18T00-00-00-session.jsonl")
	mustWriteFile(t, rollout, "{}")

	// Turn 2 runs under Desktop, same machine and same backend.
	turnTwo := resolveScope(t, testBackendA, testDesktop, "")

	if storeTwo := execenv.CodexSessionStorePath(turnTwo.CodexNamespace, task); storeTwo != store {
		t.Fatalf("turn 2 Codex store = %q, want the turn 1 store %q", storeTwo, store)
	}
	if _, err := os.Stat(rollout); err != nil {
		t.Fatalf("turn 2 cannot reach the rollout turn 1 wrote: %v", err)
	}
	rel, err := filepath.Rel(turnTwo.WorkspacesRoot, workdir)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("turn 1 workdir %q is outside the turn 2 root %q (rel %q)", workdir, turnTwo.WorkspacesRoot, rel)
	}
	if _, err := os.Stat(filepath.Join(turnTwo.WorkspacesRoot, rel, "checkout.txt")); err != nil {
		t.Fatalf("turn 2 cannot reach the workdir turn 1 wrote: %v", err)
	}
}

// TestWorkStateScope_GcOwnsTheTreeItServes closes the loop on GC ownership: the
// GC runs with the same scope task preparation mounts, so it reclaims exactly
// the store this daemon serves and never another backend state.
func TestWorkStateScope_GcOwnsTheTreeItServes(t *testing.T) {
	stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, "other", testBackendB, "")

	served := resolveScope(t, testBackendA, "", "")
	other := resolveScope(t, testBackendB, "other", "")

	task := execenv.TaskContextForEnv{AgentID: testAgentID, IssueID: testIssueID}
	store := execenv.CodexSessionStorePath(served.CodexNamespace, task)
	mustWriteFile(t, filepath.Join(store, "sessions", "rollout.jsonl"), "{}")
	ageWorkStateTree(t, store, time.Now().Add(-30*24*time.Hour))

	// Another backend GC scans its own namespace, where this store is not.
	if removed, _ := execenv.PruneCodexSessionStores(other.CodexNamespace, 14*24*time.Hour, time.Now(), nil, quietTaskLog()); removed != 0 {
		t.Fatalf("removed = %d, want 0 - another backend must not reclaim this store", removed)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("store was reclaimed by the wrong scope: %v", err)
	}

	// The owning scope reclaims it.
	if removed, _ := execenv.PruneCodexSessionStores(served.CodexNamespace, 14*24*time.Hour, time.Now(), nil, quietTaskLog()); removed != 1 {
		t.Fatalf("removed = %d, want 1 - the owning scope reclaims its idle store", removed)
	}
}
