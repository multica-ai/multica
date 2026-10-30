//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codebuddyACPSessionResult is the shape CodeBuddy 2.130.0 actually returns from
// `session/new` over `codebuddy --acp`, trimmed to four models. Captured from the
// real CLI: the catalog lives under models.availableModels with an advertised
// currentModelId, and the effort catalog rides along in configOptions as
// thought_level — including the `enabled` choice that is a session toggle rather
// than a valid `--effort` argument.
const codebuddyACPSessionResult = `{"sessionId":"ses-codebuddy","models":{"currentModelId":"hy3",` +
	`"availableModels":[` +
	`{"modelId":"hy3","name":"Hy3","description":"x0.00 credits"},` +
	`{"modelId":"glm-5.2","name":"GLM-5.2","description":"x0.79 credits"},` +
	`{"modelId":"kimi-k3-1","name":"Kimi-K3","description":"x1.62 credits"},` +
	`{"modelId":"deepseek-v3-2-volc","name":"DeepSeek-V3.2","description":"x0.29 credits"}]},` +
	`"configOptions":[` +
	`{"type":"select","id":"mode","name":"Permission Mode","currentValue":"default","options":[{"value":"default","name":"Default"}]},` +
	`{"type":"select","id":"thought_level","name":"Deep Thinking","category":"thought_level","currentValue":"enabled","options":[` +
	`{"value":"minimal","name":"Minimal"},{"value":"low","name":"Low"},{"value":"medium","name":"Medium"},` +
	`{"value":"high","name":"High"},{"value":"xhigh","name":"X-High"},{"value":"max","name":"Max"},` +
	`{"value":"enabled","name":"On (default)"}]}]}`

