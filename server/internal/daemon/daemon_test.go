package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
	"github.com/pelletier/go-toml/v2"
)

func createDaemonTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup failed: %s: %v", out, err)
		}
	}
	return dir
}

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestTriggerRestart_BrewLinuxCellarDeleted(t *testing.T) {
	originalIsBrewInstall := isBrewInstall
	originalGetBrewPrefix := getBrewPrefix
	t.Cleanup(func() {
		isBrewInstall = originalIsBrewInstall
		getBrewPrefix = originalGetBrewPrefix
	})

	prefix := filepath.Join(t.TempDir(), "home", "linuxbrew", ".linuxbrew")
	deletedCellarPath := filepath.Join(prefix, "Cellar", "multica", "0.2.9", "bin", "multica")
	isBrewInstall = func() bool { return true }
	getBrewPrefix = func() string { return prefix }

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.triggerRestart()

	want := filepath.Join(prefix, "bin", "multica")
	if got := d.RestartBinary(); got != want {
		t.Fatalf("restart binary = %q, want %q", got, want)
	}
	if got := d.RestartBinary(); got == deletedCellarPath {
		t.Fatalf("restart binary used deleted Cellar path %q", got)
	}
}

func TestTriggerRestart_UsesResolvedFallback(t *testing.T) {
	originalResolveSelfExecutable := resolveSelfExecutable
	originalIsBrewInstall := isBrewInstall
	t.Cleanup(func() {
		resolveSelfExecutable = originalResolveSelfExecutable
		isBrewInstall = originalIsBrewInstall
	})

	want := filepath.Join(t.TempDir(), "multica")
	if err := os.WriteFile(want, []byte("test executable"), 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}
	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve executable fixture: %v", err)
	}
	resolveSelfExecutable = func() (string, error) { return want, nil }
	isBrewInstall = func() bool { return false }

	canceled := false
	d := &Daemon{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancelFunc: func() { canceled = true },
	}
	d.triggerRestart()

	if got := d.RestartBinary(); got != wantResolved {
		t.Fatalf("restart binary = %q, want resolved fallback executable %q", got, wantResolved)
	}
	if !canceled {
		t.Fatal("triggerRestart did not initiate graceful shutdown")
	}
}

func TestTriggerRestart_ResolveFailureLeavesDaemonRunning(t *testing.T) {
	originalResolveSelfExecutable := resolveSelfExecutable
	t.Cleanup(func() { resolveSelfExecutable = originalResolveSelfExecutable })
	resolveSelfExecutable = func() (string, error) {
		return "", errors.New("cannot resolve executable")
	}

	canceled := false
	d := &Daemon{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancelFunc: func() { canceled = true },
	}
	d.triggerRestart()

	if got := d.RestartBinary(); got != "" {
		t.Fatalf("restart binary = %q, want empty", got)
	}
	if canceled {
		t.Fatal("triggerRestart initiated shutdown without a restart binary")
	}
}

func TestIsBlockedEnvKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{key: "MULTICA_TOKEN", want: true},
		{key: "multica_runtime_id", want: true},
		{key: "HOME", want: true},
		{key: "PATH", want: true},
		{key: "TMPDIR", want: true},
		{key: "tmp", want: true},
		{key: "TEMP", want: true},
		{key: "CODEX_HOME", want: true},
		{key: "REASONIX_STATE_HOME", want: true},
		{key: "CURSOR_DATA_DIR", want: true},
		{key: "cursor_data_dir", want: true},
		{key: "CURSOR_MCP_AUTH_SOURCE", want: true},
		{key: "OPENCLAW_CONFIG_PATH", want: true},
		{key: "OPENCLAW_INCLUDE_ROOTS", want: true},
		{key: "ANTHROPIC_API_KEY", want: false},
		{key: "CURSOR_AGENT", want: false},
		// HERMES_HOME is intentionally NOT blocked: a skill-less Hermes task
		// must be able to honor a user-set profile/home, and when skills are
		// bound the per-task overlay overrides it after custom_env is layered.
		{key: "HERMES_HOME", want: false},
		{key: "hermes_home", want: false},
		// Reasonix credentials/config remain tool-owned and may use a custom
		// home; only the daemon-owned state overlay is blocked above.
		{key: "REASONIX_HOME", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			if got := isBlockedEnvKey(tt.key); got != tt.want {
				t.Fatalf("isBlockedEnvKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestPrepareReasonixTaskStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := prepareReasonixTaskStateHome("work", "runtime-1", "agent_2")
	if err != nil {
		t.Fatalf("prepareReasonixTaskStateHome: %v", err)
	}
	want := filepath.Join(home, ".multica", "profiles", "work", "reasonix-state", "runtime-1", "agent_2")
	if got != want {
		t.Fatalf("state home = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat state home: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("state home mode = %o, want 700", info.Mode().Perm())
	}
}

func TestLayerCustomEnvKeepsReasonixCredentialsHomeButBlocksStateHome(t *testing.T) {
	t.Parallel()
	agentEnv := map[string]string{}
	layerCustomEnvAndHermesHome(agentEnv, map[string]string{
		"REASONIX_HOME":       "/operator/reasonix-home",
		"REASONIX_STATE_HOME": "/unsafe/shared-state",
	}, "", nil)
	if got := agentEnv["REASONIX_HOME"]; got != "/operator/reasonix-home" {
		t.Fatalf("REASONIX_HOME = %q, want operator-owned home", got)
	}
	if _, ok := agentEnv["REASONIX_STATE_HOME"]; ok {
		t.Fatal("daemon-owned REASONIX_STATE_HOME accepted a custom override")
	}
}

func TestValidateReasonixStateSegmentRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "../agent", "runtime/agent", "agent id"} {
		if _, err := validateReasonixStateSegment("agent", value); err == nil {
			t.Errorf("validateReasonixStateSegment(%q) succeeded, want error", value)
		}
	}
	if got, err := validateReasonixStateSegment("agent", "agent-1_uuid"); err != nil || got != "agent-1_uuid" {
		t.Fatalf("safe segment = %q, %v", got, err)
	}
}

// TestLayerCustomEnvAndHermesHome exercises the daemon-assembled child env for
// HERMES_HOME: no overlay passes the user's value through, an overlay overrides
// it, and blocklisted keys are still dropped.
func TestLayerCustomEnvAndHermesHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		customEnv   map[string]string
		overlayHome string
		wantHermes  string // "" means the key must be absent
		wantAbsent  []string
	}{
		{
			name:        "no skills: user HERMES_HOME passes through",
			customEnv:   map[string]string{"HERMES_HOME": "/home/u/.hermes-research"},
			overlayHome: "",
			wantHermes:  "/home/u/.hermes-research",
		},
		{
			name:        "skills bound: overlay overrides user HERMES_HOME",
			customEnv:   map[string]string{"HERMES_HOME": "/home/u/.hermes-research"},
			overlayHome: "/tmp/task/hermes-home",
			wantHermes:  "/tmp/task/hermes-home",
		},
		{
			name:        "no custom home, no overlay: key absent",
			customEnv:   map[string]string{"ANTHROPIC_API_KEY": "sk"},
			overlayHome: "",
			wantHermes:  "",
		},
		{
			name:        "blocklisted key dropped, overlay still applied",
			customEnv:   map[string]string{"CODEX_HOME": "/evil", "MULTICA_TOKEN": "x"},
			overlayHome: "/tmp/task/hermes-home",
			wantHermes:  "/tmp/task/hermes-home",
			wantAbsent:  []string{"CODEX_HOME", "MULTICA_TOKEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agentEnv := map[string]string{}
			layerCustomEnvAndHermesHome(agentEnv, tt.customEnv, tt.overlayHome, nil)

			if got, ok := agentEnv["HERMES_HOME"]; tt.wantHermes == "" {
				if ok {
					t.Errorf("HERMES_HOME should be absent, got %q", got)
				}
			} else if got != tt.wantHermes {
				t.Errorf("HERMES_HOME = %q, want %q", got, tt.wantHermes)
			}
			for _, k := range tt.wantAbsent {
				if _, ok := agentEnv[k]; ok {
					t.Errorf("blocklisted key %q should not be applied", k)
				}
			}
		})
	}
}

func TestRepoCheckoutModeFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, provider, goos, want string
	}{
		{name: "Linux Codex isolates Git metadata", provider: "codex", goos: "linux", want: repoCheckoutModeIsolated},
		{name: "Windows Codex isolates Git metadata", provider: "codex", goos: "windows", want: repoCheckoutModeIsolated},
		{name: "macOS Codex keeps worktree", provider: "codex", goos: "darwin"},
		{name: "Linux Claude keeps worktree", provider: "claude", goos: "linux"},
		{name: "Windows Claude keeps worktree", provider: "claude", goos: "windows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := repoCheckoutModeFor(tt.provider, tt.goos); got != tt.want {
				t.Fatalf("repoCheckoutModeFor(%q, %q) = %q, want %q", tt.provider, tt.goos, got, tt.want)
			}
		})
	}
}

func TestConfigureCodexTaskShellEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("non-Codex runtime is unchanged", func(t *testing.T) {
		t.Parallel()
		codexHome := t.TempDir()
		if err := configureCodexTaskShellEnvironment("claude", codexHome, nil, nil, nil, slog.Default()); err != nil {
			t.Fatalf("configureCodexTaskShellEnvironment: %v", err)
		}
		if _, err := os.Stat(filepath.Join(codexHome, "config.toml")); !os.IsNotExist(err) {
			t.Fatalf("non-Codex runtime unexpectedly wrote config.toml: %v", err)
		}
	})

	t.Run("Codex policy uses platform and explicit task environment", func(t *testing.T) {
		t.Parallel()
		codexHome := t.TempDir()
		inherited := []string{
			"SystemRoot=C:\\Windows",
			"USERPROFILE=C:\\Users\\test",
			"OPENAI_API_KEY=host-secret",
			"MULTICA_LLM_API_KEY=daemon-secret",
		}
		agentEnv := map[string]string{
			"CUSTOM_ACCESS_TOKEN":      "agent-secret",
			"CUSTOM_FLAG":              "enabled",
			"UNAUTHORIZED_TOKEN":       "daemon-secret",
			"MULTICA_TASK_CONFIG_ROOT": "/task/multica-config",
			"MULTICA_SERVER_URL":       "https://task.example",
			"MULTICA_TOKEN":            "mat_task",
		}
		agentCustomEnv := map[string]string{
			"CUSTOM_ACCESS_TOKEN": "agent-secret",
			"CUSTOM_FLAG":         "enabled",
		}
		if err := configureCodexTaskShellEnvironment("codex", codexHome, inherited, agentEnv, agentCustomEnv, slog.Default()); err != nil {
			t.Fatalf("configureCodexTaskShellEnvironment: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
		if err != nil {
			t.Fatalf("read config.toml: %v", err)
		}
		config := string(data)
		for _, want := range []string{"SystemRoot", "USERPROFILE", "CUSTOM_ACCESS_TOKEN", "CUSTOM_FLAG", "MULTICA_TASK_CONFIG_ROOT", "MULTICA_SERVER_URL", "MULTICA_TOKEN"} {
			if !strings.Contains(config, want) {
				t.Errorf("config.toml missing %q:\n%s", want, config)
			}
		}
		for _, unwanted := range []string{"OPENAI_API_KEY", "MULTICA_LLM_API_KEY", "UNAUTHORIZED_TOKEN", "MULTICA_*", "agent-secret", "daemon-secret", "mat_task"} {
			if strings.Contains(config, unwanted) {
				t.Errorf("config.toml unexpectedly contains %q:\n%s", unwanted, config)
			}
		}
	})

	t.Run("Codex without task home fails closed", func(t *testing.T) {
		t.Parallel()
		err := configureCodexTaskShellEnvironment("codex", "", nil, map[string]string{"MULTICA_TOKEN": "mat_task"}, nil, slog.Default())
		if err == nil || !strings.Contains(err.Error(), "CODEX_HOME is missing") {
			t.Fatalf("error = %v, want missing CODEX_HOME", err)
		}
	})
}

// TestCodexTaskShellEnvInheritsRealHome pins the MUL-5578 contract at the layer
// where the daemon assembles the environment a Codex task actually launches
// with: HOME and the XDG base dirs reach the task's shell tools from the
// *inherited* daemon process environment, so `gh`, `aws`, `kubectl`, and npm
// resolve the daemon user's real config inside a task.
//
// This guards the pass-through, not runTask's decision not to inject a HOME of
// its own — that decision lives inline in runTask and has no unit seam.
func TestCodexTaskShellEnvInheritsRealHome(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	inherited := []string{
		"HOME=/home/daemon-user",
		"XDG_CONFIG_HOME=/home/daemon-user/.config",
		"XDG_CACHE_HOME=/home/daemon-user/.cache",
		"XDG_DATA_HOME=/home/daemon-user/.local/share",
		"XDG_STATE_HOME=/home/daemon-user/.local/state",
		"PATH=/usr/local/bin:/usr/bin",
	}
	// What runTask layers on top for a Codex task: task identity plus the
	// task-scoped CODEX_HOME, and — since MUL-5578 — no HOME/XDG entry.
	explicit := map[string]string{
		"CODEX_HOME":               codexHome,
		"MULTICA_TASK_CONFIG_ROOT": "/task/multica-config",
		"MULTICA_TOKEN":            "mat_task",
		"MULTICA_SERVER_URL":       "https://task.example",
	}

	if err := configureCodexTaskShellEnvironment("codex", codexHome, inherited, explicit, nil, slog.Default()); err != nil {
		t.Fatalf("configureCodexTaskShellEnvironment: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	var parsed struct {
		ShellEnvironmentPolicy struct {
			IncludeOnly []string `toml:"include_only"`
		} `toml:"shell_environment_policy"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config.toml: %v\n%s", err, data)
	}
	include := parsed.ShellEnvironmentPolicy.IncludeOnly

	// The daemon user's real home must survive into the task's shell tools, or
	// gh / aws / kubectl / npm stop resolving their config there.
	for _, want := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		if !slices.Contains(include, want) {
			t.Errorf("include_only missing %q, got %v", want, include)
		}
	}
	// CODEX_HOME is the one home-shaped variable the daemon does own.
	if !slices.Contains(include, "CODEX_HOME") {
		t.Errorf("include_only missing CODEX_HOME, got %v", include)
	}
	if !slices.Contains(include, "MULTICA_TASK_CONFIG_ROOT") {
		t.Errorf("include_only missing MULTICA_TASK_CONFIG_ROOT, got %v", include)
	}
}

func TestCodexShellAuthorizedCustomEnvNamesUsesDaemonBlocklist(t *testing.T) {
	t.Parallel()

	got := codexShellAuthorizedCustomEnvNames(map[string]string{
		"CUSTOM_ACCESS_TOKEN": "agent-secret",
		"custom_secret":       "agent-secret",
		"MULTICA_TOKEN":       "must-not-authorize",
		"PATH":                "/must/not/override",
		"HOME":                "/must/not/override",
		"CODEX_HOME":          "/must/not/override",
		"":                    "must-not-authorize",
	})
	slices.Sort(got)
	want := []string{"CUSTOM_ACCESS_TOKEN", "custom_secret"}
	if !slices.Equal(got, want) {
		t.Fatalf("codexShellAuthorizedCustomEnvNames() = %#v, want %#v", got, want)
	}
}

func TestTaskScopedAuthToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		token   string
		want    string
		wantErr string
	}{
		{
			name:    "missing token fails closed",
			wantErr: "server did not provide task-scoped auth token",
		},
		{
			name:    "member token fails closed",
			token:   "mul_member_token",
			wantErr: "server provided non-task-scoped auth token",
		},
		{
			name:  "task token accepted",
			token: " mat_task_token ",
			want:  "mat_task_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := taskScopedAuthToken(Task{AuthToken: tt.token})
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("taskScopedAuthToken() error = nil, want %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("taskScopedAuthToken() error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("taskScopedAuthToken(): %v", err)
			}
			if got != tt.want {
				t.Fatalf("taskScopedAuthToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskMulticaEnvironmentIncludesPrivateConfigRoot(t *testing.T) {
	t.Parallel()

	const (
		fakeToken      = "mat_task_environment_sentinel"
		taskRoot       = "/task/private-multica-config"
		workspacesRoot = "/daemon/multica_workspaces_staging"
	)
	task := Task{
		ID:          "task-test",
		AgentID:     "agent-test",
		WorkspaceID: "workspace-test",
	}
	env := taskMulticaEnvironment(task, "agent-name", fakeToken, taskRoot, workspacesRoot, "https://task.example", 19514, 3, "/task/tmp")

	want := map[string]string{
		"MULTICA_TOKEN":                fakeToken,
		"MULTICA_TASK_CONFIG_ROOT":     taskRoot,
		"MULTICA_TASK_WORKSPACES_ROOT": workspacesRoot,
		"MULTICA_SERVER_URL":           "https://task.example",
		"MULTICA_DAEMON_PORT":          "19514",
		"MULTICA_WORKSPACE_ID":         "workspace-test",
		"MULTICA_AGENT_NAME":           "agent-name",
		"MULTICA_AGENT_ID":             "agent-test",
		"MULTICA_TASK_ID":              "task-test",
		"MULTICA_TASK_SLOT":            "3",
		"TMPDIR":                       "/task/tmp",
		"TMP":                          "/task/tmp",
		"TEMP":                         "/task/tmp",
	}
	if !maps.Equal(env, want) {
		t.Fatalf("taskMulticaEnvironment() = %#v, want %#v", env, want)
	}

	layerCustomEnvAndHermesHome(env, map[string]string{
		"MULTICA_TASK_CONFIG_ROOT":     "/owner/config",
		"MULTICA_TASK_WORKSPACES_ROOT": "/owner/multica_workspaces",
		"MULTICA_TOKEN":                "mul_owner_sentinel",
	}, "", nil)
	if env["MULTICA_TASK_CONFIG_ROOT"] != taskRoot {
		t.Fatalf("custom env replaced task config root: %q", env["MULTICA_TASK_CONFIG_ROOT"])
	}
	if env[TaskWorkspacesRootEnv] != workspacesRoot {
		t.Fatalf("custom env replaced task workspaces root: %q", env[TaskWorkspacesRootEnv])
	}
	if env["MULTICA_TOKEN"] != fakeToken {
		t.Fatal("custom env replaced task-scoped token")
	}
}

// When `brew --prefix` is unavailable but the executable path is under a
// known Cellar root, triggerRestart must recover the prefix from the
// known-prefix list and target <prefix>/bin/multica.
func TestTriggerRestart_BrewPrefixUnavailable_FallsBackToKnownPrefix(t *testing.T) {
	originalIsBrewInstall := isBrewInstall
	originalGetBrewPrefix := getBrewPrefix
	originalMatchKnownBrewPrefix := matchKnownBrewPrefix
	originalResolveSelfExecutable := resolveSelfExecutable
	t.Cleanup(func() {
		isBrewInstall = originalIsBrewInstall
		getBrewPrefix = originalGetBrewPrefix
		matchKnownBrewPrefix = originalMatchKnownBrewPrefix
		resolveSelfExecutable = originalResolveSelfExecutable
	})

	const knownPrefix = "/home/linuxbrew/.linuxbrew"
	cellarPath := filepath.Join(knownPrefix, "Cellar", "multica", "0.2.9", "bin", "multica")
	isBrewInstall = func() bool { return true }
	getBrewPrefix = func() string { return "" }
	resolveSelfExecutable = func() (string, error) { return cellarPath, nil }
	matchKnownBrewPrefix = func(path string) string {
		if path != cellarPath {
			t.Fatalf("MatchKnownBrewPrefix path = %q, want resolver result %q", path, cellarPath)
		}
		return knownPrefix
	}

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.triggerRestart()

	want := filepath.Join(knownPrefix, "bin", "multica")
	if got := d.RestartBinary(); got != want {
		t.Fatalf("restart binary = %q, want %q", got, want)
	}
}

// When `brew --prefix` is unavailable AND the executable is not under any
// known Cellar root, triggerRestart logs a warning and keeps the executable
// path (no fabricated <prefix>/bin/multica path).
func TestTriggerRestart_BrewPrefixUnavailable_NoKnownPrefix_KeepsExecutable(t *testing.T) {
	originalIsBrewInstall := isBrewInstall
	originalGetBrewPrefix := getBrewPrefix
	originalMatchKnownBrewPrefix := matchKnownBrewPrefix
	t.Cleanup(func() {
		isBrewInstall = originalIsBrewInstall
		getBrewPrefix = originalGetBrewPrefix
		matchKnownBrewPrefix = originalMatchKnownBrewPrefix
	})

	isBrewInstall = func() bool { return true }
	getBrewPrefix = func() string { return "" }
	matchKnownBrewPrefix = func(string) string { return "" }

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.triggerRestart()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if got := d.RestartBinary(); got != exe {
		t.Fatalf("restart binary = %q, want unchanged executable %q", got, exe)
	}
}

func TestNewTaskSlotSemaphoreReturnsStableSlotIndexes(t *testing.T) {
	t.Parallel()

	sem := newTaskSlotSemaphore(4)
	seen := make(map[int]bool)
	for i := 0; i < 4; i++ {
		select {
		case slot := <-sem:
			if slot < 0 || slot > 3 {
				t.Fatalf("slot out of range: %d", slot)
			}
			if seen[slot] {
				t.Fatalf("duplicate slot: %d", slot)
			}
			seen[slot] = true
		default:
			t.Fatalf("expected slot %d to be available", i)
		}
	}

	select {
	case slot := <-sem:
		t.Fatalf("expected semaphore to be empty, got slot %d", slot)
	default:
	}

	sem <- 2
	select {
	case slot := <-sem:
		if slot != 2 {
			t.Fatalf("expected released slot 2, got %d", slot)
		}
	default:
		t.Fatal("expected released slot to be available")
	}
}

func TestProviderNeedsInlineSystemPrompt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		want     bool
	}{
		{provider: "openclaw", want: true},
		// Hermes ACP starts in the task cwd and loads AGENTS.md / .agent_context
		// directly. Inlining the full runtime brief duplicates that context and
		// can trip upstream provider safety filters on otherwise harmless tasks.
		{provider: "hermes", want: false},
		// Kiro CLI loads a root AGENTS.md in ACP sessions. Inlining the same
		// runtime brief duplicates it at the start of every user turn.
		{provider: "kiro", want: false},
		{provider: "kimi", want: true},
		// Reasonix loads AGENTS.md from the ACP session cwd.
		{provider: "reasonix", want: false},
		{provider: "traecli", want: true},
		// Qwen Code loads the per-task QWEN.md file natively.
		{provider: "qwen", want: false},
		{provider: "codex", want: false},
		{provider: "claude", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			if got := providerNeedsInlineSystemPrompt(tc.provider); got != tc.want {
				t.Fatalf("providerNeedsInlineSystemPrompt(%q) = %v, want %v", tc.provider, got, tc.want)
			}
		})
	}
}

// TestComposeOpenclawIncludeRoots — the Elon must-fix regression: the
// daemon must grant OpenClaw permission to follow the wrapper's $include
// link from envRoot into the user's active config dir, while preserving
// any roots the user already configured in their shell env so their own
// cross-directory layouts keep working.
func TestComposeOpenclawIncludeRoots(t *testing.T) {
	t.Parallel()

	sep := string(os.PathListSeparator)
	cases := []struct {
		name    string
		add     string
		user    string
		want    string
		wantSet bool
	}{
		{
			// Fresh install — preparer emits no $include, so daemon
			// shouldn't touch OPENCLAW_INCLUDE_ROOTS at all.
			name:    "fresh_install_no_root_to_grant",
			add:     "",
			user:    "/some/user/dir",
			wantSet: false,
		},
		{
			// User has no existing value — output is just the granted dir.
			name:    "no_user_value",
			add:     "/home/alice/.openclaw",
			user:    "",
			want:    "/home/alice/.openclaw",
			wantSet: true,
		},
		{
			// User has their own include roots — daemon must prepend
			// granted dir AND preserve user's entries verbatim.
			name:    "preserves_user_value",
			add:     "/home/alice/.openclaw",
			user:    "/etc/openclaw" + sep + "/opt/openclaw/shared",
			want:    "/home/alice/.openclaw" + sep + "/etc/openclaw" + sep + "/opt/openclaw/shared",
			wantSet: true,
		},
		{
			// User's value already contains the granted dir — daemon
			// must dedupe rather than emit a redundant entry that would
			// trip OpenClaw confused-deputy heuristics.
			name:    "dedupes_when_user_already_grants_same_dir",
			add:     "/home/alice/.openclaw",
			user:    "/home/alice/.openclaw" + sep + "/etc/openclaw",
			want:    "/home/alice/.openclaw" + sep + "/etc/openclaw",
			wantSet: true,
		},
		{
			// Stray empty segments from a malformed user env are skipped.
			name:    "skips_empty_segments_in_user_value",
			add:     "/home/alice/.openclaw",
			user:    "" + sep + "/etc/openclaw" + sep + "",
			want:    "/home/alice/.openclaw" + sep + "/etc/openclaw",
			wantSet: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := composeOpenclawIncludeRoots(tc.add, tc.user)
			if ok != tc.wantSet {
				t.Fatalf("ok = %v, want %v (got = %q)", ok, tc.wantSet, got)
			}
			if got != tc.want {
				t.Errorf("got = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildPromptContainsIssueID(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	}, "claude")

	// Prompt should contain the issue ID and CLI hint.
	for _, want := range []string{
		issueID,
		"multica issue get",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

// TestSessionContinuityNoticeMatchesSurface locks the MUL-5722 split. The same
// event costs each surface something different, so it cannot be reported with
// one sentence. The dividing question is whether the conversation can still be
// READ, not whether it is a chat: an issue's comments, a Slack channel's
// history, and a web chat's / Feishu's / WeCom's / DingTalk's stored
// chat_message transcript all can; only a surface Multica stores no transcript
// for cannot. Announcing a loss on the readable ones describes something that
// did not happen — the user hears "the discussion is gone" when every word
// survives.
func TestSessionContinuityNoticeMatchesSurface(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		task         Task
		tellUser     bool
		wantMentions string
	}{
		{
			name:         "issue rebuilds from comments",
			task:         Task{IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
			tellUser:     false,
			wantMentions: "comment history",
		},
		{
			// Slack has a history reader, so the conversation is recoverable —
			// just from the channel rather than from Multica. Telling the user it
			// was lost contradicts the commands the same prompt hands the agent.
			name:         "slack rebuilds from the channel",
			task:         Task{ChatSessionID: "chat-1", ChatChannelType: execenv.ChannelTypeSlack},
			tellUser:     false,
			wantMentions: "multica chat history",
		},
		{
			// Web chat history is persisted in chat_message, which `multica chat
			// history` reads back — recoverable, just from Multica's store.
			name:         "web chat rebuilds from the stored transcript",
			task:         Task{ChatSessionID: "chat-1"},
			tellUser:     false,
			wantMentions: "multica chat history",
		},
		{
			// Feishu's conversation is persisted to chat_message too, and the
			// handler's non-Slack fallback reads it back via `multica chat history`.
			name:         "feishu rebuilds from the stored transcript",
			task:         Task{ChatSessionID: "chat-1", ChatChannelType: execenv.ChannelTypeFeishu},
			tellUser:     false,
			wantMentions: "multica chat history",
		},
		{
			// WeCom is fully wired on main (persists chat_message, stamps
			// ChatChannelType="wecom"), so its transcript is readable too — the
			// handler's non-Slack fallback serves it just like Feishu's.
			name:         "wecom rebuilds from the stored transcript",
			task:         Task{ChatSessionID: "chat-1", ChatChannelType: execenv.ChannelTypeWecom},
			tellUser:     false,
			wantMentions: "multica chat history",
		},
		{
			// DingTalk persists to chat_message through the same AppendUserMessage
			// path as Feishu/WeCom, so its transcript is readable the same way.
			name:         "dingtalk rebuilds from the stored transcript",
			task:         Task{ChatSessionID: "chat-1", ChatChannelType: execenv.ChannelTypeDingtalk},
			tellUser:     false,
			wantMentions: "multica chat history",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			notice := sessionContinuityNoticeFor(tc.task)
			if got := strings.Contains(notice, "tell the user up front"); got != tc.tellUser {
				t.Errorf("tells the user = %v, want %v:\n%s", got, tc.tellUser, notice)
			}
			if !strings.Contains(notice, tc.wantMentions) {
				t.Errorf("notice missing %q, so the agent is not told where to rebuild from:\n%s", tc.wantMentions, notice)
			}
			// Where the conversation survives, the ONLY loss is the agent's
			// unrecorded working memory, and it has to be told so or it assumes
			// it still remembers work it no longer has. Where nothing survives
			// that distinction is meaningless — everything is gone — so the
			// requirement applies to the recoverable surfaces only.
			if !tc.tellUser && !strings.Contains(notice, "your own working memory") {
				t.Errorf("recoverable surface must name the real loss:\n%s", notice)
			}
		})
	}

	// The notice only renders when the resume actually failed.
	if blocks := perTurnContextBlocks(Task{IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"}); strings.Contains(blocks, "Session Continuity Notice") {
		t.Errorf("continuity notice leaked into a run that resumed fine:\n%s", blocks)
	}
	lost := perTurnContextBlocks(Task{
		IssueID:                       "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		PriorSessionResumeUnavailable: true,
	})
	if !strings.Contains(lost, "Session Continuity Notice") {
		t.Errorf("continuity notice missing when the resume was unavailable:\n%s", lost)
	}
}

// TestBackendResumeContinuityNoticeSuppressedWhenPromptAlreadyHasIt is the
// count guard Elon asked for, on the combination that actually occurs: the
// codex overflow fresh retry. The daemon appends the notice to the prompt AND
// hands the backend a notice to prepend, so before MUL-5722 one turn carried
// the same paragraph twice — at full token price, from two hand-written
// strings. Suppression is keyed on the prompt already carrying it, so the two
// injectors cannot both fire.
func TestBackendResumeContinuityNoticeSuppressedWhenPromptAlreadyHasIt(t *testing.T) {
	t.Parallel()

	const heading = "## Session Continuity Notice"

	// Live resume rejection the daemon cannot see pre-launch: the prompt says
	// nothing, so the backend must.
	fresh := Task{IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890", TriggerCommentID: "c0ffee00-0000-0000-0000-000000000000"}
	if got := backendResumeContinuityNotice(fresh); !strings.Contains(got, heading) {
		t.Fatal("backend must disclose when the prompt does not")
	}
	if strings.Contains(BuildPrompt(fresh, "codex"), heading) {
		t.Fatal("prompt should not carry the notice when the resume has not failed yet")
	}

	// The daemon's fresh-session retry sets this, which is what makes
	// BuildPrompt append the notice — so the backend must now stay silent.
	retry := fresh
	retry.PriorSessionResumeUnavailable = true
	prompt := BuildPrompt(retry, "codex")
	if n := strings.Count(prompt, heading); n != 1 {
		t.Fatalf("prompt must carry exactly one notice, got %d", n)
	}
	backendNotice := backendResumeContinuityNotice(retry)
	if backendNotice != "" {
		t.Fatalf("backend must not add a second copy, got:\n%s", backendNotice)
	}
	// The whole turn, as codex would assemble it.
	turn := backendNotice + prompt
	if n := strings.Count(turn, heading); n != 1 {
		t.Fatalf("assembled turn carries %d notices, want exactly 1:\n%s", n, turn)
	}
}

// TestReconcileFreshRetryResult locks the GH #5975 review must-fix: the
// poisoned prior session id (carried only on the first result) must never be
// grafted onto the fresh retry's result, and a fresh retry that does not
// establish a new session must not relabel the poisoned session as resumable.
func TestReconcileFreshRetryResult(t *testing.T) {
	t.Parallel()

	firstUsage := map[string]agent.TokenUsage{"m1": {InputTokens: 5}}
	// The first (poisoned) result keeps its id for classification/auditing;
	// its failure is classified unrecoverable elsewhere.
	first := agent.Result{Status: "failed", Error: "oversized history image", SessionID: "ses_poisoned", Usage: firstUsage}
	const firstTools = int32(0)

	tests := []struct {
		name        string
		retry       agent.Result
		retryTools  int32
		retryErr    error
		wantStatus  string
		wantSession string
		wantTools   int32
	}{
		{
			// Fresh attempt never produced a result — keep the poisoned first
			// result so the bad session stays recorded as unrecoverable.
			name:        "retryErr keeps first poisoned result",
			retry:       agent.Result{},
			retryErr:    context.DeadlineExceeded,
			wantStatus:  "failed",
			wantSession: "ses_poisoned",
			wantTools:   firstTools,
		},
		{
			// Fresh session established — new result wins with its OWN id.
			name:        "new session id wins",
			retry:       agent.Result{Status: "completed", Output: "done", SessionID: "ses_new", Usage: map[string]agent.TokenUsage{"m1": {OutputTokens: 7}}},
			retryTools:  2,
			wantStatus:  "completed",
			wantSession: "ses_new",
			wantTools:   2,
		},
		{
			// Fresh attempt failed/timed out WITHOUT a new session id — the
			// poisoned id must NOT be resurrected; keep the first result.
			name:        "failed retry without new session keeps first",
			retry:       agent.Result{Status: "failed", Error: "connection refused", SessionID: ""},
			retryTools:  0,
			wantStatus:  "failed",
			wantSession: "ses_poisoned",
			wantTools:   firstTools,
		},
		{
			name:        "timeout retry without new session keeps first",
			retry:       agent.Result{Status: "timeout", Error: "timed out", SessionID: ""},
			wantStatus:  "failed",
			wantSession: "ses_poisoned",
			wantTools:   firstTools,
		},
		{
			// Fresh attempt succeeded but produced no resumable session id —
			// take the success but keep the id EMPTY, never the poisoned one.
			name:        "completed retry with empty session id keeps empty id",
			retry:       agent.Result{Status: "completed", Output: "ok", SessionID: "", Usage: map[string]agent.TokenUsage{"m1": {OutputTokens: 3}}},
			retryTools:  1,
			wantStatus:  "completed",
			wantSession: "",
			wantTools:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, gotTools := reconcileFreshRetryResult(first, firstUsage, firstTools, tt.retry, tt.retryTools, tt.retryErr)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.SessionID != tt.wantSession {
				t.Fatalf("session id = %q, want %q (the poisoned id must never leak onto a resumable result)", got.SessionID, tt.wantSession)
			}
			if gotTools != tt.wantTools {
				t.Fatalf("tools = %d, want %d", gotTools, tt.wantTools)
			}
			// Usage is always merged so billing is complete.
			if got.Usage["m1"].InputTokens != 5 {
				t.Fatalf("expected first-attempt input usage to be preserved, got %+v", got.Usage["m1"])
			}
		})
	}
}

func TestBuildPromptNoIssueDetails(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	}, "claude")

	// Prompt should not contain issue title/description (agent fetches via CLI).
	for _, absent := range []string{"**Issue:**", "**Summary:**"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q — agent fetches details via CLI", absent)
		}
	}
}

func TestBuildPromptAutopilotRunOnly(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		AutopilotRunID:       "run-1",
		AutopilotID:          "autopilot-1",
		AutopilotTitle:       "Daily dependency check",
		AutopilotDescription: "Check dependencies and report outdated packages.",
		AutopilotSource:      "manual",
	}, "claude")

	for _, want := range []string{
		"run-only mode",
		"Autopilot run ID: run-1",
		"Daily dependency check",
		"Check dependencies and report outdated packages.",
		"multica autopilot get autopilot-1 --output json",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("autopilot prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// The issue-command boundary is emitted ONCE, by the brief's autopilot
	// workflow section (execenv.AutopilotIssueCommandsGuard). MUL-5696 found
	// that a second hand-maintained per-turn copy drifts, so the per-turn
	// prompt must not restate it in any form.
	if strings.Contains(prompt, "Do not run `multica issue get`") {
		t.Fatalf("autopilot prompt restates the issue-command boundary the brief owns (MUL-5696)\n---\n%s", prompt)
	}
	if strings.Contains(prompt, "Your assigned issue ID is:") {
		t.Fatalf("autopilot prompt should not use issue assignment template\n---\n%s", prompt)
	}
}

func TestBuildPromptCommentTriggered(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	commentID := "c1c2c3c4-d5d6-7890-abcd-ef1234567890"
	commentContent := "请把报告翻译成英文"

	prompt := BuildPrompt(Task{
		IssueID:               issueID,
		TriggerCommentID:      commentID,
		TriggerCommentContent: commentContent,
		Agent:                 &AgentData{Name: "Test"},
	}, "claude")

	// Prompt should contain the comment content, the trigger comment id, and
	// the full reply command with --parent. Re-emitting --parent on every turn
	// is what prevents resumed sessions from reusing the previous turn's
	// --parent UUID.
	for _, want := range []string{
		issueID,
		commentContent,
		"Focus on THIS comment",
		commentID,
		"multica issue comment add " + issueID + " --parent " + commentID,
		"do NOT reuse --parent values from previous turns",
		// MUL-5442 (2026-08-06): with the generic no-reply rule retired,
		// the reply command is framed as a plain imperative again — the
		// squad leader's `no_action` block states its own exception.
		"Post your reply as a comment",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// Should still contain CLI hint for fetching issue context.
	if !strings.Contains(prompt, "multica issue get") {
		t.Fatal("prompt missing CLI hint for issue context")
	}
}

// TestBuildPromptCommentTriggeredByAgent covers the trigger-attribution
// line for agent-authored triggers, and guards the RETIREMENT of the
// generic no-reply warning block that used to follow it (MUL-1323 /
// GH#1576). Retired by MUL-5442 owner decision (2026-08-06): an agent
// comment cannot wake an ordinary agent without an explicit @mention
// (computeCommentAgentTriggers routes agent-authored comments only via
// mentions, plus the squad-leader wake), so loop prevention belongs to the
// mention discipline in the brief's `## Mentions`, and recorded silence
// stays squad-leader-only (`no_action`). Retired pins, now negative
// guards: "do not @mention the other agent as a sign-off", "Silence is
// the preferred way".
func TestBuildPromptCommentTriggeredByAgent(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "thanks, looks good!",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Atlas",
		Agent:                 &AgentData{Name: "Test"},
	}, "claude")

	if !strings.Contains(prompt, "Another agent (Atlas)") {
		t.Fatalf("prompt missing trigger attribution\n---\n%s", prompt)
	}
	for _, banned := range []string{
		"Silence is the preferred way",
		"do not @mention the other agent as a sign-off",
		"reply is warranted",
		"exit with no output",
		// The conditional reply framing was the door the retired rule
		// walked through — guard it shut (MUL-5442 #6493 review).
		"If you decide to reply",
	} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("per-turn prompt still carries retired no-reply warning %q\n---\n%s", banned, prompt)
		}
	}
}

// TestBuildPromptCommentTriggeredByMember guards against the agent-loop warning
// leaking into human-authored triggers — a human asking a question should not
// be pre-discouraged from getting a reply.
func TestBuildPromptCommentTriggeredByMember(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "can you translate this?",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Alice",
		Agent:                 &AgentData{Name: "Test"},
	}, "claude")

	if !strings.Contains(prompt, "A user just left a new comment") {
		t.Fatalf("member-triggered prompt should label the author as a user\n---\n%s", prompt)
	}
	if strings.Contains(prompt, "Another agent") {
		t.Fatalf("member-triggered prompt should not claim the author was another agent")
	}
	// Must NOT use the old "You MUST respond" language: the reply command is
	// shared across turn types, and a squad leader's `no_action` exit — the
	// one silent path left after MUL-5442 retired the generic no-reply rule —
	// must not be shouted over by the per-turn channel. The unconditional
	// one-comment contract for ordinary agents lives in the brief.
	if strings.Contains(prompt, "MUST respond") {
		t.Fatalf("prompt should not contain unconditional \"MUST respond\" language\n---\n%s", prompt)
	}
	if !strings.Contains(prompt, "Post your reply as a comment") {
		t.Fatalf("prompt should carry the imperative reply command\n---\n%s", prompt)
	}
}

