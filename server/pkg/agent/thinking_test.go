package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// ── Claude help parsing ──────────────────────────────────────────────

func TestParseClaudeEffortHelp_OldFormat(t *testing.T) {
	t.Parallel()
	// claude 2.1.109 — the older help omits xhigh.
	help := `Usage: claude [options]

Options:
  --model <model>     Model to use
  --effort <level>    Effort level for the current session (low, medium, high, max)
  --verbose
`
	got := parseClaudeEffortHelp(help)
	want := []string{"low", "medium", "high", "max"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseClaudeEffortHelp: got %v, want %v", got, want)
	}
}

func TestParseClaudeEffortHelp_NewFormat(t *testing.T) {
	t.Parallel()
	// claude 2.1.121 — the newer help adds xhigh.
	help := `Usage: claude [options]

Options:
  --effort <level>    Effort level for the current session (low, medium, high, xhigh, max)
`
	got := parseClaudeEffortHelp(help)
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseClaudeEffortHelp: got %v, want %v", got, want)
	}
}

func TestParseClaudeEffortHelp_Missing(t *testing.T) {
	t.Parallel()
	help := `Usage: claude [options]

Options:
  --model <model>     Model to use
  --verbose
`
	got := parseClaudeEffortHelp(help)
	if got != nil {
		t.Fatalf("parseClaudeEffortHelp: expected nil, got %v", got)
	}
}

func TestClaudeEffortLevelsFromHelp_DriftedFormatFallsBackToFullSuperset(t *testing.T) {
	t.Parallel()
	// The flag is advertised but the parenthesised value list is gone —
	// genuine help drift, so keep offering the last known good superset.
	help := `Usage: claude [options]

Options:
  --effort <level>    Choose how hard the model thinks
`
	got := claudeEffortLevelsFromHelp(help)
	if !reflect.DeepEqual(got, claudeStaticEffortFullSuperset) {
		t.Fatalf("claudeEffortLevelsFromHelp: got %v, want full superset %v", got, claudeStaticEffortFullSuperset)
	}
}

func TestClaudeEffortLevelsFromHelp_PreEffortCLIReturnsNoLevels(t *testing.T) {
	t.Parallel()
	// A CLI released before --effort existed (e.g. claude 2.1.2) has no
	// mention of the flag anywhere in --help. This must yield NO levels —
	// the old fallback-to-full-superset here made the daemon inject
	// --effort, which the binary rejects with "unknown option", failing
	// the task outright.
	help := `Usage: claude [options]

Options:
  --model <model>     Model to use
  --verbose
`
	if got := claudeEffortLevelsFromHelp(help); got != nil {
		t.Fatalf("claudeEffortLevelsFromHelp: expected nil for pre-effort CLI, got %v", got)
	}
}

func TestProjectClaudeLevels_PerModelSubset(t *testing.T) {
	t.Parallel()
	superset := []string{"low", "medium", "high", "xhigh", "max"}
	// Sonnet should drop xhigh per claudeModelEffortAllow.
	got := projectClaudeLevels(superset, claudeModelEffortAllow["claude-sonnet-4-6"])
	values := make([]string, 0, len(got))
	for _, lvl := range got {
		values = append(values, lvl.Value)
	}
	want := []string{"low", "medium", "high", "max"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("projectClaudeLevels: got %v, want %v", values, want)
	}
	// Opus keeps xhigh.
	got = projectClaudeLevels(superset, claudeModelEffortAllow["claude-opus-4-7"])
	values = values[:0]
	for _, lvl := range got {
		values = append(values, lvl.Value)
	}
	if !reflect.DeepEqual(values, superset) {
		t.Fatalf("projectClaudeLevels for Opus: got %v, want %v", values, superset)
	}
}

// ── Codex discovery argv ────────────────────────────────────────────
//
// Elon's PR1 review found that `codex debug models --output json` is
// rejected by codex-cli 0.131.0 — there is no `--output` flag on the
// subcommand. The fix was to drop the flag and add `--bundled` (which
// just skips network refresh). These two tests pin the contract:
//
//   - TestCodexDebugModelsArgs_Pinned asserts the literal argv we pass
//     so a future "let's add a flag" refactor breaks loudly instead of
//     silently swallowing the discovery output.
//   - TestRunCodexDebugModels_ArgvSeenByBinary plugs a fake `codex`
//     binary on PATH and verifies that what *actually* reaches the
//     process matches the pinned argv, not just what the var holds.

