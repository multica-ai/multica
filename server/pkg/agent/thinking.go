package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// thinking.go discovers per-model reasoning/effort catalogs for the
// claude, codex, and opencode backends so the daemon can advertise them to the
// UI without hard-coding (and getting wrong) what's installed locally.
//
// MUL-2339: we deliberately do not flatten Claude's `low|medium|high|
// xhigh|max` and Codex's `none|minimal|low|medium|high|xhigh|max|ultra`
// onto a shared enum. OpenCode exposes provider-specific model variants through
// `opencode run --variant`, and those names can be extended by local
// opencode.json config. What users pick must round-trip exactly through
// each CLI's own value vocabulary.

// ── Cache ────────────────────────────────────────────────────────────
//
// Discovery is keyed on (provider, executablePath, cliVersion). Bumping
// the local CLI invalidates entries that referenced the older version's
// help/`debug models` output, which is exactly the failure mode we hit
// when Anthropic / OpenAI add or remove a level (Elon's review note).

type thinkingCacheKey struct {
	provider       string
	executablePath string
	cliVersion     string
}

type thinkingCacheEntry struct {
	value     map[string]*ModelThinking // keyed by model ID
	expiresAt time.Time
}

const thinkingDiscoveryTTL = 10 * time.Minute

var (
	thinkingCacheMu sync.Mutex
	thinkingCache   = map[thinkingCacheKey]thinkingCacheEntry{}
)

func thinkingCacheGet(key thinkingCacheKey) (map[string]*ModelThinking, bool) {
	thinkingCacheMu.Lock()
	defer thinkingCacheMu.Unlock()
	entry, ok := thinkingCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func thinkingCachePut(key thinkingCacheKey, value map[string]*ModelThinking) {
	thinkingCacheMu.Lock()
	defer thinkingCacheMu.Unlock()
	thinkingCache[key] = thinkingCacheEntry{value: value, expiresAt: time.Now().Add(thinkingDiscoveryTTL)}
}

// resetThinkingCacheForTests is exposed for tests only; production code
// must rely on the TTL or process restart for invalidation.
func resetThinkingCacheForTests() {
	thinkingCacheMu.Lock()
	thinkingCache = map[thinkingCacheKey]thinkingCacheEntry{}
	thinkingCacheMu.Unlock()
}

// ── Claude ───────────────────────────────────────────────────────────
//
// `claude --help` advertises `--effort <level>` with the full superset
// in parentheses; we parse that line to learn which levels the CLI
// version on this host accepts. Per-model gaps (Opus-only `xhigh`,
// session-only `max`) come from a hand-maintained table because the
// CLI does not expose model→effort mappings programmatically.

// claudeEffortRe matches the help line emitted by `claude --help`:
//
//	--effort <level>   Effort level for the current session (low, medium, high, xhigh, max)
//
// Anchored on `--effort` and lenient about whitespace so flag-name
// reformats (`--effort=…`, indented help blocks) do not break parsing.
var claudeEffortRe = regexp.MustCompile(`--effort\s*(?:<[^>]+>)?\s*(?:Effort level[^(]*)?\(([^)]+)\)`)

// claudeEffortLabel maps Claude's raw level token to the display label
// the UI should render. Title-case matches Anthropic's own slash UI.
var claudeEffortLabel = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"xhigh":  "Extra high",
	"max":    "Max",
}