func TestBuildPromptCommentTriggeredNoContent(t *testing.T) {
	t.Parallel()

	// When TriggerCommentID is set but content is empty (e.g. fetch failed),
	// it should still use the comment prompt path.
	prompt := BuildPrompt(Task{
		IssueID:          "test-id",
		TriggerCommentID: "comment-id",
		Agent:            &AgentData{Name: "Test"},
	}, "claude")

	if !strings.Contains(prompt, "multica issue get") {
		t.Fatal("prompt missing CLI hint")
	}
}

// TestBuildPromptSquadLeaderNoActionProhibition verifies that when a squad
// leader is triggered by another agent's comment, the per-turn prompt
// explicitly forbids posting a comment whose only purpose is to announce
// no_action or "exiting silently". This is the fix for MUL-2168.
func TestBuildPromptSquadLeaderNoActionProhibition(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "Progress update: tests passing.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Worker",
		IsLeaderTask:          true,
		LeaderRoleResolved:    true,
		Agent: &AgentData{
			Name:         "Leader",
			Instructions: "You lead the team.\n\n## Squad Operating Protocol\n\nYou are the LEADER.",
		},
	}, "claude")

	for _, want := range []string{
		"Squad leader no_action rule",
		"DO NOT post any comment",
		"multica squad activity",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("squad leader prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// Non-squad-leader agent should NOT get the squad leader rule.
	nonLeaderPrompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "Progress update: tests passing.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Worker",
		Agent: &AgentData{
			Name:         "Regular",
			Instructions: "You are a regular agent.",
		},
	}, "claude")

	if strings.Contains(nonLeaderPrompt, "Squad leader no_action rule") {
		t.Fatalf("non-squad-leader prompt should NOT contain squad leader rule\n---\n%s", nonLeaderPrompt)
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}

func TestIsTaskNotFoundError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "404 with task not found body",
			err: &requestError{
				Method:     http.MethodPost,
				Path:       "/api/daemon/tasks/abc/messages",
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"task not found"}`,
			},
			want: true,
		},
		{
			name: "404 with mixed-case body still matches",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"Task Not Found"}`,
			},
			want: true,
		},
		{
			name: "500 with same body is not task-not-found",
			err: &requestError{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"task not found"}`,
			},
			want: false,
		},
		{
			name: "404 with workspace-not-found body is not task-not-found",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"workspace not found"}`,
			},
			want: false,
		},
		{
			name: "non-requestError",
			err:  errors.New("network down"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isTaskNotFoundError(tc.err); got != tc.want {
				t.Fatalf("isTaskNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsRuntimeNotFoundError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "404 with runtime not found body from heartbeat",
			err: &requestError{
				Method:     http.MethodPost,
				Path:       "/api/daemon/heartbeat",
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"runtime not found"}`,
			},
			want: true,
		},
		{
			name: "404 with runtime not found body from claim",
			err: &requestError{
				Method:     http.MethodPost,
				Path:       "/api/daemon/runtimes/abc/tasks/claim",
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"runtime not found"}`,
			},
			want: true,
		},
		{
			name: "mixed-case body still matches",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"Runtime Not Found"}`,
			},
			want: true,
		},
		{
			name: "500 with same body must NOT be treated as runtime-not-found",
			err: &requestError{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"runtime not found"}`,
			},
			want: false,
		},
		{
			name: "404 with task-not-found body is not runtime-not-found",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"task not found"}`,
			},
			want: false,
		},
		{
			name: "404 with workspace-not-found body is not runtime-not-found",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"workspace not found"}`,
			},
			want: false,
		},
		{
			name: "non-requestError",
			err:  errors.New("network down"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isRuntimeNotFoundError(tc.err); got != tc.want {
				t.Fatalf("isRuntimeNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestShouldInterruptAgent(t *testing.T) {
	t.Parallel()

	notFound := &requestError{
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"task not found"}`,
	}
	transient := &requestError{
		StatusCode: http.StatusBadGateway,
		Body:       `<html>...</html>`,
	}

	cases := []struct {
		name   string
		status string
		err    error
		want   bool
	}{
		{name: "status cancelled", status: "cancelled", err: nil, want: true},
		{name: "status failed (offline sweeper)", status: "failed", err: nil, want: true},
		{name: "status completed (finished elsewhere)", status: "completed", err: nil, want: true},
		{name: "task deleted (404)", status: "", err: notFound, want: true},
		{name: "running normally", status: "running", err: nil, want: false},
		{name: "waiting_local_directory keeps running", status: "waiting_local_directory", err: nil, want: false},
		{name: "dispatched keeps running", status: "dispatched", err: nil, want: false},
		{name: "transient 5xx is not a cancel signal", status: "", err: transient, want: false},
		{name: "no information yet", status: "", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldInterruptAgent(tc.status, tc.err); got != tc.want {
				t.Fatalf("shouldInterruptAgent(%q, %v) = %v, want %v", tc.status, tc.err, got, tc.want)
			}
		})
	}
}

// TestWatchTaskCancellation_TaskDeleted reproduces the zombie-task bug:
// when the server deletes a task while it is running (issue removed,
// agent reassigned, etc.), GetTaskStatus starts returning 404. Before the
// fix the daemon kept polling and never interrupted the running agent —
// codex would keep emitting tool calls for minutes against a dead task.
//
// After the fix, watchTaskCancellation must close its channel within a
// few poll intervals so the caller can cancel the agent context.
func TestWatchTaskCancellation_TaskDeleted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"task not found"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-deleted", 10*time.Millisecond, slog.Default())

	select {
	case <-cancelled:
		// Expected: the watcher detected the 404 and signalled cancellation.
	case <-time.After(2 * time.Second):
		t.Fatal("watchTaskCancellation did not signal cancellation when task was deleted (404)")
	}
}

// TestWatchTaskCancellation_StatusCancelled keeps the existing behaviour
// (server transitions task status to "cancelled") working alongside the
// new 404 path.
func TestWatchTaskCancellation_StatusCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-cancelled", 10*time.Millisecond, slog.Default())

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchTaskCancellation did not signal cancellation when status=cancelled")
	}
}

// TestWatchTaskCancellation_RunningTaskNotInterrupted ensures the watcher
// does NOT trigger on transient errors or while the task is still running.
func TestWatchTaskCancellation_RunningTaskNotInterrupted(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-running", 10*time.Millisecond, slog.Default())

	select {
	case <-cancelled:
		t.Fatal("watchTaskCancellation should not signal cancellation while task is running")
	case <-time.After(150 * time.Millisecond):
	}
	if calls.Load() < 5 {
		t.Fatalf("expected the watcher to poll at least 5 times in 150ms, got %d", calls.Load())
	}
}

func TestMergeUsage(t *testing.T) {
	t.Parallel()

	a := map[string]agent.TokenUsage{
		"model-a": {InputTokens: 10, OutputTokens: 5},
	}
	b := map[string]agent.TokenUsage{
		"model-a": {InputTokens: 20, OutputTokens: 10, CacheReadTokens: 3},
		"model-b": {InputTokens: 100},
	}
	merged := mergeUsage(a, b)

	if got := merged["model-a"]; got.InputTokens != 30 || got.OutputTokens != 15 || got.CacheReadTokens != 3 {
		t.Fatalf("model-a: expected {30,15,3,0}, got %+v", got)
	}
	if got := merged["model-b"]; got.InputTokens != 100 {
		t.Fatalf("model-b: expected InputTokens=100, got %+v", got)
	}

	if got := mergeUsage(nil, b); len(got) != 2 {
		t.Fatal("mergeUsage(nil, b) should return b")
	}
	if got := mergeUsage(a, nil); len(got) != 1 {
		t.Fatal("mergeUsage(a, nil) should return a")
	}
}

// fakeBackend is a test double for agent.Backend that returns preconfigured
// results. Each call to Execute pops the next entry from the results slice.
type fakeBackend struct {
	calls   []agent.ExecOptions
	results []agent.Result
	errors  []error
	idx     atomic.Int32
}

func (b *fakeBackend) Execute(_ context.Context, _ string, opts agent.ExecOptions) (*agent.Session, error) {
	i := int(b.idx.Add(1)) - 1
	b.calls = append(b.calls, opts)
	if i < len(b.errors) && b.errors[i] != nil {
		return nil, b.errors[i]
	}
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result, 1)
	close(msgCh)
	resCh <- b.results[i]
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return &Daemon{
		client: NewClient(srv.URL),
		logger: slog.Default(),
	}
}

func newRepoReadyTestDaemon(t *testing.T, handler http.HandlerFunc) *Daemon {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	d := &Daemon{
		client:       NewClient(srv.URL),
		repoCache:    repocache.New(t.TempDir(), slog.Default()),
		logger:       slog.Default(),
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
	}
	// Drain background syncs (started by registerTaskRepos) before the
	// t.TempDir cache root is cleaned up, otherwise an in-flight clone/fetch
	// races against the deletion and the test fails with a misleading
	// "directory not empty" cleanup error.
	t.Cleanup(d.waitBackgroundSyncs)
	return d
}

func TestGateCodexResumeToRolloutPresence(t *testing.T) {
	t.Parallel()

	// Seed a codex-home whose sessions dir holds exactly one rollout.
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	present := filepath.Join(codexHome, "sessions", "2026", "07", "13",
		"rollout-2026-07-13T00-00-00-present-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(present), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.WriteFile(present, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	tests := []struct {
		name        string
		provider    string
		sessionID   string
		codexHome   string
		wantSession string
	}{
		{name: "rollout present keeps resume", provider: "codex", sessionID: "present-session", codexHome: codexHome, wantSession: "present-session"},
		{name: "rollout absent drops resume", provider: "codex", sessionID: "gone-session", codexHome: codexHome, wantSession: ""},
		{name: "non-codex provider is a no-op", provider: "claude", sessionID: "present-session", codexHome: codexHome, wantSession: "present-session"},
		{name: "empty codex home is a no-op", provider: "codex", sessionID: "present-session", codexHome: "", wantSession: "present-session"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{PriorSessionID: tt.sessionID}
			taskCtx := execenv.TaskContextForEnv{PriorSessionResumed: tt.sessionID != ""}

			gateCodexResumeToRolloutPresence(&task, &taskCtx, tt.provider, tt.codexHome, slog.Default())

			if task.PriorSessionID != tt.wantSession {
				t.Fatalf("PriorSessionID = %q, want %q", task.PriorSessionID, tt.wantSession)
			}
			if taskCtx.PriorSessionResumed != (tt.wantSession != "") {
				t.Fatalf("PriorSessionResumed = %v, want %v", taskCtx.PriorSessionResumed, tt.wantSession != "")
			}
			// A dropped resume (had a session, now cleared) must be surfaced to
			// the user via the brief; a kept/no-op resume must not.
			wantUnavailable := tt.sessionID != "" && tt.wantSession == ""
			if taskCtx.PriorSessionResumeUnavailable != wantUnavailable {
				t.Fatalf("PriorSessionResumeUnavailable = %v, want %v", taskCtx.PriorSessionResumeUnavailable, wantUnavailable)
			}
		})
	}
}

func TestCodexSessionResumable(t *testing.T) {
	t.Parallel()

	seedRollout := func(t *testing.T, codexHome, sessionID string) {
		t.Helper()
		p := filepath.Join(codexHome, "sessions", "2026", "07", "27",
			"rollout-2026-07-27T00-00-00-"+sessionID+".jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir sessions: %v", err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write rollout: %v", err)
		}
	}

	t.Run("rollout present is resumable", func(t *testing.T) {
		t.Parallel()
		home := filepath.Join(t.TempDir(), "codex-home")
		seedRollout(t, home, "sess-present")
		if !codexSessionResumable(home, "sess-present", 50*time.Millisecond) {
			t.Fatal("expected present rollout to be resumable")
		}
	})

	t.Run("rollout absent is not resumable", func(t *testing.T) {
		t.Parallel()
		home := filepath.Join(t.TempDir(), "codex-home")
		if codexSessionResumable(home, "sess-gone", 40*time.Millisecond) {
			t.Fatal("expected absent rollout to be unresumable")
		}
	})

	t.Run("rollout appearing within the wait is resumable", func(t *testing.T) {
		t.Parallel()
		home := filepath.Join(t.TempDir(), "codex-home")
		go func() {
			time.Sleep(30 * time.Millisecond)
			seedRollout(t, home, "sess-delayed")
		}()
		if !codexSessionResumable(home, "sess-delayed", 2*time.Second) {
			t.Fatal("expected a rollout flushed within the wait window to be resumable")
		}
	})

	t.Run("non-codex (empty codex home) is always resumable", func(t *testing.T) {
		t.Parallel()
		// Non-Codex providers have no rollout store; the gate must be a no-op so
		// their existing pin/report behavior is unchanged.
		if !codexSessionResumable("", "sess-any", 0) {
			t.Fatal("expected empty codex home to be a no-op (resumable)")
		}
	})

	t.Run("empty session is a no-op", func(t *testing.T) {
		t.Parallel()
		home := filepath.Join(t.TempDir(), "codex-home")
		if !codexSessionResumable(home, "", 0) {
			t.Fatal("expected empty session id to be a no-op (resumable)")
		}
	})
}

// statusOnlyBackend emits a single status message carrying a session id (the
// point at which the daemon pins the resume pointer) and then completes.
type statusOnlyBackend struct {
	sessionID string
	result    agent.Result
}

func (b *statusOnlyBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 1)
	msgCh <- agent.Message{Type: agent.MessageStatus, SessionID: b.sessionID}
	close(msgCh)
	resCh := make(chan agent.Result, 1)
	resCh <- b.result
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// statusThenHoldBackend emits the session-id status, then keeps the run alive
// for `hold` before completing — so a rollout that only appears AFTER the
// status (mid-run) still has a live run for the pin waiter to observe.
type statusThenHoldBackend struct {
	sessionID string
	hold      time.Duration
	result    agent.Result
}

func (b *statusThenHoldBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result, 1)
	go func() {
		msgCh <- agent.Message{Type: agent.MessageStatus, SessionID: b.sessionID}
		time.Sleep(b.hold)
		close(msgCh)
		resCh <- b.result
	}()
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// pinRecorder captures the session ids the daemon pins mid-flight.
type pinRecorder struct {
	mu       sync.Mutex
	sessions []string
}

func (r *pinRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sessions)
}

func newPinRecorder(t *testing.T) (*Daemon, *pinRecorder) {
	t.Helper()
	rec := &pinRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/session") {
			var body struct {
				SessionID string `json:"session_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.SessionID != "" {
				rec.mu.Lock()
				rec.sessions = append(rec.sessions, body.SessionID)
				rec.mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return &Daemon{client: NewClient(srv.URL), logger: slog.Default()}, rec
}

// TestExecuteAndDrain_PinsResumableCodexSession pins the write-time invariant
// (MUL-5305): a Codex session is pinned mid-flight only once its rollout exists
// in the task's CODEX_HOME, so the daemon never persists a resume pointer the
// next follow-up would just discover is unrecoverable and drop.
func TestExecuteAndDrain_PinsResumableCodexSession(t *testing.T) {
	t.Parallel()

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	rollout := filepath.Join(codexHome, "sessions", "2026", "07", "27",
		"rollout-2026-07-27T00-00-00-sess-x.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.WriteFile(rollout, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	d, rec := newPinRecorder(t)
	backend := &statusOnlyBackend{sessionID: "sess-x", result: agent.Result{Status: "completed", Output: "done", SessionID: "sess-x"}}

	if _, _, err := d.executeAndDrain(context.Background(), backend, "p", agent.ExecOptions{}, slog.Default(), "task-pin", codexHome, new(atomic.Int32)); err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}

	// The pin fires in a goroutine; wait briefly for it to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := rec.snapshot(); len(got) == 1 && got[0] == "sess-x" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected the resumable codex session to be pinned, got %+v", rec.snapshot())
}

// TestExecuteAndDrain_SkipsPinWhenRolloutAbsent pins the negative half of the
// write-time invariant (MUL-5305): a Codex session with no rollout in the store
// is never pinned mid-flight, so a pointer the daemon cannot resume is not left
// on the task row for the next follow-up to inherit.
func TestExecuteAndDrain_SkipsPinWhenRolloutAbsent(t *testing.T) {
	t.Parallel()

	codexHome := filepath.Join(t.TempDir(), "codex-home") // sessions dir never created
	d, rec := newPinRecorder(t)
	backend := &statusOnlyBackend{sessionID: "sess-absent", result: agent.Result{Status: "failed", Error: "boom", SessionID: "sess-absent"}}

	if _, _, err := d.executeAndDrain(context.Background(), backend, "p", agent.ExecOptions{}, slog.Default(), "task-nopin", codexHome, new(atomic.Int32)); err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	// Give any (incorrectly-spawned) pin goroutine time to fire before asserting.
	time.Sleep(100 * time.Millisecond)
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("expected no pin when the rollout is absent, got %+v", got)
	}
}

// TestExecuteAndDrain_PinsWhenRolloutAppearsAfterStatus covers the crash-
// recovery half of MUL-5305: Codex reveals the session id on a single
// task_started status, so the pin waiter must keep watching for the life of the
// run and pin as soon as the rollout lands — even when it flushes AFTER that one
// status. A fixed one-shot check at status time would miss it and lose in-flight
// crash recovery.
func TestExecuteAndDrain_PinsWhenRolloutAppearsAfterStatus(t *testing.T) {
	t.Parallel()

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	rolloutDir := filepath.Join(codexHome, "sessions", "2026", "07", "27")
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	rollout := filepath.Join(rolloutDir, "rollout-2026-07-27T00-00-00-sess-late.jsonl")

	d, rec := newPinRecorder(t)
	backend := &statusThenHoldBackend{
		sessionID: "sess-late",
		hold:      400 * time.Millisecond,
		result:    agent.Result{Status: "completed", Output: "done", SessionID: "sess-late"},
	}
	// The rollout appears only AFTER the status message, while the run is alive.
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = os.WriteFile(rollout, []byte("{}"), 0o644)
	}()

	if _, _, err := d.executeAndDrain(context.Background(), backend, "p", agent.ExecOptions{}, slog.Default(), "task-late", codexHome, new(atomic.Int32)); err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}

	// The pin fires from a background waiter; poll briefly for it to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := rec.snapshot(); len(got) == 1 && got[0] == "sess-late" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected the codex session to be pinned once its rollout appeared mid-run, got %+v", rec.snapshot())
}

func TestGateResumeToReusedWorkdir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		priorDir  string
		envDir    string
		// sessionHomeUnreachable models a provider whose session store this run
		// cannot reach even though the workdir matches — the Hermes
		// local_directory case (GH #6806). Zero value keeps the cwd-keyed
		// providers' behaviour.
		sessionHomeUnreachable bool
		wantSession            string
		wantReused             bool
	}{
		{
			name:        "same workdir keeps session",
			sessionID:   "sess-1",
			priorDir:    "/ws/task-a/workdir",
			envDir:      "/ws/task-a/workdir",
			wantSession: "sess-1",
			wantReused:  true,
		},
		{
			name:        "fresh workdir drops session",
			sessionID:   "sess-1",
			priorDir:    "/ws/task-a/workdir",
			envDir:      "/ws/task-b/workdir",
			wantSession: "",
			wantReused:  false,
		},
		{
			name:        "session without recorded workdir drops session",
			sessionID:   "sess-1",
			priorDir:    "",
			envDir:      "/ws/task-b/workdir",
			wantSession: "",
			wantReused:  false,
		},
		{
			name:        "no prior session is a no-op",
			sessionID:   "",
			priorDir:    "/ws/task-a/workdir",
			envDir:      "/ws/task-b/workdir",
			wantSession: "",
			wantReused:  false,
		},
		{
			// The local_directory flow: workdir is the user's own directory
			// and therefore identical across tasks, but reuse is disabled so
			// the session store is a fresh, empty one. Forwarding the id here
			// is what made every turn silently restart the conversation.
			name:                   "matching workdir but unreachable session store drops session",
			sessionID:              "sess-1",
			priorDir:               "/repo",
			envDir:                 "/repo",
			sessionHomeUnreachable: true,
			wantSession:            "",
			wantReused:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{PriorSessionID: tt.sessionID, PriorWorkDir: tt.priorDir}
			taskCtx := execenv.TaskContextForEnv{PriorSessionResumed: tt.sessionID != ""}

			reused := gateResumeToReusedWorkdir(&task, &taskCtx, tt.envDir, !tt.sessionHomeUnreachable, slog.Default())

			if reused != tt.wantReused {
				t.Fatalf("reused = %v, want %v", reused, tt.wantReused)
			}
			if task.PriorSessionID != tt.wantSession {
				t.Fatalf("PriorSessionID = %q, want %q", task.PriorSessionID, tt.wantSession)
			}
			if taskCtx.PriorSessionResumed != (tt.wantSession != "") {
				t.Fatalf("PriorSessionResumed = %v, want %v", taskCtx.PriorSessionResumed, tt.wantSession != "")
			}
			// A dropped resume (had a session, now cleared) must be surfaced to
			// the user via the brief; a kept/no-op resume must not.
			wantUnavailable := tt.sessionID != "" && tt.wantSession == ""
			if taskCtx.PriorSessionResumeUnavailable != wantUnavailable {
				t.Fatalf("PriorSessionResumeUnavailable = %v, want %v", taskCtx.PriorSessionResumeUnavailable, wantUnavailable)
			}
		})
	}
}

