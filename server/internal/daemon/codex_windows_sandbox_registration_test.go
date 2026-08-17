package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func boolPointer(value bool) *bool {
	return &value
}

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

func TestCodexWindowsSandboxRegistrationSnapshotFallsBackForLegacyServer(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(
		filepath.Join(codexHome, "config.toml"),
		[]byte("[windows]\nsandbox = \"elevated\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	task := Task{}
	captureCodexWindowsSandboxRegistrationSnapshot(
		&task,
		Runtime{Provider: "codex"},
	)
	if !task.codexWindowsSandboxConfigOwnsAtRegistration {
		t.Fatal("missing registration metadata should use the legacy safe fallback")
	}
}

func TestCodexWindowsSandboxTaskPolicyTracksConfigChanges(t *testing.T) {
	const sandbox = `windows.sandbox="unelevated"`
	tests := []struct {
		name          string
		initialConfig string
		changedConfig string
		persistedArgs []string
		managed       bool
		wantPreview   []string
	}{
		{
			name:          "added after registration",
			changedConfig: "[windows]\nsandbox = \"elevated\"\n",
			persistedArgs: []string{"-c", sandbox, "--profile", "research"},
			managed:       true,
			wantPreview:   []string{"--profile", "research"},
		},
		{
			name:          "removed after registration",
			initialConfig: "[windows]\nsandbox = \"elevated\"\n",
			persistedArgs: []string{"--profile", "research"},
			wantPreview:   []string{"-c", sandbox, "--profile", "research"},
		},
		{
			name:          "changed after registration",
			initialConfig: "[windows]\nsandbox = \"elevated\"\n",
			changedConfig: "[windows]\nsandbox = \"unelevated\"\n",
			persistedArgs: []string{"--profile", "research"},
			wantPreview:   []string{"--profile", "research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codexHome := t.TempDir()
			t.Setenv("CODEX_HOME", codexHome)
			configPath := filepath.Join(codexHome, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.initialConfig), 0o600); err != nil {
				t.Fatal(err)
			}

			entry := map[string]string{}
			setCodexWindowsSandboxRegistrationMetadata(
				entry,
				"codex",
				[]string{"--profile", "research"},
			)
			registeredConfigOwns :=
				entry[codexWindowsSandboxConfigConfiguredKey] == "true"
			registered := Runtime{
				Provider: "codex",
				Metadata: RuntimeRegistrationMetadata{
					CodexWindowsSandboxConfigConfigured: boolPointer(registeredConfigOwns),
				},
			}
			task := Task{
				Agent: &AgentData{
					CustomArgs:                      tt.persistedArgs,
					IsCodexWindowsSandboxArgManaged: tt.managed,
				},
			}
			captureCodexWindowsSandboxRegistrationSnapshot(&task, registered)
			policy := newCodexWindowsSandboxSessionPolicy(task)

			if err := os.WriteFile(configPath, []byte(tt.changedConfig), 0o600); err != nil {
				t.Fatal(err)
			}
			liveConfigOwns := execenv.SharedCodexWindowsSandboxConfigOwns()

			opts := agent.ExecOptions{
				GOOS:       "windows",
				CustomArgs: tt.persistedArgs,
			}
			previewArgs := policy.effectiveLaunchArgs(opts, nil)
			if !reflect.DeepEqual(previewArgs, tt.wantPreview) {
				t.Fatalf("preview args = %v, want %v", previewArgs, tt.wantPreview)
			}

			launchOpts := policy.applyToExecOptions(opts)
			launchArgs := agent.EffectiveCodexLaunchArgs(launchOpts, nil)
			if !reflect.DeepEqual(launchArgs, previewArgs) {
				t.Fatalf("spawn args drifted after config change: preview=%v launch=%v", previewArgs, launchArgs)
			}
			if launchOpts.CodexWindowsSandboxConfigOwns != liveConfigOwns {
				t.Fatalf(
					"spawn config ownership = %v, want prepared config ownership %v",
					launchOpts.CodexWindowsSandboxConfigOwns,
					liveConfigOwns,
				)
			}
		})
	}

	nonWindows := newCodexWindowsSandboxSessionPolicy(Task{
		Agent: &AgentData{
			CustomArgs:                      []string{"-c", sandbox, "--profile", "research"},
			IsCodexWindowsSandboxArgManaged: true,
		},
	})
	got := nonWindows.effectiveLaunchArgs(agent.ExecOptions{
		GOOS:       "linux",
		CustomArgs: []string{"-c", sandbox, "--profile", "research"},
	}, nil)
	if want := []string{"--profile", "research"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("non-Windows args = %v, want %v", got, want)
	}
}

