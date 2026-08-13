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
// claude, codex, opencode, pi, and kimi backends so the daemon can advertise
// them to the UI without hard-coding (and getting wrong) what's installed
// locally.
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
	"claude-opus-5":             {"low": true, "medium": true, "high": true, "xhigh": true, "max": true},
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
// `codex debug models --bundled` is the structured discovery hook for the
// visible model catalog, each model's reasoning catalog, and service tiers. OpenAI added
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
// model/thinking catalog. It does not guess service-tier availability.

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
	ServiceTiers            []codexDebugServiceTier    `json:"service_tiers"`
}

type codexDebugReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type codexDebugServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
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
		label = normalizeCodexModelLabel(m.Slug, label)
		models = append(models, Model{
			ID:           m.Slug,
			Label:        label,
			Provider:     "openai",
			Thinking:     codexThinkingFromDebugModel(m),
			ServiceTiers: codexServiceTiersFromDebugModel(m),
		})
	}
	if len(models) > 0 {
		models[0].Default = true
	}
	return models, nil
}

func normalizeCodexModelLabel(id, label string) string {
	switch id {
	case "gpt-5.6-sol":
		return "GPT-5.6 Sol"
	case "gpt-5.6-terra":
		return "GPT-5.6 Terra"
	case "gpt-5.6-luna":
		return "GPT-5.6 Luna"
	default:
		return label
	}
}

