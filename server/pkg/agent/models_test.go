package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStaticModelCatalogsAreValid(t *testing.T) {
	t.Parallel()
	catalogs := map[string][]Model{
		"claude":  claudeStaticModels(),
		"codex":   codexStaticModels(),
		"cursor":  cursorStaticModels(),
		"copilot": copilotStaticModels(),
	}
	for provider, models := range catalogs {
		if len(models) == 0 {
			t.Errorf("%s static catalog returned no models", provider)
		}
		defaultCount := 0
		for i, model := range models {
			if model.ID == "" {
				t.Errorf("%s static catalog[%d] has empty ID", provider, i)
			}
			if model.Label == "" {
				t.Errorf("%s static catalog[%d] has empty Label", provider, i)
			}
			if model.Default {
				defaultCount++
			}
		}
		if defaultCount > 1 {
			t.Errorf("%s: %d models marked Default, want 0 or 1", provider, defaultCount)
		}
	}
}

func TestListModelsQwenUsesRuntimeDefaultAndManualEntry(t *testing.T) {
	// Qwen returns its manual-entry catalog without resolving or executing a CLI.
	got, err := ListModels(context.Background(), "qwen", Command{Path: ""})
	if err != nil {
		t.Fatalf("ListModels(qwen) error: %v", err)
	}
	if len(got.Models) != 0 {
		t.Fatalf("ListModels(qwen) = %+v, want no account-specific static catalog", got)
	}
	if got.Fallback {
		t.Error("qwen's empty catalog is deliberate, not a discovery fallback")
	}
}

func TestListModelsCopilotFallsBackToStatic(t *testing.T) {
	// Copilot uses dynamic ACP discovery, but with no `copilot`
	// binary on PATH (the discovery LookPath fails) it must fall
	// back to copilotStaticModels() so the UI dropdown stays
	// populated. This is the "binary missing on the daemon host"
	// path we care about for self-hosted runtimes.
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "copilot")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "copilot", Command{Path: missingAgentExecutable(t, "copilot")})
	if err != nil {
		t.Fatalf("ListModels(copilot) error: %v", err)
	}
	if len(got.Models) == 0 {
		t.Fatal("expected static fallback models, got empty list")
	}
	if !got.Fallback {
		t.Error("a static stand-in must be marked Fallback so it is never cached as the real catalog")
	}
	ids := map[string]bool{}
	for _, m := range got.Models {
		ids[m.ID] = true
	}
	if !ids["gpt-5.4"] || !ids["claude-sonnet-4.6"] {
		t.Errorf("static fallback missing expected models: %+v", got)
	}
}

func TestParseKimiProviderThinking(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "models": {
    "kimi-code/kimi-for-coding": {
      "displayName": "K2.7 Coding"
    },
    "kimi-code/k3": {
      "supportEfforts": ["low", "high", "max", "high", "bad value"],
      "defaultEffort": "high"
    },
    "kimi-code/k3-256k": {
      "support_efforts": ["low", "max"],
      "default_effort": "missing"
    }
  }
}`)

	got, err := parseKimiProviderThinking(raw)
	if err != nil {
		t.Fatalf("parseKimiProviderThinking: %v", err)
	}
	if thinking, ok := got["kimi-code/kimi-for-coding"]; ok {
		t.Fatalf("model without supportEfforts = %+v, want no entry at all", thinking)
	}
	k3 := got["kimi-code/k3"]
	if k3 == nil {
		t.Fatal("k3 thinking catalog is nil")
	}
	if k3.DefaultLevel != "high" {
		t.Errorf("k3 default = %q, want high", k3.DefaultLevel)
	}
	if values := thinkingValues(k3); !reflect.DeepEqual(values, []string{"low", "high", "max"}) {
		t.Errorf("k3 levels = %v, want [low high max]", values)
	}
	if labels := []string{k3.SupportedLevels[0].Label, k3.SupportedLevels[1].Label, k3.SupportedLevels[2].Label}; !reflect.DeepEqual(labels, []string{"Low", "High", "Max"}) {
		t.Errorf("k3 labels = %v, want [Low High Max]", labels)
	}
	k3Short := got["kimi-code/k3-256k"]
	if k3Short == nil {
		t.Fatal("snake_case k3-256k thinking catalog is nil")
	}
	if k3Short.DefaultLevel != "" {
		t.Errorf("unsupported default = %q, want empty", k3Short.DefaultLevel)
	}
}

func TestFindACPConfigOptionRequiresExactID(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "config_options": [{
    "type": "select",
    "id": "thinking",
    "category": "thought_level",
    "current_value": "high"
  }]
}`)
	option, ok := findACPConfigOption(raw, "thinking")
	if !ok || option.ID != "thinking" {
		t.Fatalf("findACPConfigOption = (%+v, %v), want exact thinking option", option, ok)
	}
	value, ok := acpConfigOptionCurrentValue(raw, "thinking")
	if !ok || value != "high" {
		t.Errorf("acpConfigOptionCurrentValue = (%q, %v), want (high, true)", value, ok)
	}

	for _, invalidID := range []string{"reasoning", "Thinking", " thinking "} {
		renamed := []byte(fmt.Sprintf(`{"configOptions":[{"id":%q,"category":"thought_level","currentValue":"high"}]}`, invalidID))
		if option, ok := findACPConfigOption(renamed, "thinking"); ok {
			t.Fatalf("non-exact id %q exposed config option: %+v", invalidID, option)
		}
	}
}

func TestDiscoverKimiModelsAnnotatesThinkingPerModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	script := `#!/bin/sh
if [ "$1" = "provider" ]; then
  printf '%s\n' '{"models":{"kimi-code/kimi-for-coding":{"displayName":"K2.7 Coding"},"kimi-code/k3":{"supportEfforts":["low","high","max"],"defaultEffort":"high"},"kimi-code/k3-256k":{"supportEfforts":["low","high","max"],"defaultEffort":"high"}}}'
  exit 0
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"Kimi Code CLI","version":"0.33.0"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session","configOptions":[{"type":"select","id":"model","category":"model","currentValue":"kimi-code/k3","options":[{"value":"kimi-code/kimi-for-coding","name":"K2.7 Coding"},{"value":"kimi-code/k3","name":"K3"},{"value":"kimi-code/k3-256k","name":"K3-256k"}]},{"type":"select","id":"thinking","category":"thought_level","currentValue":"high","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"},{"value":"max","name":"Max"}]}]}}\n' "$id"
      ;;
  esac
done
`
	fake := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverKimiModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverKimiModels: %v", err)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	if byID["kimi-code/kimi-for-coding"].Thinking != nil {
		t.Errorf("kimi-for-coding must not inherit the current model's efforts: %+v", byID["kimi-code/kimi-for-coding"].Thinking)
	}
	for _, id := range []string{"kimi-code/k3", "kimi-code/k3-256k"} {
		thinking := byID[id].Thinking
		if thinking == nil || thinking.DefaultLevel != "high" ||
			!reflect.DeepEqual(thinkingValues(thinking), []string{"low", "high", "max"}) {
			t.Errorf("%s thinking = %+v", id, thinking)
		}
	}

	valid, err := ValidateThinkingLevel(context.Background(), "kimi", Command{Path: fake}, "kimi-code/k3", "high")
	if err != nil || !valid {
		t.Errorf("ValidateThinkingLevel(k3, high) = (%v, %v), want (true, nil)", valid, err)
	}
	// Unsupported persisted values are ordinary catalog results, not discovery
	// errors. The daemon logs a warning, ignores the value, and starts the task
	// with the runtime's own setting, just as it does for other providers.
	valid, err = ValidateThinkingLevel(context.Background(), "kimi", Command{Path: fake}, "kimi-code/k3", "medium")
	if err != nil || valid {
		t.Errorf("ValidateThinkingLevel(k3, medium) = (%v, %v), want unsupported without an error", valid, err)
	}
	valid, err = ValidateThinkingLevel(context.Background(), "kimi", Command{Path: fake}, "kimi-code/kimi-for-coding", "high")
	if err != nil || valid {
		t.Errorf("ValidateThinkingLevel(kimi-for-coding, high) = (%v, %v), want unsupported without an error", valid, err)
	}
}