func TestCodexDebugModelsArgs_Pinned(t *testing.T) {
	t.Parallel()
	want := []string{"debug", "models", "--bundled"}
	if !reflect.DeepEqual(codexDebugModelsArgs, want) {
		t.Fatalf("codexDebugModelsArgs drifted: got %v, want %v", codexDebugModelsArgs, want)
	}
	for _, arg := range codexDebugModelsArgs {
		if arg == "--output" || arg == "-o" {
			t.Errorf("--output / -o leaked back into argv (codex CLI does not accept it): %v", codexDebugModelsArgs)
		}
	}
}

// TestRunCodexDebugModels_ArgvSeenByBinary executes runCodexDebugModels
// against a shell-script stand-in for `codex` that records its argv to
// a file and prints a minimal valid JSON payload. The check is on what
// the binary actually received (one argument per element, no merging
// or splitting), not just the package var — the original bug surfaced
// because a real codex saw `--output json` as two extra unknown args.
func TestRunCodexDebugModels_ArgvSeenByBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	fake := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + argvFile + "'\n" +
		"echo '{\"models\":[]}'\n"
	// Use the ForkLock-protected helper instead of os.WriteFile: under
	// t.Parallel() with the rest of this package, a sibling test's
	// concurrent fork can inherit our still-open write fd, causing
	// Linux ETXTBSY when we exec the file (Go #22315).
	writeTestExecutable(t, fake, []byte(script))

	raw, err := runCodexDebugModels(context.Background(), fake)
	if err != nil {
		t.Fatalf("runCodexDebugModels: %v (output=%q)", err, raw)
	}

	data, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	got := splitNonEmptyLines(string(data))
	want := []string{"debug", "models", "--bundled"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fake codex received argv %v, want %v", got, want)
	}
}

func splitNonEmptyLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// ── Codex debug models version/catalog discovery ────────────────────

func TestCodexSupportsDebugModels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"codex-cli 0.121.0", false},
		{"codex-cli 0.122.0", true},
		{"codex-cli 0.144.1", true},
		{"invalid", false},
	} {
		if got := codexSupportsDebugModels(tc.version); got != tc.want {
			t.Errorf("codexSupportsDebugModels(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestParseCodexModelCatalog(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"models": [
			{
				"slug": "gpt-5.6-sol",
				"display_name": "GPT-5.6-Sol",
				"visibility": "list",
				"default_reasoning_level": "low",
				"supported_reasoning_levels": [
					{"effort": "low", "description": "Fast"},
					{"effort": "max", "description": "Maximum"},
					{"effort": "ultra", "description": "Delegates"},
					{"effort": "future", "description": "New CLI value"}
				]
			},
			{
				"slug": "hidden-model",
				"display_name": "Hidden",
				"visibility": "hide",
				"supported_reasoning_levels": [{"effort": "low"}]
			},
			{
				"slug": "no-reasoning",
				"display_name": "No Reasoning",
				"visibility": "list",
				"supported_reasoning_levels": []
			}
		]
	}`)
	got, err := parseCodexModelCatalog(raw)
	if err != nil {
		t.Fatalf("parseCodexModelCatalog: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two visible models, got %+v", got)
	}
	if got[0].ID != "gpt-5.6-sol" || got[0].Label != "GPT-5.6-Sol" || !got[0].Default {
		t.Errorf("unexpected first model: %+v", got[0])
	}
	if got[0].Thinking == nil || got[0].Thinking.DefaultLevel != "low" || !hasThinkingLevel(got[0].Thinking, "max") || !hasThinkingLevel(got[0].Thinking, "ultra") || !hasThinkingLevel(got[0].Thinking, "future") {
		t.Errorf("unexpected per-model thinking catalog: %+v", got[0].Thinking)
	}
	if got[1].ID != "no-reasoning" || got[1].Thinking != nil {
		t.Errorf("model without reasoning should remain selectable without a thinking picker: %+v", got[1])
	}
}

func TestParseCodexModelCatalogMalformed(t *testing.T) {
	t.Parallel()
	if _, err := parseCodexModelCatalog([]byte("not json")); err == nil {
		t.Fatal("expected malformed catalog error")
	}
}

func TestDiscoverCodexModelsVersionGateAndFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	t.Run("supported version uses bundled catalog", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "codex")
		script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.122.0"
  exit 0
fi
printf '%s\n' "$@" > "` + filepath.Join(dir, "argv.txt") + `"
echo '{"models":[{"slug":"runtime-model","display_name":"Runtime Model","visibility":"list","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"high","description":"Live"}]}]}'
`
		writeTestExecutable(t, fake, []byte(script))

		got := discoverCodexModels(context.Background(), fake)
		if len(got) != 1 || got[0].ID != "runtime-model" || got[0].Thinking == nil || !hasThinkingLevel(got[0].Thinking, "high") {
			t.Fatalf("expected runtime catalog, got %+v", got)
		}
	})

	t.Run("old version uses static fallback", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "codex")
		script := "#!/bin/sh\n" +
			"if [ \"$1\" = \"--version\" ]; then echo 'codex-cli 0.121.0'; exit 0; fi\n" +
			"exit 99\n"
		writeTestExecutable(t, fake, []byte(script))

		got := discoverCodexModels(context.Background(), fake)
		if len(got) == 0 || got[0].ID != "gpt-5.6-sol" {
			t.Fatalf("expected static fallback, got %+v", got)
		}
	})

	t.Run("debug command failure uses static fallback", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "codex")
		script := "#!/bin/sh\n" +
			"if [ \"$1\" = \"--version\" ]; then echo 'codex-cli 0.144.1'; exit 0; fi\n" +
			"exit 1\n"
		writeTestExecutable(t, fake, []byte(script))

		got := discoverCodexModels(context.Background(), fake)
		if len(got) == 0 || got[0].ID != "gpt-5.6-sol" || got[0].Thinking == nil {
			t.Fatalf("expected model + thinking fallback, got %+v", got)
		}
	})
}