// writeCodebuddyACPStub writes an executable stub that speaks just enough ACP for
// discovery. sessionResult is returned from session/new; when it is empty the
// stub fails that call, which is how a not-logged-in CLI is simulated.
func writeCodebuddyACPStub(t *testing.T, sessionResult string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codebuddy")
	sessionReply := `printf '{"jsonrpc":"2.0","id":%s,"result":` + sessionResult + `}\n' "$id"`
	if sessionResult == "" {
		sessionReply = `printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"not authenticated"}}\n' "$id"`
	}
	script := `#!/bin/sh
# Minimal CodeBuddy ACP stub: initialize + session/new only, which is all model
# discovery drives. --version answers so the runtime-registration probe is happy.
case "$1" in
  --version) echo '2.130.0'; exit 0 ;;
esac
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true},"authMethods":[{"id":"external","name":"Login with Google/Github"}]}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      ` + sessionReply + `
      exit 0
      ;;
  esac
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write codebuddy ACP stub: %v", err)
	}
	return path
}

func resetCodebuddyDiscoveryCaches(t *testing.T) {
	t.Helper()
	clear := func() {
		modelCacheMu.Lock()
		delete(modelCache, "codebuddy")
		modelCacheMu.Unlock()
		resetThinkingCacheForTests()
	}
	clear()
	t.Cleanup(clear)
}

// TestDiscoverCodebuddyModelsFromACP is the core of the migration (MUL-5549):
// the catalog now comes from the ACP handshake, so IDs, display names AND the
// default model all come from CodeBuddy instead of being guessed from the ID.
func TestDiscoverCodebuddyModelsFromACP(t *testing.T) {
	resetCodebuddyDiscoveryCaches(t)
	path := writeCodebuddyACPStub(t, codebuddyACPSessionResult)

	catalog, err := discoverCodebuddyModels(context.Background(), Command{Path: path})
	if err != nil {
		t.Fatalf("discoverCodebuddyModels: %v", err)
	}
	if catalog.Fallback {
		t.Fatal("a successful ACP handshake must not be marked Fallback")
	}
	if len(catalog.Models) != 4 {
		t.Fatalf("expected 4 models, got %d: %+v", len(catalog.Models), catalog.Models)
	}

	// Labels are the CLI's own names. The old --help path had to derive these
	// from the ID and got them wrong: kimi-k3-1 became "Kimi K3 1" and
	// deepseek-v3-2-volc became "Deepseek V3 2 Volc".
	wantLabel := map[string]string{
		"hy3":                "Hy3",
		"glm-5.2":            "GLM-5.2",
		"kimi-k3-1":          "Kimi-K3",
		"deepseek-v3-2-volc": "DeepSeek-V3.2",
	}
	for _, m := range catalog.Models {
		if want, ok := wantLabel[m.ID]; !ok {
			t.Errorf("unexpected model %q", m.ID)
		} else if m.Label != want {
			t.Errorf("label(%q) = %q, want the CLI's own %q", m.ID, m.Label, want)
		}
	}

	// The default is the advertised currentModelId, not "whichever came first".
	var defaults []string
	for _, m := range catalog.Models {
		if m.Default {
			defaults = append(defaults, m.ID)
		}
	}
	if len(defaults) != 1 || defaults[0] != "hy3" {
		t.Errorf("default models = %v, want exactly [hy3] from currentModelId", defaults)
	}

	// The ACP payload has no vendor field and CodeBuddy's ids are bare, so
	// without the prefix post-pass every model would land in one unlabelled
	// group instead of the picker's per-vendor sections.
	wantProvider := map[string]string{
		"hy3":                "hunyuan",
		"glm-5.2":            "zhipu",
		"kimi-k3-1":          "kimi",
		"deepseek-v3-2-volc": "deepseek",
	}
	for _, m := range catalog.Models {
		if want := wantProvider[m.ID]; m.Provider != want {
			t.Errorf("provider(%q) = %q, want %q — the picker groups on this", m.ID, m.Provider, want)
		}
	}
}

// TestCodebuddyModelProviderCoversRealCatalog pins vendor inference against every
// ID shape CodeBuddy 2.130.0 actually advertises. A miss here is invisible in the
// backend but collapses the picker into one unlabelled list.
func TestCodebuddyModelProviderCoversRealCatalog(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		// Live ACP catalog, all 16 ids.
		"hy3": "hunyuan", "glm-5.2": "zhipu", "glm-5.1": "zhipu", "glm-5.0": "zhipu",
		"glm-5.0-turbo": "zhipu", "glm-5v-turbo": "zhipu", "glm-4.7": "zhipu",
		"minimax-m3": "minimax", "minimax-m2.7": "minimax",
		"kimi-k3-1": "kimi", "kimi-k2.7": "kimi", "kimi-k2.6": "kimi", "kimi-k2.5": "kimi",
		"deepseek-v4-pro": "deepseek", "deepseek-v4-flash": "deepseek", "deepseek-v3-2-volc": "deepseek",
		// Static fallback ids, which must group too.
		"claude-sonnet-4.6": "anthropic", "claude-opus-4.7": "anthropic",
		"gemini-3.1-pro": "google", "gpt-5.5": "openai",
		"deepseek-v3-2-volc-ioa": "deepseek",
	} {
		if got := codebuddyModelProvider(id); got != want {
			t.Errorf("codebuddyModelProvider(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestCodebuddyStaticModelsCarryProviders guards the fallback path's grouping:
// those entries hardcode Provider rather than going through the post-pass, so a
// new entry added without one would silently break grouping there instead.
func TestCodebuddyStaticModelsCarryProviders(t *testing.T) {
	t.Parallel()
	for _, m := range codebuddyStaticModels() {
		if m.Provider == "" {
			t.Errorf("static fallback model %q has no Provider; the picker would not group it", m.ID)
		}
		if want := codebuddyModelProvider(m.ID); m.Provider != want {
			t.Errorf("static fallback %q has Provider %q but prefix inference says %q", m.ID, m.Provider, want)
		}
	}
}

// TestDiscoverCodebuddyModelsACPEffort pins the effort catalog coming out of the
// same handshake — no second process — and the `enabled` filtering.
func TestDiscoverCodebuddyModelsACPEffort(t *testing.T) {
	resetCodebuddyDiscoveryCaches(t)
	path := writeCodebuddyACPStub(t, codebuddyACPSessionResult)

	catalog, err := discoverCodebuddyModels(context.Background(), Command{Path: path})
	if err != nil {
		t.Fatalf("discoverCodebuddyModels: %v", err)
	}
	thinking := catalog.Models[0].Thinking
	if thinking == nil {
		t.Fatal("expected the effort catalog to be annotated from the same session/new response")
	}
	var got []string
	for _, lvl := range thinking.SupportedLevels {
		got = append(got, lvl.Value)
	}
	want := []string{"minimal", "low", "medium", "high", "xhigh", "max"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("levels = %v, want %v", got, want)
	}
	// `enabled` is advertised by ACP but `--effort enabled` is not a valid
	// command line, so it must not reach the picker...
	for _, lvl := range thinking.SupportedLevels {
		if lvl.Value == "enabled" {
			t.Error("`enabled` is a session toggle, not an --effort value; it must be filtered out")
		}
	}
	// ...and since it is the advertised currentValue, DefaultLevel must stay
	// empty ("runtime decides") rather than echo a value we cannot pass.
	if thinking.DefaultLevel != "" {
		t.Errorf("DefaultLevel = %q, want empty because currentValue was the unusable `enabled`", thinking.DefaultLevel)
	}
}

// TestParseACPCodebuddyEffortDefault covers the other branch: when CodeBuddy
// advertises a real level as current, we echo it.
func TestParseACPCodebuddyEffortDefault(t *testing.T) {
	raw := json.RawMessage(`{"configOptions":[{"id":"thought_level","currentValue":"high","options":[` +
		`{"value":"low"},{"value":"high"},{"value":"enabled"}]}]}`)
	levels, def := parseACPCodebuddyEffort(raw)
	if strings.Join(levels, ",") != "low,high" {
		t.Errorf("levels = %v, want [low high]", levels)
	}
	if def != "high" {
		t.Errorf("DefaultLevel = %q, want high", def)
	}
}

// TestDiscoverCodebuddyModelsFallsBackOnACPFailure covers the not-logged-in /
// unreachable-CLI cases. The stand-in is still offered so the picker stays
// usable, but it must be marked Fallback so the server can never cache it as
// this runtime's real catalog (MUL-5549), and the effort picker must still work.
func TestDiscoverCodebuddyModelsFallsBackOnACPFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "binary missing",
			path: func(t *testing.T) string { return missingAgentExecutable(t, "codebuddy") },
		},
		{
			name: "session/new refused (not logged in)",
			path: func(t *testing.T) string { return writeCodebuddyACPStub(t, "") },
		},
		{
			name: "session/new carries no catalog",
			path: func(t *testing.T) string { return writeCodebuddyACPStub(t, `{"sessionId":"ses-empty"}`) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetCodebuddyDiscoveryCaches(t)
			catalog, err := discoverCodebuddyModels(context.Background(), Command{Path: tc.path(t)})
			if err != nil {
				t.Fatalf("discoverCodebuddyModels: %v", err)
			}
			if !catalog.Fallback {
				t.Error("a degraded discovery must be marked Fallback")
			}
			if len(catalog.Models) == 0 {
				t.Error("expected the static stand-in to still be offered to the UI")
			}
			if catalog.Models[0].Thinking == nil || len(catalog.Models[0].Thinking.SupportedLevels) == 0 {
				t.Error("the fallback path must still annotate effort levels")
			}
		})
	}
}

// TestCodebuddyStaticEffortFallbackCoversFlagValues keeps the offline fallback
// honest against the flag it feeds: every level offered must be one `--effort`
// accepts, and the set should not silently shrink below what the CLI supports.
func TestCodebuddyStaticEffortFallbackCoversFlagValues(t *testing.T) {
	t.Parallel()
	for _, level := range codebuddyStaticEffortFallback {
		if !codebuddyFlagEffortValues[level] {
			t.Errorf("static fallback offers %q, which `--effort` does not accept", level)
		}
		if !IsKnownThinkingValue("codebuddy", level) {
			t.Errorf("static fallback offers %q, which the server-side gate rejects", level)
		}
	}
	if len(codebuddyStaticEffortFallback) != len(codebuddyFlagEffortValues) {
		t.Errorf("static fallback has %d levels but --effort accepts %d; they drifted apart",
			len(codebuddyStaticEffortFallback), len(codebuddyFlagEffortValues))
	}
}

// TestCachedDiscoveryDoesNotCacheFallback pins that a fallback never occupies
// the daemon's 60s discovery cache: the next request must be free to retry.
func TestCachedDiscoveryDoesNotCacheFallback(t *testing.T) {
	const key = "test-cache-fallback"
	reset := func() {
		modelCacheMu.Lock()
		delete(modelCache, key)
		modelCacheMu.Unlock()
	}
	reset()
	t.Cleanup(reset)

	calls := 0
	fn := func() (Catalog, error) {
		calls++
		return Catalog{Models: []Model{{ID: "stand-in"}}, Fallback: true}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := cachedDiscovery(key, fn)
		if err != nil {
			t.Fatalf("cachedDiscovery: %v", err)
		}
		if !got.Fallback {
			t.Error("cachedDiscovery must preserve the Fallback marker")
		}
	}
	if calls != 2 {
		t.Fatalf("a fallback must not be cached: expected fn called 2x, got %d", calls)
	}
}