func TestSessionHomeReachable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provider   string
		env        *execenv.Environment
		envReused  bool
		wantReturn bool
	}{
		{
			// Every non-Hermes backend keys its sessions by cwd (or resolves
			// its own store), so this predicate must not narrow their gate.
			name:       "cwd-keyed provider is always reachable",
			provider:   "claude",
			env:        &execenv.Environment{},
			wantReturn: true,
		},
		{
			name:     "hermes with a mounted store holding history",
			provider: "hermes",
			env: &execenv.Environment{
				HermesSessionStore:          "/profile/hermes-sessions/a/default/issue-1",
				HermesSessionHistoryPresent: true,
			},
			wantReturn: true,
		},
		{
			// Mounted onto nothing: a first turn, a store the GC reclaimed
			// between turns, a switched profile, or a dangling link. Reading
			// "mounted" as "resumable" here would forward a dead session id.
			name:       "hermes with a mounted but empty session store",
			provider:   "hermes",
			env:        &execenv.Environment{HermesSessionStore: "/profile/hermes-sessions/a/default/issue-1"},
			wantReturn: false,
		},
		{
			// A store is mounted, so the env-reuse fallback must not override
			// the store's own answer — the transcript lives in the store now.
			name:       "hermes with an empty store is not rescued by env reuse",
			provider:   "hermes",
			env:        &execenv.Environment{HermesSessionStore: "/profile/hermes-sessions/a/default/issue-1"},
			envReused:  true,
			wantReturn: false,
		},
		{
			// No store, but the prior task's env root — and therefore its
			// overlay's task-local state.db — carried over.
			name:       "hermes on a reused env root",
			provider:   "hermes",
			env:        &execenv.Environment{},
			envReused:  true,
			wantReturn: true,
		},
		{
			// The GH #6806 shape: a fresh overlay with an empty state.db, so
			// no session recorded by a prior task can be found here.
			name:       "hermes on a fresh overlay with no store",
			provider:   "hermes",
			env:        &execenv.Environment{},
			wantReturn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionHomeReachable(tt.provider, tt.env, tt.envReused); got != tt.wantReturn {
				t.Fatalf("sessionHomeReachable() = %v, want %v", got, tt.wantReturn)
			}
		})
	}
}

// newCodexStoreGuardDaemon builds a Daemon with only the per-issue Codex session
// store guard initialised — enough to exercise the reserve-vs-mark protocol.
func newCodexStoreGuardDaemon() *Daemon {
	d := &Daemon{
		activeStores:   map[string]int{},
		deletingStores: map[string]bool{},
	}
	d.activeStoresCond = sync.NewCond(&d.activeStoresMu)
	return d
}

// A task that marks a store active before the GC reserves it must block the
// deletion — this is exactly Elon's mark-then-delete interleaving, which the old
// point-in-time active check allowed.
func TestCodexStoreGuard_MarkBeforeReserveBlocksDeletion(t *testing.T) {
	t.Parallel()
	d := newCodexStoreGuardDaemon()
	const store = "/stores/agent/issue"

	d.markActiveStore(store)
	if _, ok := d.reserveStoreForDeletion(store); ok {
		t.Fatal("reserve must refuse a store a live task already holds")
	}
	d.unmarkActiveStore(store)

	commit, ok := d.reserveStoreForDeletion(store)
	if !ok {
		t.Fatal("reserve should succeed once the store is inactive")
	}
	commit()
}

// The reverse interleaving: once the GC has reserved a store, a task's
// markActive must block until the removal commits (so it never mounts a store
// mid-removal), then proceed.
func TestCodexStoreGuard_ReserveBlocksMarkUntilCommit(t *testing.T) {
	t.Parallel()
	d := newCodexStoreGuardDaemon()
	const store = "/stores/agent/issue"

	commit, ok := d.reserveStoreForDeletion(store)
	if !ok {
		t.Fatal("reserve should succeed on an inactive store")
	}

	marked := make(chan struct{})
	go func() {
		d.markActiveStore(store)
		close(marked)
	}()

	select {
	case <-marked:
		t.Fatal("markActiveStore must block while the store is reserved for deletion")
	case <-time.After(100 * time.Millisecond):
	}

	commit()

	select {
	case <-marked:
	case <-time.After(2 * time.Second):
		t.Fatal("markActiveStore must proceed after the reservation is committed")
	}
	// The store is now active, so a fresh reserve must be refused.
	if _, ok := d.reserveStoreForDeletion(store); ok {
		t.Fatal("store must be active after the blocked mark proceeds")
	}
}

// Two GC passes cannot both reserve the same store; the second is refused until
// the first commits.
func TestCodexStoreGuard_SecondReserveRefusedWhileDeleting(t *testing.T) {
	t.Parallel()
	d := newCodexStoreGuardDaemon()
	const store = "/stores/agent/issue"

	commit, ok := d.reserveStoreForDeletion(store)
	if !ok {
		t.Fatal("first reserve should succeed")
	}
	if _, ok := d.reserveStoreForDeletion(store); ok {
		t.Fatal("a second reserve must be refused while a removal is in flight")
	}
	commit()
	commit2, ok := d.reserveStoreForDeletion(store)
	if !ok {
		t.Fatal("reserve should succeed again after the prior removal committed")
	}
	commit2()
}

func TestExecuteAndDrain_ResumeFailureFallback(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	ctx := context.Background()
	taskLog := slog.Default()

	fb := &fakeBackend{
		results: []agent.Result{
			{Status: "failed", Error: "no conversation found", ResumeRejected: true, Usage: map[string]agent.TokenUsage{
				"m1": {InputTokens: 5},
			}},
			{Status: "completed", Output: "done", SessionID: "new-sess", Usage: map[string]agent.TokenUsage{
				"m1": {InputTokens: 10, OutputTokens: 20},
			}},
		},
	}

	// First attempt: resume fails (no SessionID in result).
	opts := agent.ExecOptions{ResumeSessionID: "stale-id"}
	var msgSeq atomic.Int32
	result, tools, err := d.executeAndDrain(ctx, fb, "prompt", opts, taskLog, "task-1", "", &msgSeq)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if result.Status != "failed" || result.SessionID != "" {
		t.Fatalf("expected failed result with empty SessionID, got %+v", result)
	}

	// Mirrors the retry in runTask, gated on the same production predicate.
	if shouldRetryWithFreshSession(result, opts.ResumeSessionID, tools, "claude") {
		firstUsage := result.Usage
		opts.ResumeSessionID = ""
		retryResult, _, retryErr := d.executeAndDrain(ctx, fb, "prompt", opts, taskLog, "task-1", "", &msgSeq)
		if retryErr != nil {
			t.Fatalf("retry error: %v", retryErr)
		}
		result = retryResult
		result.Usage = mergeUsage(firstUsage, result.Usage)
	}

	if result.Status != "completed" || result.Output != "done" {
		t.Fatalf("expected completed result, got %+v", result)
	}
	if result.SessionID != "new-sess" {
		t.Fatalf("expected new-sess, got %s", result.SessionID)
	}
	// Usage should be merged.
	if u := result.Usage["m1"]; u.InputTokens != 15 || u.OutputTokens != 20 {
		t.Fatalf("expected merged usage {15,20}, got %+v", u)
	}
	// Second call should NOT have ResumeSessionID.
	if fb.calls[1].ResumeSessionID != "" {
		t.Fatal("retry should not have ResumeSessionID")
	}
}

// transcriptBackend emits a tool_use and a text message before returning its
// result — the first call fails like a broken resume, the second completes.
type transcriptBackend struct {
	calls atomic.Int32
}

func (b *transcriptBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	n := b.calls.Add(1)
	msgCh := make(chan agent.Message, 2)
	msgCh <- agent.Message{Type: agent.MessageToolUse, Tool: "bash"}
	msgCh <- agent.Message{Type: agent.MessageText, Content: fmt.Sprintf("attempt %d", n)}
	close(msgCh)
	resCh := make(chan agent.Result, 1)
	if n == 1 {
		resCh <- agent.Result{Status: "failed", Error: "session not found"}
	} else {
		resCh <- agent.Result{Status: "completed", Output: "done", SessionID: "sess-2"}
	}
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// transcriptRecorder collects the task messages a daemon reports to its
// message endpoint.
type transcriptRecorder struct {
	mu       sync.Mutex
	messages []TaskMessageData
}

func (r *transcriptRecorder) snapshot() []TaskMessageData {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.messages)
}