func TestValidateThinkingLevelCodexPerModelFallbackCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		model string
		level string
		want  bool
	}{
		{model: "gpt-5.6-sol", level: "ultra", want: true},
		{model: "gpt-5.6-terra", level: "ultra", want: true},
		{model: "gpt-5.6-luna", level: "max", want: true},
		{model: "gpt-5.6-luna", level: "ultra", want: false},
		{model: "gpt-5.3-codex", level: "xhigh", want: true},
		{model: "gpt-5.3-codex", level: "max", want: false},
	} {
		got, err := ValidateThinkingLevel(context.Background(), "codex", "/nonexistent/codex", tc.model, tc.level)
		if err != nil {
			t.Fatalf("ValidateThinkingLevel(%q, %q): %v", tc.model, tc.level, err)
		}
		if got != tc.want {
			t.Errorf("ValidateThinkingLevel(%q, %q) = %v, want %v", tc.model, tc.level, got, tc.want)
		}
	}
}

// TestParseCodexModelCatalog_PreservesFutureEfforts pins the dynamic-catalog
// contract: a future Codex effort should reach the picker without a Multica
// code update, pass the server's safe-token gate, and remain scoped to the
// model that advertised it.
func TestParseCodexModelCatalog_PreservesFutureEfforts(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"models": [
			{
				"slug": "gpt-5.6-sol",
				"display_name": "GPT-5.6-Sol",
				"visibility": "list",
				"default_reasoning_level": "high",
				"supported_reasoning_levels": [
					{"effort": "medium"},
					{"effort": "high"},
					{"effort": "max"},
					{"effort": "ultra"},
					{"effort": "hyper"}
				]
			},
			{
				"slug": "gpt-5.6-luna",
				"display_name": "GPT-5.6-Luna",
				"visibility": "list",
				"default_reasoning_level": "medium",
				"supported_reasoning_levels": [
					{"effort": "medium"},
					{"effort": "max"}
				]
			}
		]
	}`)
	got, err := parseCodexModelCatalog(raw)
	if err != nil {
		t.Fatalf("parseCodexModelCatalog: %v", err)
	}
	byID := make(map[string]Model, len(got))
	for _, model := range got {
		byID[model.ID] = model
	}

	sol := byID["gpt-5.6-sol"]
	if sol.Thinking == nil {
		t.Fatalf("missing gpt-5.6-sol thinking entry: %+v", got)
	}
	if !hasThinkingLevel(sol.Thinking, "hyper") {
		t.Errorf("future effort should be preserved for sol: %+v", sol.Thinking.SupportedLevels)
	}
	if !IsKnownThinkingValue("codex", "hyper") {
		t.Error("future safe Codex effort should pass the server token gate")
	}
	luna := byID["gpt-5.6-luna"]
	if luna.Thinking == nil {
		t.Fatalf("missing gpt-5.6-luna thinking entry: %+v", got)
	}
	if hasThinkingLevel(luna.Thinking, "hyper") {
		t.Errorf("future effort must remain model-specific: %+v", luna.Thinking.SupportedLevels)
	}
}

func hasThinkingLevel(mt *ModelThinking, value string) bool {
	for _, lvl := range mt.SupportedLevels {
		if lvl.Value == value {
			return true
		}
	}
	return false
}

// ── IsKnownThinkingValue (server-side enum gate) ─────────────────────

func TestIsKnownThinkingValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider string
		value    string
		want     bool
	}{
		{"claude", "", true},
		{"claude", "low", true},
		{"claude", "xhigh", true},
		{"claude", "max", true},
		{"claude", "none", false}, // Codex-only token rejected for Claude
		{"codex", "", true},
		{"codex", "none", true},
		{"codex", "minimal", true},
		{"codex", "xhigh", true},
		{"codex", "max", true},
		{"codex", "ultra", true},
		{"codex", "future-level", true}, // exact support is checked against the daemon catalog
		{"codex", ".hidden", false},
		{"codex", "bad value", false},
		{"opencode", "", true},
		{"opencode", "max", true},
		{"opencode", "fast-mode", true},  // custom opencode.json variant names are valid
		{"opencode", ".hidden", false},   // reject suspicious / malformed names server-side
		{"opencode", "bad value", false}, // spaces are not valid variant names
		{"hermes", "", true},
		{"hermes", "low", false}, // hermes has no thinking concept
		{"grok", "", true},
		{"grok", "low", true},
		{"grok", "medium", true},
		{"grok", "high", true},
		{"grok", "none", false},
		{"grok", "minimal", false},
		{"grok", "xhigh", false},
		{"grok", "max", false},
		{"grok", "bogus", false},
	}
	for _, tc := range tests {
		if got := IsKnownThinkingValue(tc.provider, tc.value); got != tc.want {
			t.Errorf("IsKnownThinkingValue(%q, %q) = %v, want %v",
				tc.provider, tc.value, got, tc.want)
		}
	}
}

// TestCodexAdvertisedLevelsArePersistable pins the catalog → API contract:
// every effort token Codex discovery can label (a key in codexEffortLabel)
// must pass the server's Create/Update enum gate. Otherwise the daemon
// advertises a level the picker shows but the server 400s on save — the
// exact drift the gpt-5.6 `max`/`ultra` additions introduced.
func TestCodexAdvertisedLevelsArePersistable(t *testing.T) {
	t.Parallel()
	for effort := range codexEffortLabel {
		if !IsKnownThinkingValue("codex", effort) {
			t.Errorf("Codex advertises effort %q but IsKnownThinkingValue rejects it; "+
				"keep the dynamic Codex token gate compatible so it can be saved", effort)
		}
	}
}

// ── ValidateThinkingLevel default-model handling ─────────────────────
//
// Elon's PR1 review called out that an empty model on a default-model
// task must not be misjudged as "unknown model → reject". The fix is to
// resolve empty model to the catalog's default entry inside the
// validator. Both the daemon's per-model guard and the server's API
// layer call this; if it gets default-model wrong, any agent without an
// explicit model set would have its thinking_level dropped silently.

func TestValidateThinkingLevel_EmptyModelResolvesToDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	// We need a `claude` whose --help advertises the full superset
	// (low/medium/high/xhigh/max) so per-model projection actually has
	// something to filter. A non-existent path falls back to a conservative
	// [low,medium,high] which would hide the per-model behaviour we're
	// trying to verify.
	fakeClaude := writeFakeClaudeHelpBinary(t)
	resetThinkingCacheForTests()
	defer resetThinkingCacheForTests()

	ctx := context.Background()

	t.Run("valid level on default model passes", func(t *testing.T) {
		// Claude's catalog flags Sonnet 4.6 as Default. Sonnet supports
		// low/medium/high/max (no xhigh) per claudeModelEffortAllow, so
		// "high" must round-trip when model is left empty.
		ok, err := ValidateThinkingLevel(ctx, "claude", fakeClaude, "", "high")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !ok {
			t.Errorf("default-model high should be valid for claude; got false")
		}
	})

	t.Run("invalid level on default model fails", func(t *testing.T) {
		// "xhigh" is opus-only; resolving "" to default (sonnet 4.6)
		// should reject it, not silently accept.
		ok, err := ValidateThinkingLevel(ctx, "claude", fakeClaude, "", "xhigh")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ok {
			t.Errorf("xhigh should be invalid on sonnet (the default model); got true")
		}
	})

	t.Run("empty value always valid", func(t *testing.T) {
		// Empty value means "use runtime default" — should pass
		// regardless of model resolution.
		ok, err := ValidateThinkingLevel(ctx, "claude", fakeClaude, "", "")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !ok {
			t.Errorf("empty value must always be valid")
		}
	})
}

func TestValidateThinkingLevel_ExplicitModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()
	fakeClaude := writeFakeClaudeHelpBinary(t)
	resetThinkingCacheForTests()
	defer resetThinkingCacheForTests()

	ctx := context.Background()

	// xhigh IS valid on Opus 4.7.
	ok, err := ValidateThinkingLevel(ctx, "claude", fakeClaude, "claude-opus-4-7", "xhigh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Errorf("xhigh should be valid on opus-4-7; got false")
	}

	// xhigh is NOT valid on Sonnet — should fail.
	ok, err = ValidateThinkingLevel(ctx, "claude", fakeClaude, "claude-sonnet-4-6", "xhigh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Errorf("xhigh must not be valid on sonnet-4-6; got true")
	}

	// An unknown model with a valid token still fails closed (no guess).
	ok, err = ValidateThinkingLevel(ctx, "claude", fakeClaude, "claude-nonexistent", "high")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Errorf("unknown model must fail closed; got true")
	}
}

// TestValidateThinkingLevel_CodexEmptyModelFailsClosed pins the MUL-4347
// fix: an explicit codex model is validated against its own per-model
// catalog, but an EMPTY model (follow config.toml, which can resolve to any
// installed model) must NOT borrow the flagged Default entry's catalog. The
// Default (gpt-5.6-sol) alone advertises `ultra`; letting an empty model
// inherit it would green-light a level Luna / gpt-5.5 don't support and Codex
// won't reject. So an empty codex model fails closed for every level and the
// daemon drops it — users must pick an explicit model to pin an effort.
func TestValidateThinkingLevel_CodexEmptyModelFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	fakeCodex := writeFakeCodexModelsBinary(t)
	ctx := context.Background()

	check := func(model, value string, want bool) {
		t.Helper()
		ok, err := ValidateThinkingLevel(ctx, "codex", fakeCodex, model, value)
		if err != nil {
			t.Fatalf("ValidateThinkingLevel(codex, %q, %q): unexpected err: %v", model, value, err)
		}
		if ok != want {
			t.Errorf("ValidateThinkingLevel(codex, %q, %q) = %v, want %v", model, value, ok, want)
		}
	}

	// Explicit models resolve against their own per-model catalog.
	check("gpt-5.6-sol", "ultra", true)   // sol advertises ultra
	check("gpt-5.6-terra", "ultra", true) // ...and so does terra
	check("gpt-5.6-luna", "ultra", false) // luna tops out at max
	check("gpt-5.6-luna", "max", true)    // ...which is valid
	check("gpt-5.6-luna", "medium", true) // as are the base levels

	// Empty model cannot be validated per-model, so it fails closed for EVERY
	// level — including `ultra` (must not pass via the Sol Default) and even a
	// level every model supports (`medium`). The daemon drops it.
	check("", "ultra", false)
	check("", "medium", false)

	// Empty VALUE always means "use runtime default" and stays valid — this is
	// the path a follow-CLI-config agent takes, and the honest orphan/clear
	// flow relies on it (an already-persisted level round-trips to empty).
	check("", "", true)
	check("gpt-5.6-luna", "", true)
}

func TestValidateThinkingLevel_PreEffortCLIRejectsAllLevels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	t.Parallel()

	// End-to-end guard for the daemon's pre-execution check against a CLI
	// that predates --effort: the catalog must offer no levels, so any
	// persisted thinking_level is dropped (with a warning) instead of being
	// injected as a flag the binary rejects with "unknown option".
	fakeClaude := writeFakeClaudePreEffortHelpBinary(t)
	resetThinkingCacheForTests()
	defer resetThinkingCacheForTests()

	ctx := context.Background()

	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		ok, err := ValidateThinkingLevel(ctx, "claude", fakeClaude, "claude-fable-5", level)
		if err != nil {
			t.Fatalf("unexpected err for %q: %v", level, err)
		}
		if ok {
			t.Errorf("level %q must be invalid on a pre-effort CLI; got true", level)
		}
	}

	// Empty value still means "use runtime default" and must stay valid.
	ok, err := ValidateThinkingLevel(ctx, "claude", fakeClaude, "claude-fable-5", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Errorf("empty value must always be valid")
	}
}

func TestValidateThinkingLevel_OpenCodeEmptyModelUsesAdvertisedVariants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	modelCacheMu.Lock()
	delete(modelCache, "opencode")
	modelCacheMu.Unlock()
	defer func() {
		modelCacheMu.Lock()
		delete(modelCache, "opencode")
		modelCacheMu.Unlock()
	}()

	dir := t.TempDir()
	fake := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$1" = "models" ]; then
  cat <<'EOF'
opencode/deepseek-v4
{
  "id": "deepseek-v4",
  "reasoning": true,
  "variants": {
    "high": {},
    "max": {}
  }
}
EOF
  exit 0
fi
echo "opencode 9.9.9"
`
	writeTestExecutable(t, fake, []byte(script))

	ctx := context.Background()
	ok, err := ValidateThinkingLevel(ctx, "opencode", fake, "", "max")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatalf("expected empty-model opencode max to pass when any advertised model supports it")
	}

	ok, err = ValidateThinkingLevel(ctx, "opencode", fake, "", "xhigh")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ok {
		t.Fatalf("xhigh should fail when no advertised OpenCode model exposes it")
	}
}