func TestDiscoverKimiModelsProviderListFailureHidesThinking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	script := `#!/bin/sh
if [ "$1" = "provider" ]; then
  exit 1
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"Kimi Code CLI","version":"0.33.0"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session","configOptions":[{"type":"select","id":"model","category":"model","currentValue":"kimi-code/kimi-for-coding","options":[{"value":"kimi-code/kimi-for-coding","name":"K2.7 Coding"},{"value":"kimi-code/k3","name":"K3"}]},{"type":"select","id":"thinking","category":"thought_level","currentValue":"high","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]}}\n' "$id"
      ;;
  esac
done
`
	fake := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverKimiModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverKimiModels: %v", err)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	for _, id := range []string{"kimi-code/kimi-for-coding", "kimi-code/k3"} {
		if thinking := byID[id].Thinking; thinking != nil {
			t.Errorf("%s received session-local thinking after provider-list failure: %+v", id, thinking)
		}
	}
}

func TestDiscoverKimiModelsDoesNotMigrateSessionThinkingToAnotherModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	script := `#!/bin/sh
if [ "$1" = "provider" ]; then
  printf '%s\n' '{"models":{"kimi-code/kimi-for-coding":{"displayName":"K2.7 Coding"}}}'
  exit 0
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"Kimi Code CLI","version":"0.33.0"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session","configOptions":[{"id":"model","category":"model","currentValue":"kimi-code/k3","options":[{"value":"kimi-code/kimi-for-coding","name":"K2.7 Coding"},{"value":"kimi-code/k3","name":"K3"}]},{"id":"thinking","category":"thought_level","currentValue":"high","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]}}\n' "$id"
      ;;
  esac
done
`
	fake := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverKimiModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverKimiModels: %v", err)
	}
	for _, model := range models {
		if model.Thinking != nil {
			t.Errorf("%s received thinking from a different ACP session model: %+v", model.ID, model.Thinking)
		}
	}
}

// TestDiscoverKimiModelsIgnoresTheDiscoverySessionsOwnThinkingSupport pins the
// per-model rule against the case that makes a session-wide gate wrong. The CLI
// default model here has no thinking capability at all, so Kimi's session/new
// omits the `thinking` config id entirely — yet K3 still advertises
// supportEfforts in the provider catalog and must keep its levels. The mirror
// case matters just as much: the session does advertise thinking, but that says
// nothing about a model provider-list gives no efforts for.
func TestDiscoverKimiModelsIgnoresTheDiscoverySessionsOwnThinkingSupport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	script := `#!/bin/sh
if [ "$1" = "provider" ]; then
  printf '%s\n' '{"models":{"kimi-code/kimi-for-coding":{"displayName":"K2.7 Coding"},"kimi-code/k3":{"supportEfforts":["low","high","max"],"defaultEffort":"high"}}}'
  exit 0
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"Kimi Code CLI","version":"0.33.0"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session","configOptions":[{"type":"select","id":"model","category":"model","currentValue":"kimi-code/kimi-for-coding","options":[{"value":"kimi-code/kimi-for-coding","name":"K2.7 Coding"},{"value":"kimi-code/k3","name":"K3"}]}]}}\n' "$id"
      ;;
  esac
done
`
	fake := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverKimiModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverKimiModels: %v", err)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	k3 := byID["kimi-code/k3"].Thinking
	if k3 == nil || k3.DefaultLevel != "high" ||
		!reflect.DeepEqual(thinkingValues(k3), []string{"low", "high", "max"}) {
		t.Errorf("k3 lost its efforts because the default model has none: %+v", k3)
	}
	if thinking := byID["kimi-code/kimi-for-coding"].Thinking; thinking != nil {
		t.Errorf("kimi-for-coding has no efforts in the provider catalog: %+v", thinking)
	}
}

// TestDiscoverKimiModelsHidesThinkingBelowTheEffortCapableCLI covers the case
// provider-list cannot detect on its own. Checked against the real binaries:
// Kimi 0.28.1 reports K3's supportEfforts exactly like 0.33.0 does, but its ACP
// only implements the on/off toggle — set_config_option("max") answers success
// while confirming "on". Advertising Low/High/Max there would let a user save a
// level the runtime silently ignores, so the version decides.
func TestDiscoverKimiModelsHidesThinkingBelowTheEffortCapableCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	// The `thinking` config id is present here on purpose: 0.28.1 advertises it
	// too, which is exactly why it cannot stand in for the capability check.
	script := `#!/bin/sh
if [ "$1" = "provider" ]; then
  printf '%s\n' '{"models":{"kimi-code/k3":{"supportEfforts":["low","high","max"],"defaultEffort":"high"}}}'
  exit 0
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"Kimi Code CLI","version":"VERSION"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session","configOptions":[{"id":"model","category":"model","currentValue":"kimi-code/k3","options":[{"value":"kimi-code/k3","name":"K3"}]},{"id":"thinking","category":"thought_level","currentValue":"on","options":[{"value":"off","name":"Off"},{"value":"on","name":"On"}]}]}}\n' "$id"
      ;;
  esac
done
`
	tests := []struct {
		name         string
		version      string
		wantThinking bool
	}{
		{name: "0.28.1 applies on/off only", version: "0.28.1", wantThinking: false},
		{name: "0.29.0 is the first effort-capable build", version: "0.29.0", wantThinking: true},
		{name: "unidentifiable build stays hidden", version: "", wantThinking: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := filepath.Join(t.TempDir(), "kimi")
			writeTestExecutable(t, fake, []byte(strings.Replace(script, "VERSION", tt.version, 1)))

			models, err := discoverKimiModels(context.Background(), Command{Path: fake})
			if err != nil {
				t.Fatalf("discoverKimiModels: %v", err)
			}
			if len(models) == 0 {
				t.Fatal("model discovery must keep working on every CLI build")
			}
			var k3 *ModelThinking
			for _, model := range models {
				if model.ID == "kimi-code/k3" {
					k3 = model.Thinking
				}
			}
			if got := k3 != nil; got != tt.wantThinking {
				t.Errorf("k3 thinking present = %v, want %v (version %q): %+v", got, tt.wantThinking, tt.version, k3)
			}
		})
	}
}

func TestACPAgentInfoVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "kimi shape", raw: `{"protocolVersion":1,"agentInfo":{"name":"Kimi Code CLI","version":"0.33.0"}}`, want: "0.33.0"},
		{name: "snake_case", raw: `{"agent_info":{"version":"0.29.0"}}`, want: "0.29.0"},
		{name: "padded", raw: `{"agentInfo":{"version":"  0.30.1  "}}`, want: "0.30.1"},
		{name: "absent", raw: `{"protocolVersion":1,"agentCapabilities":{}}`, want: ""},
		{name: "malformed json", raw: `not json`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := acpAgentInfoVersion([]byte(tt.raw)); got != tt.want {
				t.Errorf("acpAgentInfoVersion(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestKimiSupportsThinkingEfforts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    bool
	}{
		{version: "0.29.0", want: true},  // first effort-capable build
		{version: "0.28.1", want: false}, // confirms "on" for any effort
		{version: "0.33.0", want: true},
		{version: "1.0.0", want: true},
		{version: "0.9.0", want: false}, // minor is compared numerically, not lexically
		{version: "", want: false},      // agent reported no version
		{version: "not-a-version", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := kimiSupportsThinkingEfforts(tt.version); got != tt.want {
				t.Errorf("kimiSupportsThinkingEfforts(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func thinkingValues(thinking *ModelThinking) []string {
	if thinking == nil {
		return nil
	}
	values := make([]string, 0, len(thinking.SupportedLevels))
	for _, level := range thinking.SupportedLevels {
		values = append(values, level.Value)
	}
	return values
}

func TestModelKnownIncompatibleWithProvider(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{
			name:     "claude model is incompatible with codex",
			provider: "codex",
			model:    "claude-sonnet-4-6",
			want:     true,
		},
		{
			name:     "codex model is compatible with codex",
			provider: "codex",
			model:    "gpt-5.5",
			want:     false,
		},
		{
			name:     "codex model is incompatible with claude",
			provider: "claude",
			model:    "o3",
			want:     true,
		},
		{
			name:     "exact claude model is compatible with claude",
			provider: "claude",
			model:    "claude-opus-4-7",
			want:     false,
		},
		{
			name:     "claude context variant is compatible with claude",
			provider: "claude",
			model:    "claude-opus-5[1m]",
			want:     false,
		},
		{
			name:     "future-shaped claude context variant is compatible with claude",
			provider: "claude",
			model:    "claude-opus-5[500k]",
			want:     false,
		},
		{
			name:     "malformed claude context variant is incompatible with claude",
			provider: "claude",
			model:    "claude-opus-5[weird]",
			want:     true,
		},
		{
			name:     "unknown claude base stays incompatible after context normalization",
			provider: "claude",
			model:    "claude-fake-9[1m]",
			want:     true,
		},
		{
			name:     "provider-prefixed openai model is incompatible with codex",
			provider: "codex",
			model:    "openai/gpt-4o",
			want:     true,
		},
		{
			name:     "provider-prefixed anthropic model is incompatible with claude",
			provider: "claude",
			model:    "anthropic/claude-opus-4.7",
			want:     true,
		},
		{
			name:     "known openai-looking model outside codex catalog is incompatible",
			provider: "codex",
			model:    "gpt-99",
			want:     true,
		},
		{
			name:     "unknown custom model is not classified",
			provider: "codex",
			model:    "private-lab-model",
			want:     false,
		},
		{
			name:     "unknown target provider does not clear",
			provider: "opencode",
			model:    "claude-sonnet-4-6",
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModelKnownIncompatibleWithProvider(tc.provider, tc.model); got != tc.want {
				t.Fatalf("ModelKnownIncompatibleWithProvider(%q, %q) = %v, want %v", tc.provider, tc.model, got, tc.want)
			}
		})
	}
}

func TestInferCopilotProvider(t *testing.T) {
	cases := map[string]string{
		"gpt-5.5":           "openai",
		"gpt-5.4-mini":      "openai",
		"gpt-5.3-codex":     "openai",
		"gpt-4.1":           "openai",
		"o1":                "openai",
		"o3":                "openai",
		"o3-mini":           "openai",
		"o4-mini":           "openai",
		"o5":                "openai", // future-proof: any o<digit>+
		"o6-mini-high":      "openai",
		"claude-opus-4.7":   "anthropic",
		"claude-sonnet-4.6": "anthropic",
		"claude-haiku-4.5":  "anthropic",
		"gemini-3-pro":      "google",
		"grok-code-fast-1":  "xai",
		"auto":              "",
		"raptor-mini":       "",
		// negative cases: must not be misidentified as OpenAI
		// reasoning series even though they start with `o`.
		"opus-fake": "",
		"omni":      "",
		"o":         "",
	}
	for id, want := range cases {
		if got := inferCopilotProvider(id); got != want {
			t.Errorf("inferCopilotProvider(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestListModelsHermesWithoutBinary(t *testing.T) {
	// Hermes reports discovery failures instead of swallowing them into an
	// empty list, unlike the kiro / qoder cases below (MUL-6606). Those two
	// have a caller that substitutes something; hermes has no static catalog,
	// so an empty list here would be indistinguishable from "this runtime
	// genuinely has no models" and the picker would render it as an
	// authoritative empty dropdown. The error keeps the picker in its
	// discovery-failed state, which still offers manual entry — the same
	// fallback, minus the false confidence.
	//
	// This test only verifies the executable-lookup fast path; an actual ACP
	// session is exercised in hermes_model_discovery_test.go.
	ctx := context.Background()
	// Prime the cache miss so we hit the live discovery function.
	modelCacheMu.Lock()
	delete(modelCache, "hermes")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "hermes", Command{Path: missingAgentExecutable(t, "hermes")})
	if err == nil {
		t.Fatalf("expected a missing binary to be reported, got catalog %+v", got.Models)
	}
	if len(got.Models) != 0 {
		t.Errorf("a failed discovery must carry no models, got %+v", got.Models)
	}
	// The reason has to name what actually went wrong: this text is what the
	// daemon forwards as the request's error and what the picker displays.
	if !strings.Contains(err.Error(), "executable lookup") {
		t.Errorf("error should name the failing stage, got: %v", err)
	}
}

func TestListModelsKiroWithoutBinary(t *testing.T) {
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "kiro")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "kiro", Command{Path: missingAgentExecutable(t, "kiro-cli")})
	if err != nil {
		t.Fatalf("ListModels(kiro) error: %v", err)
	}
	if got.Models == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsQoderWithoutBinary(t *testing.T) {
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "qoder")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "qoder", Command{Path: missingAgentExecutable(t, "qodercli")})
	if err != nil {
		t.Fatalf("ListModels(qoder) error: %v", err)
	}
	if got.Models == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsQoderCNWithoutBinary(t *testing.T) {
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "qoderclicn")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "qoderclicn", Command{Path: missingAgentExecutable(t, "qoderclicn")})
	if err != nil {
		t.Fatalf("ListModels(qoderclicn) error: %v", err)
	}
	if got.Models == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	_, err := ListModels(ctx, "nonexistent", Command{Path: ""})
	if err == nil {
		t.Fatal("ListModels(unknown) expected error")
	}
}

func TestParseOpenCodeModels(t *testing.T) {
	input := `PROVIDER/MODEL                     CONTEXT  MAX_OUT
openai/gpt-4o                      128000   16384
anthropic/claude-sonnet-4-6        200000   8192
openai/gpt-4o                      128000   16384
nonprefixed-line
`
	models := parseOpenCodeModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (header skipped, duplicate deduped, non-slash skipped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" || models[0].Provider != "openai" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "anthropic/claude-sonnet-4-6" || models[1].Provider != "anthropic" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
}

func TestParseOpenCodeModelsVerboseVariants(t *testing.T) {
	input := `openai/gpt-5
{
  "id": "gpt-5",
  "name": "GPT-5",
  "reasoning": true,
  "variants": {
    "high": { "reasoningEffort": "high" },
    "low": { "reasoningEffort": "low" },
    "xhigh": { "reasoningEffort": "xhigh" },
    "fast-mode": { "reasoningEffort": "low" },
    "disabled": { "disabled": true }
  }
}
anthropic/claude-sonnet-4-6
{
  "id": "claude-sonnet-4-6",
  "reasoning": true,
  "variants": {
    "max": { "thinking": { "type": "enabled", "budgetTokens": 32000 } },
    "high": { "thinking": { "type": "enabled", "budgetTokens": 16000 } }
  }
}
`
	models := parseOpenCodeModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].Thinking == nil {
		t.Fatalf("expected first model to expose thinking variants")
	}
	got := make([]string, 0, len(models[0].Thinking.SupportedLevels))
	for _, lvl := range models[0].Thinking.SupportedLevels {
		got = append(got, lvl.Value)
		if lvl.Value == "xhigh" && lvl.Label != "Extra high" {
			t.Errorf("xhigh label: got %q, want Extra high", lvl.Label)
		}
		if lvl.Value == "fast-mode" && lvl.Label != "Fast Mode" {
			t.Errorf("custom variant label: got %q, want Fast Mode", lvl.Label)
		}
	}
	want := []string{"low", "high", "xhigh", "fast-mode"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("variant order/values: got %v, want %v", got, want)
	}
	if models[1].Thinking == nil || len(models[1].Thinking.SupportedLevels) != 2 {
		t.Fatalf("expected second model variants, got %+v", models[1].Thinking)
	}
}

func TestParseOpenCodeModelsMalformedVerboseBlockKeepsFollowingModels(t *testing.T) {
	input := `openai/gpt-5
{
  "id": "gpt-5",
  "reasoning": true,
  "variants": {
    "high": {}
  }
anthropic/claude-sonnet-4-6
{
  "id": "claude-sonnet-4-6",
  "reasoning": true,
  "variants": {
    "high": {},
    "max": {}
  }
}
`
	models := parseOpenCodeModels(input)
	if len(models) != 2 {
		t.Fatalf("expected both model rows to survive malformed JSON, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-5" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
	if models[0].Thinking != nil {
		t.Fatalf("malformed first JSON block should not annotate thinking: %+v", models[0].Thinking)
	}
	if models[1].ID != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("unexpected second model: %+v", models[1])
	}
	if models[1].Thinking == nil || len(models[1].Thinking.SupportedLevels) != 2 {
		t.Fatalf("valid following JSON block should still annotate thinking: %+v", models[1].Thinking)
	}
}

func TestDiscoverOpenCodeModelsFallsBackWhenVerboseFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fake := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$1" = "models" ] && [ "$2" = "--verbose" ]; then
  exit 2
fi
if [ "$1" = "models" ]; then
  cat <<'EOF'
PROVIDER/MODEL                     CONTEXT  MAX_OUT
openai/gpt-4o                      128000   16384
EOF
  exit 0
fi
exit 1
`
	writeTestExecutable(t, fake, []byte(script))

	models, err := discoverOpenCodeModels(context.Background(), Command{Path: fake})
	if err != nil {
		t.Fatalf("discoverOpenCodeModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected fallback non-verbose model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" || models[0].Thinking != nil {
		t.Fatalf("unexpected fallback model: %+v", models[0])
	}
}

// TestCachedDiscoveryDoesNotCacheEmpty verifies that an empty discovery result
// is not cached, so a transient failure (e.g. a `pi --list-models` timeout)
// doesn't keep the model picker blank for the full TTL. A non-empty result is
// still cached. See #3729.
func TestCachedDiscoveryDoesNotCacheEmpty(t *testing.T) {
	const emptyKey, nonEmptyKey = "test-cache-empty", "test-cache-nonempty"
	// modelCache is a package-level global; clear our keys up front and on
	// cleanup so the test stays hermetic under `go test -count=N` (a leftover
	// non-empty entry from a prior run would otherwise skip the callback).
	resetCache := func() {
		modelCacheMu.Lock()
		delete(modelCache, emptyKey)
		delete(modelCache, nonEmptyKey)
		modelCacheMu.Unlock()
	}
	resetCache()
	t.Cleanup(resetCache)

	emptyCalls := 0
	empty := func() (Catalog, error) {
		emptyCalls++
		return Catalog{Models: []Model{}}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := cachedDiscovery(emptyKey, empty)
		if err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
		if len(got.Models) != 0 {
			t.Fatalf("expected empty result, got %+v", got)
		}
	}
	if emptyCalls != 2 {
		t.Fatalf("empty result must not be cached: expected fn called 2x, got %d", emptyCalls)
	}

	nonEmptyCalls := 0
	nonEmpty := func() (Catalog, error) {
		nonEmptyCalls++
		return Catalog{Models: []Model{{ID: "provider/model"}}}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := cachedDiscovery(nonEmptyKey, nonEmpty); err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
	}
	if nonEmptyCalls != 1 {
		t.Fatalf("non-empty result must be cached: expected fn called 1x, got %d", nonEmptyCalls)
	}
}

func writeFakePiRPCModelsBinary(t *testing.T) string {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
if [ "$1" = "--mode" ] && [ "$2" = "rpc" ]; then
  IFS= read -r _state_request
  IFS= read -r _models_request
  printf '%s\n' '{"id":"multica-state","type":"response","command":"get_state","success":true,"data":{"model":{"id":"gpt-5.6-luna","name":"Luna","provider":"openai-multi","reasoning":true,"thinkingLevelMap":{"off":"none","minimal":"none","low":"low","medium":null,"high":"high","xhigh":"xhigh","max":"max"}},"thinkingLevel":"max"}}'
  printf '%s\n' '{"id":"multica-models","type":"response","command":"get_available_models","success":true,"data":{"models":[{"id":"gpt-5.6-sol","name":"Sol","provider":"openai-multi","reasoning":true},{"id":"gpt-5.6-luna","name":"Luna","provider":"openai-multi","reasoning":true,"thinkingLevelMap":{"off":"none","minimal":"none","low":"low","medium":null,"high":"high","xhigh":"xhigh","max":"max"}},{"id":"plain-chat","name":"Plain chat","provider":"openai-multi","reasoning":false}]}}'
  exit 0
fi
printf '%s\n' 'provider model context max-out thinking images'
printf '%s\n' 'fallback fallback-model 128K 8K yes no'
`
	writeTestExecutable(t, fakePath, []byte(script))
	return fakePath
}

func TestDiscoverPiModelsRPCThinkingCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}
	fakePath := writeFakePiRPCModelsBinary(t)

	models, err := discoverPiModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverPiModels: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 RPC models, got %d: %+v", len(models), models)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}

	sol := byID["openai-multi/gpt-5.6-sol"]
	if sol.Thinking == nil || !hasThinkingLevel(sol.Thinking, "medium") || hasThinkingLevel(sol.Thinking, "xhigh") || hasThinkingLevel(sol.Thinking, "max") {
		t.Fatalf("unexpected Sol thinking catalog: %+v", sol.Thinking)
	}
	luna := byID["openai-multi/gpt-5.6-luna"]
	if !luna.Default {
		t.Fatal("current Pi model must be marked as the runtime default")
	}
	if luna.Thinking == nil || luna.Thinking.DefaultLevel != "max" {
		t.Fatalf("unexpected Luna thinking default: %+v", luna.Thinking)
	}
	for _, level := range []string{"off", "minimal", "low", "high", "xhigh", "max"} {
		if !hasThinkingLevel(luna.Thinking, level) {
			t.Errorf("Luna missing level %q: %+v", level, luna.Thinking)
		}
	}
	if hasThinkingLevel(luna.Thinking, "medium") {
		t.Errorf("explicitly null Pi level must stay disabled: %+v", luna.Thinking)
	}
	if got := byID["openai-multi/plain-chat"].Thinking; got != nil {
		t.Fatalf("non-reasoning Pi model must not expose a picker: %+v", got)
	}
	if _, ok := byID["fallback/fallback-model"]; ok {
		t.Fatal("successful RPC discovery must not append the table fallback")
	}
}