// newTranscriptRecorder returns a daemon whose message endpoint appends every
// reported batch to the returned recorder.
func newTranscriptRecorder(t *testing.T) (*Daemon, *transcriptRecorder) {
	t.Helper()
	rec := &transcriptRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			var body struct {
				Messages []TaskMessageData `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				rec.mu.Lock()
				rec.messages = append(rec.messages, body.Messages...)
				rec.mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return &Daemon{client: NewClient(srv.URL), logger: slog.Default()}, rec
}

// TestExecuteAndDrain_FlushesTranscriptBeforeReturningResult pins the
// completion contract: once the agent result is handed back, the task's
// messages have been reported. Without the wait the drain goroutine is still
// in flight, so a consumer reading the transcript sees it truncated.
func TestExecuteAndDrain_FlushesTranscriptBeforeReturningResult(t *testing.T) {
	t.Parallel()

	d, rec := newTranscriptRecorder(t)

	result, _, err := d.executeAndDrain(context.Background(), &transcriptBackend{}, "p", agent.ExecOptions{}, slog.Default(), "task-flush", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("expected the backend's result, got %+v", result)
	}

	if got := rec.snapshot(); len(got) != 2 {
		t.Fatalf("expected the transcript flushed before the result hand-off, got %d messages: %+v", len(got), got)
	}
}

// TestExecuteAndDrain_SeqContinuesAcrossRetry pins the transcript's ordering
// key: the server sorts a task's messages by seq alone, so a same-task resume
// retry must keep numbering upwards instead of restarting at 1 and
// interleaving its rows with the failed attempt's.
func TestExecuteAndDrain_SeqContinuesAcrossRetry(t *testing.T) {
	t.Parallel()

	d, rec := newTranscriptRecorder(t)
	fb := &transcriptBackend{}
	var msgSeq atomic.Int32

	result, _, err := d.executeAndDrain(context.Background(), fb, "p", agent.ExecOptions{ResumeSessionID: "stale"}, slog.Default(), "task-seq", "", &msgSeq)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("expected failed first result, got %+v", result)
	}

	result, _, err = d.executeAndDrain(context.Background(), fb, "p", agent.ExecOptions{}, slog.Default(), "task-seq", "", &msgSeq)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed retry result, got %+v", result)
	}

	got := rec.snapshot()
	if len(got) != 4 {
		t.Fatalf("expected 4 messages total, got %d: %+v", len(got), got)
	}
	for i, m := range got {
		if m.Seq != i+1 {
			t.Fatalf("expected strictly ascending seq across the retry, got %+v", got)
		}
	}
}

// sessionBackend hands out a pre-built session, leaving the test in control
// of message and result delivery.
type sessionBackend struct {
	session *agent.Session
}

func (b sessionBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	return b.session, nil
}

// TestExecuteAndDrain_ContextCancelled_FlushesPendingTranscript pins the same
// flush-before-return contract for the drainCtx.Done() terminal: when a
// cancellation (or timeout/watchdog) ends the run with a message still
// pending, executeAndDrain must not return until that tail is reported —
// otherwise runTask fails-and-broadcasts while the last batch is in flight.
func TestExecuteAndDrain_ContextCancelled_FlushesPendingTranscript(t *testing.T) {
	t.Parallel()

	d, rec := newTranscriptRecorder(t)

	// Unbuffered: the send below returns only once the drain loop has
	// consumed the message. The result channel never delivers, so only the
	// context cancellation can end the drain.
	msgCh := make(chan agent.Message)
	b := sessionBackend{session: &agent.Session{Messages: msgCh, Result: make(chan agent.Result)}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	type ret struct {
		result agent.Result
		err    error
	}
	retCh := make(chan ret, 1)
	go func() {
		result, _, err := d.executeAndDrain(ctx, b, "p", agent.ExecOptions{}, slog.Default(), "task-cancel-flush", "", new(atomic.Int32))
		retCh <- ret{result, err}
	}()

	msgCh <- agent.Message{Type: agent.MessageText, Content: "pending tail"}
	cancel()

	r := <-retCh
	if r.err != nil {
		t.Fatalf("executeAndDrain: %v", r.err)
	}
	if r.result.Status != "cancelled" {
		t.Fatalf("expected status=cancelled, got %q (err=%q)", r.result.Status, r.result.Error)
	}

	got := rec.snapshot()
	if len(got) != 1 || got[0].Type != "text" || got[0].Content != "pending tail" {
		t.Fatalf("expected the pending tail flushed before the cancelled return, got %+v", got)
	}
}

func TestExecuteAndDrain_NoRetryAfterToolsExecuted(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)

	fb := &fakeBackend{
		results: []agent.Result{
			{Status: "failed", Error: "model error", SessionID: "valid-sess"},
		},
	}

	opts := agent.ExecOptions{ResumeSessionID: "some-id"}
	result, tools, err := d.executeAndDrain(context.Background(), fb, "p", opts, slog.Default(), "t", "", new(atomic.Int32))
	if err != nil {
		t.Fatal(err)
	}

	// A run that executed tools may have mutated the world (posted a comment,
	// opened a PR, written commits into the reused workdir). Re-running it
	// from scratch would duplicate those side effects, so the fallback must
	// stay out regardless of what the backend reported as SessionID.
	if shouldRetryWithFreshSession(result, opts.ResumeSessionID, tools+1, "claude") {
		t.Fatal("should not retry once tools have executed")
	}
	if int(fb.idx.Load()) != 1 {
		t.Fatalf("expected 1 call, got %d", fb.idx.Load())
	}
}

// A mid-stream provider disconnect on a resumed run must leave the session
// pointer alone: internal/service/task.go treats provider_network as
// resume-safe and expects its retry to continue the truncated conversation.
// If the daemon reset the session first, that contract would be unsatisfiable.
func TestExecuteAndDrain_NetworkFailureKeepsResumeSession(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)

	fb := &fakeBackend{
		results: []agent.Result{
			{
				Status:    "failed",
				Error:     "API Error: Connection closed mid-response",
				SessionID: "live-sess",
			},
		},
	}

	opts := agent.ExecOptions{ResumeSessionID: "live-sess"}
	result, tools, err := d.executeAndDrain(context.Background(), fb, "p", opts, slog.Default(), "t", "", new(atomic.Int32))
	if err != nil {
		t.Fatal(err)
	}

	if result.ResumeRejected {
		t.Fatal("a network drop must not be reported as a rejected resume")
	}
	if shouldRetryWithFreshSession(result, opts.ResumeSessionID, tools, "claude") {
		t.Fatal("network failure must not trigger a fresh-session retry")
	}
	if int(fb.idx.Load()) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", fb.idx.Load())
	}
	// The pointer the platform retry needs is still intact.
	if result.SessionID != "live-sess" {
		t.Fatalf("expected session to survive, got %q", result.SessionID)
	}
}

// TestExecuteAndDrain_AuthResolutionOnResumeRecoversInTurn is the GH #6777
// regression, driven through the same two-attempt sequence the daemon runs.
//
// The reported symptom was a Chat that alternated success / failure forever:
// turn 1 opened a session and worked; turn 2 resumed it and died with the
// provider's auth-resolution error; the server-side resume guards then
// blacklisted that session so turn 3 started fresh and worked again, and so on.
// Server-side blacklisting alone can only produce that alternation — curing the
// FAILING turn needs the in-turn fresh retry, which never fired because nothing
// identified the error as fatal-to-resume. This asserts the whole recovery:
// the gate opens on attempt 1, and a cold attempt 2 succeeds with its own
// session id.
func TestExecuteAndDrain_AuthResolutionOnResumeRecoversInTurn(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)

	const authErr = `hermes provider error: "Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted"`

	fb := &fakeBackend{
		results: []agent.Result{
			// Attempt 1: resumed session, provider identity unusable.
			{Status: "failed", Error: authErr, SessionID: "ses_resumed"},
			// Attempt 2: cold session re-resolves the provider from config.
			{Status: "completed", Output: "hello", SessionID: "ses_fresh"},
		},
	}

	opts := agent.ExecOptions{ResumeSessionID: "ses_resumed"}
	result, tools, err := d.executeAndDrain(context.Background(), fb, "p", opts, slog.Default(), "t", "", new(atomic.Int32))
	if err != nil {
		t.Fatal(err)
	}
	// The adapter must NOT claim the resume was rejected — nothing rejected it,
	// and Result.ResumeRejected is documented as excluding auth failures. The
	// recovery has to come from the gate instead.
	if result.ResumeRejected {
		t.Fatal("an auth-resolution failure must not be reported as a rejected resume")
	}
	if !shouldRetryWithFreshSession(result, opts.ResumeSessionID, tools, "hermes") {
		t.Fatal("a resumed session that cannot resolve auth must retry once from a fresh session; without this the failing turn is never cured and the user sees alternating failures")
	}

	// What runTask does on that verdict: retire the prior session and re-run
	// cold. Mirrored here so the sequence — not just the predicate — is pinned.
	retired := opts.ResumeSessionID
	freshOpts := opts
	freshOpts.ResumeSessionID = ""
	retry, retryTools, err := d.executeAndDrain(context.Background(), fb, "p", freshOpts, slog.Default(), "t", "", new(atomic.Int32))
	if err != nil {
		t.Fatal(err)
	}
	final, _ := reconcileFreshRetryResult(result, result.Usage, tools, retry, retryTools, nil)

	if final.Status != "completed" {
		t.Fatalf("status = %q, want completed — the user's message must succeed on this turn", final.Status)
	}
	if final.SessionID != "ses_fresh" {
		t.Fatalf("session id = %q, want ses_fresh so the next turn resumes the working session", final.SessionID)
	}
	if final.SessionID == retired {
		t.Fatal("the dead session must not become the resume pointer again")
	}
	if int(fb.idx.Load()) != 2 {
		t.Fatalf("expected exactly 2 attempts (one retry, not a loop), got %d", fb.idx.Load())
	}
	if fb.calls[1].ResumeSessionID != "" {
		t.Fatalf("retry asked to resume %q; it must be a cold start", fb.calls[1].ResumeSessionID)
	}
	// The server-side half of the fix still has to hold: even though this turn
	// recovered, the failed attempt's error must keep that session out of every
	// later resume lookup.
	if !taskfailure.AuthMethodUnresolved(result.Error) {
		t.Fatal("the failed attempt's error must still be recognised so ResumeUnsafeFailure and the resume queries retire the dead session")
	}
}

func TestShouldRetryWithFreshSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		result         agent.Result
		priorSessionID string
		tools          int32
		// provider defaults to claude — a backend that CAN detect rejections,
		// so cases that leave it unset assert the capable-backend contract.
		provider string
		want     bool
	}{
		{
			name:           "rejected resume before any tool retries",
			result:         agent.Result{Status: "failed", Error: "no conversation found", ResumeRejected: true},
			priorSessionID: "stale-id",
			want:           true,
		},
		{
			// The reported bug: the session belongs to another provider
			// account and the backend echoes the requested id back on the
			// rejection, so SessionID stays non-empty. The backend still
			// reports ResumeRejected, which is what the gate reads now.
			name: "account rejection echoing session id retries",
			result: agent.Result{
				Status:         "failed",
				Error:          "400 此 session 已绑定另外的ai账号，请执行 /new 开启新 session",
				SessionID:      "stale-id",
				ResumeRejected: true,
			},
			priorSessionID: "stale-id",
			want:           true,
		},
		{
			// qwen-code 0.20.0's real wording, from
			// pkg/agent/testdata/qwen-code-0.20.0-resume-not-found.stderr.txt.
			// The backend reports no session at all here, so before the
			// phrase was recognised this path silently lost its recovery.
			name: "qwen stale session rejection retries",
			result: agent.Result{
				Status:         "failed",
				Error:          "No saved session found with ID session-redacted. Run `qwen --resume` without an ID to choose from existing sessions.",
				ResumeRejected: true,
			},
			priorSessionID: "session-redacted",
			want:           true,
		},
		{
			name:           "no resume requested never retries",
			result:         agent.Result{Status: "failed", Error: "boom", ResumeRejected: true},
			priorSessionID: "",
			want:           false,
		},

		// The compatibility path for backends that cannot detect a rejection
		// (antigravity, copilot, cursor, deveco, opencode). An empty
		// SessionID is all they can offer, and it only proves no session was
		// established — so it still gates a retry, but only for failures a
		// fresh session could plausibly cure.
		{
			name:           "undetectable backend with no session established retries",
			result:         agent.Result{Status: "failed", Error: "agent exited before dispatching"},
			priorSessionID: "stale-id",
			provider:       "copilot",
			want:           true,
		},
		{
			name:           "undetectable backend that established a session does not retry",
			result:         agent.Result{Status: "failed", Error: "agent exited before dispatching", SessionID: "live-sess"},
			priorSessionID: "stale-id",
			provider:       "copilot",
			want:           false,
		},
		{
			// The regression that motivated the positive signal: without the
			// classification guard, every unsignalled network blip would
			// reset the session these backends were about to resume.
			name:           "undetectable backend network drop does not retry",
			result:         agent.Result{Status: "failed", Error: "API Error: Connection closed mid-response"},
			priorSessionID: "stale-id",
			provider:       "cursor",
			want:           false,
		},
		{
			name:           "undetectable backend rate limit does not retry",
			result:         agent.Result{Status: "failed", Error: "API Error: 429 rate limit exceeded"},
			priorSessionID: "stale-id",
			provider:       "deveco",
			want:           false,
		},

		// Failures with a defined non-session remedy. Restarting the
		// conversation cannot fix a missing binary or an unavailable model,
		// so these must not burn a second run even on the compat path.
		{
			name:           "missing config does not retry",
			result:         agent.Result{Status: "failed", Error: "missing environment variable: ANTHROPIC_API_KEY"},
			priorSessionID: "stale-id",
			provider:       "copilot",
			want:           false,
		},
		{
			name:           "unavailable model does not retry",
			result:         agent.Result{Status: "failed", Error: "model not found: gpt-99"},
			priorSessionID: "stale-id",
			provider:       "opencode",
			want:           false,
		},
		{
			name:           "missing executable does not retry",
			result:         agent.Result{Status: "failed", Error: "cursor-agent: executable not found"},
			priorSessionID: "stale-id",
			provider:       "cursor",
			want:           false,
		},
		{
			name:           "unsupported runtime version does not retry",
			result:         agent.Result{Status: "failed", Error: "installed CLI is below the minimum supported version"},
			priorSessionID: "stale-id",
			provider:       "antigravity",
			want:           false,
		},
		{
			name:           "successful run never retries",
			result:         agent.Result{Status: "completed", SessionID: "sess"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "failure after a tool ran never retries",
			result:         agent.Result{Status: "failed", Error: "no conversation found", ResumeRejected: true},
			priorSessionID: "stale-id",
			tools:          1,
			want:           false,
		},

		// Failures a fresh session cannot cure. Each of these previously
		// slipped through the exclusion-based gate and cost a wasted run plus
		// a destroyed session pointer. provider_network is the sharpest case:
		// internal/service/task.go marks it resume-safe precisely so the
		// platform retry can continue the truncated conversation.
		{
			name:           "provider network drop keeps the session",
			result:         agent.Result{Status: "failed", Error: "API Error: Connection closed mid-response"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "dns failure keeps the session",
			result:         agent.Result{Status: "failed", Error: "dial tcp: lookup api.anthropic.com: no such host"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "rate limit keeps the session",
			result:         agent.Result{Status: "failed", Error: "API Error: 429 rate limit exceeded"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "overloaded keeps the session",
			result:         agent.Result{Status: "failed", Error: "API Error: 529 overloaded_error"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "quota exhaustion keeps the session",
			result:         agent.Result{Status: "failed", Error: "API Error: 402 credit balance is too low"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "provider 5xx keeps the session",
			result:         agent.Result{Status: "failed", Error: "API Error: 500 internal_server_error"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			// Unclassified AND a session was established: nothing suggests
			// the resume was the problem, so leave the pointer alone.
			name:           "unrecognised failure with a live session keeps it",
			result:         agent.Result{Status: "failed", Error: "exit status 1", SessionID: "live-sess"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			// Bad credentials cannot succeed on a second attempt. A 401
			// before the first stream message leaves SessionID empty, which
			// is exactly why an empty id never meant "resume was rejected".
			name:           "provider auth failure does not retry",
			result:         agent.Result{Status: "failed", Error: "API Error: 401 Unauthorized"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "revoked oauth token does not retry",
			result:         agent.Result{Status: "failed", Error: "OAuth access token has been revoked"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			// Mid-execution terminals carry their own status and must never
			// reach the fallback.
			name:           "idle watchdog terminal does not retry",
			result:         agent.Result{Status: "idle_watchdog", Error: "no activity", ResumeRejected: true},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			name:           "cancelled terminal does not retry",
			result:         agent.Result{Status: "cancelled"},
			priorSessionID: "stale-id",
			want:           false,
		},
		{
			// GH #5975: the Kiro backend reports ResumeRejected for an
			// oversized historical image while KEEPING the poisoned session
			// id for auditing. The gate reads the boolean, not an empty id,
			// so a non-empty SessionID must still allow the fresh retry.
			name: "kiro oversized history image retries despite non-empty session id",
			result: agent.Result{
				Status:         "failed",
				Error:          "kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)",
				SessionID:      "ses_poisoned",
				ResumeRejected: true,
			},
			priorSessionID: "ses_poisoned",
			provider:       "kiro",
			want:           true,
		},
		{
			// Same oversized-image rejection, but a tool already ran this
			// attempt — the retry could duplicate external side effects
			// (comments, commits), so the single-retry gate must refuse.
			name: "kiro oversized history image after a tool ran never retries",
			result: agent.Result{
				Status:         "failed",
				Error:          "kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)",
				SessionID:      "ses_poisoned",
				ResumeRejected: true,
			},
			priorSessionID: "ses_poisoned",
			tools:          1,
			provider:       "kiro",
			want:           false,
		},
		{
			// MUL-5722: a codex thread/resume whose response overflowed the
			// stdout line buffer. This is the case #5715 accidentally
			// stranded — codex reported no rejection and is not in the
			// undetectable set, so the gate returned false and the same
			// oversized thread got resumed on every later turn. The backend
			// now supplies the positive evidence; this asserts the gate
			// actually acts on it.
			name: "codex resume overflow retries on a fresh session",
			result: agent.Result{
				Status:         "failed",
				Error:          "codex thread/resume failed: codex process exited: bufio.Scanner: token too long",
				ResumeRejected: true,
			},
			priorSessionID: "thr_oversized",
			tools:          0,
			provider:       "codex",
			want:           true,
		},
		{
			// The tools gate is not weakened by the new evidence: a run that
			// already acted is never replayed, poisoned resume or not. Such a
			// task still gets its session retired — by the layer-3 classifier
			// at report time rather than by an in-turn retry.
			name: "codex resume overflow after tool use does not retry",
			result: agent.Result{
				Status:         "failed",
				Error:          "codex thread/resume failed: codex process exited: bufio.Scanner: token too long",
				ResumeRejected: true,
			},
			priorSessionID: "thr_oversized",
			tools:          1,
			provider:       "codex",
			want:           false,
		},
		{
			// GH #6777: the alternating Chat failure. Hermes rebuilds the
			// resumed session but persists a normalised provider identity that
			// can no longer resolve its credentials, so the turn dies with the
			// SDK's auth-resolution message. Nothing rejected the resume, so
			// ResumeRejected is false and correctly so — the adapter cannot
			// tell this from a bad credential. Only this gate knows the run was
			// a resume, and a fresh session re-resolves the provider from
			// current config.
			name: "hermes resumed session that cannot resolve auth retries",
			result: agent.Result{
				Status:    "failed",
				Error:     `hermes provider error: "Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted"`,
				SessionID: "ses_resumed",
			},
			priorSessionID: "ses_resumed",
			provider:       "hermes",
			want:           true,
		},
		{
			// The same failure surfacing one ACP step earlier: a resumed
			// session whose persisted provider was flattened gets a
			// non-redundant session/set_model, which re-runs provider
			// auto-detection and mis-routes (MUL-5029). The adapter wraps the
			// message instead of replacing it, which is why matching the
			// phrase here covers all three lifecycle steps at once.
			name: "hermes set_model auth-resolution failure on resume retries",
			result: agent.Result{
				Status:    "failed",
				Error:     `hermes could not switch to model "custom:deepseek-v4-pro": session/set_model: Could not resolve authentication method. Expected either api_key or auth_token to be set.`,
				SessionID: "ses_resumed",
			},
			priorSessionID: "ses_resumed",
			provider:       "hermes",
			want:           true,
		},
		{
			// The tools gate is untouched by the new evidence, exactly as for
			// the poisoned-history branch above: a turn that already acted is
			// never replayed. Its session is still retired at report time by
			// ResumeUnsafeFailure's matching text guard.
			name: "hermes auth-resolution failure after a tool ran never retries",
			result: agent.Result{
				Status:    "failed",
				Error:     `hermes provider error: "Could not resolve authentication method. Expected either api_key or auth_token to be set."`,
				SessionID: "ses_resumed",
			},
			priorSessionID: "ses_resumed",
			tools:          1,
			provider:       "hermes",
			want:           false,
		},
		{
			// The load-bearing scope limit: on a COLD run the same message
			// means the agent's provider config really is incomplete. There is
			// no session to blame and no fresh session to fall back to, so the
			// error must reach the user instead of burning a second run. The
			// priorSessionID gate is what encodes that distinction.
			name: "cold run that cannot resolve auth does not retry",
			result: agent.Result{
				Status: "failed",
				Error:  `hermes provider error: "Could not resolve authentication method. Expected either api_key or auth_token to be set."`,
			},
			priorSessionID: "",
			provider:       "hermes",
			want:           false,
		},
		{
			// Narrowness guard, mirroring the SQL side's auth-adjacent test: a
			// rejected credential is about the credential, not the session's
			// copy of the provider, and a fresh session replays it verbatim.
			// Widening the phrase to any authentication-shaped error would
			// discard healthy conversation pointers on every such failure.
			name: "hermes rejected credential on resume keeps the session",
			result: agent.Result{
				Status:    "failed",
				Error:     `hermes provider error: "401 authentication_error: invalid x-api-key"`,
				SessionID: "ses_resumed",
			},
			priorSessionID: "ses_resumed",
			provider:       "hermes",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider := tt.provider
			if provider == "" {
				provider = "claude"
			}
			if got := shouldRetryWithFreshSession(tt.result, tt.priorSessionID, tt.tools, provider); got != tt.want {
				t.Fatalf("shouldRetryWithFreshSession(provider=%q) = %v, want %v", provider, got, tt.want)
			}
		})
	}
}

// The compatibility path must be reachable ONLY by backends that cannot
// report a rejection. One identical result, opposite answers: a backend that
// can detect has already said "not a rejection" by leaving ResumeRejected
// false, and must be taken at its word rather than second-guessed by
// exclusion.
func TestShouldRetryWithFreshSession_CompatPathIsBackendScoped(t *testing.T) {
	t.Parallel()

	// Unclassified startup failure, no session established — the exact shape
	// the compat path exists to catch.
	result := agent.Result{Status: "failed", Error: "exit status 1"}

	undetectable := []string{"antigravity", "copilot", "cursor", "deveco", "opencode"}
	for _, provider := range undetectable {
		t.Run(provider+" retries", func(t *testing.T) {
			t.Parallel()
			if !shouldRetryWithFreshSession(result, "stale-id", 0, provider) {
				t.Fatalf("%s cannot detect rejections; it must keep its empty-session recovery", provider)
			}
		})
	}

	detectable := []string{"claude", "codebuddy", "qwen", "codex", "grok", "hermes", "kimi", "reasonix", "kiro", "qoder", "qoderclicn", "traecli", "pi", "omp", "openclaw"}
	for _, provider := range detectable {
		t.Run(provider+" does not retry", func(t *testing.T) {
			t.Parallel()
			if shouldRetryWithFreshSession(result, "stale-id", 0, provider) {
				t.Fatalf("%s reports rejections; a false ResumeRejected is an answer, not an absence", provider)
			}
		})
	}

	t.Run("unknown provider fails closed", func(t *testing.T) {
		t.Parallel()
		if shouldRetryWithFreshSession(result, "stale-id", 0, "some-future-backend") {
			t.Fatal("a backend not listed as undetectable must not inherit the compat path")
		}
	})
}

// TestShouldRetryWithFreshSession_UnresumableHistoryIsBackendAgnostic pins the
// DECISION layer for GH #6066 / GH #5760: given the same provider error, every
// supported backend gets the same verdict, so recovery is not a property of
// whichever adapter happened to get a bug report. ResumeRejected is false for
// all of them here — nothing rejected the resume; the transcript loaded and the
// provider refused to replay it.
//
// Scope, deliberately: this feeds each backend a hand-built agent.Result, so it
// proves the shared decision ignores the provider NAME. It does not prove all
// 17 adapters surface an upstream error into Result.Error in the first place —
// #5760 is the counter-example, where the Kimi/ACP adapter swallowed the error
// and reported completed. The fix therefore covers every backend that surfaces
// the error; adapter-level surfacing is tracked separately (#5785 for ACP).
//
// The safety half is asserted in the same loop: tools > 0 must still veto the
// retry for every backend, because re-running a turn that already posted a
// comment or wrote a commit duplicates that side effect. Those tasks recover
// on the next trigger instead — classifyPoisonedError retires the session at
// report time.
func TestShouldRetryWithFreshSession_UnresumableHistoryIsBackendAgnostic(t *testing.T) {
	t.Parallel()

	// Both real-world wordings, neither of which the pre-fix detector matched.
	errors := map[string]string{
		"gh6066": "Invalid request: the message at position 37 with role 'assistant' must not be empty",
		"gh5760": "kimi provider error: provider.api_error: 400 the message at position 43 with role 'assistant' must not be empty",
	}

	for _, provider := range agent.SupportedTypes {
		for label, errMsg := range errors {
			t.Run(provider+"/"+label+" retries fresh", func(t *testing.T) {
				t.Parallel()
				result := agent.Result{Status: "failed", Error: errMsg, SessionID: "poisoned-id"}
				if !shouldRetryWithFreshSession(result, "poisoned-id", 0, provider) {
					t.Fatalf("%s: an unresumable history must trigger the fresh-session retry on every backend", provider)
				}
			})

			t.Run(provider+"/"+label+" respects the tool gate", func(t *testing.T) {
				t.Parallel()
				result := agent.Result{Status: "failed", Error: errMsg, SessionID: "poisoned-id"}
				if shouldRetryWithFreshSession(result, "poisoned-id", 1, provider) {
					t.Fatalf("%s: a run that already used a tool must never be re-run automatically", provider)
				}
			})
		}
	}
}

func TestExecuteAndDrain_CodexInactivityReportsToolResultTranscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\n" +
		`read line` + "\n" +
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n" +
		`read line` + "\n" +
		`read line` + "\n" +
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-drain"}}}'` + "\n" +
		`read line` + "\n" +
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'` + "\n" +
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-drain","turn":{"id":"turn-drain"}}}'` + "\n" +
		`echo '{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thr-drain","item":{"type":"commandExecution","id":"cmd-1","command":"git status"}}}'` + "\n" +
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-drain","item":{"type":"commandExecution","id":"cmd-1","aggregatedOutput":"clean"}}}'` + "\n" +
		`sleep 5` + "\n"
	if err := os.WriteFile(fakePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	if err := os.Chmod(fakePath, 0o755); err != nil {
		t.Fatalf("chmod fake codex: %v", err)
	}

	var mu sync.Mutex
	var reported []TaskMessageData
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/task-stale/messages" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []TaskMessageData `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode task messages: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		reported = append(reported, body.Messages...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	backend, err := agent.New("codex", agent.Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}
	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	result, tools, err := d.executeAndDrain(context.Background(), backend, "prompt", agent.ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	}, slog.Default(), "task-stale", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	if tools != 1 {
		t.Fatalf("expected one tool use, got %d", tools)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		var gotToolUse, gotToolResult bool
		for _, msg := range reported {
			if msg.Seq == 1 && msg.Type == "tool_use" && msg.Tool == "exec_command" {
				gotToolUse = true
			}
			if msg.Seq == 2 && msg.Type == "tool_result" && msg.Tool == "exec_command" && msg.Output == "clean" {
				gotToolResult = true
			}
		}
		mu.Unlock()
		if gotToolUse && gotToolResult {
			return
		}
		if time.Now().After(deadline) {
			mu.Lock()
			defer mu.Unlock()
			t.Fatalf("expected tool_use seq=1 and tool_result seq=2 in transcript, got %+v", reported)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// blockingBackend returns a Session whose Result channel is never written to,
// so executeAndDrain can only exit via the drainCtx.Done() path.
type blockingBackend struct{}

func (blockingBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result)
	close(msgCh)
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

type countedRunningBackend struct {
	release <-chan struct{}
}

func (b countedRunningBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result, 1)
	go func() {
		<-b.release
		close(msgCh)
		resCh <- agent.Result{Status: "completed"}
	}()
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func TestExecuteAndDrainTracksRunningTaskCount(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := d.executeAndDrain(
			context.Background(),
			countedRunningBackend{release: release},
			"p",
			agent.ExecOptions{},
			slog.Default(),
			"task-counted-running",
			"",
			new(atomic.Int32),
		)
		done <- err
	}()

	deadline := time.Now().Add(time.Second)
	for d.runningTasks.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := d.runningTasks.Load(); got != 1 {
		t.Fatalf("running task count while backend is live = %d, want 1", got)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeAndDrain: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("executeAndDrain did not return after backend release")
	}
	if got := d.runningTasks.Load(); got != 0 {
		t.Fatalf("running task count after backend exit = %d, want 0", got)
	}
}

func TestExecuteAndDrain_ContextCancelled_ReportsCancelled(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, _, err := d.executeAndDrain(ctx, blockingBackend{}, "p", agent.ExecOptions{}, slog.Default(), "t", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "cancelled" {
		t.Fatalf("expected status=cancelled when parent ctx is cancelled, got %q (err=%q)", result.Status, result.Error)
	}
}

// idleWatchdogBackend simulates the MUL-2225 hang: emit one message to mark
// activity, then go silent forever. With a short AgentIdleWatchdog, the
// watchdog should fire and short-circuit executeAndDrain. With no wall-clock
// cap (opts.Timeout = 0) the drain loop imposes no deadline of its own, so the
// idle watchdog is the only thing that ends this otherwise-forever-silent run.
type idleWatchdogBackend struct {
	emitOne bool // when true, emit one message before going silent; when false, never emit anything
}

func (b idleWatchdogBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 1)
	resCh := make(chan agent.Result)
	if b.emitOne {
		msgCh <- agent.Message{Type: agent.MessageText, Content: "hello"}
	}
	// Deliberately do NOT close msgCh and never write to resCh — this models
	// a backend whose subprocess is hung and will never naturally complete.
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func TestExecuteAndDrain_IdleWatchdog_FiresOnInactivity(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	start := time.Now()
	result, _, err := d.executeAndDrain(ctx, idleWatchdogBackend{emitOne: true}, "p", agent.ExecOptions{}, slog.Default(), "t-idle", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("expected status=idle_watchdog, got %q (err=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "idle watchdog") {
		t.Fatalf("expected error to mention idle watchdog, got %q", result.Error)
	}
	// The watchdog should fire within a few ticks (interval = window/2 with
	// no floor for sub-minute windows). 5× window is generous and keeps the
	// test from racing in slow CI.
	if elapsed := time.Since(start); elapsed > 5*d.cfg.AgentIdleWatchdog {
		t.Fatalf("watchdog took too long to fire: %s (window=%s)", elapsed, d.cfg.AgentIdleWatchdog)
	}
}

func TestExecuteAndDrain_IdleWatchdog_FiresWhenNoMessageEverArrives(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// emitOne=false models a backend that hangs before sending any message.
	// lastActivityAt is initialised at executeAndDrain entry, so the same
	// window applies even with zero traffic.
	result, _, err := d.executeAndDrain(ctx, idleWatchdogBackend{emitOne: false}, "p", agent.ExecOptions{}, slog.Default(), "t-idle-zero", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("expected status=idle_watchdog when backend never emits, got %q (err=%q)", result.Status, result.Error)
	}
}

func TestExecuteAndDrain_IdleWatchdog_UsesPerRunOverride(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	start := time.Now()
	result, _, err := d.executeAndDrain(
		ctx,
		idleWatchdogBackend{emitOne: true},
		"p",
		agent.ExecOptions{IdleWatchdogTimeout: 50 * time.Millisecond},
		slog.Default(),
		"t-idle-override",
		"",
		new(atomic.Int32),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("expected status=idle_watchdog, got %q (err=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "50ms") {
		t.Fatalf("expected error to report the per-run threshold, got %q", result.Error)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("per-run watchdog override did not fire promptly: %s", elapsed)
	}
}

func TestExecuteAndDrain_IdleWatchdog_GlobalDisableWinsOverPerRunOverride(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 0

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	result, _, err := d.executeAndDrain(
		ctx,
		idleWatchdogBackend{emitOne: true},
		"p",
		agent.ExecOptions{IdleWatchdogTimeout: 20 * time.Millisecond},
		slog.Default(),
		"t-idle-global-off",
		"",
		new(atomic.Int32),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "cancelled" {
		t.Fatalf("global watchdog disable must win; got status=%q (err=%q)", result.Status, result.Error)
	}
}

func TestExecuteAndDrain_IdleWatchdog_PerRunOverrideCannotExtendGlobalWindow(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	result, _, err := d.executeAndDrain(
		ctx,
		idleWatchdogBackend{emitOne: true},
		"p",
		agent.ExecOptions{IdleWatchdogTimeout: 500 * time.Millisecond},
		slog.Default(),
		"t-idle-global-bound",
		"",
		new(atomic.Int32),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("expected status=idle_watchdog, got %q (err=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "50ms") {
		t.Fatalf("provider override must not extend the global threshold, got %q", result.Error)
	}
}

func TestExecuteAndDrain_IdleWatchdog_DisabledWhenZero(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	// Default zero value — watchdog disabled. Without a parent cancel the
	// blockingBackend would otherwise hang the test, so we cancel after a
	// short delay to confirm the run does NOT terminate as idle_watchdog.
	d.cfg.AgentIdleWatchdog = 0

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(80*time.Millisecond, cancel)

	result, _, err := d.executeAndDrain(ctx, idleWatchdogBackend{emitOne: true}, "p", agent.ExecOptions{}, slog.Default(), "t-idle-off", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status == "idle_watchdog" {
		t.Fatalf("watchdog should not fire when AgentIdleWatchdog=0, got status=%q", result.Status)
	}
	if result.Status != "cancelled" {
		t.Fatalf("expected status=cancelled (parent ctx fired), got %q", result.Status)
	}
}

func TestExecuteAndDrain_IdleWatchdog_HappyPathDoesNotFire(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 200 * time.Millisecond

	// fakeBackend completes immediately with a normal result, well inside the
	// idle window. The watchdog must not corrupt the disposition.
	fb := &fakeBackend{
		results: []agent.Result{
			{Status: "completed", Output: "done"},
		},
	}

	result, _, err := d.executeAndDrain(context.Background(), fb, "p", agent.ExecOptions{}, slog.Default(), "t-idle-happy", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status=completed on happy path, got %q (err=%q)", result.Status, result.Error)
	}
	if result.Output != "done" {
		t.Fatalf("expected output preserved, got %q", result.Output)
	}
}

// longToolCallBackend simulates a legitimate long-running tool call (e.g.
// `npm install`, `docker build`, full test suite). The backend emits a
// tool_use, stays silent past the idle window while the tool runs, then emits
// a tool_result and completes. This is the false-positive case the watchdog
// must NOT misfire on: an in-flight tool call is forward progress, not a hang.
type longToolCallBackend struct {
	toolSilence time.Duration // how long to stay silent between tool_use and tool_result
}

func (b longToolCallBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 4)
	resCh := make(chan agent.Result, 1)

	msgCh <- agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "Bash",
		CallID: "call-1",
		Input:  map[string]any{"cmd": "npm install"},
	}

	go func() {
		select {
		case <-time.After(b.toolSilence):
		case <-ctx.Done():
			// Watchdog cancelled us — propagate so the caller sees aborted.
			resCh <- agent.Result{Status: "aborted", Error: ctx.Err().Error()}
			close(msgCh)
			close(resCh)
			return
		}
		msgCh <- agent.Message{
			Type:   agent.MessageToolResult,
			Tool:   "Bash",
			CallID: "call-1",
			Output: "installed 142 packages",
		}
		msgCh <- agent.Message{Type: agent.MessageText, Content: "done"}
		close(msgCh)
		resCh <- agent.Result{Status: "completed", Output: "done"}
		close(resCh)
	}()

	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func TestExecuteAndDrain_IdleWatchdog_DoesNotFireDuringInFlightToolCall(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	// 50 ms window; tool stays silent for ~4× the window. Without the
	// in-flight-tool gate, the watchdog would fire and the run would come
	// back as idle_watchdog. With the gate, it must complete normally.
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond

	result, _, err := d.executeAndDrain(
		context.Background(),
		longToolCallBackend{toolSilence: 200 * time.Millisecond},
		"p",
		agent.ExecOptions{},
		slog.Default(),
		"t-long-tool",
		"",
		new(atomic.Int32),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status == "idle_watchdog" {
		t.Fatalf("watchdog must not fire while a tool_use is in flight, got status=%q (err=%q)", result.Status, result.Error)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (err=%q)", result.Status, result.Error)
	}
}

func TestExecuteAndDrain_IdleWatchdog_PerRunOverrideStillUsesToolWindow(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 500 * time.Millisecond
	d.cfg.AgentToolWatchdog = 500 * time.Millisecond

	result, _, err := d.executeAndDrain(
		context.Background(),
		longToolCallBackend{toolSilence: 200 * time.Millisecond},
		"p",
		agent.ExecOptions{IdleWatchdogTimeout: 50 * time.Millisecond},
		slog.Default(),
		"t-long-tool-override",
		"",
		new(atomic.Int32),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("per-run idle override must not replace the tool window; got status=%q (err=%q)", result.Status, result.Error)
	}
}

// stuckInFlightToolBackend models a hung tool: it emits a tool_use and then
// goes silent forever — the matching tool_result never arrives, so inFlightTools
// stays at 1 (e.g. a child process that never returns). With no wall-clock cap
// (the MUL-3064 default), AgentToolWatchdog is the only thing that ends it.
type stuckInFlightToolBackend struct{}

func (stuckInFlightToolBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 2)
	resCh := make(chan agent.Result)
	msgCh <- agent.Message{Type: agent.MessageToolUse, Tool: "Bash", CallID: "c1"}
	// Deliberately leave msgCh open, never emit tool_result, never write resCh.
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func TestExecuteAndDrain_IdleWatchdog_FiresOnStuckInFlightTool(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	// The normal idle window would be skipped while a tool is in flight; the
	// AgentToolWatchdog budget is what must fire here.
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond
	d.cfg.AgentToolWatchdog = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	start := time.Now()
	result, _, err := d.executeAndDrain(ctx, stuckInFlightToolBackend{}, "p", agent.ExecOptions{}, slog.Default(), "t-stuck-tool", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("expected status=idle_watchdog for a hung in-flight tool, got %q (err=%q)", result.Status, result.Error)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("tool watchdog took too long to fire: %s (window=%s)", elapsed, d.cfg.AgentToolWatchdog)
	}
}

// tailIdleAfterToolBackend exercises the boundary case: a tool call completes,
// and THEN the backend goes silent without ever finishing. After the
// tool_result lands, in-flight count returns to zero and lastActivityAt is
// fresh; the watchdog should fire exactly one window later, not earlier.
type tailIdleAfterToolBackend struct{}

func (tailIdleAfterToolBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 4)
	resCh := make(chan agent.Result)
	msgCh <- agent.Message{Type: agent.MessageToolUse, Tool: "Bash", CallID: "c1"}
	msgCh <- agent.Message{Type: agent.MessageToolResult, Tool: "Bash", CallID: "c1", Output: "ok"}
	// Deliberately leave msgCh open and never write to resCh.
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func TestExecuteAndDrain_IdleWatchdog_FiresAfterToolResultIfBackendStaysSilent(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	result, _, err := d.executeAndDrain(ctx, tailIdleAfterToolBackend{}, "p", agent.ExecOptions{}, slog.Default(), "t-tail-idle", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("expected status=idle_watchdog after tool_result with no further activity, got %q (err=%q)", result.Status, result.Error)
	}
}

// ensureRepoReady must refresh `workspaceState.settings` on every checkout —
// even when the repo cache already holds the URL. The /repo/checkout handler
// reads `workspaceCoAuthoredByEnabled` right after, and the 30s workspace
// sync tick is too slow to make a freshly-flipped GitHub toggle feel live.
// PR #2847 review by Emacs caught this fast-path regression; the test
// asserts the cached-repo path still issues exactly one refresh.
func TestEnsureRepoReadyCachedRepoStillRefreshesSettings(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/ws-1/repos" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: sourceRepo}},
			ReposVersion: "v2",
			Settings:     json.RawMessage(`{"github_enabled":false,"co_authored_by_enabled":true}`),
		})
	})
	if err := d.repoCache.Sync("ws-1", []repocache.RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("seed repo cache: %v", err)
	}
	// Workspace starts with the master switch ON. The server above will return
	// the user's just-flipped OFF state — ensureRepoReady must pick that up
	// before the handler reads workspaceCoAuthoredByEnabled.
	d.workspaces["ws-1"] = newWorkspaceState(
		"ws-1",
		nil,
		"v1",
		[]RepoData{{URL: sourceRepo}},
		json.RawMessage(`{"github_enabled":true,"co_authored_by_enabled":true}`),
	)
	if !d.workspaceCoAuthoredByEnabled("ws-1") {
		t.Fatalf("precondition: expected co-author hook enabled before checkout")
	}

	if err := d.ensureRepoReady(context.Background(), "ws-1", sourceRepo); err != nil {
		t.Fatalf("ensureRepoReady: %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 refresh call on cached repo, got %d", got)
	}
	if d.workspaceCoAuthoredByEnabled("ws-1") {
		t.Fatalf("expected co-author hook disabled after server-side toggle; daemon used stale workspaceState.settings via cache fast path")
	}
}

func TestEnsureRepoReadyTrimsURL(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/ws-1/repos" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: sourceRepo}},
			ReposVersion: "v2",
		})
	})
	if err := d.repoCache.Sync("ws-1", []repocache.RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("seed repo cache: %v", err)
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "v1", []RepoData{{URL: sourceRepo}}, nil)

	// URL with trailing whitespace should still resolve to the cached repo.
	if err := d.ensureRepoReady(context.Background(), "ws-1", "  "+sourceRepo+"  "); err != nil {
		t.Fatalf("ensureRepoReady with padded URL: %v", err)
	}
	// Even on cache hit we refresh settings once so toggle flips feel live.
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected 1 refresh call for trimmed URL, got %d", got)
	}
}

func TestEnsureRepoReadyRefreshesOnMiss(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/ws-1/repos" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: sourceRepo}},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	if err := d.ensureRepoReady(context.Background(), "ws-1", sourceRepo); err != nil {
		t.Fatalf("ensureRepoReady: %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected 1 refresh call, got %d", got)
	}
	if d.repoCache.Lookup("ws-1", sourceRepo) == "" {
		t.Fatal("expected repo to be cached after refresh")
	}
}