// writeFakeClaudeHelpBinary writes a small shell script that mimics
// `claude --help`, emitting the full effort superset line so per-model
// projection has something to filter. Returns the path to the executable.
func writeFakeClaudeHelpBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"Usage: claude [options]\n" +
		"\n" +
		"Options:\n" +
		"  --model <model>     Model to use\n" +
		"  --effort <level>    Effort level for the current session (low, medium, high, xhigh, max)\n" +
		"EOF\n"
	// Same ForkLock rationale as TestRunCodexDebugModels_ArgvSeenByBinary —
	// the parser tests that consume this helper exec the script in parallel,
	// so a sibling fork can otherwise inherit our write fd and trip ETXTBSY.
	writeTestExecutable(t, path, []byte(script))
	return path
}

// writeFakeClaudePreEffortHelpBinary mimics a Claude Code release from
// before the --effort flag existed (e.g. 2.1.2): --help succeeds but has
// no --effort line at all.
func writeFakeClaudePreEffortHelpBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"Usage: claude [options]\n" +
		"\n" +
		"Options:\n" +
		"  --model <model>     Model to use\n" +
		"  --verbose\n" +
		"EOF\n"
	writeTestExecutable(t, path, []byte(script))
	return path
}

// writeFakeCodexModelsBinary writes a stand-in `codex` that answers
// `debug models --bundled` with a Codex 0.144.1-shaped gpt-5.6 catalog
// (sol/terra advertise max+ultra, luna tops out at max) and prints a version
// string for any other invocation (DetectVersion's probe). Used to exercise
// ValidateThinkingLevel against a real per-model catalog without a codex install.
func writeFakeCodexModelsBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"debug\" ]; then\n" +
		"cat <<'EOF'\n" +
		`{"models":[` +
		`{"slug":"gpt-5.6-sol","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"},{"effort":"ultra"}]},` +
		`{"slug":"gpt-5.6-terra","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"},{"effort":"ultra"}]},` +
		`{"slug":"gpt-5.6-luna","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"}]}` +
		`]}` + "\n" +
		"EOF\n" +
		"exit 0\n" +
		"fi\n" +
		"echo 'codex-cli 0.144.1'\n"
	writeTestExecutable(t, path, []byte(script))
	return path
}