func codexServiceTiersFromDebugModel(m codexDebugModel) []ModelServiceTier {
	tiers := make([]ModelServiceTier, 0, len(m.ServiceTiers))
	for _, tier := range m.ServiceTiers {
		if tier.ID == "" {
			continue
		}
		name := tier.Name
		if name == "" {
			name = tier.ID
		}
		tiers = append(tiers, ModelServiceTier{
			ID:          tier.ID,
			Name:        name,
			Description: tier.Description,
		})
	}
	return tiers
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
// CodeBuddy uses the same `--effort <level>` flag as Claude. The level set is
// discovered from the `thought_level` config option in the ACP session/new
// response — the same handshake that yields the model catalog — so no extra
// process is spawned for it. All models share one effort catalog because
// CodeBuddy advertises it per session, not per model.

var codebuddyEffortLabel = map[string]string{
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
}

// codebuddyStaticEffortFallback is used when discovery cannot reach the CLI.
// It lists every level `--effort` accepts (confirmed against CodeBuddy 2.130.0,
// which advertises minimal/low/medium/high/xhigh/max) — the previous value
// omitted `minimal` and `max`, so a working install still lost two real levels
// whenever discovery degraded.
var codebuddyStaticEffortFallback = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// codebuddyThinkingByModel maps every model onto the shared effort catalog
// built from levels. CodeBuddy advertises one `--effort` set for the whole CLI,
// not per model, so every entry gets the same ModelThinking pointer.
func codebuddyThinkingByModel(models []Model, levels []string) map[string]*ModelThinking {
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
	return result
}

// applyCodebuddyStaticThinking annotates models with the static effort fallback.
// Used when discovery could not reach the CLI, or reached it but got no
// recognisable thought_level option back.
func applyCodebuddyStaticThinking(models []Model) {
	result := codebuddyThinkingByModel(models, codebuddyStaticEffortFallback)
	for i := range models {
		if t, ok := result[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

// codebuddyFlagEffortValues are the tokens `codebuddy --effort <level>` accepts.
//
// The ACP `thought_level` option advertises one extra choice, `enabled`
// ("On (default)"), which is a session-level toggle rather than a flag argument.
// The daemon passes the selected level straight through to `--effort`
// (codebuddy.go), so surfacing `enabled` in the picker would let a user build a
// command line CodeBuddy rejects. Filter against this set instead of trusting
// the advertised list wholesale.
var codebuddyFlagEffortValues = map[string]bool{
	"minimal": true,
	"low":     true,
	"medium":  true,
	"high":    true,
	"xhigh":   true,
	"max":     true,
}

// annotateCodebuddyThinkingFromACP fills in each model's effort catalog from the
// `thought_level` config option carried by the SAME `session/new` response the
// models came from — so the effort catalog costs no extra process at all. It
// replaces a second regex pass over `codebuddy --help` (MUL-5549).
//
// CodeBuddy advertises one effort set for the whole CLI rather than per model,
// so every entry shares it. Levels the `--effort` flag would reject are dropped,
// and a currentValue outside the flag set (the default `enabled`) becomes an
// empty DefaultLevel, which the UI renders as a generic "Default" instead of
// inventing a level we cannot pass through.
func annotateCodebuddyThinkingFromACP(models []Model, sessionResult json.RawMessage) {
	levels, defaultLevel := parseACPCodebuddyEffort(sessionResult)
	if len(levels) == 0 {
		applyCodebuddyStaticThinking(models)
		return
	}
	result := codebuddyThinkingByModel(models, levels)
	for _, thinking := range result {
		thinking.DefaultLevel = defaultLevel
	}
	for i := range models {
		if t, ok := result[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

// parseACPCodebuddyEffort extracts the effort levels and the advertised default
// from an ACP session/new result. Returns no levels when the response carries no
// recognisable effort option, which makes the caller fall back to the static
// set rather than hiding the thinking picker entirely.
//
// This is the shared parser (parseACPEffortOption) plus CodeBuddy's flag
// overlay. The overlay stays CodeBuddy-specific on purpose: it exists because
// this backend applies the level through `--effort` rather than over ACP, so
// its usable vocabulary is narrower than what its session advertises. Every
// other runtime takes the advertised list verbatim.
func parseACPCodebuddyEffort(raw json.RawMessage) (levels []string, defaultLevel string) {
	option, ok := parseACPEffortOption(raw)
	if !ok {
		return nil, ""
	}
	for _, choice := range option.Choices {
		if !codebuddyFlagEffortValues[choice.Value] {
			continue
		}
		levels = append(levels, choice.Value)
	}
	// Only echo a default we could actually pass to --effort.
	if codebuddyFlagEffortValues[option.CurrentValue] {
		defaultLevel = option.CurrentValue
	}
	return levels, defaultLevel
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
	catalog, err := ListModels(ctx, providerType, executablePath)
	if err != nil {
		return false, err
	}
	models := catalog.Models
	target := modelIDForCapabilityLookup(providerType, model)
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

// ValidateServiceTier reports whether value is advertised by the current
// Codex catalog for the explicit model. An empty value is always valid and
// means "inherit runtime configuration". An empty Codex model fails closed:
// its effective model comes from config.toml and may not support the tier.
func ValidateServiceTier(ctx context.Context, providerType, executablePath, model, value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	if providerType != "codex" || model == "" {
		return false, nil
	}
	catalog, err := ListModels(ctx, providerType, executablePath)
	if err != nil {
		return false, err
	}
	for _, m := range catalog.Models {
		if m.ID != model {
			continue
		}
		for _, tier := range m.ServiceTiers {
			if tier.ID == value {
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
	// Confirmed against CodeBuddy 2.130.0's advertised thought_level catalog.
	// `minimal` and `max` were missing here, so the server rejected two levels
	// the CLI genuinely accepts.
	"codebuddy": {
		"minimal": true,
		"low":     true,
		"medium":  true,
		"high":    true,
		"xhigh":   true,
		"max":     true,
	},
	// Grok 4.5's documented --effort levels. It cannot disable reasoning and
	// does not accept none, minimal, or xhigh.
	"grok": {
		"low":    true,
		"medium": true,
		"high":   true,
	},
	// Pi owns a fixed CLI vocabulary; RPC discovery narrows this universe to
	// the exact subset supported by each model before execution.
	"pi": {
		"off":     true,
		"minimal": true,
		"low":     true,
		"medium":  true,
		"high":    true,
		"xhigh":   true,
		"max":     true,
	},
}

// thinkingDynamicCatalogProviders are the runtimes whose effort vocabulary is
// owned by a daemon-local model catalog instead of a fixed enum above. The
// server accepts any well-formed token for them and lets the daemon's
// per-model check decide before execution.
var thinkingDynamicCatalogProviders = map[string]bool{
	"codex":    true,
	"dsh":      true,
	"opencode": true,
	"kimi":     true,
}

// acpCatalogThinkingProviders are the ACP runtimes that discover their effort
// catalog from `session/new` and apply it with `session/set_config_option`.
// They behave like the dynamic-catalog providers above — the server accepts a
// well-formed token and the daemon checks it against the discovered catalog —
// but they are listed separately because membership means something stricter:
// the runtime's Execute must actually call applyACPEffortOption.
//
// Do NOT add a runtime here just because it speaks ACP. Two things have to be
// true, and neither is implied by the protocol:
//
//   - Its Execute wires up applyACPEffortOption. Copilot is the counterexample
//     — its discovery runs over ACP but it executes through its own CLI
//     surface (`--acp` is blocked in copilot.go), so a catalog here would
//     render a picker with nothing behind it.
//   - Someone has confirmed the runtime actually threads the setting into its
//     provider request, from its source or a real run. Advertising is not
//     evidence — Hermes accepts set_config_option and ignores it, Kimi ≤0.28.1
//     confirms "on" after being set to "max" — and neither is the read-back in
//     applyACPEffortOption, which only proves the session reports the new
//     value. This list is where that offline verification is recorded; the
//     read-back is runtime diagnostics on top of it.
var acpCatalogThinkingProviders = map[string]bool{
	// reasonix v1.21.5: session/new advertises option id `effort` (category
	// `thought_level`), set_config_option returns the refreshed options, and
	// the effort reaches the session controller rather than stopping at the
	// config surface. Its catalog is per model — see
	// annotateACPThinkingForSessionModel.
	"reasonix": true,
	// hermes covers two unrelated binaries, and membership here is safe only
	// because the catalog decides per session which one answered:
	//
	//   - jcode advertises option id `reasoning_effort` (category
	//     `thought_level`) and genuinely applies it — set_config_option waits
	//     for an `effort_changed` ack, and the provider request carries
	//     `reasoning.effort` upstream. Confirmed against jcode v0.71.1 and
	//     v0.73.0 (GitHub #6720). Its catalog is per model too: jcode
	//     revalidates the effort against the new model's advertised list on a
	//     model switch.
	//   - Hermes Agent advertises no configOptions at all, so it gets an empty
	//     catalog, no picker, and no set_config_option call. Re-verified
	//     against v0.20.0 on 2026-08-11: session/new still returns only
	//     `_meta`, `models`, `modes`, `sessionId` — unchanged from the v0.18.2
	//     finding in MUL-5770.
	//
	// That split is why this feature is catalog-driven rather than gated on a
	// version string: one provider, two binaries, and the session answers the
	// capability question directly.
	"hermes": true,
}

// usesDynamicThinkingCatalog reports whether a provider's effort vocabulary is
// owned by a daemon-local catalog rather than a fixed server-side enum.
func usesDynamicThinkingCatalog(providerType string) bool {
	return thinkingDynamicCatalogProviders[providerType] || acpCatalogThinkingProviders[providerType]
}

// UsesACPCatalogThinking reports whether a provider's effort support is decided
// per session by what its ACP handshake advertises, rather than by the provider
// name alone.
//
// Callers that can reach a discovered catalog should use it to answer the
// capability question for a specific runtime: `hermes` covers both jcode (which
// advertises and applies an effort) and Hermes Agent (which advertises none), so
// the provider name is not a sufficient answer for either. See
// acpCatalogThinkingProviders.
func UsesACPCatalogThinking(providerType string) bool {
	return acpCatalogThinkingProviders[providerType]
}

// ThinkingControlSupported reports whether Multica can deliver a per-agent
// reasoning effort to this runtime at all. False means the answer to any
// thinking_level is "no", regardless of the token: the runtime exposes no
// effort dial on the surface the daemon speaks to it over, so there is nothing
// to inject and nothing a different spelling would fix.
//
// Copilot is the instructive case. It speaks ACP for model discovery but
// executes through its own CLI surface (`--acp` is blocked in copilot.go), so
// there is no live ACP session to carry an effort onto — a picker there would
// be inert no matter what discovery advertised.
//
// True at provider granularity only. The `hermes` provider covers two
// unrelated binaries whose answers differ — jcode applies an advertised
// effort, Hermes Agent has no effort surface on ACP at all — so it reports
// true here and the per-session catalog decides whether a picker actually
// appears. See acpCatalogThinkingProviders for the evidence on each.
func ThinkingControlSupported(providerType string) bool {
	if usesDynamicThinkingCatalog(providerType) {
		return true
	}
	_, ok := providerThinkingEnums[providerType]
	return ok
}

// IsKnownThinkingValue reports whether `value` is a recognised effort
// token for the given provider. Empty string is always accepted (means
// "use runtime default"). Providers with no reasoning control accept
// only empty; Codex, OpenCode, Kimi, and the ACP catalog runtimes accept
// well-formed tokens here because their daemon-local catalogs perform the
// exact per-model check before execution.
//
// This is the cheap synchronous gate the server uses on CreateAgent /
// UpdateAgent. Unlike ValidateThinkingLevel it does NOT consult the live
// catalog or per-model subset. Callers that surface a rejection to a user
// should ask ThinkingControlSupported first so the message says "this runtime
// has no reasoning control" instead of implying a bad token.
func IsKnownThinkingValue(providerType, value string) bool {
	if value == "" {
		return true
	}
	if usesDynamicThinkingCatalog(providerType) {
		return isValidDynamicThinkingValue(value)
	}
	enum, ok := providerThinkingEnums[providerType]
	if !ok {
		return false
	}
	return enum[value]
}

// IsKnownServiceTier is the server-side literal gate. The exact per-model
// catalog lives on the daemon host, so Codex accepts safe future catalog IDs
// here and ValidateServiceTier performs the execution-time compatibility
// check. Other providers do not currently expose service tiers.
func IsKnownServiceTier(providerType, value string) bool {
	if value == "" {
		return true
	}
	return providerType == "codex" && isValidDynamicThinkingValue(value)
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
