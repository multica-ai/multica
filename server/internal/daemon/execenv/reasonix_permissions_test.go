package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareDeniesReasonixAskTool(t *testing.T) {
	t.Parallel()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-reasonix-001",
		TaskID:         "b1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Reasonix Agent",
		Provider:       "reasonix",
		Task:           TaskContextForEnv{IssueID: "b1b2c3d4-e5f6-7890-abcd-ef1234567890"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	configPath := filepath.Join(env.WorkDir, reasonixProjectConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", reasonixProjectConfigFile, err)
	}
	if !strings.Contains(string(data), "[permissions]") || !strings.Contains(string(data), `deny = ["ask"]`) {
		t.Fatalf("project config does not deny ask:\n%s", data)
	}

	// The sidecar is daemon-owned state, so CleanupSidecars must take it back
	// out — a local_directory run has to be byte-exactly reversible.
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("reasonix.toml survived sidecar cleanup (stat err = %v)", err)
	}
}

func TestReuseRewritesReasonixAskDeny(t *testing.T) {
	t.Parallel()
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-reasonix-003",
		TaskID:         "c1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Reasonix Agent",
		Provider:       "reasonix",
		Task:           TaskContextForEnv{IssueID: "c1b2c3d4-e5f6-7890-abcd-ef1234567890"},
	}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	// Reuse rolls back the prior run's sidecars before rebuilding them, so the
	// deny rule has to be re-laid — otherwise the second turn of a task would
	// run with the ask tool available again.
	reused := Reuse(ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot,
		WorkDir:        env.WorkDir,
		Provider:       "reasonix",
		Task:           params.Task,
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	data, err := os.ReadFile(filepath.Join(reused.WorkDir, reasonixProjectConfigFile))
	if err != nil {
		t.Fatalf("read %s after reuse: %v", reasonixProjectConfigFile, err)
	}
	if !strings.Contains(string(data), `deny = ["ask"]`) {
		t.Fatalf("reused task lost the ask deny rule:\n%s", data)
	}
}

func TestReasonixProjectConfigKeepsRepositoryFile(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	repoConfig := "[permissions]\nallow = [\"bash\"]\n"
	configPath := filepath.Join(workDir, reasonixProjectConfigFile)
	if err := os.WriteFile(configPath, []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("seed repository reasonix.toml: %v", err)
	}

	// A repository's own config belongs to the user: the daemon leaves it
	// byte-for-byte alone rather than trading it for the ask deny rule, and
	// reports success so the task still runs (with ask enabled, caught by the
	// backend's fail-closed question handling).
	manifest := &sidecarManifest{}
	if err := writeReasonixProjectConfig(workDir, manifest, testLogger()); err != nil {
		t.Fatalf("writeReasonixProjectConfig: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", reasonixProjectConfigFile, err)
	}
	if string(data) != repoConfig {
		t.Fatalf("repository reasonix.toml was rewritten:\n%s", data)
	}
	// Nothing was created, so cleanup must not claim the user's file.
	if len(manifest.Files) != 0 {
		t.Fatalf("manifest recorded a file it did not write: %+v", manifest.Files)
	}
}

func TestReasonixProjectConfigSkippedForOtherProviders(t *testing.T) {
	t.Parallel()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-hermes-001",
		TaskID:         "d1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Hermes Agent",
		Provider:       "hermes",
		Task:           TaskContextForEnv{IssueID: "d1b2c3d4-e5f6-7890-abcd-ef1234567890"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := os.Stat(filepath.Join(env.WorkDir, reasonixProjectConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("reasonix.toml written for a non-reasonix provider (stat err = %v)", err)
	}
}