func TestDiscoverPiModelsIDLessRPCErrorFallsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
if [ "$1" = "--mode" ] && [ "$2" = "rpc" ]; then
  IFS= read -r _state_request
  IFS= read -r _models_request
  printf '%s\n' '{"id":"multica-state","type":"response","command":"get_state","success":true,"data":{"thinkingLevel":"high"}}'
  printf '%s\n' '{"type":"response","command":"get_available_models","success":false,"error":"Unknown command: get_available_models"}'
  cat >/dev/null
  exit 0
fi
printf '%s\n' 'provider model context max-out thinking images'
printf '%s\n' 'fallback fallback-model 128K 8K yes no'
`
	writeTestExecutable(t, fakePath, []byte(script))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	models, err := discoverPiModels(ctx, Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverPiModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fallback/fallback-model" {
		t.Fatalf("ID-less RPC error must terminate that request and use the table fallback, got %+v", models)
	}
}

func TestDiscoverPiModelsHungRPCPreservesTableFallbackBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
if [ "$1" = "--mode" ] && [ "$2" = "rpc" ]; then
  IFS= read -r _state_request
  IFS= read -r _models_request
  exec sleep 30
fi
printf '%s\n' 'provider model context max-out thinking images'
printf '%s\n' 'fallback fallback-model 128K 8K yes no'
`
	writeTestExecutable(t, fakePath, []byte(script))

	started := time.Now()
	models, err := discoverPiModelsWithin(context.Background(), Command{Path: fakePath}, 100*time.Millisecond, time.Second)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("discoverPiModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fallback/fallback-model" {
		t.Fatalf("hung RPC must leave time for the table fallback, got %+v after %s", models, elapsed)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("RPC phase consumed the table fallback budget: elapsed %s", elapsed)
	}
}

