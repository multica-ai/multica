package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Persisted canonical mapping tests (GH #8280 Blocker B).
//
// The scope decision has to be machine-visible: two daemons of one backend may
// run in different processes, with different environments, started by different
// tools. Anything one of them can see and the other cannot - an exported
// MULTICA_WORKSPACES_ROOT, a profile config, the order they happened to start
// in - must not be able to produce two persistent trees for one runtime.

func scopeParams(baseURL, profile, explicitRoot string) WorkStateScopeParams {
	return WorkStateScopeParams{
		ServerBaseURL:          baseURL,
		Profile:                profile,
		ExplicitWorkspacesRoot: explicitRoot,
	}
}

func manifestPath(t *testing.T, home, key string) string {
	t.Helper()
	return filepath.Join(home, ".multica", workStateRootDirName, key, scopeManifestFile)
}

// TestScopeManifest_OverrideInOneProcessReachesTheOthers is the blocker: the CLI
// daemon exports MULTICA_WORKSPACES_ROOT, the Desktop daemon inherits nothing.
// The override establishes the mapping in the first process; the second must
// follow it rather than resolve a root of its own.
func TestScopeManifest_OverrideInOneProcessReachesTheOthers(t *testing.T) {
	stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, testDesktop, testBackendA, "")
	rootA := filepath.Join(t.TempDir(), "root-a")

	established := resolveScope(t, testBackendA, "", rootA)
	if established.WorkspacesRoot != rootA {
		t.Fatalf("workspaces root = %q, want the override %q", established.WorkspacesRoot, rootA)
	}

	// The other profile has no override, a different config and a different
	// environment. It must land on the persisted mapping.
	followed := resolveScope(t, testBackendA, testDesktop, "")
	if followed.WorkspacesRoot != rootA {
		t.Fatalf("profile %q resolved root %q, want the persisted %q", testDesktop, followed.WorkspacesRoot, rootA)
	}
	if followed.StateRoot != established.StateRoot || followed.CodexNamespace != established.CodexNamespace {
		t.Fatalf("provider state diverged: %+v vs %+v", followed, established)
	}
}

// TestScopeManifest_ConflictingOverrideFailsClosed is the other half: once the
// backend is bound, a later process asking for a different root must be refused
// instead of quietly taking effect.
func TestScopeManifest_ConflictingOverrideFailsClosed(t *testing.T) {
	stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	rootA := filepath.Join(t.TempDir(), "root-a")
	rootB := filepath.Join(t.TempDir(), "root-b")
	if _, err := ResolveOrCreateWorkStateScope(scopeParams(testBackendA, "", rootA)); err != nil {
		t.Fatalf("establish mapping: %v", err)
	}

	_, err := ResolveOrCreateWorkStateScope(scopeParams(testBackendA, testDesktop, rootB))
	var conflict *WorkStateConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a binding conflict", err)
	}
	if conflict.Tree != "workspaces root" {
		t.Fatalf("conflict tree = %q, want the workspaces root", conflict.Tree)
	}
	if !strings.Contains(conflict.Error(), "persisted mapping") || !strings.Contains(conflict.Error(), rootA) {
		t.Fatalf("conflict error does not name both sides: %q", conflict.Error())
	}
}

