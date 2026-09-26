package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

// The CLI resolver reaches the daemon's shared workspace root while config,
// logs, PID files, and health ports remain profile-scoped.
func TestWorkStateSharedWhileProfilesStayIsolated(t *testing.T) {
	home := t.TempDir()
	stageTestHome(t, home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	t.Setenv("MULTICA_SERVER_URL", "")

	const desktop = "desktop-example"
	for _, profile := range []string{"", desktop} {
		if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: "https://same.example"}, profile); err != nil {
			t.Fatalf("save profile %q config: %v", profile, err)
		}
	}

	defaultScope, err := daemon.WorkStateScopeForProfile("", "")
	if err != nil {
		t.Fatalf("resolve default profile scope: %v", err)
	}
	// The human disk-usage resolver has to land on the same tree the daemon uses.
	if root, err := resolveWorkspacesRootForProfile(desktop, ""); err != nil || root != defaultScope.WorkspacesRoot {
		t.Fatalf("resolveWorkspacesRootForProfile = %q (%v), want %q", root, err, defaultScope.WorkspacesRoot)
	}

	// Profile-scoped by design, and unchanged by this fix.
	defaultConfigPath, err := cli.CLIConfigPathForProfile("")
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	desktopConfigPath, err := cli.CLIConfigPathForProfile(desktop)
	if err != nil {
		t.Fatalf("desktop config path: %v", err)
	}
	if defaultConfigPath == desktopConfigPath {
		t.Fatalf("auth/config path is shared: %q", defaultConfigPath)
	}
	if got, want := daemonLogPathForProfile(desktop), daemonLogPathForProfile(""); got == want {
		t.Fatalf("log path is shared: %q", got)
	}
	if got, want := daemonPIDPathForProfile(desktop), daemonPIDPathForProfile(""); got == want {
		t.Fatalf("pid path is shared: %q", got)
	}
	if got, want := healthPortForProfile(desktop), healthPortForProfile(""); got == want {
		t.Fatalf("health port is shared: %d", got)
	}
}