// A project github_repo URL that the workspace itself does not bind must still
// be allowed for `multica repo checkout` after registerTaskRepos runs. Without
// this, the new project-repos-override-workspace-repos behavior would surface
// repos in the meta-skill that the agent then can't actually clone.
func TestRegisterTaskReposAllowsProjectOnlyURL(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		// If the workspace endpoint is hit it returns an empty list — the
		// project-only URL must NOT depend on this for allowlist membership.
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{},
			ReposVersion: "v1",
		})
	})
	// Workspace has zero workspace-bound repos; the project resource gives us
	// the only repo URL the agent should be able to check out.
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	d.registerTaskRepos("ws-1", "task-project-only", []RepoData{{URL: sourceRepo}})

	// The async clone goroutine in registerTaskRepos may not have finished;
	// poll briefly until the cache is populated so the test isn't racy.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d.repoCache.Lookup("ws-1", sourceRepo) != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if d.repoCache.Lookup("ws-1", sourceRepo) == "" {
		t.Fatalf("expected repo to be cached after registerTaskRepos, but Lookup returned empty")
	}

	if !d.workspaceRepoAllowed("ws-1", sourceRepo) {
		t.Fatal("expected project repo to pass workspaceRepoAllowed")
	}

	if err := d.ensureRepoReady(context.Background(), "ws-1", sourceRepo); err != nil {
		t.Fatalf("ensureRepoReady: %v", err)
	}
	// ensureRepoReady refreshes settings on every call (RFC MUL-2414 §4.8; PR
	// #2847 review by Emacs) so a freshly-flipped GitHub toggle takes effect
	// without waiting for the 30s sync tick. We expect exactly one refresh —
	// the project-only URL still skips re-cloning because the cache is warm.
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected 1 workspace-repos refresh (settings live-refresh on checkout), got %d", got)
	}
}

// Confirms that a workspace refresh wiping allowedRepoURLs does not also wipe
// task-scoped URLs (project repos). Without the separate taskRepoURLs map a
// concurrent refresh would silently revoke project-only URLs and the next
// checkout would fail.
func TestRegisterTaskReposSurvivesWorkspaceRefresh(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)
	d.registerTaskRepos("ws-1", "task-refresh", []RepoData{{URL: sourceRepo}})

	// Wait for the registration to populate the cache.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && d.repoCache.Lookup("ws-1", sourceRepo) == "" {
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := d.refreshWorkspaceRepos(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRepos: %v", err)
	}

	if !d.workspaceRepoAllowed("ws-1", sourceRepo) {
		t.Fatal("project repo URL was wiped by workspace refresh")
	}
}

func TestTaskRepoDefaultRefScopedByTask(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/example/shared"
	d := &Daemon{
		workspaces: map[string]*workspaceState{
			"ws-1": newWorkspaceState("ws-1", nil, "", nil, nil),
		},
	}

	d.registerTaskRepos("ws-1", "task-a", []RepoData{
		{URL: repoURL, Ref: "release/a"},
		{URL: repoURL, Ref: "late-duplicate"},
	})
	d.registerTaskRepos("ws-1", "task-b", []RepoData{{URL: repoURL, Ref: "release/b"}})

	if got := d.taskRepoDefaultRef("ws-1", "task-a", repoURL); got != "release/a" {
		t.Fatalf("task-a default ref = %q, want release/a", got)
	}
	if got := d.taskRepoDefaultRef("ws-1", "task-b", repoURL); got != "release/b" {
		t.Fatalf("task-b default ref = %q, want release/b", got)
	}

	d.clearTaskRepoRefs("ws-1", "task-a")

	if got := d.taskRepoDefaultRef("ws-1", "task-a", repoURL); got != "" {
		t.Fatalf("task-a default ref after cleanup = %q, want empty", got)
	}
	if got := d.taskRepoDefaultRef("ws-1", "task-b", repoURL); got != "release/b" {
		t.Fatalf("task-b default ref after task-a cleanup = %q, want release/b", got)
	}
}

func TestEnsureRepoReadyReturnsNotConfigured(t *testing.T) {
	t.Parallel()

	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{},
			ReposVersion: "v1",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	err := d.ensureRepoReady(context.Background(), "ws-1", "git@example.com:team/api.git")
	if !errors.Is(err, ErrRepoNotConfigured) {
		t.Fatalf("expected ErrRepoNotConfigured, got %v", err)
	}
}

func TestEnsureRepoReadyReportsSyncFailure(t *testing.T) {
	t.Parallel()

	missingRepo := filepath.Join(t.TempDir(), "missing-repo")
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: missingRepo}},
			ReposVersion: "v1",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	err := d.ensureRepoReady(context.Background(), "ws-1", missingRepo)
	if err == nil || !strings.Contains(err.Error(), "repo is configured but not synced:") {
		t.Fatalf("expected sync failure error, got %v", err)
	}
	if got := d.workspaceLastRepoSyncErr("ws-1"); got == "" {
		t.Fatal("expected lastRepoSyncErr to be recorded")
	}
}

func TestEnsureRepoReadyConcurrentMissRefreshesOnce(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/ws-1/repos" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: sourceRepo}},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	const concurrency = 8
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- d.ensureRepoReady(context.Background(), "ws-1", sourceRepo)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("ensureRepoReady returned error: %v", err)
		}
	}
	// All 8 goroutines race on a cold miss; the per-workspace mutex
	// must serialize them so the server is only called once.
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 refresh call, got %d", got)
	}
}

func TestContextLockCancelsWaitWithoutConsumingToken(t *testing.T) {
	t.Parallel()

	var lock contextLock
	if err := lock.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := lock.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Lock error = %v, want deadline exceeded", err)
	}
	lock.Unlock()

	if err := lock.Lock(context.Background()); err != nil {
		t.Fatalf("Lock after cancelled waiter: %v", err)
	}
	lock.Unlock()
}

func TestShellArgsFromEnv(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_ARGS", `--max-turns 60 --append-system-prompt "multi word"`)
	got, err := shellArgsFromEnv("MULTICA_CLAUDE_ARGS")
	if err != nil {
		t.Fatalf("shellArgsFromEnv: %v", err)
	}
	want := []string{"--max-turns", "60", "--append-system-prompt", "multi word"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestShellArgsFromEnvEmptyIsNil(t *testing.T) {
	t.Setenv("MULTICA_CODEX_ARGS", "   ")
	got, err := shellArgsFromEnv("MULTICA_CODEX_ARGS")
	if err != nil {
		t.Fatalf("shellArgsFromEnv: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty env, got %#v", got)
	}
}

func TestDefaultArgsForProvider(t *testing.T) {
	cfg := Config{ClaudeArgs: []string{"--max-turns", "60"}, CodexArgs: []string{"--sandbox", "workspace-write"}}
	if got := defaultArgsForProvider(cfg, "claude"); strings.Join(got, " ") != "--max-turns 60" {
		t.Fatalf("unexpected claude args: %#v", got)
	}
	if got := defaultArgsForProvider(cfg, "codex"); strings.Join(got, " ") != "--sandbox workspace-write" {
		t.Fatalf("unexpected codex args: %#v", got)
	}
	if got := defaultArgsForProvider(cfg, "unsupported"); got != nil {
		t.Fatalf("expected nil for unsupported provider, got %#v", got)
	}
}

// reportTaskResultRecorder captures which terminal endpoint
// (.../complete or .../fail) reportTaskResult hits and the body it
// posts, so the tests can assert the disposition (success vs fail)
// independently of the rest of handleTask.
type reportTaskResultRecorder struct {
	mu      sync.Mutex
	path    string
	method  string
	payload map[string]any
}

func TestTerminalTaskReportTimeoutCoversRetrySchedule(t *testing.T) {
	client := NewClient("http://example.invalid")
	worstCase := time.Duration(len(defaultTerminalRetrySchedule)+1) * client.client.Timeout
	for _, delay := range defaultTerminalRetrySchedule {
		worstCase += delay
	}
	if terminalTaskReportTimeout < worstCase {
		t.Fatalf("terminal report timeout = %s, want at least retry worst case %s", terminalTaskReportTimeout, worstCase)
	}
}

func (r *reportTaskResultRecorder) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var payload map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		r.mu.Lock()
		r.path = req.URL.Path
		r.method = req.Method
		r.payload = payload
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func TestReportTaskResult_CompletedHitsCompleteEndpoint(t *testing.T) {
	t.Parallel()

	rec := &reportTaskResultRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-1", TaskResult{
		Status:     "completed",
		Comment:    "all good",
		BranchName: "agent/foo",
		SessionID:  "ses-1",
		WorkDir:    "/tmp/foo",
	}, slog.Default())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.path != "/api/daemon/tasks/task-1/complete" {
		t.Fatalf("expected /complete endpoint, got %s", rec.path)
	}
	if rec.payload["output"] != "all good" {
		t.Errorf("output: got %v", rec.payload["output"])
	}
	if rec.payload["branch_name"] != "agent/foo" {
		t.Errorf("branch_name: got %v", rec.payload["branch_name"])
	}
	if rec.payload["session_id"] != "ses-1" {
		t.Errorf("session_id: got %v", rec.payload["session_id"])
	}
}