// ── Cache key invalidation ───────────────────────────────────────────

func TestThinkingCacheKeyDistinct(t *testing.T) {
	t.Parallel()
	resetThinkingCacheForTests()
	defer resetThinkingCacheForTests()

	a := thinkingCacheKey{provider: "claude", executablePath: "/bin/claude", cliVersion: "2.1.121"}
	b := thinkingCacheKey{provider: "claude", executablePath: "/bin/claude", cliVersion: "2.1.122"}
	c := thinkingCacheKey{provider: "claude", executablePath: "/opt/claude", cliVersion: "2.1.121"}

	thinkingCachePut(a, map[string]*ModelThinking{"x": {DefaultLevel: "a"}})
	thinkingCachePut(b, map[string]*ModelThinking{"x": {DefaultLevel: "b"}})
	thinkingCachePut(c, map[string]*ModelThinking{"x": {DefaultLevel: "c"}})

	if got, _ := thinkingCacheGet(a); got["x"].DefaultLevel != "a" {
		t.Errorf("cache key A: got %q, want a", got["x"].DefaultLevel)
	}
	if got, _ := thinkingCacheGet(b); got["x"].DefaultLevel != "b" {
		t.Errorf("cache key B: got %q, want b", got["x"].DefaultLevel)
	}
	if got, _ := thinkingCacheGet(c); got["x"].DefaultLevel != "c" {
		t.Errorf("cache key C: got %q, want c", got["x"].DefaultLevel)
	}
}

