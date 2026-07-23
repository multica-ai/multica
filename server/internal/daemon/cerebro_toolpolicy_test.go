package daemon

// Cerebro local-runtime tool-policy integration tests.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolPolicyFailDirection(t *testing.T) {
	allowed, _ := toolPolicyFailDirection("Bash", os.ErrDeadlineExceeded)
	if allowed {
		t.Fatal("enforce transport failure must fail closed")
	}
}

func TestPrepareToolPolicySpawn_AllLocalProvidersEnforce(t *testing.T) {
	d := &Daemon{}
	for _, provider := range []string{"claude", "codex", "cursor", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			workdir := t.TempDir()
			providerHome := ""
			if provider == "codex" {
				providerHome = filepath.Join(workdir, "codex-home")
			}
			got, err := d.prepareToolPolicySpawn(provider, workdir, providerHome, false)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if got == nil {
				t.Fatalf("spawn = %+v, want mandatory enforce", got)
			}
			if got.SettingsPath == "" {
				t.Fatal("provider has no call-time policy hook")
			}
		})
	}
}

func TestWriteToolPolicySettingsJSON_ProviderContracts(t *testing.T) {
	for _, tc := range []struct {
		provider string
		path     string
		event    string
	}{
		{"claude", filepath.Join(".claude", "cerebro-tool-policy-settings.json"), "PreToolUse"},
		{"gemini", filepath.Join(".gemini", "settings.json"), "BeforeTool"},
		{"cursor", filepath.Join(".cursor", "hooks.json"), "preToolUse"},
		{"codex", "hooks.json", "PreToolUse"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			workdir := t.TempDir()
			providerHome := ""
			base := workdir
			if tc.provider == "codex" {
				providerHome = filepath.Join(workdir, "codex-home")
				base = providerHome
			}
			path, err := writeToolPolicySettingsJSON(tc.provider, workdir, providerHome, "/opt/multica/multica", false)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			if path != filepath.Join(base, tc.path) {
				t.Fatalf("path = %q", path)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("json: %v", err)
			}
			if !strings.Contains(string(raw), tc.event) || !strings.Contains(string(raw), "cerebro-tool-policy-hook") {
				t.Fatalf("settings missing event/hook: %s", raw)
			}
			if tc.provider == "cursor" && !strings.Contains(string(raw), `"failClosed": true`) {
				t.Fatalf("cursor hook is not fail-closed: %s", raw)
			}
		})
	}
}

func TestPrepareToolPolicySpawn_NonTargetProviderUnaffected(t *testing.T) {
	got, err := (&Daemon{}).prepareToolPolicySpawn("firtal-gateway", t.TempDir(), "", false)
	if err != nil || got != nil {
		t.Fatalf("non-target provider = %+v, %v", got, err)
	}
}

func TestLocalToolPolicyRolloutFlagsCannotReturn(t *testing.T) {
	master := "cerebro_local_" + "tool_policy"
	enforce := master + "_enforce"
	for _, path := range []string{
		filepath.Join("..", "..", "..", "packages", "cerebro-feature-flags", "registry.ts"),
		filepath.Join("..", "..", "cmd", "multica", "cerebro_feature_catalog.json"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(raw), master) || strings.Contains(string(raw), enforce) {
			t.Fatalf("retired local tool-policy rollout flag returned in %s", path)
		}
	}
}

func TestWriteToolPolicySettingsJSON_MergesFastMode(t *testing.T) {
	path, err := writeToolPolicySettingsJSON("claude", t.TempDir(), "", "/opt/multica/multica", true)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed struct {
		FastMode bool           `json:"fastMode"`
		Hooks    map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("settings json: %v", err)
	}
	if !parsed.FastMode || parsed.Hooks == nil {
		t.Fatalf("expected fast mode and hooks in one settings document: %+v", parsed)
	}
}
