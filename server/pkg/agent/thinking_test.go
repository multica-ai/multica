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
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

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

// ── Codex debug models JSON parsing ──────────────────────────────────

func TestParseCodexDebugModels(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"models": [
			{
				"slug": "gpt-5.5",
				"default_reasoning_level": "medium",
				"supported_reasoning_levels": [
					{"effort": "low", "description": "Fast"},
					{"effort": "medium", "description": "Balanced"},
					{"effort": "high", "description": "Deeper"},
					{"effort": "xhigh", "description": "Maximum"}
				]
			},
			{
				"slug": "gpt-5",
				"default_reasoning_level": "low",
				"supported_reasoning_levels": [
					{"effort": "minimal", "description": "Quick"},
					{"effort": "low", "description": "Fast"}
				]
			},
			{
				"slug": "no-reasoning",
				"supported_reasoning_levels": []
			}
		]
	}`)
	got := parseCodexDebugModels(raw)

	gpt55, ok := got["gpt-5.5"]
	if !ok || gpt55 == nil {
		t.Fatalf("missing gpt-5.5 entry: %+v", got)
	}
	if gpt55.DefaultLevel != "medium" {
		t.Errorf("gpt-5.5 default: got %q, want medium", gpt55.DefaultLevel)
	}
	if len(gpt55.SupportedLevels) != 4 {
		t.Errorf("gpt-5.5 supported count: got %d, want 4", len(gpt55.SupportedLevels))
	}
	// Labels should come from codexEffortLabel mapping, not from raw effort.
	for _, lvl := range gpt55.SupportedLevels {
		if lvl.Value == "xhigh" && lvl.Label != "Extra high" {
			t.Errorf("xhigh label: got %q, want Extra high", lvl.Label)
		}
	}

	gpt5, ok := got["gpt-5"]
	if !ok || gpt5 == nil {
		t.Fatalf("missing gpt-5 entry: %+v", got)
	}
	if gpt5.DefaultLevel != "low" {
		t.Errorf("gpt-5 default: got %q, want low", gpt5.DefaultLevel)
	}

	// Models with empty supported_reasoning_levels should be omitted to
	// keep the wire payload small and avoid rendering empty pickers.
	if _, ok := got["no-reasoning"]; ok {
		t.Errorf("no-reasoning should be omitted, got %+v", got["no-reasoning"])
	}
}

func TestParseCodexDebugModels_Malformed(t *testing.T) {
	t.Parallel()
	got := parseCodexDebugModels([]byte("not json"))
	if len(got) != 0 {
		t.Fatalf("expected empty map on malformed input, got %+v", got)
	}
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
		{"codex", "max", false}, // Claude-only token rejected for Codex
		{"hermes", "", true},
		{"hermes", "low", false}, // hermes has no thinking concept
	}
	for _, tc := range tests {
		if got := IsKnownThinkingValue(tc.provider, tc.value); got != tc.want {
			t.Errorf("IsKnownThinkingValue(%q, %q) = %v, want %v",
				tc.provider, tc.value, got, tc.want)
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
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
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