// ── Shared injection fixture (Trump's MUL-2339 constraint) ───────────
//
// The three Codex injection points (thread/start.config,
// thread/resume.config, turn/start.effort) must encode the same
// thinking_level value, in the same shape per call type, with no
// drift. This fixture defines the expected payload once and asserts
// it across all three sites so a future refactor of any one site
// breaks the test if the other two aren't kept in sync.

// codexReasoningInjection is the shared expectation table for the
// three Codex injection points. value→{turnStartEffort, configKey}.
// One row per scenario.
type codexReasoningCase struct {
	name  string
	level string
}

var codexReasoningCases = []codexReasoningCase{
	{"empty-level-is-noop", ""},
	{"low", "low"},
	{"medium", "medium"},
	{"high", "high"},
	{"xhigh", "xhigh"},
	{"none-codex-only", "none"},
}

func TestApplyCodexReasoningEffort_ThreePoints(t *testing.T) {
	t.Parallel()
	for _, tc := range codexReasoningCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// 1. thread/start params shape.
			startParams := map[string]any{
				"model": "gpt-5.5",
				"cwd":   "/work",
			}
			applyCodexReasoningEffort(startParams, tc.level)
			assertCodexThreadConfigEffort(t, "thread/start", startParams, tc.level)

			// 2. thread/resume params shape.
			resumeParams := map[string]any{
				"threadId": "thr_prior",
				"cwd":      "/work",
				"model":    "gpt-5.5",
			}
			applyCodexReasoningEffort(resumeParams, tc.level)
			assertCodexThreadConfigEffort(t, "thread/resume", resumeParams, tc.level)

			// 3. turn/start params shape.
			turnParams := map[string]any{
				"threadId": "thr_x",
				"input":    []map[string]any{{"type": "text", "text": "hi"}},
			}
			applyCodexReasoningEffort(turnParams, tc.level)
			assertCodexTurnEffort(t, "turn/start", turnParams, tc.level)
		})
	}
}

