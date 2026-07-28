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

func TestPrepareToolPolicySpawn_RejectsLocalProvidersWithoutEnforcementAdapter(t *testing.T) {
	d := &Daemon{}
	for _, provider := range []string{"copilot", "openclaw", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			got, err := d.prepareToolPolicySpawn(provider, t.TempDir(), "", false)
			if err == nil {
				t.Fatalf("spawn = %+v, want provider rejected until it has a mandatory tool-policy adapter", got)
			}
			if !strings.Contains(err.Error(), "does not support mandatory tool-policy enforcement") {
				t.Fatalf("error = %q, want explicit tool-policy rejection", err)
			}
		})
	}
}

// The ACP family is enforced by the daemon's own ACP client answering
// session/request_permission, so the spawn is allowed and writes no settings
// file. A settings path here would mean someone re-routed them through a
// provider-native hook that these CLIs do not have.
func TestPrepareToolPolicySpawn_ACPFamilyNeedsNoSettingsFile(t *testing.T) {
	d := &Daemon{}
	for _, provider := range []string{"hermes", "kimi", "kiro"} {
		t.Run(provider, func(t *testing.T) {
			got, err := d.prepareToolPolicySpawn(provider, t.TempDir(), "", false)
			if err != nil {
				t.Fatalf("spawn error = %v, want the ACP client to satisfy enforcement", err)
			}
			if got != nil {
				t.Fatalf("spawn = %+v, want no settings file for an ACP-gated provider", got)
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

// Pi is enforced by the harness extension, not a settings file, so the spawn
// preparer must accept it and write nothing. The refusal path when the harness
// is disabled is covered by TestPreparePiHarnessKillSwitchRefusesPiSpawn.
func TestPrepareToolPolicySpawn_HarnessProviderWritesNoSettingsFile(t *testing.T) {
	workdir := t.TempDir()
	got, err := (&Daemon{}).prepareToolPolicySpawn("pi", workdir, "", false)
	if err != nil {
		t.Fatalf("pi must be runnable through its harness adapter: %v", err)
	}
	if got != nil {
		t.Fatalf("spawn = %+v, want no settings file for a harness provider", got)
	}
	entries, err := os.ReadDir(workdir)
	if err != nil {
		t.Fatalf("read workdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("harness provider wrote %d file(s) into the workdir", len(entries))
	}
}

func TestPrepareToolPolicySpawn_NonTargetProviderUnaffected(t *testing.T) {
	got, err := (&Daemon{}).prepareToolPolicySpawn("firtal-gateway", t.TempDir(), "", false)
	if err != nil || got != nil {
		t.Fatalf("non-target provider = %+v, %v", got, err)
	}
}

func TestPrepareToolPolicySpawn_RejectsUnknownLocalProviderByDefault(t *testing.T) {
	got, err := (&Daemon{}).prepareToolPolicySpawn("new-local-cli", t.TempDir(), "", false)
	if err == nil || got != nil {
		t.Fatalf("unknown provider = %+v, %v; want fail-closed rejection", got, err)
	}
	if !strings.Contains(err.Error(), "does not support mandatory tool-policy enforcement") {
		t.Fatalf("error = %q, want explicit tool-policy rejection", err)
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