func TestParsePiModels(t *testing.T) {
	input := `openai:gpt-4o
anthropic:claude-opus-4-7
openai:gpt-4o
bareword
`
	models := parsePiModels(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "openai/gpt-4o" {
		t.Errorf("expected colon normalized to slash: %+v", models[0])
	}
}

func TestParsePiModelsTableFormat(t *testing.T) {
	input := `provider             model                   context  max-out  thinking  images
bailian-coding-plan  glm-4.7                 202.8K   16.4K    no        no
bailian-coding-plan  qwen3.6-plus            1M       65.5K    no        yes
opencode             claude-sonnet-4-6       1M       64K      yes       yes
opencode             claude-sonnet-4-6:exp   1M       64K      yes       yes
opencode             claude-sonnet-4-6       1M       64K      yes       yes
bareword-only-line
`
	models := parsePiModels(input)
	if len(models) != 4 {
		t.Fatalf("expected 4 models (header skipped, duplicate deduped, bareword skipped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "bailian-coding-plan/glm-4.7" || models[0].Provider != "bailian-coding-plan" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "bailian-coding-plan/qwen3.6-plus" || models[1].Provider != "bailian-coding-plan" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
	if models[2].ID != "opencode/claude-sonnet-4-6" || models[2].Provider != "opencode" {
		t.Errorf("unexpected third model: %+v", models[2])
	}
	// Colon inside a model name in column 1 must be preserved — only
	// the legacy `provider:model` form gets colon→slash normalization.
	if models[3].ID != "opencode/claude-sonnet-4-6:exp" || models[3].Provider != "opencode" {
		t.Errorf("expected ':' inside table-format model name to be preserved: %+v", models[3])
	}
}

// TestParsePiModelsSkipsForkUsageHints pins the second half of GitHub #4482: a
// pi-family custom runtime profile can point at a fork with no `--list-models`,
// which exits printing usage text. Those lines carry no diagnostic prefix, so
// the field splitter used to coin them into models like `Run/`omp`. An empty
// catalog is the correct answer — the UI falls back to manual entry, which is
// strictly better than offering IDs the CLI will reject.
func TestParsePiModelsSkipsForkUsageHints(t *testing.T) {
	input := "Error: unknown flag: --list-models\n" +
		"Run `omp --help` for available flags.\n" +
		"Usage: omp [command]\n" +
		"unknown command \"models\" for \"omp\"\n"

	if models := parsePiModels(input); len(models) != 0 {
		t.Fatalf("expected usage text to yield no models, got %+v", models)
	}
}

// TestParsePiModelsKeepsCatalogAlongsideUsageHints pins that the widened noise
// filter only drops the prose: a real catalog printed next to a usage hint
// still parses. Without this the #3729 behaviour (catalog on a non-zero exit)
// could be silently traded away for the #4482 fix.
func TestParsePiModelsKeepsCatalogAlongsideUsageHints(t *testing.T) {
	input := "Run `omp --help` for available flags.\n" +
		"provider  model    context\n" +
		"opencode  glm-4.7  202.8K\n"

	models := parsePiModels(input)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "opencode/glm-4.7" {
		t.Errorf("unexpected model: %+v", models[0])
	}
}

// TestDiscoverPiModelsNonZeroExit verifies that discoverPiModels still returns
// the resolvable catalog when `pi --list-models` exits non-zero. Pi exits
// non-zero (and warns) when an agent config references stale provider/model
// patterns that no longer match the local catalog. Before the fix the daemon
// discarded the populated output on any non-zero exit and returned an empty
// list, so the UI model picker was blank even though the runtime was online and
// agents ran fine. See GitHub #3729.
func TestDiscoverPiModelsNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pi binary is a /bin/sh script")
	}

	const table = "provider         model        context  max-out  thinking  images\n" +
		"glm-coding-plan  glm-4.7      202.8K   16.4K    no        no"
	// The unmatched-pattern warning, with and without the `Warning:` prefix —
	// the prefix is not guaranteed across pi versions, and the bare form is
	// what slips past a naive guard into a bogus `No/models` model.
	const prefixed = `Warning: No models match pattern "opencode-go/mimo-v2-omni"`
	const bare = `No models match pattern "opencode-go/mimo-v2-pro"`

	cases := []struct {
		name   string
		script string
	}{
		{
			// Newer pi prints the catalog to stdout; the stale-pattern
			// warning goes to stderr and the process exits non-zero.
			name: "catalog on stdout",
			script: "#!/bin/sh\n" +
				"cat <<'EOF'\n" + table + "\nEOF\n" +
				"echo " + strconv.Quote(prefixed) + " >&2\n" +
				"exit 1\n",
		},
		{
			// Older pi prints the catalog (and the warning) to stderr; same
			// non-zero exit. The stderr fallback must still parse the catalog.
			name: "catalog and prefixed warning on stderr",
			script: "#!/bin/sh\n" +
				"cat >&2 <<'EOF'\n" + table + "\n" + prefixed + "\nEOF\n" +
				"exit 1\n",
		},
		{
			// Same, but the warning has no `Warning:` prefix — must not leak in
			// as a `No/models` row.
			name: "catalog and bare warning on stderr",
			script: "#!/bin/sh\n" +
				"cat >&2 <<'EOF'\n" + table + "\n" + bare + "\nEOF\n" +
				"exit 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakePath := filepath.Join(t.TempDir(), "pi")
			writeTestExecutable(t, fakePath, []byte(tc.script))

			models, err := discoverPiModels(context.Background(), Command{Path: fakePath})
			if err != nil {
				t.Fatalf("discoverPiModels: %v", err)
			}
			// Exactly the resolvable model — no warning line coined into a
			// bogus entry, no header row.
			if len(models) != 1 || models[0].ID != "glm-coding-plan/glm-4.7" {
				t.Fatalf("expected exactly [glm-coding-plan/glm-4.7] despite non-zero exit, got %+v", models)
			}
			if models[0].Thinking != nil {
				t.Fatalf("human-table fallback must not guess thinking levels: %+v", models[0].Thinking)
			}
		})
	}
}

// TestDiscoverOpenCodeModelsFallsBackOnVerboseNoise verifies that a non-zero
// `opencode models --verbose` whose stdout is unparseable noise still falls
// back to the plain `opencode models` command instead of returning empty. The
// earlier fix skipped the fallback whenever verbose printed any bytes, which
// regressed this case. Mirrors the pi hardening in #3729.
func TestDiscoverOpenCodeModelsFallsBackOnVerboseNoise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode binary is a /bin/sh script")
	}

	// `opencode models --verbose` => $2 == "--verbose": emit noise + exit 1.
	// `opencode models`           => no $2: print the plain catalog.
	script := "#!/bin/sh\n" +
		"if [ \"$2\" = \"--verbose\" ]; then\n" +
		"  echo 'panic: catalog sync failed'\n" +
		"  exit 1\n" +
		"fi\n" +
		"echo 'openai/gpt-4o'\n"

	fakePath := filepath.Join(t.TempDir(), "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	models, err := discoverOpenCodeModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverOpenCodeModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "openai/gpt-4o" {
		t.Fatalf("expected fallback to plain `opencode models` to yield [openai/gpt-4o], got %+v", models)
	}
}

