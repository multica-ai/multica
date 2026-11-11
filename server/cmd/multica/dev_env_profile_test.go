package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	devEnvTestName    = "wt-3"
	devEnvTestProfile = "dev-wt-3"
	devEnvTestPort    = "18083"
)

// setupDevEnvTask puts the test inside a daemon-task workdir, with the task's
// identity stripped the way dev-env's CLEAN_ENV strips it, and registers one
// environment whose profile points at its own backend.
func setupDevEnvTask(t *testing.T) {
	t.Helper()
	chdirWithDaemonTaskMarker(t)
	clearDaemonTaskEnv(t)
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_SERVER_URL", "")
	redirectTestHome(t, t.TempDir())
	t.Setenv(devEnvHomeEnv, t.TempDir())

	writeDevEnvManifest(t, devEnvTestName, "NAME="+devEnvTestName+"\nDIR=/tmp/checkout\nBACKEND_PORT="+devEnvTestPort+"\nPROFILE="+devEnvTestProfile+"\n")
	writeDevEnvProfile(t, devEnvTestProfile, "http://localhost:"+devEnvTestPort)
}

func writeDevEnvManifest(t *testing.T, name, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv(devEnvHomeEnv), "envs", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDevEnvProfile(t *testing.T, profile, serverURL string) {
	t.Helper()
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{ServerURL: serverURL, Token: "mul_dev"}, profile); err != nil {
		t.Fatal(err)
	}
}

func devEnvDaemonCmd(t *testing.T, profile string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("output", "", "")
	cmd.Flags().Bool("force", false, "")
	if profile != "" {
		if err := cmd.Flags().Set("profile", profile); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func TestDevEnvDaemonProfileAllowedInsideTask(t *testing.T) {
	for _, profile := range []string{devEnvTestProfile, "desktop-localhost-" + devEnvTestPort} {
		t.Run(profile, func(t *testing.T) {
			setupDevEnvTask(t)
			writeDevEnvProfile(t, profile, "http://localhost:"+devEnvTestPort+"/")

			cmd := devEnvDaemonCmd(t, profile)
			for _, command := range []string{"daemon start", "daemon stop", "daemon probe-runtimes"} {
				if err := requireHumanLocalDaemonCommand(cmd, command); err != nil {
					t.Fatalf("%s --profile %s: %v", command, profile, err)
				}
			}
			port, err := daemonStatusHealthPort(cmd)
			if err != nil {
				t.Fatalf("daemon status --profile %s: %v", profile, err)
			}
			if want := healthPortForProfile(profile); port != want {
				t.Fatalf("status port = %d, want the profile's own %d", port, want)
			}

			// The real command, end to end, against a daemon this test owns.
			// The daemon.port record steers stop off the hashed port, which
			// another profile's daemon on this machine may already hold.
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			pid := startBlockingChild(t)
			stub := serveStubDaemon(t, ln, map[string]any{"profile": profile}, pid)
			if err := publishDaemonPort(profile, daemonPortRecord{Port: stub.port, PID: pid}); err != nil {
				t.Fatal(err)
			}
			if err := runDaemonStop(cmd, nil); err != nil {
				t.Fatalf("runDaemonStop --profile %s: %v", profile, err)
			}
			if got := stub.shutdownCount(); got != 1 {
				t.Fatalf("stub daemon received %d shutdown requests, want 1", got)
			}
		})
	}
}

func TestDevEnvDaemonProfileExemptionStaysNarrow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		setup   func(t *testing.T)
	}{
		{name: "default profile", profile: ""},
		{
			name:    "profile the registry does not assign",
			profile: "staging",
			setup: func(t *testing.T) {
				writeDevEnvProfile(t, "staging", "http://localhost:"+devEnvTestPort)
			},
		},
		{
			name:    "desktop profile of another port",
			profile: "desktop-localhost-18084",
			setup: func(t *testing.T) {
				writeDevEnvProfile(t, "desktop-localhost-18084", "http://localhost:"+devEnvTestPort)
			},
		},
		{
			name:    "profile pointed at another server",
			profile: devEnvTestProfile,
			setup: func(t *testing.T) {
				writeDevEnvProfile(t, devEnvTestProfile, "http://localhost:18084")
			},
		},
		{
			name:    "server URL overridden by the task environment",
			profile: devEnvTestProfile,
			setup: func(t *testing.T) {
				t.Setenv("MULTICA_SERVER_URL", "https://api.multica.ai")
			},
		},
		{
			name:    "task ID still in the environment",
			profile: devEnvTestProfile,
			setup:   func(t *testing.T) { t.Setenv("MULTICA_TASK_ID", "task-1") },
		},
		{
			name:    "task token still in the environment",
			profile: devEnvTestProfile,
			setup:   func(t *testing.T) { t.Setenv("MULTICA_TOKEN", "mat_task") },
		},
		{
			name:    "task daemon port still in the environment",
			profile: devEnvTestProfile,
			setup:   func(t *testing.T) { t.Setenv("MULTICA_DAEMON_PORT", "19514") },
		},
		{
			name:    "task config root still in the environment",
			profile: devEnvTestProfile,
			setup:   func(t *testing.T) { t.Setenv(cli.TaskConfigRootEnv, t.TempDir()) },
		},
		{
			name:    "manifest named for another environment",
			profile: "dev-other-4",
			setup: func(t *testing.T) {
				writeDevEnvManifest(t, "other-4", "NAME=wt-3\nBACKEND_PORT=18084\nPROFILE=dev-other-4\n")
				writeDevEnvProfile(t, "dev-other-4", "http://localhost:18084")
			},
		},
		{
			name:    "no registry",
			profile: devEnvTestProfile,
			setup:   func(t *testing.T) { t.Setenv(devEnvHomeEnv, t.TempDir()) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDevEnvTask(t)
			if tc.setup != nil {
				tc.setup(t)
			}
			cmd := devEnvDaemonCmd(t, tc.profile)
			for _, command := range []string{"daemon start", "daemon stop", "daemon probe-runtimes"} {
				if err := requireHumanLocalDaemonCommand(cmd, command); err == nil || !strings.Contains(err.Error(), "daemon-managed task") {
					t.Fatalf("%s was not refused: %v", command, err)
				}
			}
			if _, err := daemonStatusHealthPort(cmd); err == nil {
				t.Fatal("daemon status was not refused")
			}
		})
	}
}

// Only start, stop, status and probe-runtimes take the exemption; every other
// human-local command stays refused for a dev-env profile too.
func TestDevEnvDaemonProfileLeavesOtherCommandsRefused(t *testing.T) {
	setupDevEnvTask(t)
	cmd := devEnvDaemonCmd(t, devEnvTestProfile)
	cmd.Flags().Bool("foreground", false, "")
	if err := runDaemonRestart(cmd, nil); err == nil || !strings.Contains(err.Error(), "daemon-managed task") {
		t.Fatalf("daemon restart --profile %s was not refused: %v", devEnvTestProfile, err)
	}
	for _, command := range []string{"login", "logout", "daemon autostart enable"} {
		if err := requireHumanLocalCommand(command); err == nil {
			t.Fatalf("%s was allowed inside a task", command)
		}
	}
}