// assertCodexThreadConfigEffort verifies the nested
// `config.model_reasoning_effort` shape used by thread/start and
// thread/resume. Empty level means the helper must be a no-op
// (no key emitted), not an empty-string value.
func assertCodexThreadConfigEffort(t *testing.T, method string, params map[string]any, want string) {
	t.Helper()
	cfgAny, hasCfg := params["config"]
	if want == "" {
		// Empty level → helper must not touch `config`. We allow the
		// caller to have pre-populated config with other keys, but the
		// reasoning effort key must NOT appear.
		if !hasCfg {
			return
		}
		cfg, _ := cfgAny.(map[string]any)
		if _, has := cfg["model_reasoning_effort"]; has {
			t.Errorf("%s: empty level must not emit model_reasoning_effort, got %v", method, cfg["model_reasoning_effort"])
		}
		return
	}
	if !hasCfg {
		t.Fatalf("%s: expected config block when level=%q", method, want)
	}
	cfg, ok := cfgAny.(map[string]any)
	if !ok {
		t.Fatalf("%s: config has wrong type %T", method, cfgAny)
	}
	got, ok := cfg["model_reasoning_effort"]
	if !ok {
		t.Fatalf("%s: missing config.model_reasoning_effort for level=%q (params=%+v)", method, want, params)
	}
	if got != want {
		t.Errorf("%s: config.model_reasoning_effort = %v, want %q", method, got, want)
	}
	// `effort` (turn/start key) must NOT leak into a thread call.
	if _, leaked := params["effort"]; leaked {
		t.Errorf("%s: top-level effort key leaked into thread params: %+v", method, params)
	}
}

