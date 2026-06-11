package daemon

// CEREBRO-PATCH(daemon-tool-policy-ipc): TECH-2563 — tests for the local-runtime tool-policy spawn wiring.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolPolicyFailDirection(t *testing.T) {
	cases := []struct {
		stage       string
		wantAllowed bool
	}{
		{"enforce", false},
		{"observe", true},
		{"off", true},
		{"", true},
		{"weird", true},
	}
	for _, c := range cases {
		allowed, _ := toolPolicyFailDirection(c.stage, "Bash", os.ErrDeadlineExceeded)
		if allowed != c.wantAllowed {
			t.Errorf("toolPolicyFailDirection(%q) allowed=%v, want %v", c.stage, allowed, c.wantAllowed)
		}
	}
}

func TestPrepareToolPolicySpawn_Gating(t *testing.T) {
	d := &Daemon{}
	workdir := t.TempDir()

	// Off / empty stage → no wiring regardless of provider.
	for _, stage := range []string{"", "off"} {
		got, err := d.prepareToolPolicySpawn("claude", stage, workdir)
		if err != nil {
			t.Fatalf("stage %q: unexpected error %v", stage, err)
		}
		if got != nil {
			t.Errorf("stage %q: expected nil spawn, got %+v", stage, got)
		}
	}

	// Non-Claude provider → no wiring even when enabled (follow-up runtimes).
	for _, provider := range []string{"codex", "cursor", "gemini", "firtal-gateway"} {
		got, err := d.prepareToolPolicySpawn(provider, "enforce", workdir)
		if err != nil {
			t.Fatalf("provider %q: unexpected error %v", provider, err)
		}
		if got != nil {
			t.Errorf("provider %q: expected nil spawn (follow-up), got %+v", provider, got)
		}
	}
}

func TestPrepareToolPolicySpawn_ClaudeWiresHook(t *testing.T) {
	d := &Daemon{}
	workdir := t.TempDir()

	for _, stage := range []string{"observe", "enforce"} {
		got, err := d.prepareToolPolicySpawn("claude", stage, workdir)
		if err != nil {
			t.Fatalf("stage %q: unexpected error %v", stage, err)
		}
		if got == nil {
			t.Fatalf("stage %q: expected a spawn, got nil", stage)
		}
		if got.Env["CEREBRO_TOOLPOLICY_STAGE"] != stage {
			t.Errorf("stage %q: env CEREBRO_TOOLPOLICY_STAGE = %q", stage, got.Env["CEREBRO_TOOLPOLICY_STAGE"])
		}
		if got.SettingsPath == "" {
			t.Fatalf("stage %q: empty settings path", stage)
		}
		raw, err := os.ReadFile(got.SettingsPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		var parsed struct {
			Hooks struct {
				PreToolUse []struct {
					Matcher string `json:"matcher"`
					Hooks   []struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					} `json:"hooks"`
				} `json:"PreToolUse"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("settings json: %v", err)
		}
		if len(parsed.Hooks.PreToolUse) != 1 || len(parsed.Hooks.PreToolUse[0].Hooks) != 1 {
			t.Fatalf("unexpected PreToolUse shape: %+v", parsed.Hooks.PreToolUse)
		}
		cmd := parsed.Hooks.PreToolUse[0].Hooks[0].Command
		if !strings.Contains(cmd, "cerebro-tool-policy-hook") {
			t.Errorf("hook command %q does not invoke cerebro-tool-policy-hook", cmd)
		}
	}
}

func TestWriteToolPolicySettingsJSON_Path(t *testing.T) {
	workdir := t.TempDir()
	path, err := writeToolPolicySettingsJSON(workdir, "/opt/multica/multica")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	want := filepath.Join(workdir, ".claude", "cerebro-tool-policy-settings.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	raw, _ := os.ReadFile(path)
	var parsed struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("settings json: %v", err)
	}
	got := parsed.Hooks.PreToolUse[0].Hooks[0].Command
	if got != `"/opt/multica/multica" cerebro-tool-policy-hook` {
		t.Errorf("command = %q, want quoted exe + subcommand", got)
	}
}