// TestScopeManifest_ConcurrentFirstStartupAgrees is the race the lock exists for:
// several resolvers initialise one backend at the same time. Exactly one mapping
// must survive, every racer must return it, and the file must never be observed
// half-written.
func TestScopeManifest_ConcurrentFirstStartupAgrees(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	// Legacy state gives the racers something real to adopt, so a losing racer
	// that continued with its own locally computed answer would be visible.
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))

	const racers = 4
	scopes := make([]WorkStateScope, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			scopes[i], errs[i] = ResolveOrCreateWorkStateScope(scopeParams(testBackendA, "", ""))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
	}
	for i := 1; i < racers; i++ {
		if scopes[i].WorkspacesRoot != scopes[0].WorkspacesRoot ||
			scopes[i].StateRoot != scopes[0].StateRoot ||
			scopes[i].CodexNamespace != scopes[0].CodexNamespace {
			t.Fatalf("racer %d got %+v, want %+v", i, scopes[i], scopes[0])
		}
	}

	data, err := os.ReadFile(manifestPath(t, home, scopes[0].Key))
	if err != nil {
		t.Fatalf("raced startup produced no manifest: %v", err)
	}
	var m workStateManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest is not intact JSON: %v\n%s", err, data)
	}
	if m.WorkspacesRoot != scopes[0].WorkspacesRoot || m.CodexNamespace != scopes[0].CodexNamespace {
		t.Fatalf("manifest %+v does not match the returned scope %+v", m, scopes[0])
	}

	// And the single mapping is the adopted legacy tree, not a fresh default.
	if scopes[0].WorkspacesRoot != filepath.Join(home, workspacesRootDirName) {
		t.Fatalf("raced startup skipped adoption: %q", scopes[0].WorkspacesRoot)
	}
}

// TestScopeManifest_EnvOnlyBackendIsAdoptedAndFound covers an env-only backend:
// the profile never recorded a server_url, but the running process proves which
// backend it belongs to, so its legacy state is adopted and recorded - and a
// later profile finds that record without reading the first profile config.
func TestScopeManifest_EnvOnlyBackendIsAdoptedAndFound(t *testing.T) {
	home := stageWorkStateHome(t)
	// No config for the default profile at all: its backend exists only in the
	// environment, which is exactly the case that used to orphan legacy state.
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))

	established, err := ResolveOrCreateWorkStateScope(scopeParams(testBackendA, "", ""))
	if err != nil {
		t.Fatalf("establish from env-only backend: %v", err)
	}
	if established.WorkspacesRoot != filepath.Join(home, workspacesRootDirName) {
		t.Fatalf("env-only backend resolved %q, want its own legacy tree", established.WorkspacesRoot)
	}
	cfg, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("load default profile config: %v", err)
	}
	if strings.TrimSpace(cfg.ServerURL) != "" {
		t.Fatalf("test setup recorded a backend (%q); this case is about not having one", cfg.ServerURL)
	}

	writeProfileConfig(t, testDesktop, testBackendA, "")
	later, err := ResolveOrCreateWorkStateScope(scopeParams(testBackendA, testDesktop, ""))
	if err != nil {
		t.Fatalf("later profile: %v", err)
	}
	if later.WorkspacesRoot != established.WorkspacesRoot || later.CodexNamespace != established.CodexNamespace {
		t.Fatalf("later profile resolved %+v, want the persisted %+v", later, established)
	}
}

// TestScopeManifest_AmbiguousLegacyProfileFailsClosed covers the state that
// cannot be attributed: another profile holds non-empty legacy state but never
// recorded which backend it belongs to. Neither adopting it nor ignoring it can
// be proven right, so the decision fails closed and nothing is written.
func TestScopeManifest_AmbiguousLegacyProfileFailsClosed(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	writeProfileConfig(t, "stray", "", "")
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName+"_stray"))

	_, err := ResolveOrCreateWorkStateScope(scopeParams(testBackendA, "", ""))
	var ambiguous *AmbiguousLegacyStateError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want AmbiguousLegacyStateError", err)
	}
	if len(ambiguous.Profiles) != 1 || ambiguous.Profiles[0] != "stray" {
		t.Fatalf("ambiguous profiles = %v, want [stray]", ambiguous.Profiles)
	}
	if !strings.Contains(ambiguous.Error(), "Nothing was moved or deleted") {
		t.Fatalf("error does not promise the trees are untouched: %q", ambiguous.Error())
	}
	if _, err := os.Stat(manifestPath(t, home, WorkStateKey(testBackendA))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused decision still wrote a mapping: %v", err)
	}

	// The diagnostic path makes no decision, so it still answers.
	if _, err := ReadWorkStateScope(scopeParams(testBackendA, "", "")); err != nil {
		t.Fatalf("read-only resolution must not fail closed on ambiguity: %v", err)
	}
}