func TestReportTaskResult_CancelledParentStillReportsTerminalState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		result     TaskResult
		wantSuffix string
	}{
		{
			name: "complete",
			result: TaskResult{
				Status:     "completed",
				Comment:    "all good",
				BranchName: "agent/foo",
				SessionID:  "ses-complete",
				WorkDir:    "/tmp/complete",
			},
			wantSuffix: "/complete",
		},
		{
			name: "fail",
			result: TaskResult{
				Status:        "blocked",
				Comment:       "provider unavailable",
				SessionID:     "ses-fail",
				WorkDir:       "/tmp/fail",
				FailureReason: "agent_error.provider_unavailable",
			},
			wantSuffix: "/fail",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if !strings.HasSuffix(req.URL.Path, tc.wantSuffix) {
					t.Errorf("terminal callback path = %q, want suffix %q", req.URL.Path, tc.wantSuffix)
				}
				calls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
			d.reportTaskResult(ctx, "task-cancelled-parent", tc.result, slog.Default())

			if got := calls.Load(); got != 1 {
				t.Fatalf("terminal callback calls = %d, want 1", got)
			}
		})
	}
}

// Pins the GitHub multica#1952 fail-closed behaviour: a task whose
// agent run never produced a real result (blocked, cancelled, or any
// future status we forget to enumerate) MUST go through FailTask, so
// the UI never shows a green "Completed" badge for a run that didn't
// actually do anything (e.g. provider 429 / out-of-credit).
func TestReportTaskResult_NonCompletedHitsFailEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		status            string
		comment           string
		failureReasonIn   string
		wantFailureReason string
	}{
		{
			name:              "blocked with explicit reason preserves it",
			status:            "blocked",
			comment:           "rate limit reached",
			failureReasonIn:   "iteration_limit",
			wantFailureReason: "iteration_limit",
		},
		{
			// MUL-2946: when the daemon doesn't supply a refined
			// reason, the comment text is run through
			// taskfailure.Classify so the failure_reason column
			// lands in the canonical refined taxonomy instead of
			// the legacy "agent_error" coarse bucket.
			name:              "blocked without reason classifies comment as rate-limit",
			status:            "blocked",
			comment:           "rate limit reached",
			failureReasonIn:   "",
			wantFailureReason: "agent_error.provider_capacity_or_rate_limit",
		},
		{
			name:              "blocked without reason and unrecognized comment lands in agent_error.unknown",
			status:            "blocked",
			comment:           "the agent gave up for reasons we don't recognize",
			failureReasonIn:   "",
			wantFailureReason: "agent_error.unknown",
		},
		{
			name:              "cancelled defaults to cancelled reason regardless of comment",
			status:            "cancelled",
			comment:           "rate limit reached",
			failureReasonIn:   "",
			wantFailureReason: "cancelled",
		},
		{
			name:              "unknown status routes through classifier",
			status:            "weird_new_status",
			comment:           "rate limit reached",
			failureReasonIn:   "",
			wantFailureReason: "agent_error.provider_capacity_or_rate_limit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &reportTaskResultRecorder{}
			srv := httptest.NewServer(rec.handler(t))
			t.Cleanup(srv.Close)

			d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
			d.reportTaskResult(context.Background(), "task-x", TaskResult{
				Status:        tc.status,
				Comment:       tc.comment,
				SessionID:     "ses-x",
				WorkDir:       "/tmp/x",
				FailureReason: tc.failureReasonIn,
			}, slog.Default())

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.path != "/api/daemon/tasks/task-x/fail" {
				t.Fatalf("expected /fail endpoint for status=%q, got %s", tc.status, rec.path)
			}
			if rec.payload["error"] != tc.comment {
				t.Errorf("error body: got %v", rec.payload["error"])
			}
			if got := rec.payload["failure_reason"]; got != tc.wantFailureReason {
				t.Errorf("failure_reason: got %v, want %q", got, tc.wantFailureReason)
			}
			if rec.payload["session_id"] != "ses-x" {
				t.Errorf("session_id should be forwarded on failure paths so chat resume keeps working, got %v", rec.payload["session_id"])
			}
		})
	}
}

// Regression test for the MUL-2780 incident: a short 502 burst on the
// /complete callback used to (a) drop the task at the first failure and
// (b) wrongly fall back to /fail, surfacing a successful run as red.
// With the retry helper in place, a transient 502 followed by a 200 must
// resolve via /complete without ever touching /fail.
func TestReportTaskResult_RetriesTransientCompleteThenSucceeds(t *testing.T) {
	defer noSleepRetry(t)()

	var completeCalls, failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/complete"):
			n := completeCalls.Add(1)
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-retry", TaskResult{
		Status:  "completed",
		Comment: "ok",
	}, slog.Default())

	if got := completeCalls.Load(); got != 2 {
		t.Fatalf("expected 2 complete attempts (one 502, one 200), got %d", got)
	}
	if got := failCalls.Load(); got != 0 {
		t.Fatalf("transient 502 must not fall back to /fail (would lose successful result), got %d /fail calls", got)
	}
}

// Pins the new "don't downgrade success to failure on transient errors"
// rule: when /complete is 502 across the entire retry schedule, we must
// NOT fall through to /fail — that would surface a real success as a
// failure in the UI. The task is left in running for a future recovery
// path to pick up.
func TestReportTaskResult_TransientCompleteExhaustedDoesNotFallback(t *testing.T) {
	defer noSleepRetry(t)()

	prevSchedule := defaultTerminalRetrySchedule
	defaultTerminalRetrySchedule = []time.Duration{time.Nanosecond, time.Nanosecond}
	t.Cleanup(func() { defaultTerminalRetrySchedule = prevSchedule })

	var completeCalls, failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-stuck", TaskResult{
		Status:  "completed",
		Comment: "ok",
	}, slog.Default())

	if got := completeCalls.Load(); got != int32(len(defaultTerminalRetrySchedule)+1) {
		t.Fatalf("expected %d complete attempts, got %d", len(defaultTerminalRetrySchedule)+1, got)
	}
	if got := failCalls.Load(); got != 0 {
		t.Fatalf("exhausted transient retries must NOT fall back to /fail; got %d /fail calls", got)
	}
}

// On permanent 4xx from /complete (e.g. 400 bad body, 404 task not found)
// the helper bails immediately and the daemon falls back to /fail so the
// UI shows a concrete failure rather than a perpetually-running task.
func TestReportTaskResult_PermanentCompleteFallsBackToFail(t *testing.T) {
	defer noSleepRetry(t)()

	var completeCalls, failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-bad", TaskResult{
		Status:  "completed",
		Comment: "ok",
	}, slog.Default())

	if got := completeCalls.Load(); got != 1 {
		t.Fatalf("permanent 400 should not retry, got %d complete attempts", got)
	}
	if got := failCalls.Load(); got != 1 {
		t.Fatalf("permanent /complete should fall back to /fail exactly once, got %d", got)
	}
}

func TestReportTaskResult_CancelledParentStillRunsPermanentFailureFallback(t *testing.T) {
	defer noSleepRetry(t)()

	var completeCalls, failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(ctx, "task-cancelled-fallback", TaskResult{
		Status:  "completed",
		Comment: "ok",
	}, slog.Default())

	if got := completeCalls.Load(); got != 1 {
		t.Fatalf("complete calls = %d, want 1", got)
	}
	if got := failCalls.Load(); got != 1 {
		t.Fatalf("fallback fail calls = %d, want 1", got)
	}
}

func TestHandleTask_BareErrorReportsFailureWithCancelledParent(t *testing.T) {
	t.Parallel()

	var failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/fail") {
			t.Errorf("unexpected daemon call: %s %s", req.Method, req.URL.Path)
		}
		failCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "codex"}},
		cancelPollInterval: time.Hour,
	}
	d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		if !errors.Is(runCtx.Err(), context.Canceled) {
			t.Errorf("runner context error = %v, want context.Canceled", runCtx.Err())
		}
		return TaskResult{}, errors.New("runner exited during shutdown")
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.handleTask(ctx, Task{ID: "task-bare-error", RuntimeID: "rt-1"}, 0)

	if got := failCalls.Load(); got != 1 {
		t.Fatalf("fail callback calls = %d, want 1", got)
	}
}

// TestHandleTask_UntrackedRuntimeFailsBackForRetry covers the window between a
// batch claim leaving with a runtime ID and the claimed task arriving here.
//
// A below-minimum demotion or a drift convergence to zero drops runtime rows
// without coordinating with in-flight claims — deliberately, since gating claims
// on that is the machinery this daemon does not have. So the task can arrive for
// a runtime the daemon no longer holds, and the unchecked map read used to hand
// it a zero-value Runtime: an empty provider that died hundreds of lines later
// as `no agent configured for provider ""`, a reason the server does not retry.
func TestHandleTask_UntrackedRuntimeFailsBackForRetry(t *testing.T) {
	t.Parallel()

	var failBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/fail") {
			t.Errorf("unexpected daemon call: %s %s", req.Method, req.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		failBody.Store(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtimeIndex:       map[string]Runtime{},
		cancelPollInterval: time.Hour,
	}
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		t.Error("runner ran for a runtime the daemon no longer hosts")
		return TaskResult{}, nil
	})

	d.handleTask(context.Background(), Task{ID: "task-gone-runtime", RuntimeID: "rt-demoted"}, 0)

	body, _ := failBody.Load().(map[string]any)
	if body == nil {
		t.Fatal("task was never reported as failed; the server would wait out its dispatch timeout")
	}
	if got := body["failure_reason"]; got != "runtime_offline" {
		t.Errorf("failure_reason = %v, want runtime_offline — the reason the server auto-retries on", got)
	}
}

// TestHandleTask_ReportsUsageBeforeCancel verifies that ReportTaskUsage is called
// even when the server marks the task as cancelled during the post-run status
// check. Regression test for the ordering bug where the cancel check ran before
// usage was reported, silently discarding accumulated tokens.
func TestHandleTask_ReportsUsageBeforeCancel(t *testing.T) {
	t.Parallel()

	var callOrder []string
	var mu sync.Mutex
	recordCall := func(name string) {
		mu.Lock()
		callOrder = append(callOrder, name)
		mu.Unlock()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			recordCall("start")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/progress"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/usage"):
			recordCall("usage")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			recordCall("status")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"cancelled"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: time.Hour, // effectively disable poll-cancel path; we want the post-run status check
	}

	// Inject a fake runner that returns a result with usage tokens, bypassing
	// real agent process execution.
	d.runner = taskRunnerFunc(func(_ context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		return TaskResult{
			Status: "completed",
			Usage: []TaskUsageEntry{
				{Provider: "anthropic", Model: "claude-opus-4-6", InputTokens: 100, OutputTokens: 50},
			},
		}, nil
	})

	task := Task{
		ID:        "task-abc",
		RuntimeID: "rt-1",
		IssueID:   "issue-xyz",
		Agent:     &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	// usage must appear before status in the call order.
	usageIdx, statusIdx := -1, -1
	for i, name := range order {
		switch name {
		case "usage":
			usageIdx = i
		case "status":
			statusIdx = i
		}
	}

	if usageIdx == -1 {
		t.Fatal("ReportTaskUsage was never called — usage is lost for cancelled tasks")
	}
	if statusIdx == -1 {
		t.Fatal("GetTaskStatus was never called")
	}
	if usageIdx > statusIdx {
		t.Fatalf("usage was reported AFTER status check (order: %v) — regression", order)
	}
}