// assertCodexTurnEffort verifies the top-level `effort` shape used by
// turn/start. Empty level means the helper must be a no-op (no key
// emitted), not an empty-string value.
func assertCodexTurnEffort(t *testing.T, method string, params map[string]any, want string) {
	t.Helper()
	got, has := params["effort"]
	if want == "" {
		if has {
			t.Errorf("%s: empty level must not emit effort, got %v", method, got)
		}
		// Nested config must also stay empty for the turn/start shape.
		if cfg, hasCfg := params["config"]; hasCfg {
			t.Errorf("%s: turn-shape params must not gain a config block, got %v", method, cfg)
		}
		return
	}
	if !has {
		t.Fatalf("%s: missing top-level effort for level=%q (params=%+v)", method, want, params)
	}
	if got != want {
		t.Errorf("%s: effort = %v, want %q", method, got, want)
	}
	// `config.model_reasoning_effort` must NOT leak into a turn call.
	if cfg, hasCfg := params["config"]; hasCfg {
		cfgMap, _ := cfg.(map[string]any)
		if _, leaked := cfgMap["model_reasoning_effort"]; leaked {
			t.Errorf("%s: config.model_reasoning_effort leaked into turn params: %+v", method, params)
		}
	}
}

func TestApplyCodexReasoningEffort_NilParamsSafe(t *testing.T) {
	t.Parallel()
	// Must not panic — defensive against future call sites passing nil.
	applyCodexReasoningEffort(nil, "high")
}

func TestApplyCodexReasoningEffort_PreservesPreExistingConfig(t *testing.T) {
	t.Parallel()
	// thread/start may already have other config keys (e.g. future Codex
	// fields). Reasoning effort must be additive, not destructive.
	startParams := map[string]any{
		"model": "gpt-5.5",
		"config": map[string]any{
			"some_future_key": "preserve_me",
		},
	}
	applyCodexReasoningEffort(startParams, "high")
	cfg, _ := startParams["config"].(map[string]any)
	if cfg["some_future_key"] != "preserve_me" {
		t.Errorf("pre-existing config key was clobbered: %+v", cfg)
	}
	if cfg["model_reasoning_effort"] != "high" {
		t.Errorf("reasoning effort not injected: %+v", cfg)
	}
}

// ── End-to-end: build*Args + thinking_level wiring ───────────────────

func TestBuildClaudeArgs_InjectsEffort(t *testing.T) {
	t.Parallel()
	args := buildClaudeArgs(ExecOptions{Model: "claude-opus-4-7", ThinkingLevel: "xhigh"}, slog.Default())
	if !containsAdjacent(args, "--effort", "xhigh") {
		t.Errorf("expected --effort xhigh in args: %v", args)
	}
	// Must appear after --model (cosmetic but enforced for log readability).
	modelIdx := argIndexOf(args, "--model")
	effortIdx := argIndexOf(args, "--effort")
	if modelIdx < 0 || effortIdx < 0 || modelIdx > effortIdx {
		t.Errorf("expected --model before --effort: %v", args)
	}
}

func TestBuildClaudeArgs_OmitsEffortWhenEmpty(t *testing.T) {
	t.Parallel()
	args := buildClaudeArgs(ExecOptions{Model: "claude-sonnet-4-6"}, slog.Default())
	if argIndexOf(args, "--effort") >= 0 {
		t.Errorf("expected no --effort when level empty: %v", args)
	}
}

func TestBuildClaudeArgs_BlocksUserEffortOverride(t *testing.T) {
	t.Parallel()
	args := buildClaudeArgs(ExecOptions{
		Model:         "claude-opus-4-7",
		ThinkingLevel: "high",
		CustomArgs:    []string{"--effort", "max", "--keep-me"},
	}, slog.Default())
	// Daemon-injected --effort survives.
	if !containsAdjacent(args, "--effort", "high") {
		t.Errorf("daemon-injected --effort high should remain: %v", args)
	}
	// User attempt to override is filtered out: no second --effort,
	// no `max` token.
	count := 0
	for _, a := range args {
		if a == "--effort" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one --effort, got %d: %v", count, args)
	}
	if argIndexOf(args, "max") >= 0 {
		t.Errorf("filtered user --effort value still appears: %v", args)
	}
	// Other custom args pass through.
	if argIndexOf(args, "--keep-me") < 0 {
		t.Errorf("non-blocked custom arg was dropped: %v", args)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────

func containsAdjacent(haystack []string, a, b string) bool {
	for i := 0; i < len(haystack)-1; i++ {
		if haystack[i] == a && haystack[i+1] == b {
			return true
		}
	}
	return false
}

func argIndexOf(slice []string, target string) int {
	for i, v := range slice {
		if v == target {
			return i
		}
	}
	return -1
}
