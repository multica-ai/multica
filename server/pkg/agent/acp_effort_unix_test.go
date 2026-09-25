//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// These cases need a POSIX shell stub, or the CodeBuddy session capture in
// codebuddy_discovery_fallback_test.go, which is itself !windows. The
// provider-neutral parser/apply tests stay in acp_effort_test.go so they run
// on every platform.

// ── CodeBuddy overlay regression ─────────────────────────────────────

// TestCodebuddyOverlaySurvivesGeneralization: CodeBuddy applies the level
// through `--effort`, not over ACP, so its narrower flag vocabulary must keep
// filtering the advertised list even though the parser underneath is shared.
func TestCodebuddyOverlaySurvivesGeneralization(t *testing.T) {
	t.Parallel()
	levels, def := parseACPCodebuddyEffort(json.RawMessage(codebuddyACPSessionResult))
	if strings.Join(levels, ",") != "minimal,low,medium,high,xhigh,max" {
		t.Errorf("levels = %v, want the six --effort values without `enabled`", levels)
	}
	if def != "" {
		t.Errorf("DefaultLevel = %q, want empty because currentValue was the unusable `enabled`", def)
	}

	// The same payload through the generic path keeps `enabled`: the overlay is
	// CodeBuddy's, not everyone's.
	option, ok := parseACPEffortOption(json.RawMessage(codebuddyACPSessionResult))
	if !ok || !option.supports("enabled") {
		t.Error("the shared parser must not apply CodeBuddy's flag whitelist")
	}
}

// ── End-to-end discovery ─────────────────────────────────────────────

// reasonixStubFor answers a discovery handshake with the given session/new
// result and nothing else, which is all model discovery drives.
func reasonixStubFor(sessionResult string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id" ;;
    *'"method":"session/new"'*) printf '{"jsonrpc":"2.0","id":%s,"result":` + sessionResult + `}\n' "$id" ;;
  esac