func TestParseOpenclawAgents(t *testing.T) {
	input := `deepseek-v4   deepseek-v4
claude-sonnet claude-sonnet-4-6
deepseek-v4   deepseek-v4
`
	models := parseOpenclawAgents(input)
	// duplicate deduped; label includes model name.
	if len(models) != 2 {
		t.Fatalf("expected 2 agents, got %d: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4" {
		t.Errorf("unexpected first agent: %+v", models[0])
	}
	if models[0].Label != "deepseek-v4 (deepseek-v4)" {
		t.Errorf("unexpected label: %+v", models[0])
	}
	if models[0].Provider != "openclaw" {
		t.Errorf("expected provider openclaw, got %q", models[0].Provider)
	}
}

func TestParseOpenclawAgentsRejectsDecoratedTUI(t *testing.T) {
	// Reproduces the shape of real `openclaw agents list` output
	// that leaked header tokens like "Identity:" / "Workspace:"
	// and single-character box-drawing icons into the dropdown.
	input := `╭───────────────────────────────╮
│                               │
│  ◇  Agents:                   │
│  │                            │
│  │    Identity:               │
│  │    Workspace:              │
│  │    Agent                   │
│  │                            │
╰───────────────────────────────╯
deepseek-v4   deepseek-v4
claude-sonnet claude-sonnet-4-6
`
	models := parseOpenclawAgents(input)
	if len(models) != 2 {
		t.Fatalf("expected 2 agents (decoration skipped), got %d: %+v", len(models), models)
	}
	for _, m := range models {
		if strings.HasSuffix(m.ID, ":") {
			t.Errorf("section header leaked into result: %+v", m)
		}
	}
	if models[0].ID != "deepseek-v4" || models[1].ID != "claude-sonnet" {
		t.Errorf("unexpected agents: %+v", models)
	}
}

func TestParseOpenclawAgentsJSONArray(t *testing.T) {
	input := []byte(`[
    {"name": "deepseek-v4", "model": "deepseek-v4"},
    {"name": "claude-sonnet", "model": "claude-sonnet-4-6"}
]`)
	models, ok := parseOpenclawAgentsJSON(input)
	if !ok {
		t.Fatal("expected parseOpenclawAgentsJSON to accept an array")
	}
	if len(models) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4" || models[0].Label != "deepseek-v4 (deepseek-v4)" {
		t.Errorf("unexpected first entry: %+v", models[0])
	}
}

func TestParseOpenclawAgentsJSONWrapped(t *testing.T) {
	input := []byte(`{"agents": [{"name": "foo", "model": "bar"}]}`)
	models, ok := parseOpenclawAgentsJSON(input)
	if !ok {
		t.Fatal("expected parseOpenclawAgentsJSON to accept wrapped object")
	}
	if len(models) != 1 || models[0].ID != "foo" {
		t.Errorf("unexpected: %+v", models)
	}
}

func TestOpenclawEntriesToModelsUsesIDOverName(t *testing.T) {
	// When both id and name are present, Model.ID should use the id field
	// because openclaw resolves --agent by id. Names with spaces (e.g.
	// "Sub2API OPS") would be mangled by openclaw's normalizeAgentId.
	input := []byte(`[{"id": "sub2api", "name": "Sub2API OPS", "model": "gpt-4o"}]`)
	models, ok := parseOpenclawAgentsJSON(input)
	if !ok {
		t.Fatal("expected parseOpenclawAgentsJSON to accept array")
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].ID != "sub2api" {
		t.Errorf("Model.ID = %q, want %q (should use id, not name)", models[0].ID, "sub2api")
	}
	if models[0].Label != "Sub2API OPS (gpt-4o)" {
		t.Errorf("Model.Label = %q, want %q (should use name for display)", models[0].Label, "Sub2API OPS (gpt-4o)")
	}
}

func TestParseOpenclawAgentsJSONRejectsGarbage(t *testing.T) {
	if _, ok := parseOpenclawAgentsJSON([]byte("not json")); ok {
		t.Error("expected ok=false for non-JSON")
	}
}

func TestParseCursorModels(t *testing.T) {
	input := `Available models

auto - Auto
composer-2-fast - Composer 2 Fast (current, default)
composer-2 - Composer 2
claude-4.6-sonnet-medium - Sonnet 4.6 1M
claude-opus-4-7-high - Opus 4.7 1M
gemini-3.1-pro - Gemini 3.1 Pro
`
	models := parseCursorModels(input)
	if len(models) != 6 {
		t.Fatalf("expected 6 models, got %d: %+v", len(models), models)
	}
	ids := map[string]Model{}
	for _, m := range models {
		ids[m.ID] = m
	}
	for _, want := range []string{"auto", "composer-2-fast", "composer-2", "claude-4.6-sonnet-medium", "claude-opus-4-7-high", "gemini-3.1-pro"} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing expected model %q in: %+v", want, models)
		}
	}
	if def := ids["composer-2-fast"]; !def.Default {
		t.Errorf("composer-2-fast should be marked default, got %+v", def)
	}
	if def := ids["composer-2-fast"]; def.Label != "Composer 2 Fast" {
		t.Errorf("default label should be stripped of parenthetical, got %q", def.Label)
	}
	// Non-default entry should not carry Default=true.
	if auto := ids["auto"]; auto.Default {
		t.Errorf("non-default entry should not be flagged default: %+v", auto)
	}
}

func TestParseCursorModelsSkipsHeaderAndBlankLines(t *testing.T) {
	input := `Available models

composer-2 - Composer 2
`
	models := parseCursorModels(input)
	if len(models) != 1 || models[0].ID != "composer-2" {
		t.Fatalf("unexpected: %+v", models)
	}
}

