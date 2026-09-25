package execenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// reasonixEnvWith writes a Reasonix user config.toml into a fresh home and
// returns the task env pointing at it, so a test never reads the machine's real
// Reasonix config.
func reasonixEnvWith(t *testing.T, config string) map[string]string {
	t.Helper()
	home := t.TempDir()
	if config != "" {
		if err := os.WriteFile(filepath.Join(home, reasonixUserConfigFile), []byte(config), 0o600); err != nil {
			t.Fatalf("seed reasonix user config: %v", err)
		}
	}
	return map[string]string{"REASONIX_HOME": home}
}

// assertTaskDenyList checks the deny rules in a written per-task reasonix.toml.
func assertTaskDenyList(t *testing.T, path string, want []string) {
	t.Helper()
	if got := taskDenyList(t, path); !slices.Equal(got, want) {
		t.Fatalf("deny = %v, want %v", got, want)
	}
}

// taskDenyList decodes the deny rules from a written per-task reasonix.toml.
func taskDenyList(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", reasonixProjectConfigFile, err)
	}
	var cfg struct {
		Permissions struct {
			Deny  []string `toml:"deny"`
			Allow []string `toml:"allow"`
		} `toml:"permissions"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse written %s: %v\n%s", reasonixProjectConfigFile, err, data)
	}
	return cfg.Permissions.Deny
}

func TestPrepareDeniesReasonixAskTool(t *testing.T) {
	t.Parallel()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-reasonix-001",
		TaskID:         "b1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Reasonix Agent",
		Provider:       "reasonix",
		ReasonixEnv:    reasonixEnvWith(t, ""),
		Task:           TaskContextForEnv{IssueID: "b1b2c3d4-e5f6-7890-abcd-ef1234567890"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	configPath := filepath.Join(env.WorkDir, reasonixProjectConfigFile)
	if got := taskDenyList(t, configPath); !slices.Equal(got, []string{"ask"}) {
		t.Fatalf("deny = %v, want [ask]", got)
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
		ReasonixEnv:    reasonixEnvWith(t, "[permissions]\ndeny = [\"bash\"]\n"),
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
		ReasonixEnv:    params.ReasonixEnv,
		Task:           params.Task,
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	got := taskDenyList(t, filepath.Join(reused.WorkDir, reasonixProjectConfigFile))
	if !slices.Equal(got, []string{"bash", "ask"}) {
		t.Fatalf("deny after reuse = %v, want [bash ask]", got)
	}
}

// TestReasonixProjectConfigMergesOwnerPermissions is the regression test for the
// task config replacing the runtime owner's deny list instead of extending it:
// a global `deny = ["bash"]` must still deny bash inside the task.
func TestReasonixProjectConfigMergesOwnerPermissions(t *testing.T) {
	t.Parallel()
	env := reasonixEnvWith(t, `[permissions]
deny = ["bash", "config_write"]
allow = ["read"]

[model]
default = "some-model"
`)
	workDir := t.TempDir()

	if err := writeReasonixProjectConfig(workDir, env, &sidecarManifest{}, testLogger()); err != nil {
		t.Fatalf("writeReasonixProjectConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, reasonixProjectConfigFile))
	if err != nil {
		t.Fatalf("read %s: %v", reasonixProjectConfigFile, err)
	}
	var cfg struct {
		Permissions struct {
			Deny  []string `toml:"deny"`
			Allow []string `toml:"allow"`
		} `toml:"permissions"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse written %s: %v\n%s", reasonixProjectConfigFile, err, data)
	}
	if !slices.Equal(cfg.Permissions.Deny, []string{"bash", "config_write", "ask"}) {
		t.Fatalf("deny = %v, want the owner's rules plus ask", cfg.Permissions.Deny)
	}
	// The rest of the owner's table is restated too: the project file overrides
	// what it declares, so dropping allow here would widen the task as well.
	if !slices.Equal(cfg.Permissions.Allow, []string{"read"}) {
		t.Fatalf("allow = %v, want the owner's [read]", cfg.Permissions.Allow)
	}
	// Only permissions are restated; unrelated owner settings stay in their own
	// config, where the task still inherits them.
	if strings.Contains(string(data), "some-model") {
		t.Fatalf("project config copied unrelated owner settings:\n%s", data)
	}
}

// TestReasonixProjectConfigKeepsOwnerAskDeny checks the already-denied case: the
// merge must not append a duplicate rule.
func TestReasonixProjectConfigKeepsOwnerAskDeny(t *testing.T) {
	t.Parallel()
	env := reasonixEnvWith(t, "[permissions]\ndeny = [\"ask\", \"bash\"]\n")
	workDir := t.TempDir()

	if err := writeReasonixProjectConfig(workDir, env, &sidecarManifest{}, testLogger()); err != nil {
		t.Fatalf("writeReasonixProjectConfig: %v", err)
	}
	got := taskDenyList(t, filepath.Join(workDir, reasonixProjectConfigFile))
	if !slices.Equal(got, []string{"ask", "bash"}) {
		t.Fatalf("deny = %v, want the owner's list unchanged", got)
	}
}

// TestReasonixProjectConfigSkipsUnreadableOwnerConfig covers the fail-closed
// path: a config the daemon cannot restate must leave the task without a
// project config rather than with one that drops the owner's rules.
func TestReasonixProjectConfigSkipsUnreadableOwnerConfig(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		config string
	}{
		{name: "malformed toml", config: "[permissions\ndeny = "},
		{name: "deny is not an array", config: "[permissions]\ndeny = \"bash\"\n"},
		{name: "deny holds a table", config: "[permissions]\ndeny = [{ tool = \"bash\" }]\n"},
		{name: "permissions is not a table", config: "permissions = \"strict\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			manifest := &sidecarManifest{}
			if err := writeReasonixProjectConfig(workDir, reasonixEnvWith(t, tc.config), manifest, testLogger()); err != nil {
				t.Fatalf("writeReasonixProjectConfig: %v", err)
			}
			if _, err := os.Stat(filepath.Join(workDir, reasonixProjectConfigFile)); !os.IsNotExist(err) {
				t.Fatalf("wrote a project config from an owner config it could not restate (stat err = %v)", err)
			}
			if len(manifest.Files) != 0 {
				t.Fatalf("manifest recorded a file it did not write: %+v", manifest.Files)
			}
		})
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
	if err := writeReasonixProjectConfig(workDir, reasonixEnvWith(t, ""), manifest, testLogger()); err != nil {
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
