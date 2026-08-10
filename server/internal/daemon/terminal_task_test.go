package daemon

import (
	"testing"

	daemonterminal "github.com/multica-ai/multica/server/internal/daemon/terminal"
)

func TestPTYTaskAllowedRequiresEveryCapabilityGate(t *testing.T) {
	daemon := &Daemon{
		cfg: Config{
			PTYEnabled:            true,
			PTYRuntimeAllowlist:   []string{"codex"},
			PTYWorkspaceAllowlist: []string{"workspace-1"},
		},
		terminalManager: daemonterminal.NewManager(daemonterminal.Options{}),
	}
	daemon.terminalConnected.Store(true)

	if !daemon.ptyTaskAllowed("codex", "workspace-1", false) {
		t.Fatal("fully allowed Codex task did not select PTY")
	}
	for _, test := range []struct {
		name      string
		provider  string
		workspace string
		custom    bool
		mutate    func()
	}{
		{name: "runtime unsupported", provider: "claude", workspace: "workspace-1"},
		{name: "workspace denied", provider: "codex", workspace: "workspace-2"},
		{name: "custom profile", provider: "codex", workspace: "workspace-1", custom: true},
		{name: "feature disabled", provider: "codex", workspace: "workspace-1", mutate: func() { daemon.cfg.PTYEnabled = false }},
		{name: "old server", provider: "codex", workspace: "workspace-1", mutate: func() { daemon.terminalConnected.Store(false) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			originalEnabled := daemon.cfg.PTYEnabled
			originalConnected := daemon.terminalConnected.Load()
			defer func() {
				daemon.cfg.PTYEnabled = originalEnabled
				daemon.terminalConnected.Store(originalConnected)
			}()
			if test.mutate != nil {
				test.mutate()
			}
			if daemon.ptyTaskAllowed(test.provider, test.workspace, test.custom) {
				t.Fatal("task unexpectedly selected PTY")
			}
		})
	}
}

func TestCSVValuesFromEnvUsesSafePTYDefaults(t *testing.T) {
	t.Setenv("MULTICA_PTY_RUNTIME_ALLOWLIST", "")
	t.Setenv("MULTICA_PTY_WORKSPACE_ALLOWLIST", "")
	if got := csvValuesFromEnv("MULTICA_PTY_RUNTIME_ALLOWLIST", []string{"codex"}); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("runtime default = %v", got)
	}
	if got := csvValuesFromEnv("MULTICA_PTY_WORKSPACE_ALLOWLIST", nil); len(got) != 0 {
		t.Fatalf("workspace default = %v", got)
	}

	t.Setenv("MULTICA_PTY_RUNTIME_ALLOWLIST", " codex, claude, codex ")
	if got := csvValuesFromEnv("MULTICA_PTY_RUNTIME_ALLOWLIST", nil); len(got) != 2 || got[1] != "claude" {
		t.Fatalf("parsed runtime allowlist = %v", got)
	}
}