// TestHandleTask_ReportsUsageWhenCancelledByPoll verifies that ReportTaskUsage is
// called even when the task is cancelled mid-execution by the poll goroutine.
// Regression test for the cancelledByPoll early-return path that previously
// discarded accumulated usage before calling ReportTaskUsage.
func TestHandleTask_ReportsUsageWhenCancelledByPoll(t *testing.T) {
	t.Parallel()

	var callOrder []string
	var mu sync.Mutex
	recordCall := func(name string) {
		mu.Lock()
		callOrder = append(callOrder, name)
		mu.Unlock()
	}

	// statusCallCount lets the poll goroutine return "cancelled" on first call
	// while still handling later calls from the post-run status check.
	var statusCallCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/progress"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/usage"):
			recordCall("usage")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			// First call is from the poll goroutine — return "cancelled" to
			// trigger runCancel() and close(cancelledByPoll).
			if statusCallCount.Add(1) == 1 {
				recordCall("poll-status")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"cancelled"}`))
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: 10 * time.Millisecond, // fire quickly so test is fast
	}

	// Inject a runner that blocks until runCtx is cancelled (simulating a real
	// agent being interrupted), then returns usage tokens as claude.go does.
	d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		<-runCtx.Done()
		return TaskResult{
			Status: "aborted",
			Usage: []TaskUsageEntry{
				{Provider: "anthropic", Model: "claude-opus-4-6", InputTokens: 200, OutputTokens: 80},
			},
		}, nil
	})

	task := Task{
		ID:        "task-poll",
		RuntimeID: "rt-1",
		IssueID:   "issue-poll",
		Agent:     &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	// Verify the poll goroutine actually fired — without this assertion the test
	// could pass via the post-run GetTaskStatus check without ever taking the
	// cancelledByPoll path, making it a vacuous regression guard.
	pollStatusIdx := -1
	usageIdx := -1
	for i, name := range order {
		switch name {
		case "poll-status":
			pollStatusIdx = i
		case "usage":
			usageIdx = i
		}
	}
	if pollStatusIdx == -1 {
		t.Fatalf("poll goroutine never fired (order: %v) — cancelledByPoll path not exercised", order)
	}
	if usageIdx == -1 {
		t.Fatalf("ReportTaskUsage was never called on poll-cancelled path (order: %v) — tokens lost", order)
	}
	// poll-status must precede usage: poll fires → runCtx cancelled → runner unblocks → usage flushed.
	// If usage comes first, usage was reported before the runner was interrupted, which is impossible
	// given that the runner blocks on runCtx.Done().
	if usageIdx < pollStatusIdx {
		t.Fatalf("usage reported before poll-status (order: %v) — poll-status must come first", order)
	}
}

// TestWatchTaskCancellation_ReconcileBroadcastTriggersImmediateCheck pins the
// fix for #4665. The 5s ticker in watchTaskCancellation is too coarse to catch
// a server-side cancellation that landed during a WS disconnect: without the
// reconcile broadcast, a reconnect followed by an immediate status flip is
// invisible until the next tick fires. The test sets the ticker to a long
// interval (so a tick would fail the test) and asserts the watcher reacts to
// reconcile.broadcast() sub-second.
func TestWatchTaskCancellation_ReconcileBroadcastTriggersImmediateCheck(t *testing.T) {
	t.Parallel()

	var status atomic.Value
	status.Store("running")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"` + status.Load().(string) + `"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:    NewClient(srv.URL),
		logger:    slog.Default(),
		reconcile: newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0 // tight timing under test
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// 30s ticker: if the watcher only reacts to its own ticker the test will
	// time out at 2s, which is exactly the failure we want to catch.
	cancelled := d.watchTaskCancellation(ctx, "task-reconcile", 30*time.Second, slog.Default())

	// Simulate the gap: status flips during the WS disconnect.
	status.Store("cancelled")

	// And then the WS reconnects — broadcast must be heard.
	if !d.reconcile.broadcast() {
		t.Fatal("broadcast() returned false; expected first broadcast to fire")
	}

	select {
	case <-cancelled:
		// Expected: reconcile woke the watcher, it called GetTaskStatus,
		// saw cancelled, and closed the channel.
	case <-time.After(2 * time.Second):
		t.Fatalf("watchTaskCancellation did not react to reconcile broadcast within 2s (calls=%d)", calls.Load())
	}

	if calls.Load() == 0 {
		t.Fatal("GetTaskStatus was never called — reconcile path did not fire")
	}
}

// TestWatchTaskCancellation_ReconcileWithRunningTaskStaysAlive ensures the
// reconcile path does not falsely interrupt a still-running task. The watcher
// must call GetTaskStatus on broadcast, see status=running, and continue.
func TestWatchTaskCancellation_ReconcileWithRunningTaskStaysAlive(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:    NewClient(srv.URL),
		logger:    slog.Default(),
		reconcile: newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-still-running", 30*time.Second, slog.Default())

	// Three broadcasts back-to-back: watcher should call GetTaskStatus at
	// least once and NOT close the cancelled channel.
	for i := 0; i < 3; i++ {
		d.reconcile.broadcast()
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case <-cancelled:
		t.Fatal("watchTaskCancellation closed cancelled channel for a running task")
	case <-time.After(200 * time.Millisecond):
		// Expected: still running.
	}

	if calls.Load() == 0 {
		t.Fatal("reconcile broadcasts did not result in any GetTaskStatus call")
	}
}

// TestWorkspaceSyncLoop_ReconcileBroadcastTriggersImmediateSync pins that the
// long-period workspace consistency timer is short-circuited by reconnect
// broadcasts. Without this, changes made during a WS disconnect stay invisible
// until the next fallback sync.
func TestWorkspaceSyncLoop_ReconcileBroadcastTriggersImmediateSync(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/workspaces" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:     NewClient(srv.URL),
		logger:     slog.Default(),
		workspaces: make(map[string]*workspaceState),
		reconcile:  newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		d.workspaceSyncLoop(ctx)
	}()

	// Two broadcasts spaced apart. workspaceSyncLoop should re-acquire its
	// subscription after the first wake and react to the second too.
	d.reconcile.broadcast()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() < 1 {
		cancel()
		<-loopDone
		t.Fatal("workspaceSyncLoop did not react to first reconcile broadcast within 2s")
	}

	d.reconcile.broadcast()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() < 2 {
		cancel()
		<-loopDone
		t.Fatalf("workspaceSyncLoop did not react to second reconcile broadcast within 2s (calls=%d)", calls.Load())
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(time.Second):
		t.Fatal("workspaceSyncLoop did not return after ctx cancel")
	}
}

func TestWorkspaceSyncLoop_WorkspaceChangeTriggersImmediateSync(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/workspaces" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:           NewClient(srv.URL),
		logger:           slog.Default(),
		workspaces:       make(map[string]*workspaceState),
		workspaceChanges: newWorkspaceChangeSignal(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		d.workspaceSyncLoop(ctx)
	}()

	d.workspaceChanges.broadcast()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 1 {
		cancel()
		<-loopDone
		t.Fatal("workspace change did not trigger an immediate sync")
	}

	cancel()
	<-loopDone
}

// TestWorkspaceSyncLoop_DoesNotDropChangeAfterSuccessfulSync covers the
// request-changes race from MUL-4480: a real membership hint arriving within
// one second of a completed sync must trigger another sync, not be treated as
// a duplicate of the earlier read and deferred to the 30-minute fallback.
func TestWorkspaceSyncLoop_DoesNotDropChangeAfterSuccessfulSync(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/workspaces" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:           NewClient(srv.URL),
		logger:           slog.Default(),
		workspaces:       make(map[string]*workspaceState),
		workspaceChanges: newWorkspaceChangeSignal(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		d.workspaceSyncLoop(ctx)
	}()

	waitForCalls := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && calls.Load() < want {
			time.Sleep(10 * time.Millisecond)
		}
		if got := calls.Load(); got < want {
			t.Fatalf("workspace sync calls = %d, want at least %d", got, want)
		}
	}

	d.workspaceChanges.broadcast()
	waitForCalls(1)
	deadline := time.Now().Add(time.Second)
	for !d.reloading.TryLock() {
		if time.Now().After(deadline) {
			t.Fatal("first workspace sync did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	d.reloading.Unlock()
	// The second edge lands immediately after a successful API read. It
	// represents a later commit and therefore cannot be coalesced backward.
	d.workspaceChanges.broadcast()
	waitForCalls(2)

	cancel()
	<-loopDone
}

func TestWorkspaceSyncBackoff(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: 30 * time.Second},
		{failures: 1, want: time.Minute},
		{failures: 2, want: 2 * time.Minute},
		{failures: 10, want: DefaultWorkspaceLegacySyncInterval},
	}
	for _, tt := range tests {
		if got := workspaceSyncBackoff(30*time.Second, tt.failures); got != tt.want {
			t.Fatalf("workspaceSyncBackoff(30s, %d) = %s, want %s", tt.failures, got, tt.want)
		}
	}
}

// TestWorkspaceSyncLoop_ReplaysBroadcastFromBeforeStart pins the daemon-
// startup race fix: broadcast() that fired while no one was subscribed
// MUST still wake the loop on its first subscription. Without the
// reconcile broadcaster's replay slot, this race manifests in production
// when the WS connects (and broadcasts) before workspaceSyncLoop has
// finished its first notify() call.
func TestWorkspaceSyncLoop_ReplaysBroadcastFromBeforeStart(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/workspaces" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:     NewClient(srv.URL),
		logger:     slog.Default(),
		workspaces: make(map[string]*workspaceState),
		reconcile:  newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0

	// Broadcast BEFORE the loop subscribes — the level-triggered replay slot
	// must hold the event for the loop's first notify().
	if !d.reconcile.broadcast() {
		t.Fatal("seed broadcast suppressed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		d.workspaceSyncLoop(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 1 {
		cancel()
		<-loopDone
		t.Fatal("workspaceSyncLoop did not replay broadcast issued before its start")
	}

	cancel()
	<-loopDone
}

// TestWatchTaskCancellation_BroadcastWakesAllConcurrentWatchers ensures the
// fix scales: a single WS reconnect that fires broadcast() must drive every
// in-flight task's watcher to re-check the server. Without fan-out, a busy
// daemon with N tasks would still hit a wall of sequential 5s gaps.
func TestWatchTaskCancellation_BroadcastWakesAllConcurrentWatchers(t *testing.T) {
	t.Parallel()

	var status atomic.Value
	status.Store("running")
	var totalCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		totalCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"` + status.Load().(string) + `"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:    NewClient(srv.URL),
		logger:    slog.Default(),
		reconcile: newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const watchers = 8
	chans := make([]<-chan struct{}, watchers)
	for i := 0; i < watchers; i++ {
		// 30s ticker: only the broadcast path can satisfy the assertion.
		chans[i] = d.watchTaskCancellation(ctx, fmt.Sprintf("task-%d", i), 30*time.Second, slog.Default())
	}

	// Server state flips during the WS gap; broadcast lands the news to
	// every watcher at once.
	status.Store("cancelled")
	if !d.reconcile.broadcast() {
		t.Fatal("broadcast suppressed")
	}

	deadline := time.After(3 * time.Second)
	for i, ch := range chans {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("watcher %d did not react to broadcast (totalCalls=%d, woke=%d)", i, totalCalls.Load(), i)
		}
	}
}

// TestWatchTaskCancellation_ReconcileDoesNotPanicAfterCtxCancel ensures
// teardown order is safe: even if a broadcast arrives after ctx is cancelled,
// the watcher must exit cleanly without panic or double-close of cancelled.
func TestWatchTaskCancellation_ReconcileDoesNotPanicAfterCtxCancel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:    NewClient(srv.URL),
		logger:    slog.Default(),
		reconcile: newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	cancelled := d.watchTaskCancellation(ctx, "task-ctx-cancelled", 30*time.Second, slog.Default())

	cancel()
	// Give the watcher a beat to observe ctx.Done() and exit.
	time.Sleep(50 * time.Millisecond)

	// Broadcast after cancel — must not panic and must not unblock the
	// cancelled channel (the task was not interrupted server-side).
	for i := 0; i < 5; i++ {
		d.reconcile.broadcast()
	}

	select {
	case <-cancelled:
		t.Fatal("cancelled channel was closed after ctx cancel; server still reported running")
	case <-time.After(150 * time.Millisecond):
		// Expected: nothing happened.
	}
}

// TestWatchTaskCancellation_ReconcileRunningKeepsTickerAlive proves that a
// reconcile broadcast that sees status=running does not disturb the ticker;
// a subsequent ticker fire still detects a later cancellation.
func TestWatchTaskCancellation_ReconcileRunningKeepsTickerAlive(t *testing.T) {
	t.Parallel()

	var status atomic.Value
	status.Store("running")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"` + status.Load().(string) + `"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:    NewClient(srv.URL),
		logger:    slog.Default(),
		reconcile: newReconcileBroadcaster(),
	}
	d.reconcile.minBroadcastInterval = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Short ticker so the test stays fast.
	cancelled := d.watchTaskCancellation(ctx, "task-runs-then-cancelled", 50*time.Millisecond, slog.Default())

	// First broadcast: server still reports running — watcher must NOT
	// close cancelled.
	if !d.reconcile.broadcast() {
		t.Fatal("first broadcast suppressed")
	}
	select {
	case <-cancelled:
		t.Fatal("watcher closed cancelled on a running task")
	case <-time.After(100 * time.Millisecond):
	}

	// Now flip status — ticker should pick it up within ~50ms.
	status.Store("cancelled")
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatalf("ticker did not detect cancellation after broadcast-running path (calls=%d)", calls.Load())
	}
}

// TestSanitizeAgentEnv asserts the effective env handed to the Hermes overlay
// for ${VAR} expansion drops daemon-blocklisted keys (so a blocked HOME in
// custom_env can't repoint external_dirs away from what the child sees) while
// keeping ordinary vars.
func TestSanitizeAgentEnv(t *testing.T) {
	t.Parallel()
	in := map[string]string{
		"HOME":        "/evil",
		"PATH":        "/evil/bin",
		"MULTICA_X":   "1",
		"TEAM_SKILLS": "/srv/team",
		"HERMES_HOME": "/some/home",
	}
	got := sanitizeAgentEnv(in)
	for _, blocked := range []string{"HOME", "PATH", "MULTICA_X"} {
		if _, ok := got[blocked]; ok {
			t.Errorf("blocklisted key %q must be dropped from the effective env", blocked)
		}
	}
	if got["TEAM_SKILLS"] != "/srv/team" {
		t.Errorf("ordinary var dropped: %v", got)
	}
	// HERMES_HOME is not blocklisted (it drives source-home resolution), so it
	// survives here even though the child's value is the overlay.
	if got["HERMES_HOME"] != "/some/home" {
		t.Errorf("HERMES_HOME should survive sanitization, got %v", got)
	}
	if sanitizeAgentEnv(nil) != nil {
		t.Error("nil in should yield nil out")
	}
}

// TestHermesLaunchArgsAndEnvByScenario covers the profile chain end to end at
// the decision level: the final launch args + the final HERMES_HOME the child
// sees, for a skill-less vs. an overlay-active task that both set a profile.
func TestHermesLaunchArgsAndEnvByScenario(t *testing.T) {
	t.Parallel()
	customArgs := []string{"-p", "research", "--yolo"}
	customEnv := map[string]string{"HERMES_HOME": "/home/u/.hermes"}

	// No overlay (skill-less): profile flag passes through, and the user's
	// HERMES_HOME passes through — behavior unchanged.
	noOverlayArgs := hermesLaunchArgs(customArgs, false)
	if len(noOverlayArgs) != 3 || noOverlayArgs[0] != "-p" || noOverlayArgs[1] != "research" {
		t.Errorf("skill-less task must keep its profile flags, got %v", noOverlayArgs)
	}
	noOverlayEnv := map[string]string{}
	layerCustomEnvAndHermesHome(noOverlayEnv, customEnv, "", nil)
	if noOverlayEnv["HERMES_HOME"] != "/home/u/.hermes" {
		t.Errorf("skill-less task must keep the user HERMES_HOME, got %q", noOverlayEnv["HERMES_HOME"])
	}

	// Overlay active: profile flag is stripped, and HERMES_HOME is the overlay.
	overlayArgs := hermesLaunchArgs(customArgs, true)
	if len(overlayArgs) != 1 || overlayArgs[0] != "--yolo" {
		t.Errorf("overlay task must strip profile flags, got %v", overlayArgs)
	}
	overlayEnv := map[string]string{}
	layerCustomEnvAndHermesHome(overlayEnv, customEnv, "/task/hermes-home", nil)
	if overlayEnv["HERMES_HOME"] != "/task/hermes-home" {
		t.Errorf("overlay task must redirect HERMES_HOME to the overlay, got %q", overlayEnv["HERMES_HOME"])
	}
}

// TestHandleTask_AcksCancelAfterPollCancelled verifies the daemon posts
// cancel-ack when the poll goroutine interrupts the run — by then
// runner.run has returned, so the transcript flush is complete and the
// server may settle its deferred chat finalization (#5219).
func TestHandleTask_AcksCancelAfterPollCancelled(t *testing.T) {
	t.Parallel()

	var callOrder []string
	var mu sync.Mutex
	recordCall := func(name string) {
		mu.Lock()
		callOrder = append(callOrder, name)
		mu.Unlock()
	}

	var statusCallCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			recordCall("cancel-ack")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			if statusCallCount.Add(1) == 1 {
				recordCall("poll-status")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"cancelled"}`))
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: 10 * time.Millisecond,
	}

	d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		<-runCtx.Done()
		return TaskResult{Status: "aborted"}, nil
	})

	task := Task{
		ID:        "task-ack-poll",
		RuntimeID: "rt-1",
		IssueID:   "issue-ack-poll",
		Agent:     &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	pollStatusIdx, ackIdx := -1, -1
	for i, name := range order {
		switch name {
		case "poll-status":
			pollStatusIdx = i
		case "cancel-ack":
			ackIdx = i
		}
	}
	if pollStatusIdx == -1 {
		t.Fatalf("poll goroutine never fired (order: %v)", order)
	}
	if ackIdx == -1 {
		t.Fatalf("cancel-ack was never posted on the poll-cancelled path (order: %v)", order)
	}
	if ackIdx < pollStatusIdx {
		t.Fatalf("cancel-ack before the poll observed the cancellation (order: %v)", order)
	}
}

// TestHandleTask_AcksCancelOnPostRunStatusCheck verifies cancel-ack is also
// posted when the cancellation is only discovered by the pre-completion
// status check (the run finished before the poll noticed).
func TestHandleTask_AcksCancelOnPostRunStatusCheck(t *testing.T) {
	t.Parallel()

	var ackCalls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			ackCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"cancelled"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: time.Hour, // disable the poll path; exercise the post-run check
	}

	d.runner = taskRunnerFunc(func(_ context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "completed"}, nil
	})

	task := Task{
		ID:        "task-ack-postrun",
		RuntimeID: "rt-1",
		IssueID:   "issue-ack-postrun",
		Agent:     &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	if got := ackCalls.Load(); got != 1 {
		t.Fatalf("cancel-ack calls = %d, want 1", got)
	}
}

func TestConvertDisabledRuntimeSkillsForEnvScopesToClaimedRuntime(t *testing.T) {
	t.Parallel()

	agentData := &AgentData{DisabledRuntimeSkills: []DisabledRuntimeSkillData{
		{RuntimeID: "runtime-1", Provider: "claude", Root: "provider", Key: "review", Name: "Review"},
		{RuntimeID: "runtime-2", Provider: "claude", Root: "provider", Key: "other-runtime"},
		{RuntimeID: "runtime-1", Provider: "codex", Root: "provider", Key: "other-provider"},
	}}
	got := convertDisabledRuntimeSkillsForEnv(agentData, "runtime-1", "claude")
	if len(got) != 1 || got[0].Key != "review" || got[0].Name != "Review" {
		t.Fatalf("unexpected scoped runtime skill policy: %+v", got)
	}
}

// nestedPatchSecretBackend emits a Codex-shaped file-edit payload carrying a
// credential nested inside changes[] — the legacy protocol reports a deletion
// as the whole outgoing file, so deleting a .env puts its contents here.
type nestedPatchSecretBackend struct{}

func (nestedPatchSecretBackend) Execute(
	_ context.Context,
	_ string,
	_ agent.ExecOptions,
) (*agent.Session, error) {
	msgCh := make(chan agent.Message, 2)
	resCh := make(chan agent.Result, 1)

	msgCh <- agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "patch_apply",
		CallID: "patch-1",
		Input: map[string]any{
			"changes": []any{
				map[string]any{
					"path":    ".env",
					"kind":    "delete",
					"content": "GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn\n",
				},
			},
		},
	}
	close(msgCh)
	resCh <- agent.Result{Status: "completed", Output: "done"}
	close(resCh)

	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

// TestExecuteAndDrain_RedactsNestedToolInputBeforeSending pins the daemon side
// of the redaction contract. The server scrubs again on ingest, but that is the
// remote end: a daemon that self-updated ahead of the server, or one talking to
// a server mid-rollout, must not put whole-file edit contents on the wire in
// cleartext. Deployment order is not a control we have, so this side has to be
// safe on its own.
func TestExecuteAndDrain_RedactsNestedToolInputBeforeSending(t *testing.T) {
	t.Parallel()

	d, rec := newTranscriptRecorder(t)

	if _, _, err := d.executeAndDrain(
		context.Background(),
		nestedPatchSecretBackend{},
		"p",
		agent.ExecOptions{},
		slog.Default(),
		"task-redact",
		"",
		new(atomic.Int32),
	); err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}

	msgs := rec.snapshot()
	var toolUse *TaskMessageData
	for i := range msgs {
		if msgs[i].Type == "tool_use" {
			toolUse = &msgs[i]
			break
		}
	}
	if toolUse == nil {
		t.Fatalf("no tool_use message reported: %+v", msgs)
	}

	blob, err := json.Marshal(toolUse.Input)
	if err != nil {
		t.Fatalf("marshal reported input: %v", err)
	}
	if strings.Contains(string(blob), "ghp_ABCDEFGH") {
		t.Fatalf("nested token left the daemon unredacted: %s", blob)
	}
	// The surrounding structure must survive, or the transcript loses the edit.
	if !strings.Contains(string(blob), ".env") {
		t.Fatalf("redaction destroyed the change metadata: %s", blob)
	}
}

// TestFreshSessionMayHelp pins the verdict for the Hermes auth-resolution
// failure this PR treats as session poison, so the "fresh session is the
// answer" gate can't be flipped by a future classifier change. The error is
// session-shaped (a fresh session resolves it), yet it must NOT count as one
// of the "fresh session is not the answer" buckets — in particular not
// missing-config — or the in-turn fresh-session retry on the five
// ResumeRejectionUndetectable backends (antigravity, copilot, cursor, deveco,
// opencode) would silently stop firing and the dead session would be resumed
// into the same provider error forever.
func TestFreshSessionMayHelp(t *testing.T) {
	t.Parallel()

	const hermesAuth = "hermes provider error: \"Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted\""
	if !freshSessionMayHelp(hermesAuth) {
		t.Fatalf("freshSessionMayHelp(auth-resolution error) = false; a fresh session cures this failure, it must report session-fixable")
	}
}

// TestBuildPromptSquadLeaderReplyCommandCarvesOutNoAction renders the COMPLETE
// leader prompt and pins the exception's scope relation (MUL-5442 #6493
// review): the no_action rule and the reply imperative appear in the same
// prompt, so the imperative must carry the carve-out itself — a bare
// unconditional "Post your reply as a comment" anywhere in a leader prompt
// re-opens the contradiction the carve-out exists to close.
func TestBuildPromptSquadLeaderReplyCommandCarvesOutNoAction(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "team update posted",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		IsLeaderTask:          true,
		LeaderRoleResolved:    true,
		Agent: &AgentData{
			Name:         "Lead",
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}, "claude")

	if !strings.Contains(prompt, "Squad leader no_action rule") {
		t.Fatalf("leader prompt missing the no_action rule\n---\n%s", prompt)
	}
	if !strings.Contains(prompt, "Unless your outcome is `no_action`, post your reply as a comment") {
		t.Fatalf("leader prompt missing the carve-out reply imperative\n---\n%s", prompt)
	}
	if strings.Contains(prompt, "Post your reply as a comment") {
		t.Fatalf("leader prompt still carries the unconditional reply imperative\n---\n%s", prompt)
	}
}

// TestBuildPromptSquadLeaderMultiThreadCarvesOutNoAction renders the complete
// leader prompt on the cross-thread fan-out path (MUL-5442 #6493 review): the
// fan-out imperative fires AFTER the no_action rule, so its scope sentence
// must govern the ENTIRE block — assert it precedes every later obligation,
// not just the first verb. The ordinary fan-out output keeps the
// unconditional form byte-for-byte.
func TestBuildPromptSquadLeaderMultiThreadCarvesOutNoAction(t *testing.T) {
	t.Parallel()

	leaderTask := Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-9",
		TriggerThreadID:       "thread-B",
		TriggerCommentContent: "second update",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Worker",
		CoalescedComments: []CoalescedCommentData{
			{ID: "comment-8", ThreadID: "thread-A", Content: "first update"},
		},
		IsLeaderTask:       true,
		LeaderRoleResolved: true,
		Agent: &AgentData{
			Name:         "Lead",
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	prompt := BuildPrompt(leaderTask, "claude")
	if !strings.Contains(prompt, "Squad leader no_action rule") {
		t.Fatalf("leader multi-thread prompt missing the no_action rule\n---\n%s", prompt)
	}
	scope := strings.Index(prompt, "skip this ENTIRE fan-out block")
	if scope < 0 {
		t.Fatalf("leader multi-thread prompt missing the whole-block scope sentence\n---\n%s", prompt)
	}
	// Obligation strings track the converged fan-out block (MUL-5825). Pin
	// ledger: "Post the replies in the order listed below" → the order rule
	// merged into the targets header ("OLDEST thread first"); "For EACH
	// thread above" → the embedded cookbook collapsed to the
	// `## Comment Formatting` pointer plus the per-thread file delta
	// ("DISTINCT body file per thread"). The assertion shape is unchanged:
	// every obligation must sit AFTER the no_action scope sentence.
	for _, obligation := range []string{
		"multiple replies are required and correct",
		"OLDEST thread first",
		"DISTINCT body file per thread",
	} {
		idx := strings.Index(prompt, obligation)
		if idx < 0 {
			t.Fatalf("leader multi-thread prompt missing obligation %q\n---\n%s", obligation, prompt)
		}
		if idx < scope {
			t.Fatalf("obligation %q precedes the no_action scope sentence — it escapes the carve-out\n---\n%s", obligation, prompt)
		}
	}
	if strings.Contains(prompt, ". Post ONE reply per thread") {
		t.Fatalf("leader multi-thread prompt still carries the unconditional imperative\n---\n%s", prompt)
	}

	ordinaryTask := leaderTask
	ordinaryTask.IsLeaderTask = false
	ordinaryTask.Agent = &AgentData{Name: "Reg", Instructions: "You are a regular agent."}
	ordinary := BuildPrompt(ordinaryTask, "claude")
	if !strings.Contains(ordinary, ". Post ONE reply per thread") {
		t.Fatalf("ordinary multi-thread prompt missing the unconditional imperative\n---\n%s", ordinary)
	}
	if strings.Contains(ordinary, "Unless your outcome is") || strings.Contains(ordinary, "skip this ENTIRE fan-out block") {
		t.Fatalf("ordinary multi-thread prompt leaked the leader carve-out\n---\n%s", ordinary)
	}
}
