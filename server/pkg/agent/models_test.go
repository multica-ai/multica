package agent

import (
	"context"
	"strings"
	"testing"
)

func TestListModelsStaticProviders(t *testing.T) {
	ctx := context.Background()
	for _, provider := range []string{"claude", "codex", "codebuddy", "gemini", "cursor", "copilot"} {
		got, err := ListModels(ctx, provider, "")
		if err != nil {
			t.Fatalf("ListModels(%q) error: %v", provider, err)
		}
		if len(got) == 0 {
			t.Errorf("ListModels(%q) returned no models", provider)
		}
		for i, m := range got {
			if m.ID == "" {
				t.Errorf("ListModels(%q)[%d] has empty ID", provider, i)
			}
			if m.Label == "" {
				t.Errorf("ListModels(%q)[%d] has empty Label", provider, i)
			}
		}
	}
}

func TestGeminiStaticModelsExposesAliasesAndGemini3(t *testing.T) {
	// Gemini CLI has no `models list` subcommand, so we expose the
	// CLI's own aliases (auto / pro / flash / flash-lite) plus
	// explicit version pins including Gemini 3. Regression guard
	// for multica-ai/multica#1503 — Gemini 3 must be selectable.
	models := geminiStaticModels()
	ids := map[string]Model{}
	for _, m := range models {
		ids[m.ID] = m
	}
	for _, want := range []string{
		"auto", "auto-gemini-2.5",
		"pro", "flash", "flash-lite",
		"gemini-3-pro-preview", "gemini-3-flash-preview",
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing expected Gemini model %q in: %+v", want, models)
		}
	}
	auto, ok := ids["auto"]
	if !ok || !auto.Default {
		t.Errorf("expected `auto` to be the default Gemini entry, got %+v", auto)
	}
	for _, m := range models {
		if m.Provider != "google" {
			t.Errorf("all Gemini entries must carry Provider=google, got %+v", m)
		}
	}
}

func TestCodexStaticModelsExposesGPT55(t *testing.T) {
	// Codex CLI has no `models list` subcommand so the catalog is
	// hand-maintained. Regression guard for multica-ai/multica#2009 —
	// GPT-5.5 must be selectable, and the badge default must point at
	// the latest release rather than lagging a version behind.
	models := codexStaticModels()
	ids := map[string]Model{}
	for _, m := range models {
		ids[m.ID] = m
	}
	for _, want := range []string{
		"gpt-5.5", "gpt-5.5-mini",
		"gpt-5.4", "gpt-5.4-mini",
		"gpt-5.3-codex", "gpt-5",
		"o3", "o3-mini",
	} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing expected Codex model %q in: %+v", want, models)
		}
	}
	latest, ok := ids["gpt-5.5"]
	if !ok || !latest.Default {
		t.Errorf("expected `gpt-5.5` to be the default Codex entry, got %+v", latest)
	}
	defaults := 0
	for _, m := range models {
		if m.Default {
			defaults++
		}
		if m.Provider != "openai" {
			t.Errorf("all Codex entries must carry Provider=openai, got %+v", m)
		}
	}
	if defaults != 1 {
		t.Errorf("expected exactly one default Codex entry, got %d", defaults)
	}
}

func TestListModelsHermesWithoutBinary(t *testing.T) {
	// With no `hermes` binary on PATH the discovery fast-paths to
	// an empty list (the UI then falls back to creatable manual
	// entry). This test only verifies the fast-path; an actual
	// ACP session is exercised in integration.
	ctx := context.Background()
	// Prime the cache miss so we hit the live discovery function.
	modelCacheMu.Lock()
	delete(modelCache, "hermes")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "hermes", "/nonexistent/hermes")
	if err != nil {
		t.Fatalf("ListModels(hermes) error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsKiroWithoutBinary(t *testing.T) {
	ctx := context.Background()
	modelCacheMu.Lock()
	delete(modelCache, "kiro")
	modelCacheMu.Unlock()

	got, err := ListModels(ctx, "kiro", "/nonexistent/kiro-cli")
	if err != nil {
		t.Fatalf("ListModels(kiro) error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice even when binary is missing")
	}
}

func TestListModelsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	_, err := ListModels(ctx, "nonexistent", "")
	if err == nil {
		t.Fatal("ListModels(unknown) expected error")
	}
}

func TestStaticCatalogsHaveAtMostOneDefault(t *testing.T) {
	// Each catalog should tag at most one entry as the display
	// default so the UI badge is unambiguous. More than one
	// usually means a copy/paste slip when adding new models.
	catalogs := map[string][]Model{
		"claude":  claudeStaticModels(),
		"codex":   codexStaticModels(),
		"gemini":  geminiStaticModels(),
		"cursor":  cursorStaticModels(),
		"copilot": copilotStaticModels(),
	}
	for provider, models := range catalogs {
		count := 0
		for _, m := range models {
			if m.Default {
				count++
			}
		}
		if count > 1 {
			t.Errorf("%s: %d models marked Default, want 0 or 1", provider, count)
		}
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

func TestHermesModelSelectionSupported(t *testing.T) {
	// Regression guard: hermes now supports model selection via
	// the ACP session/set_model RPC, so the UI dropdown should
	// not be disabled for it.
	if !ModelSelectionSupported("hermes") {
		t.Error("hermes should be model-selection-supported now that set_session_model is wired")
	}
}

func TestCachedDiscovery(t *testing.T) {
	calls := 0
	fn := func() ([]Model, error) {
		calls++
		return []Model{{ID: "x", Label: "x"}}, nil
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
