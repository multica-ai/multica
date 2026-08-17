package daemon

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestSetCodexWindowsSandboxRegistrationMetadataWithConfig(t *testing.T) {
	entry := map[string]string{}
	setCodexWindowsSandboxRegistrationMetadataWithConfig(
		entry,
		true,
		[]string{"--profile", "research"},
	)

	if entry[codexWindowsSandboxArgConfiguredKey] != "false" {
		t.Fatalf("argument ownership = %q, want false", entry[codexWindowsSandboxArgConfiguredKey])
	}
	if entry[codexWindowsSandboxConfigConfiguredKey] != "true" {
		t.Fatalf("config ownership = %q, want true", entry[codexWindowsSandboxConfigConfiguredKey])
	}

	setCodexWindowsSandboxRegistrationMetadataWithConfig(
		entry,
		false,
		[]string{"-c", `windows.sandbox="elevated"`},
	)
	if entry[codexWindowsSandboxArgConfiguredKey] != "true" {
		t.Fatalf("argument ownership = %q, want true", entry[codexWindowsSandboxArgConfiguredKey])
	}
	if entry[codexWindowsSandboxConfigConfiguredKey] != "false" {
		t.Fatalf("config ownership = %q, want false", entry[codexWindowsSandboxConfigConfiguredKey])
	}
}

func TestCodexWindowsSandboxTaskPolicyTracksPreparedConfig(t *testing.T) {
	const sandbox = `windows.sandbox="unelevated"`
	tests := []struct {
		name              string
		initialConfig     string
		changedConfig     string
		removeAfter       bool
		wantConfigOwns    bool
		wantCopiedSetting string
		wantPreview       []string
	}{
		{
			name:              "added after registration",
			changedConfig:     "[windows]\nsandbox = \"elevated\"\n",
			wantConfigOwns:    true,
			wantCopiedSetting: `sandbox = "elevated"`,
			wantPreview:       []string{"--profile", "research"},
		},
		{
			name:          "removed after registration",
			initialConfig: "[windows]\nsandbox = \"elevated\"\n",
			removeAfter:   true,
			wantPreview:   []string{"-c", sandbox, "--profile", "research"},
		},
		{
			name:              "changed after registration",
			initialConfig:     "[windows]\nsandbox = \"elevated\"\n",
			changedConfig:     "[windows]\nsandbox = \"unelevated\"\n",
			wantConfigOwns:    true,
			wantCopiedSetting: `sandbox = "unelevated"`,
			wantPreview:       []string{"--profile", "research"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			t.Setenv("CODEX_HOME", sharedHome)
			sharedConfig := filepath.Join(sharedHome, "config.toml")
			if tt.initialConfig != "" {
				if err := os.WriteFile(sharedConfig, []byte(tt.initialConfig), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			entry := map[string]string{}
			setCodexWindowsSandboxRegistrationMetadata(
				entry,
				"codex",
				[]string{"--profile", "research"},
			)
			registeredConfigOwns :=
				entry[codexWindowsSandboxConfigConfiguredKey] == "true"
			persistedArgs, managed := agent.NormalizeCodexWindowsSandboxCustomArgs(
				"windows",
				false,
				registeredConfigOwns,
				[]string{"--profile", "research"},
			)

			switch {
			case tt.removeAfter:
				if err := os.Remove(sharedConfig); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(sharedConfig, []byte(tt.changedConfig), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			task := Task{
				Agent: &AgentData{
					CustomArgs:                      persistedArgs,
					IsCodexWindowsSandboxArgManaged: managed,
				},
			}
			policy := newCodexWindowsSandboxTaskPolicy(task)
			baseOpts := agent.ExecOptions{
				GOOS:       "windows",
				CustomArgs: persistedArgs,
			}
			candidateArgs := policy.effectiveLaunchArgs(baseOpts, nil)

			env, err := execenv.Prepare(execenv.PrepareParams{
				WorkspacesRoot:  t.TempDir(),
				WorkspaceID:     "workspace",
				TaskID:          fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
				AgentName:       "agent",
				Provider:        "codex",
				GOOS:            "windows",
				CodexCustomArgs: candidateArgs,
				Task: execenv.TaskContextForEnv{
					AgentID:   "agent",
					AgentName: "agent",
				},
			}, logger)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}

			if env.CodexWindowsSandboxConfigOwns != tt.wantConfigOwns {
				t.Fatalf(
					"prepared config ownership = %v, want %v",
					env.CodexWindowsSandboxConfigOwns,
					tt.wantConfigOwns,
				)
			}
			policy = policy.withPreparedConfigOwnership(
				env.CodexWindowsSandboxConfigOwns,
			)
			previewArgs := policy.effectiveLaunchArgs(baseOpts, nil)
			if !reflect.DeepEqual(previewArgs, tt.wantPreview) {
				t.Fatalf("preview args = %v, want %v", previewArgs, tt.wantPreview)
			}

			copiedConfig, err := os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
			if err != nil {
				t.Fatalf("read prepared config: %v", err)
			}
			copied := string(copiedConfig)
			if tt.wantCopiedSetting == "" {
				if strings.Contains(copied, "windows.sandbox") ||
					strings.Contains(copied, "[windows]") {
					t.Fatalf("removed shared setting survived prepared config:\n%s", copied)
				}
			} else if !strings.Contains(copied, tt.wantCopiedSetting) {
				t.Fatalf("prepared config does not contain %q:\n%s", tt.wantCopiedSetting, copied)
			}
			if !strings.Contains(copied, `sandbox_mode = "workspace-write"`) {
				t.Fatalf("prepared Windows config is not workspace-write:\n%s", copied)
			}
			if strings.Contains(copied, `sandbox_mode = "danger-full-access"`) {
				t.Fatalf("prepared Windows config silently fell through to danger-full-access:\n%s", copied)
			}

			launchOpts := policy.applyToExecOptions(baseOpts)
			launchArgs := agent.EffectiveCodexLaunchArgs(launchOpts, nil)
			if !reflect.DeepEqual(launchArgs, previewArgs) {
				t.Fatalf("spawn args drifted from preview: preview=%v launch=%v", previewArgs, launchArgs)
			}
			if launchOpts.CodexWindowsSandboxConfigOwns != tt.wantConfigOwns {
				t.Fatalf(
					"spawn config ownership = %v, want %v",
					launchOpts.CodexWindowsSandboxConfigOwns,
					tt.wantConfigOwns,
				)
			}
		})
	}

	nonWindows := newCodexWindowsSandboxTaskPolicy(Task{
		Agent: &AgentData{
			CustomArgs:                      []string{"-c", sandbox, "--profile", "research"},
			IsCodexWindowsSandboxArgManaged: true,
		},
	}).withPreparedConfigOwnership(true)
	got := nonWindows.effectiveLaunchArgs(agent.ExecOptions{
		GOOS:       "linux",
		CustomArgs: []string{"-c", sandbox, "--profile", "research"},
	}, nil)
	if want := []string{"--profile", "research"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("non-Windows args = %v, want %v", got, want)
	}
}