func TestParseHermesSessionNewModels(t *testing.T) {
	// Mirrors the real shape emitted by hermes'
	// acp_adapter/server.py _build_model_state -> SessionModelState.
	raw := []byte(`{
      "sessionId": "ses_123",
      "models": {
        "availableModels": [
          {"modelId": "nous:moonshotai/kimi-k2.5", "name": "moonshotai/kimi-k2.5", "description": "Provider: Nous"},
          {"modelId": "nous:anthropic/claude-opus-4.7", "name": "anthropic/claude-opus-4.7", "description": "Provider: Nous • current"},
          {"modelId": "nous:moonshotai/kimi-k2.5", "name": "duplicate", "description": "dup"}
        ],
        "currentModelId": "nous:anthropic/claude-opus-4.7"
      }
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (duplicate deduped), got %d: %+v", len(models), models)
	}
	if models[0].ID != "nous:moonshotai/kimi-k2.5" || models[0].Provider != "nous" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[0].Default {
		t.Errorf("non-current entry must not be marked default: %+v", models[0])
	}
	if !models[1].Default {
		t.Errorf("current entry must be marked default: %+v", models[1])
	}
	if models[1].ID != "nous:anthropic/claude-opus-4.7" {
		t.Errorf("expected current model second: %+v", models[1])
	}
}

func TestParseHermesSessionNewModelsPreservesCustomModelIDsWithColons(t *testing.T) {
	raw := []byte(`{
      "sessionId": "ses_123",
      "models": {
        "availableModels": [
          {"modelId": "custom:lfm2.5:8b", "name": "lfm2.5:8b", "description": "Provider: Custom"}
        ],
        "currentModelId": "custom:lfm2.5:8b"
      }
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d: %+v", len(models), models)
	}
	if models[0].ID != "custom:lfm2.5:8b" {
		t.Errorf("model id must be preserved verbatim, got %+v", models[0])
	}
	if models[0].Provider != "custom" {
		t.Errorf("provider should be derived from the first colon only, got %+v", models[0])
	}
	if !models[0].Default {
		t.Errorf("current custom model should be marked default: %+v", models[0])
	}
}

func TestParseHermesSessionNewModelsSnakeCaseAndUnknownNames(t *testing.T) {
	raw := []byte(`{
      "session_id": "ses_123",
      "models": {
        "available_models": [
          {"model_id": "nous:moonshotai/kimi-k2.6", "name": "Unknown", "description": "Provider: Nous"},
          {"model_id": "nous:anthropic/claude-sonnet-4.6", "name": "unknown", "description": "Provider: Nous"}
        ],
        "current_model_id": "nous:moonshotai/kimi-k2.6"
      }
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].Label != "nous:moonshotai/kimi-k2.6" {
		t.Errorf("Unknown label should fall back to model id, got %+v", models[0])
	}
	if !models[0].Default {
		t.Errorf("snake_case current_model_id should mark default: %+v", models[0])
	}
	if models[1].Label != "nous:anthropic/claude-sonnet-4.6" {
		t.Errorf("lowercase unknown label should fall back to model id, got %+v", models[1])
	}
}

func TestParseHermesSessionNewModelsMissingField(t *testing.T) {
	// session/new without the models field — older hermes or
	// failed _build_model_state — should yield nil so the caller
	// can distinguish "no catalog" from "empty catalog".
	raw := []byte(`{"sessionId": "ses_123"}`)
	if got := parseACPSessionNewModels(raw); got != nil && len(got) != 0 {
		t.Errorf("expected nil/empty, got %+v", got)
	}
}

func TestParseHermesSessionNewModelsGarbage(t *testing.T) {
	if got := parseACPSessionNewModels([]byte("not json")); got != nil {
		t.Errorf("expected nil for non-JSON, got %+v", got)
	}
}

// MUL-5239: kimi-code 0.29 dropped the `models` block and advertises the
// same catalog through ACP `configOptions`. Without this the picker showed
// an empty catalog for an online kimi runtime.
func TestParseACPSessionNewModelsFromConfigOptions(t *testing.T) {
	// Trimmed copy of a real kimi 0.29 session/new result.
	raw := []byte(`{
      "sessionId": "session_abc",
      "configOptions": [
        {
          "type": "select",
          "id": "model",
          "name": "Model",
          "category": "model",
          "currentValue": "kimi-code/k3",
          "options": [
            {"value": "kimi-code/kimi-for-coding", "name": "K2.7 Coding"},
            {"value": "kimi-code/kimi-for-coding-highspeed", "name": "K2.7 Coding Highspeed"},
            {"value": "kimi-code/k3", "name": "K3"}
          ]
        },
        {
          "type": "select",
          "id": "thinking",
          "category": "thought_level",
          "currentValue": "high",
          "options": [
            {"value": "low", "name": "Low"},
            {"value": "high", "name": "High"},
            {"value": "max", "name": "Max"}
          ]
        }
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 3 {
		t.Fatalf("expected 3 models from configOptions, got %d: %+v", len(models), models)
	}
	if models[0].ID != "kimi-code/kimi-for-coding" || models[0].Label != "K2.7 Coding" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	// `kimi-code/k3` has no colon, so it must not be split into a provider
	// group off the slash.
	if models[2].Provider != "" {
		t.Errorf("slash-form model id must not derive a provider: %+v", models[2])
	}
	if !models[2].Default {
		t.Errorf("currentValue entry must be marked default: %+v", models[2])
	}
	for _, m := range models {
		if m.ID == "low" || m.ID == "high" || m.ID == "max" {
			t.Errorf("thinking-level option leaked into the model catalog: %+v", m)
		}
	}
}

// The `models` block stays authoritative: an agent emitting both shapes
// must not have its catalog replaced by configOptions.
func TestParseACPSessionNewModelsPrefersModelsBlockOverConfigOptions(t *testing.T) {
	raw := []byte(`{
      "sessionId": "session_abc",
      "models": {
        "availableModels": [{"modelId": "nous:anthropic/claude-opus-4.7", "name": "Opus"}],
        "currentModelId": "nous:anthropic/claude-opus-4.7"
      },
      "configOptions": [
        {"id": "model", "category": "model", "currentValue": "other/one",
         "options": [{"value": "other/one", "name": "Other"}]}
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 1 || models[0].ID != "nous:anthropic/claude-opus-4.7" {
		t.Fatalf("models block must win over configOptions, got %+v", models)
	}
}

func TestParseACPSessionNewModelsConfigOptionsSnakeCaseAndCategoryOnly(t *testing.T) {
	// No `id: "model"` — the option is identified by category alone — and
	// the response uses snake_case keys.
	raw := []byte(`{
      "session_id": "session_abc",
      "config_options": [
        {
          "id": "primary_model",
          "category": "MODEL",
          "current_value": "kimi-code/k3",
          "options": [
            {"value": "kimi-code/k3", "name": "K3"},
            {"value": "kimi-code/k3", "name": "duplicate"},
            {"value": "  ", "name": "blank"},
            {"value": "kimi-code/kimi-for-coding", "name": ""}
          ]
        }
      ]
    }`)
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (duplicate and blank dropped), got %d: %+v", len(models), models)
	}
	if !models[0].Default {
		t.Errorf("snake_case current_value should mark default: %+v", models[0])
	}
	if models[1].Label != "kimi-code/kimi-for-coding" {
		t.Errorf("missing name should fall back to the model id, got %+v", models[1])
	}
}

func TestParseACPSessionNewModelsIgnoresNonModelConfigOptions(t *testing.T) {
	// session/new with configOptions but no model picker must still yield an
	// empty catalog rather than inventing one from the thinking levels.
	raw := []byte(`{
      "sessionId": "session_abc",
      "configOptions": [
        {"id": "thinking", "category": "thought_level", "currentValue": "high",
         "options": [{"value": "low", "name": "Low"}, {"value": "high", "name": "High"}]}
      ]
    }`)
	if got := parseACPSessionNewModels(raw); len(got) != 0 {
		t.Errorf("expected empty catalog, got %+v", got)
	}
}

func TestACPResultTopLevelKeys(t *testing.T) {
	// Diagnostic line must expose key names only — never the values that
	// carry session ids or catalog contents.
	keys := acpResultTopLevelKeys([]byte(`{"sessionId":"session_secret","configOptions":[],"modes":{}}`))
	got := strings.Join(keys, ",")
	if got != "configOptions,modes,sessionId" {
		t.Errorf("unexpected keys: %q", got)
	}
	if acpResultTopLevelKeys([]byte("not json")) != nil {
		t.Error("expected nil for non-JSON result")
	}
}

// TestParseAntigravityModels covers the legacy single-column `agy models`
// format (pre-1.1.11): each non-blank tab-free line becomes a Model whose ID
// and Label are that verbatim value, duplicates collapse, and blanks drop.
func TestParseAntigravityModels(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"Gemini 3.5 Flash (Medium)",
		"Claude Opus 4.6 (Thinking)",
		"", // blank line — skipped
		"GPT-OSS 120B (Medium)",
		"Claude Opus 4.6 (Thinking)", // duplicate — collapsed
	}, "\n")

	got := parseAntigravityModels(out)
	want := []Model{
		{ID: "Gemini 3.5 Flash (Medium)", Label: "Gemini 3.5 Flash (Medium)", Provider: "antigravity"},
		{ID: "Claude Opus 4.6 (Thinking)", Label: "Claude Opus 4.6 (Thinking)", Provider: "antigravity"},
		{ID: "GPT-OSS 120B (Medium)", Label: "GPT-OSS 120B (Medium)", Provider: "antigravity"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseAntigravityModels len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("model[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestParseAntigravityModelsTabSeparated covers the catalog format introduced
// by agy 1.1.11: the first tab-delimited field is the value accepted by
// --model, while the second field is the human-readable picker label.
func TestParseAntigravityModelsTabSeparated(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"gemini-3.6-flash-high\tGemini 3.6 Flash (High)",
		"claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)\tfuture metadata is ignored",
		"gemini-3.6-flash-high\tDuplicate label is ignored",
	}, "\n")

	got := parseAntigravityModels(out)
	want := []Model{
		{ID: "gemini-3.6-flash-high", Label: "Gemini 3.6 Flash (High)", Provider: "antigravity"},
		{ID: "claude-opus-4-6-thinking", Label: "Claude Opus 4.6 (Thinking)", Provider: "antigravity"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseAntigravityModels = %+v, want %+v", got, want)
	}

	if err := antigravityModelError("gemini-3.6-flash-high", got); err != nil {
		t.Fatalf("exact model slug from tab-separated catalog was rejected: %v", err)
	}
}

// TestParseAntigravityModelsEmpty pins that empty / whitespace-only output
// yields no models (so cachedDiscovery treats it as a transient miss and
// retries rather than caching a blank catalog).
func TestParseAntigravityModelsEmpty(t *testing.T) {
	t.Parallel()
	if got := parseAntigravityModels("   \n\t\n"); len(got) != 0 {
		t.Errorf("expected no models for blank output, got %+v", got)
	}
}

func TestCachedDiscovery(t *testing.T) {
	calls := 0
	fn := func() (Catalog, error) {
		calls++
		return Catalog{Models: []Model{{ID: "x", Label: "x"}}}, nil
	}
	// First call populates the cache; reset for isolation.
	modelCacheMu.Lock()
	delete(modelCache, "testkey")
	modelCacheMu.Unlock()

	if _, err := cachedDiscovery("testkey", fn); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedDiscovery("testkey", fn); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected 1 underlying call due to cache, got %d", calls)
	}
}

// TestDiscoveryCacheKeyIsolatesByExecutable verifies that two different
// executable paths for the same provider type produce different cache keys,
// so a built-in and a custom Dim-compatible executable do not serve each
// other's model catalog during the TTL.
func TestDiscoveryCacheKeyIsolatesByExecutable(t *testing.T) {
	key1 := discoveryCacheKey("dim", Command{Path: "/usr/bin/dim"})
	key2 := discoveryCacheKey("dim", Command{Path: "/opt/custom/dim"})
	if key1 == key2 {
		t.Fatalf("different executables must produce different cache keys: both %q", key1)
	}
	keyEmpty := discoveryCacheKey("dim", Command{Path: ""})
	if keyEmpty != "dim" {
		t.Fatalf("empty executable should fall back to provider type, got %q", keyEmpty)
	}
	if keyEmpty == key1 {
		t.Fatal("empty executable key must differ from a non-empty executable key")
	}

	calls := 0
	fn := func() (Catalog, error) {
		calls++
		return Catalog{Models: []Model{{ID: "m"}}}, nil
	}
	modelCacheMu.Lock()
	delete(modelCache, key1)
	delete(modelCache, key2)
	modelCacheMu.Unlock()

	if _, err := cachedDiscovery(key1, fn); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedDiscovery(key2, fn); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("expected 2 underlying calls (one per executable), got %d", calls)
	}
}

// TestQualifyModelID covers GH #7300: a persisted model id that
// omits its provider must be promoted to the catalog's canonical selector,
// but only when exactly one provider claims it. opencode rejects an
// unqualified id outright, and every capability lookup keyed on the model id
// misses until the two agree.
func TestQualifyModelID(t *testing.T) {
	t.Parallel()

	gateway := []Model{
		{ID: "multica-anthropic/claude/claude-opus-5", Provider: "multica-anthropic"},
		{ID: "multica-anthropic/claude/claude-sonnet-5", Provider: "multica-anthropic"},
		{ID: "multica-codex/codex/gpt-5.6-sol", Provider: "multica-codex"},
	}

	tests := []struct {
		name          string
		catalog       Catalog
		model         string
		want          string
		wantRewritten bool
	}{
		{
			name:          "slash-shaped id gains its provider",
			catalog:       Catalog{Models: gateway},
			model:         "claude/claude-opus-5",
			want:          "multica-anthropic/claude/claude-opus-5",
			wantRewritten: true,
		},
		{
			name:    "already canonical is left alone",
			catalog: Catalog{Models: gateway},
			model:   "multica-anthropic/claude/claude-opus-5",
			want:    "multica-anthropic/claude/claude-opus-5",
		},
		{
			name: "an exact catalog id wins over a qualifiable one",
			// Both a bare `shared-id` model and a `vendor/shared-id` model
			// exist. The exact match is what the user picked; promoting it to
			// the other provider's entry would silently reroute the task.
			catalog: Catalog{Models: []Model{
				{ID: "shared-id", Provider: ""},
				{ID: "vendor/shared-id", Provider: "vendor"},
			}},
			model: "shared-id",
			want:  "shared-id",
		},
		{
			name: "ambiguous across providers stays untouched",
			catalog: Catalog{Models: []Model{
				{ID: "gateway-a/claude/claude-opus-5", Provider: "gateway-a"},
				{ID: "gateway-b/claude/claude-opus-5", Provider: "gateway-b"},
			}},
			model: "claude/claude-opus-5",
			want:  "claude/claude-opus-5",
		},
		{
			name:    "unknown model is passed through for the CLI to judge",
			catalog: Catalog{Models: gateway},
			model:   "something-nobody-advertises",
			want:    "something-nobody-advertises",
		},
		{
			name:    "empty catalog cannot qualify anything",
			catalog: Catalog{},
			model:   "claude/claude-opus-5",
			want:    "claude/claude-opus-5",
		},
		{
			// A static stand-in is not what the runtime actually supports, so
			// promoting against it would invent an id the CLI never advertised.
			name:    "a fallback catalog is never authoritative",
			catalog: Catalog{Models: gateway, Fallback: true},
			model:   "claude/claude-opus-5",
			want:    "claude/claude-opus-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, rewritten := QualifyModelID(tt.catalog, tt.model)
			if got != tt.want || rewritten != tt.wantRewritten {
				t.Errorf("QualifyModelID(%q) = (%q, %v), want (%q, %v)",
					tt.model, got, rewritten, tt.want, tt.wantRewritten)
			}
		})
	}
}

// A blank model means "let the runtime pick its own default" — there is
// nothing to qualify, and the runtime resolves its own selection.
func TestQualifyModelIDIgnoresBlankModel(t *testing.T) {
	t.Parallel()
	catalog := Catalog{Models: []Model{{ID: "vendor/only-model", Provider: "vendor"}}}
	got, rewritten := QualifyModelID(catalog, "   ")
	if got != "" || rewritten {
		t.Errorf("QualifyModelID(blank) = (%q, %v), want (\"\", false)", got, rewritten)
	}
}

// TestSlashShapedPiModelKeepsItsThinkingCatalog walks the chain that GH #7300
// reported as a dropped thinking_level: pi's RPC catalog carries a
// gateway-style model whose own id contains a slash, the agent persisted that
// bare id, and every capability lookup keyed on it missed. Qualifying the id
// first is what puts the persisted value back on the catalog entry that
// actually advertises the levels.
func TestSlashShapedPiModelKeepsItsThinkingCatalog(t *testing.T) {
	t.Parallel()

	// Verbatim shape of a real `get_available_models` RPC response for the
	// reporter's models.json.
	raw := []piRPCModel{
		{ID: "claude/claude-opus-5", Name: "Claude Opus 5", Provider: "multica-anthropic", Reasoning: true},
		{ID: "claude/claude-sonnet-5", Name: "Claude Sonnet 5", Provider: "multica-anthropic", Reasoning: true},
	}
	models := piModelsFromRPC(raw, piRPCState{})

	qualified, rewritten := QualifyModelID(Catalog{Models: models}, "claude/claude-opus-5")
	if !rewritten || qualified != "multica-anthropic/claude/claude-opus-5" {
		t.Fatalf("qualified = (%q, %v), want (%q, true)",
			qualified, rewritten, "multica-anthropic/claude/claude-opus-5")
	}

	var thinking *ModelThinking
	for _, m := range models {
		if m.ID == qualified {
			thinking = m.Thinking
		}
	}
	if thinking == nil {
		t.Fatalf("qualified model %q advertises no thinking catalog", qualified)
	}
	if !piThinkingSupports(thinking, "high") {
		t.Errorf("thinking catalog for %q missing \"high\": %+v", qualified, thinking.SupportedLevels)
	}
}

// TestModelSelectorMustBeProviderQualifiedIsAnExecutionContract pins what the
// predicate actually claims: whether a runtime's CLI refuses a model id that
// carries no provider prefix. It is deliberately NOT a statement about catalog
// shape — several runtimes (pi, omp, deveco) emit provider-prefixed ids, but
// only the ones whose CLI *rejects* the unprefixed form justify spending a
// discovery subprocess before launch.
//
// The pi entries are the load-bearing ones: buildPiArgs and its tests prove pi
// resolves a canonical selector, a bare id, and an id containing a slash all
// on its own, so a pi task must launch without the daemon reading any catalog.
func TestModelSelectorMustBeProviderQualifiedIsAnExecutionContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		want     bool
		why      string
	}{
		{"opencode", true, "run --model resolves strictly through provider/model"},
		{"deveco", true, "opencode fork with the same --model contract"},
		{"pi", false, "pi's own resolver accepts bare and slash-shaped ids"},
		{"omp", false, "pi-family fork, inherits pi's resolver"},
		{"claude", false, "bare model ids, no provider segment to miss"},
		{"codex", false, "bare model ids"},
		{"copilot", false, "bare model ids under a display-name provider"},
		{"dsh", false, "resolves its own provider-prefixed ids"},
		{"", false, "unknown provider must not spend discovery"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			if got := ModelSelectorMustBeProviderQualified(tt.provider); got != tt.want {
				t.Errorf("ModelSelectorMustBeProviderQualified(%q) = %v, want %v (%s)",
					tt.provider, got, tt.want, tt.why)
			}
		})
	}
}