// TestScopeManifest_RestartPersistenceKeepsEveryPath is acceptance criterion 10:
// every resolved location survives the loss of the evidence that produced it, so
// a later process cannot agree on the workspaces root while choosing a different
// provider or Codex location.
func TestScopeManifest_RestartPersistenceKeepsEveryPath(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))
	legacyProfileDir := profileDirFor(t, "")
	mustWriteFile(t, filepath.Join(execenv.HermesMemoryStorePath(legacyProfileDir, testAgentID, ""), "MEMORY.md"), "remembered")
	mustWriteFile(t, filepath.Join(
		execenv.CodexSessionStorePath(execenv.CodexSessionNamespaceForProfile(""), execenv.TaskContextForEnv{AgentID: testAgentID, IssueID: testIssueID}),
		"sessions", "rollout.jsonl"), "{}")

	first := resolveScope(t, testBackendA, "", "")
	if first.WorkspacesRoot != filepath.Join(home, workspacesRootDirName) || first.StateRoot != legacyProfileDir {
		t.Fatalf("first resolution did not adopt the legacy trees: %+v", first)
	}

	// Forget everything adoption could have leaned on.
	t.Setenv(workspacesRootEnv, "")
	writeProfileConfig(t, "", "", "")
	writeProfileConfig(t, testDesktop, testBackendA, "")

	second, err := ResolveOrCreateWorkStateScope(scopeParams(testBackendA, testDesktop, ""))
	if err != nil {
		t.Fatalf("second resolution: %v", err)
	}
	if second.WorkspacesRoot != first.WorkspacesRoot || second.StateRoot != first.StateRoot || second.CodexNamespace != first.CodexNamespace {
		t.Fatalf("restart changed the mapping: %+v then %+v", first, second)
	}
}

// TestReadWorkStateScope_NeverCreatesScopeState keeps the diagnostics read-only:
// a disk-usage or status call must never be the process that claims a tree and
// freezes a mapping.
func TestReadWorkStateScope_NeverCreatesScopeState(t *testing.T) {
	home := stageWorkStateHome(t)
	writeProfileConfig(t, "", testBackendA, "")
	seedWorkspaceState(t, filepath.Join(home, workspacesRootDirName))

	if _, err := ReadWorkStateScope(scopeParams(testBackendA, "", "")); err != nil {
		t.Fatalf("ReadWorkStateScope: %v", err)
	}
	if _, err := WorkStateScopeForProfile("", ""); err != nil {
		t.Fatalf("WorkStateScopeForProfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".multica", workStateRootDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a read-only caller created scope state: %v", err)
	}
}

// TestLoadConfig_RepeatedResolutionUsesThePersistedRoot pins the binding rule at
// the layer the daemon actually starts through, and documents why the config
// tests share one scratch root per test rather than calling t.TempDir() per
// load: reloading a profile follows the persisted mapping, while a load that asks
// for a different explicit root for the same backend is refused instead of
// quietly moving the machine onto a second tree.
func TestLoadConfig_RepeatedResolutionUsesThePersistedRoot(t *testing.T) {
	stageWorkStateHome(t)

	root := filepath.Join(t.TempDir(), "workspaces")
	base := Overrides{ServerURL: testBackendA, WorkspacesRoot: root, AllowNoAgents: true}

	first, err := LoadConfig(base)
	if err != nil {
		t.Fatalf("first LoadConfig: %v", err)
	}
	if first.WorkspacesRoot != root {
		t.Fatalf("workspaces root = %q, want the explicit %q", first.WorkspacesRoot, root)
	}

	second, err := LoadConfig(base)
	if err != nil {
		t.Fatalf("reloading with the same root: %v", err)
	}
	if second.WorkspacesRoot != first.WorkspacesRoot || second.WorkState.CodexNamespace != first.WorkState.CodexNamespace {
		t.Fatalf("reload changed the mapping: %+v then %+v", first.WorkState, second.WorkState)
	}

	_, err = LoadConfig(Overrides{
		ServerURL:      testBackendA,
		WorkspacesRoot: filepath.Join(t.TempDir(), "somewhere-else"),
		AllowNoAgents:  true,
	})
	var conflict *WorkStateConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a binding conflict for a different explicit root", err)
	}
}