done
`
}

// TestValidateThinkingLevelReasonix covers the daemon's pre-execution guard.
// It fails closed on an unknown (provider, model, level), so without a
// discovered catalog it would silently drop every level and the feature would
// look wired up while doing nothing.
func TestValidateThinkingLevelReasonix(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(reasonixStubFor(reasonixEffortSessionResult)))

	for _, tc := range []struct {
		model string
		level string
		want  bool
	}{
		{model: "deepseek-v4-flash", level: "max", want: true},
		// `auto` and `disabled` only survive because the shared parser takes
		// the advertised list verbatim; CodeBuddy's whitelist would drop them.
		{model: "deepseek-v4-flash", level: "auto", want: true},
		{model: "deepseek-v4-flash", level: "disabled", want: true},
		{model: "deepseek-v4-flash", level: "ultra", want: false},
		// Empty model resolves to the catalog's Default (the advertised
		// currentModelId), so a default-model agent still gets its level.
		{model: "", level: "high", want: true},
		{model: "no-such-model", level: "high", want: false},
	} {
		got, err := ValidateThinkingLevel(context.Background(), "reasonix", Command{Path: fakePath}, tc.model, tc.level)
		if err != nil {
			t.Fatalf("ValidateThinkingLevel(%q, %q): %v", tc.model, tc.level, err)
		}
		if got != tc.want {
			t.Errorf("ValidateThinkingLevel(%q, %q) = %v, want %v", tc.model, tc.level, got, tc.want)
		}
	}
}

// reasonixThreeModelSessionResult mirrors the real per-model split in reasonix
// v1.21.5's `modelReasoningCapabilities`: the session is on `deepseek-v4-flash`,
// whose catalog includes `low`, while `deepseek-v4-pro` does not support `low`
// and `glm-4-air` has no effort dial at all. session/new can only describe the
// model it is on, so the other two must come out with no catalog.
const reasonixThreeModelSessionResult = `{"sessionId":"ses-reasonix",` +
	`"models":{"currentModelId":"deepseek-v4-flash",` +
	`"availableModels":[{"modelId":"deepseek-v4-flash","name":"Flash"},` +
	`{"modelId":"deepseek-v4-pro","name":"Pro"},{"modelId":"glm-4-air","name":"GLM Air"}]},` +
	`"configOptions":[` +
	`{"type":"select","id":"model","name":"Model","category":"model","currentValue":"deepseek-v4-flash",` +
	`"options":[{"value":"deepseek-v4-flash"},{"value":"deepseek-v4-pro"},{"value":"glm-4-air"}]},` +
	`{"type":"select","id":"effort","name":"Effort","category":"thought_level","currentValue":"high","options":[` +
	`{"value":"disabled","name":"Disabled"},{"value":"low","name":"Low"},` +
	`{"value":"high","name":"High"},{"value":"max","name":"Max"}]}]}`

// TestReasonixPerModelEffortIsNotSharedAcrossModels is the regression for the
// review finding on the first cut of this feature: the session's catalog was
// copied onto every model, so `low` — real for flash, absent on pro, and
// meaningless for GLM — was offered everywhere. The picker showed it,
// ValidateThinkingLevel waved it through, and the runtime then refused it at
// apply time while the task quietly ran at the default.
func TestReasonixPerModelEffortIsNotSharedAcrossModels(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(reasonixStubFor(reasonixThreeModelSessionResult)))

	models, err := discoverReasonixModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverReasonixModels: %v", err)
	}
	byID := map[string]Model{}
	for _, m := range models {
		byID[m.ID] = m
	}

	// The model the session is on carries the catalog it actually described.
	flash := byID["deepseek-v4-flash"]
	if flash.Thinking == nil {
		t.Fatal("the current model must carry the advertised catalog")
	}
	if len(flash.Thinking.SupportedLevels) != 4 {
		t.Errorf("flash levels = %+v, want the four advertised", flash.Thinking.SupportedLevels)
	}

	// The others must not inherit it — their real vocabularies differ.
	for _, id := range []string{"deepseek-v4-pro", "glm-4-air"} {
		if got := byID[id]; got.Thinking != nil {
			t.Errorf("model %q inherited %+v; its catalog was never advertised", id, got.Thinking)
		}
	}

	// The daemon guard must agree, so a level can never reach a model whose
	// support we did not observe.
	for _, tc := range []struct {
		model string
		level string
		want  bool
	}{
		{model: "deepseek-v4-flash", level: "low", want: true},
		{model: "deepseek-v4-flash", level: "high", want: true},
		// The exact bug: `low` is real on flash and absent on pro.
		{model: "deepseek-v4-pro", level: "low", want: false},
		// Not even a level pro plausibly supports — we never saw its catalog.
		{model: "deepseek-v4-pro", level: "high", want: false},
		{model: "glm-4-air", level: "high", want: false},
	} {
		got, err := ValidateThinkingLevel(context.Background(), "reasonix", Command{Path: fakePath}, tc.model, tc.level)
		if err != nil {
			t.Fatalf("ValidateThinkingLevel(%q, %q): %v", tc.model, tc.level, err)
		}
		if got != tc.want {
			t.Errorf("ValidateThinkingLevel(%q, %q) = %v, want %v", tc.model, tc.level, got, tc.want)
		}
	}
}

// TestDiscoverReasonixModelsAnnotatesEffort proves the wiring: one ACP
// handshake yields both the model catalog and the effort catalog, with no
// reasonix-specific parsing anywhere in the path.
func TestDiscoverReasonixModelsAnnotatesEffort(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(reasonixStubFor(reasonixEffortSessionResult)))

	models, err := discoverReasonixModels(context.Background(), Command{Path: fakePath})
	if err != nil {
		t.Fatalf("discoverReasonixModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v, want 2", models)
	}
	for _, m := range models {
		// Only the session's current model is described by what session/new
		// advertised; see TestReasonixPerModelEffortIsNotSharedAcrossModels.
		if !m.Default {
			if m.Thinking != nil {
				t.Errorf("model %q inherited a catalog it never advertised: %+v", m.ID, m.Thinking)
			}
			continue
		}
		if m.Thinking == nil {
			t.Fatalf("current model %q carries no effort catalog", m.ID)
		}
		if len(m.Thinking.SupportedLevels) != 5 || m.Thinking.DefaultLevel != "max" {
			t.Errorf("model %q: thinking = %+v", m.ID, m.Thinking)
		}
	}
}