// claudeModelEffortAllow restricts the level set per model where the
// upstream documentation says only some are valid. Empty / missing
// model → use the parsed superset as-is (current Claude Code default).
// Update this map when Anthropic publishes a new model that does not
// support `xhigh` / `max`.
var claudeModelEffortAllow = map[string]map[string]bool{
	// Opus is the only model that publicly supports xhigh; the help
	// list still includes it for Sonnet / Haiku so we filter here.
	"claude-opus-4-8":           {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-opus-4-7":           {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-opus-4-6":           {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	"claude-sonnet-4-6":         {"low": true, "medium": true, "high": true, "max": true},
	"claude-sonnet-4-5":         {"low": true, "medium": true, "high": true, "max": true},
	"claude-haiku-4-5-20251001": {"low": true, "medium": true, "high": true},
}

// claudeStaticEffortFallback is the conservative subset used when
// parsing the `--effort` help line fails (binary missing, output drift,
// etc.). Picked from the lowest-common-denominator across recent
// Claude Code releases.
var claudeStaticEffortFallback = []string{"low", "medium", "high"}

// claudeStaticEffortFullSuperset is what `claude --help` listed on
// 2.1.121. Used as the catalog superset when a model isn't in the
// per-model allow-list — we'd rather over-offer and let the CLI
// reject than artificially block valid combinations.
var claudeStaticEffortFullSuperset = []string{"low", "medium", "high", "xhigh", "max"}

// annotateClaudeThinking populates each entry's Thinking field by
// running `claude --help` once and projecting the parsed superset
// through claudeModelEffortAllow. Errors are silently absorbed so a
// missing CLI doesn't break model listing — the UI just hides the
// picker for that model.
func annotateClaudeThinking(ctx context.Context, models []Model, executablePath string) {
	mapping := loadClaudeThinkingByModel(ctx, executablePath)
	for i := range models {
		if t, ok := mapping[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

func loadClaudeThinkingByModel(ctx context.Context, executablePath string) map[string]*ModelThinking {
	if executablePath == "" {
		executablePath = "claude"
	}
	version, _ := DetectVersion(ctx, executablePath)
	key := thinkingCacheKey{provider: "claude", executablePath: executablePath, cliVersion: version}
	if cached, ok := thinkingCacheGet(key); ok {
		return cached
	}

	superset := claudeEffortSuperset(ctx, executablePath)
	result := map[string]*ModelThinking{}
	for _, m := range claudeStaticModels() {
		allow := claudeModelEffortAllow[m.ID]
		levels := projectClaudeLevels(superset, allow)
		if len(levels) == 0 {
			continue
		}
		result[m.ID] = &ModelThinking{
			SupportedLevels: levels,
			DefaultLevel:    "medium",
		}
	}
	thinkingCachePut(key, result)
	return result
}

// claudeEffortSuperset returns the parsed `--effort` value list. When
// the help output can't be captured at all it returns the static
// fallback rather than nothing so callers can still render a usable
// picker.
func claudeEffortSuperset(ctx context.Context, executablePath string) []string {
	cmd := exec.CommandContext(ctx, executablePath, "--help")
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return append([]string(nil), claudeStaticEffortFallback...)
	}
	return claudeEffortLevelsFromHelp(string(out))
}

// claudeEffortLevelsFromHelp decides the effort superset from a
// successfully captured `claude --help`. Three cases:
//   - the value list parsed → use it verbatim;
//   - `--effort` is advertised but the value list didn't parse → help
//     format drifted; fall back to the last known good superset so
//     newer levels are still offered until we hand-edit the fallback;
//   - `--effort` is absent entirely → the installed CLI predates the
//     flag. Return no levels: offering any would let the daemon pass
//     ValidateThinkingLevel and inject --effort, which such a binary
//     rejects with `error: unknown option '--effort'` — hard-failing
//     every task for an agent with a persisted thinking_level instead
//     of degrading to a plain run.
func claudeEffortLevelsFromHelp(helpText string) []string {
	parsed := parseClaudeEffortHelp(helpText)
	if len(parsed) > 0 {
		return parsed
	}
	if strings.Contains(helpText, "--effort") {
		return append([]string(nil), claudeStaticEffortFullSuperset...)
	}
	return nil
}

// parseClaudeEffortHelp extracts the comma-separated value list from a
// `--effort` help line. Returns nil if the line is missing or the
// captured group is empty so callers can pick a fallback path.
func parseClaudeEffortHelp(helpText string) []string {
	match := claudeEffortRe.FindStringSubmatch(helpText)
	if len(match) < 2 {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(match[1], ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}

func projectClaudeLevels(superset []string, allow map[string]bool) []ThinkingLevel {
	out := make([]ThinkingLevel, 0, len(superset))
	for _, value := range superset {
		if allow != nil && !allow[value] {
			continue
		}
		label, ok := claudeEffortLabel[value]
		if !ok {
			// New value the daemon hasn't been taught yet — surface
			// it raw so power users can still pick it.
			label = strings.Title(value) //nolint:staticcheck
		}
		out = append(out, ThinkingLevel{Value: value, Label: label})
	}
	return out
}

// ── Codex ────────────────────────────────────────────────────────────
//
// `codex debug models --bundled` is the structured discovery hook for both
// the visible model catalog and each model's reasoning catalog. OpenAI added
// the command and `--bundled` flag together in Codex 0.122.0 (openai/codex
// #18625). Older versions, failed invocations, and malformed/empty payloads
// use codexStaticModels so the picker remains usable.
//
// We prefer this over the older config-error probe trick because:
//   1. It gives us per-model subsets without hand-maintained tables.
//   2. The schema is structured and has been stable since its 0.122.0 debut.
//   3. It doesn't pollute stderr with an intentional misconfiguration.
//
// The subcommand emits JSON on stdout by default — there is no
// `--output json` flag (a prior version of this code passed one and
// silently failed on 0.131.0). We add `--bundled` to skip the network
// refresh: discovery runs on every daemon poll and a network hop here
// would block the picker behind whatever the user's connection allows.
// The bundled catalog is what determines which `model_reasoning_effort`
// tokens the local binary actually accepts, which is the only thing we
// need for validation.
//
// The static fallback deliberately mirrors a recently verified bundled
// catalog, including thinking metadata, rather than guessing model IDs.

// codexEffortLabel is the human display string for each Codex effort
// value, matching Codex's own TUI (`Extra high`, `Minimal`, …) so
// users see the same labels across our picker and `codex /model`.
var codexEffortLabel = map[string]string{
	"none":    "None",
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
	"ultra":   "Ultra",
}

const minCodexDebugModelsVersion = "0.122.0"

// codexDebugModelsResponse mirrors the JSON shape emitted by
// `codex debug models --bundled` (Codex 0.122.0+). Only the fields we
// consume are typed; unknown keys are ignored.
type codexDebugModelsResponse struct {
	Models []codexDebugModel `json:"models"`
}

type codexDebugModel struct {
	Slug                    string                     `json:"slug"`
	DisplayName             string                     `json:"display_name"`
	Visibility              string                     `json:"visibility"`
	DefaultReasoningLevel   string                     `json:"default_reasoning_level"`
	SupportedReasoningLevel []codexDebugReasoningLevel `json:"supported_reasoning_levels"`
}

type codexDebugReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// discoverCodexModels returns the installed Codex binary's bundled visible
// catalog, including reasoning metadata. Version detection happens before the
// debug command so old binaries do not log a predictable "unknown command"
// failure on every cache refresh.
func discoverCodexModels(ctx context.Context, executablePath string) []Model {
	if executablePath == "" {
		executablePath = "codex"
	}
	version, err := DetectVersion(ctx, executablePath)
	if err != nil || !codexSupportsDebugModels(version) {
		return codexStaticModels()
	}

	raw, err := runCodexDebugModels(ctx, executablePath)
	if err != nil {
		return codexStaticModels()
	}
	models, err := parseCodexModelCatalog(raw)
	if err != nil || len(models) == 0 {
		return codexStaticModels()
	}
	return models
}

func codexSupportsDebugModels(version string) bool {
	parsed, err := parseSemver(version)
	if err != nil {
		return false
	}
	minimum, err := parseSemver(minCodexDebugModelsVersion)
	if err != nil {
		return false
	}
	return !parsed.lessThan(minimum)
}

// codexDebugModelsArgs is the argv we pass to discover the local Codex
// catalog. Kept as a package-level var (not a literal at the call site)
// so tests can assert the exact form a real `codex` invocation receives,
// not just the parser behavior on a fixture string. The argv shape is
// the contract that broke under PR1 review; the test that pins it sits
// in thinking_test.go.
var codexDebugModelsArgs = []string{"debug", "models", "--bundled"}

func runCodexDebugModels(ctx context.Context, executablePath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executablePath, codexDebugModelsArgs...)
	hideAgentWindow(cmd)
	return cmd.Output()
}

// parseCodexModelCatalog projects the CLI's raw catalog into the daemon wire
// model. Hidden entries are intentionally excluded to match Codex's own model
// picker; the first visible entry is the bundled catalog's preferred default.
func parseCodexModelCatalog(raw []byte) ([]Model, error) {
	var resp codexDebugModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(resp.Models))
	for _, m := range resp.Models {
		if m.Slug == "" || m.Visibility == "hide" {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = m.Slug
		}
		models = append(models, Model{
			ID:       m.Slug,
			Label:    label,
			Provider: "openai",
			Thinking: codexThinkingFromDebugModel(m),
		})
	}
	if len(models) > 0 {
		models[0].Default = true
	}
	return models, nil
}

func codexThinkingFromDebugModel(m codexDebugModel) *ModelThinking {
	levels := make([]ThinkingLevel, 0, len(m.SupportedReasoningLevel))
	for _, lvl := range m.SupportedReasoningLevel {
		if lvl.Effort == "" {
			continue
		}
		label, ok := codexEffortLabel[lvl.Effort]
		if !ok {
			// Codex effort tokens are catalog-owned. Surface new safe tokens
			// immediately; the server accepts their syntax and the daemon uses
			// this exact per-model catalog for compatibility validation.
			label = strings.Title(lvl.Effort) //nolint:staticcheck
		}
		levels = append(levels, ThinkingLevel{
			Value:       lvl.Effort,
			Label:       label,
			Description: lvl.Description,
		})
	}
	if len(levels) == 0 {
		return nil
	}
	return &ModelThinking{
		SupportedLevels: levels,
		DefaultLevel:    m.DefaultReasoningLevel,
	}
}

// ── CodeBuddy ────────────────────────────────────────────────────────
//
// CodeBuddy uses the same `--effort <level>` flag as Claude but with a
// different level set (no `max`). Discovery parses `--help` identically
// to the claude approach. All models get the same effort levels since
// CodeBuddy doesn't document per-model restrictions.

var codebuddyEffortRe = regexp.MustCompile(`--effort\s*(?:<[^>]+>)?\s*[^(]*\(([^)]+)\)`)

var codebuddyEffortLabel = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"xhigh":  "Extra high",
}

var codebuddyStaticEffortFallback = []string{"low", "medium", "high", "xhigh"}

// codebuddyHelpCache caches the raw --help output so both model discovery
// (models.go) and effort discovery avoid redundant slow CLI invocations.
// CodeBuddy's --help takes ~30s; calling it twice on cold start wastes ~30s.
var (
	codebuddyHelpMu    sync.Mutex
	codebuddyHelpStore = map[string]codebuddyHelpEntry{}
)

const codebuddyHelpTTL = 60 * time.Second

type codebuddyHelpEntry struct {
	output    string
	expiresAt time.Time
}

// codebuddyHelpOutput runs `codebuddy --help` (cached for codebuddyHelpTTL).
// Both discoverCodebuddyModels and codebuddyEffortSuperset call this so a
// single cold invocation feeds both.
func codebuddyHelpOutput(ctx context.Context, executablePath string) string {
	if executablePath == "" {
		executablePath = "codebuddy"
	}
	key := executablePath
	codebuddyHelpMu.Lock()
	if entry, ok := codebuddyHelpStore[key]; ok && time.Now().Before(entry.expiresAt) {
		codebuddyHelpMu.Unlock()
		return entry.output
	}
	codebuddyHelpMu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "--help")
	hideAgentWindow(cmd)
	out, _ := cmd.CombinedOutput()
	result := string(out)

	if result != "" {
		codebuddyHelpMu.Lock()
		codebuddyHelpStore[key] = codebuddyHelpEntry{output: result, expiresAt: time.Now().Add(codebuddyHelpTTL)}
		codebuddyHelpMu.Unlock()
	}
	return result
}

func annotateCodebuddyThinking(ctx context.Context, models []Model, executablePath string) {
	if executablePath == "" {
		executablePath = "codebuddy"
	}
	version, _ := DetectVersion(ctx, executablePath)
	key := thinkingCacheKey{provider: "codebuddy", executablePath: executablePath, cliVersion: version}
	if cached, ok := thinkingCacheGet(key); ok {
		for i := range models {
			if t, ok := cached[models[i].ID]; ok && t != nil {
				models[i].Thinking = t
			}
		}
		return
	}

	levels := codebuddyEffortSuperset(ctx, executablePath)
	thinkingLevels := make([]ThinkingLevel, 0, len(levels))
	for _, value := range levels {
		label, ok := codebuddyEffortLabel[value]
		if !ok {
			label = strings.Title(value) //nolint:staticcheck
		}
		thinkingLevels = append(thinkingLevels, ThinkingLevel{Value: value, Label: label})
	}

	result := map[string]*ModelThinking{}
	if len(thinkingLevels) > 0 {
		thinking := &ModelThinking{
			SupportedLevels: thinkingLevels,
			DefaultLevel:    "medium",
		}
		for _, m := range models {
			result[m.ID] = thinking
		}
	}
	thinkingCachePut(key, result)

	for i := range models {
		if t, ok := result[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

func codebuddyEffortSuperset(ctx context.Context, executablePath string) []string {
	helpOut := codebuddyHelpOutput(ctx, executablePath)
	if helpOut == "" {
		return append([]string(nil), codebuddyStaticEffortFallback...)
	}
	parsed := parseCodebuddyEffortHelp(helpOut)
	if len(parsed) == 0 {
		return append([]string(nil), codebuddyStaticEffortFallback...)
	}
	return parsed
}

func parseCodebuddyEffortHelp(helpText string) []string {
	match := codebuddyEffortRe.FindStringSubmatch(helpText)
	if len(match) < 2 {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(match[1], ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}

// ── Shared validation ────────────────────────────────────────────────

// ValidateThinkingLevel reports whether `value` is in the supported
// catalog for the given (provider, model) pair. Empty value is always
// valid — it means "use the runtime default".
//
// Empty model means "follow the runtime's own default", resolved at task
// time. How safely we can validate an effort against that depends on the
// provider:
//
//   - codex: the effective model comes from the user's local config.toml
//     and can be ANY installed model, not necessarily the catalog's flagged
//     Default. Borrowing the Default entry (gpt-5.6-sol, the only one
//     advertising `ultra`) would green-light levels the actually-configured
//     model may not support — Luna tops out at `max`, gpt-5.5/5.4 at `xhigh`
//     — and Codex does not reject the mismatch itself. We can't know the
//     effective model without parsing config.toml in the task cwd (see this
//     file's Codex header for why that's avoided), so an empty codex model
//     fails closed: the daemon drops the level rather than injecting one that
//     may not fit. Users who need a specific effort must pick an explicit
//     model. (MUL-4347 review.)
//   - other providers: empty model resolves to the catalog's Default entry
//     so a default-model task with a valid thinking_level isn't misjudged as
//     "unknown model → reject" (the misjudgement flagged in an earlier
//     review). opencode has no single default, so it accepts a level any
//     advertised model supports.
//
// The lookup goes through ListModels so it sees the *current* CLI
// catalog (including dynamic discovery for codex), not just a static
// map. The function is intentionally pure of HTTP concerns so the
// daemon's pre-execution guard and the server's UpdateAgent gate can
// share the same source of truth.
func ValidateThinkingLevel(ctx context.Context, providerType, executablePath, model, value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	// Codex empty-model fail-closed (see doc comment). Checked before
	// ListModels so the outcome is deterministic even when discovery would
	// error — an errored lookup makes the daemon pass the level through, which
	// is exactly what we must NOT do for an unresolved codex model.
	if model == "" && providerType == "codex" {
		return false, nil
	}
	models, err := ListModels(ctx, providerType, executablePath)
	if err != nil {
		return false, err
	}
	target := model
	if target == "" {
		// Default model = the entry the catalog marks as Default. If no
		// entry is flagged, fall through to the no-match return; that
		// matches the existing semantics where an unknown model fails
		// closed rather than guessing.
		for _, m := range models {
			if m.Default {
				target = m.ID
				break
			}
		}
		if target == "" {
			if providerType == "opencode" {
				return anyModelSupportsThinkingValue(models, value), nil
			}
			return false, nil
		}
	}
	for _, m := range models {
		if m.ID != target {
			continue
		}
		if m.Thinking == nil {
			return false, nil
		}
		for _, lvl := range m.Thinking.SupportedLevels {
			if lvl.Value == value {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func anyModelSupportsThinkingValue(models []Model, value string) bool {
	for _, m := range models {
		if m.Thinking == nil {
			continue
		}
		for _, lvl := range m.Thinking.SupportedLevels {
			if lvl.Value == value {
				return true
			}
		}
	}
	return false
}

// providerThinkingEnums is the server-side accept-list for runtimes with a
// fixed reasoning-effort vocabulary. Codex and OpenCode are deliberately
// absent because their values come from daemon-local model catalogs, which can
// gain new tokens without a Multica release.
//
// The server doesn't have local CLI binaries, so it cannot do per-model
// discovery the way the daemon can. Fixed-catalog providers use this enum;
// dynamic providers take the safe-token path in IsKnownThinkingValue below.
// Per-model gaps are handled by the daemon's pre-execution guard, which logs
// and skips injection rather than mutating persisted agent state.
//
// Keep fixed-provider lists permissive: this is a provider-universe check,
// not an "is this right for this model" check.
var providerThinkingEnums = map[string]map[string]bool{
	"claude": {
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
		"max":    true,
	},
	"codebuddy": {
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
	},
	// Grok 4.5's documented --effort levels. It cannot disable reasoning and
	// does not accept none, minimal, or xhigh.
	"grok": {
		"low":    true,
		"medium": true,
		"high":   true,
	},
}

// IsKnownThinkingValue reports whether `value` is a recognised effort
// token for the given provider. Empty string is always accepted (means
// "use runtime default"). Unknown providers (no thinking concept) accept
// only empty; Codex and OpenCode accept well-formed tokens here because their
// daemon-local catalogs perform the exact per-model check before execution.
//
// This is the cheap synchronous gate the server uses on CreateAgent /
// UpdateAgent. Unlike ValidateThinkingLevel it does NOT consult the live
// catalog or per-model subset.
func IsKnownThinkingValue(providerType, value string) bool {
	if value == "" {
		return true
	}
	if providerType == "codex" || providerType == "opencode" {
		return isValidDynamicThinkingValue(value)
	}
	enum, ok := providerThinkingEnums[providerType]
	if !ok {
		return false
	}
	return enum[value]
}

func isValidDynamicThinkingValue(value string) bool {
	if len(value) > 64 {
		return false
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.'
		if !valid {
			return false
		}
		if i == 0 && (r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
