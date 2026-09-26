package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
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
// Discovery is keyed on (provider, command, cliVersion). Bumping
// the local CLI invalidates entries that referenced the older version's
// help/`debug models` output, which is exactly the failure mode we hit
// when Anthropic / OpenAI add or remove a level (Elon's review note).
//
// command is Command.cacheKey(), not a bare path: two custom runtime
// profiles can wrap one binary behind different launch prefixes and get
// different answers out of it.

type thinkingCacheKey struct {
	provider   string
	command    string
	cliVersion string
}

type thinkingCacheEntry struct {
	value     claudeEffortHelp
	expiresAt time.Time
}

const thinkingDiscoveryTTL = 10 * time.Minute

var (
	thinkingCacheMu sync.Mutex
	thinkingCache   = map[thinkingCacheKey]thinkingCacheEntry{}
)

func thinkingCacheGet(key thinkingCacheKey) (claudeEffortHelp, bool) {
	thinkingCacheMu.Lock()
	defer thinkingCacheMu.Unlock()
	entry, ok := thinkingCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return claudeEffortHelp{}, false
	}
	return entry.value, true
}

func thinkingCachePut(key thinkingCacheKey, value claudeEffortHelp) {
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
// Live discovery (claude_models.go) gets each model's effort levels from the
// CLI itself. This section only serves the static catalog used when that
// discovery fails: `claude --help` advertises `--effort <level>` with the full
// superset in parentheses, and every static model offers that superset. There
// is deliberately no per-model table — a fallback catalog is a picker
// affordance, not something the daemon validates against, so narrowing it per
// model would only be one more list to keep in step with new models
// (MUL-7691).

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

// claudeStaticEffortFallback is the conservative picker used when
// `claude --help` cannot be captured at all (binary missing, timeout).
// Picked from the lowest-common-denominator across recent Claude Code
// releases.
var claudeStaticEffortFallback = []string{"low", "medium", "high"}

// claudeStaticEffortFullSuperset is what `claude --help` listed on
// 2.1.121. The picker offers it when help names `--effort` but its value
// list no longer parses — we'd rather over-offer and let the CLI reject
// than artificially block valid combinations.
var claudeStaticEffortFullSuperset = []string{"low", "medium", "high", "xhigh", "max"}

// claudeEffortHelp is what one `claude --help` run established about
// `--effort`.
type claudeEffortHelp struct {
	// flag is the binary's own vocabulary, surfaced as
	// Catalog.CLIThinkingLevels: nil when help could not establish it, a
	// non-nil empty slice when the binary predates the flag.
	flag []string
	// picker is what the static catalog offers every model.
	picker []string
}

// annotateClaudeThinking gives every static model the effort picker read
// from `claude --help` and returns the binary's own `--effort` vocabulary
// (see claudeEffortHelp.flag). Errors are silently absorbed so a missing
// CLI doesn't break model listing.
func annotateClaudeThinking(ctx context.Context, models []Model, cmd Command) []string {
	help := loadClaudeEffortHelp(ctx, cmd)
	levels := claudeThinkingLevels(help.picker)
	if len(levels) > 0 {
		for i := range models {
			models[i].Thinking = &ModelThinking{
				SupportedLevels: append([]ThinkingLevel(nil), levels...),
				DefaultLevel:    "medium",
			}
		}
	}
	return help.flag
}

func loadClaudeEffortHelp(ctx context.Context, cmd Command) claudeEffortHelp {
	if cmd.Path == "" {
		cmd.Path = "claude"
	}
	version, _ := DetectVersion(ctx, cmd)
	key := thinkingCacheKey{provider: "claude", command: cmd.cacheKey(), cliVersion: version}
	if cached, ok := thinkingCacheGet(key); ok {
		return cached
	}
	help := readClaudeEffortHelp(ctx, cmd)
	thinkingCachePut(key, help)
	return help
}

// readClaudeEffortHelp runs `claude --help`. When its output can't be
// captured at all the binary's vocabulary is unknown, and the picker still
// gets the conservative static subset so it stays usable.
func readClaudeEffortHelp(ctx context.Context, runtimeCmd Command) claudeEffortHelp {
	cmd := runtimeCmd.exec(ctx, "--help")
	hideAgentWindow(cmd)
	out, err := combinedOutputOwned(cmd, runtimeCmd.logger)
	if err != nil {
		return claudeEffortHelp{picker: append([]string(nil), claudeStaticEffortFallback...)}
	}
	return claudeEffortHelpFromText(string(out))
}

// claudeEffortHelpFromText reads a successfully captured `claude --help`.
// Three cases:
//   - the value list parsed → it is both the binary's vocabulary and the
//     picker;
//   - `--effort` is advertised but the value list didn't parse → help
//     format drifted. The vocabulary is unknown, so nothing is vetoed, and
//     the picker falls back to the last known good superset;
//   - `--effort` is absent entirely → the installed CLI predates the flag.
//     Its vocabulary is empty and the picker offers nothing: injecting
//     --effort would make such a binary reject the launch with
//     `error: unknown option '--effort'` — hard-failing every task for an
//     agent with a persisted thinking_level instead of degrading to a plain
//     run.
func claudeEffortHelpFromText(helpText string) claudeEffortHelp {
	if parsed := parseClaudeEffortHelp(helpText); len(parsed) > 0 {
		return claudeEffortHelp{flag: parsed, picker: parsed}
	}
	if strings.Contains(helpText, "--effort") {
		return claudeEffortHelp{picker: append([]string(nil), claudeStaticEffortFullSuperset...)}
	}
	return claudeEffortHelp{flag: []string{}}
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

func claudeThinkingLevels(values []string) []ThinkingLevel {
	out := make([]ThinkingLevel, 0, len(values))
	for _, value := range values {
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
// `codex debug models` is the structured discovery hook for the live visible
// model catalog, each model's reasoning catalog, and service tiers. OpenAI
// added the command in Codex 0.122.0 (openai/codex #18625). Older versions,
// failed invocations, and malformed/empty payloads use the bundled catalog and
// then codexStaticModels so the picker remains usable offline.
//
// We prefer this over the older config-error probe trick because:
//   1. It gives us per-model subsets without hand-maintained tables.
//   2. The schema is structured and has been stable since its 0.122.0 debut.
//   3. It doesn't pollute stderr with an intentional misconfiguration.
//
// The subcommand emits JSON on stdout by default — there is no `--output json`
// flag (a prior version of this code passed one and silently failed on
// 0.131.0). The live form matters because Codex can receive new account-visible
// models without a CLI release; `--bundled` only reflects the binary's compiled
// snapshot. A failed live refresh falls back to `--bundled`, marked
// non-authoritative so neither daemon nor server caches pin it after the
// network recovers.
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

const (
	minCodexDebugModelsVersion = "0.122.0"
	// Codex 0.133.0 is the first stable release containing the request-only
	// `default` sentinel added by openai/codex#23537.
	minCodexExplicitStandardServiceTierVersion = "0.133.0"
)

// codexDebugModelsResponse mirrors the JSON shape emitted by
// `codex debug models` (Codex 0.122.0+). Only the fields we
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

// discoverCodexCatalog returns the installed Codex binary's live visible
// catalog, including reasoning metadata. Version detection happens before the
// debug command so old binaries do not log a predictable "unknown command"
// failure on every cache refresh.
func discoverCodexCatalog(ctx context.Context, cmd Command) Catalog {
	if cmd.Path == "" {
		cmd.Path = "codex"
	}
	version, err := DetectVersion(ctx, cmd)
	if err != nil {
		return Catalog{Models: codexStaticModels(), Fallback: true}
	}
	supportsExplicitStandard := codexSupportsExplicitStandardServiceTier(version)
	if !codexSupportsDebugModels(version) {
		return Catalog{
			Models:   annotateCodexExplicitStandardServiceTier(codexStaticModels(), supportsExplicitStandard),
			Fallback: true,
		}
	}

	liveCtx, cancel := context.WithTimeout(ctx, codexLiveCatalogTimeout)
	raw, err := runCodexDebugModels(liveCtx, cmd, codexDebugModelsArgs...)
	cancel()
	if err == nil {
		models, parseErr := parseCodexModelCatalog(raw)
		if parseErr == nil && len(models) > 0 {
			return Catalog{Models: annotateCodexExplicitStandardServiceTier(models, supportsExplicitStandard)}
		}
	}

	raw, err = runCodexDebugModels(ctx, cmd, codexBundledDebugModelsArgs...)
	if err == nil {
		models, parseErr := parseCodexModelCatalog(raw)
		if parseErr == nil && len(models) > 0 {
			return Catalog{
				Models:   annotateCodexExplicitStandardServiceTier(models, supportsExplicitStandard),
				Fallback: true,
			}
		}
	}
	return Catalog{
		Models:   annotateCodexExplicitStandardServiceTier(codexStaticModels(), supportsExplicitStandard),
		Fallback: true,
	}
}

func codexSupportsDebugModels(version string) bool {
	return codexVersionAtLeast(version, minCodexDebugModelsVersion)
}

func codexSupportsExplicitStandardServiceTier(version string) bool {
	return codexVersionAtLeast(version, minCodexExplicitStandardServiceTierVersion)
}

func codexVersionAtLeast(version, minimumVersion string) bool {
	parsed, err := parseSemver(version)
	if err != nil {
		return false
	}
	minimum, err := parseSemver(minimumVersion)
	if err != nil {
		return false
	}
	return !parsed.lessThan(minimum)
}

func annotateCodexExplicitStandardServiceTier(models []Model, supported bool) []Model {
	if !supported {
		return models
	}
	for i := range models {
		models[i].SupportsExplicitStandardServiceTier = true
	}
	return models
}

const codexLiveCatalogTimeout = 15 * time.Second

// codexDebugModelsArgs is the argv we pass for the authoritative live catalog.
// codexBundledDebugModelsArgs is the offline fallback. Kept as package-level
// vars so tests can assert the exact form a real `codex` invocation receives.
var (
	codexDebugModelsArgs        = []string{"debug", "models"}
	codexBundledDebugModelsArgs = []string{"debug", "models", "--bundled"}
)

func runCodexDebugModels(ctx context.Context, runtimeCmd Command, args ...string) ([]byte, error) {
	cmd := runtimeCmd.exec(ctx, args...)
	hideAgentWindow(cmd)
	return outputOwned(cmd, runtimeCmd.logger)
}

// parseCodexModelCatalog projects the CLI's raw catalog into the daemon wire
// model. Hidden entries are intentionally excluded to match Codex's own model
// picker; the first visible entry is the catalog's preferred default.
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
	case "gpt-6-astra":
		return "GPT-6 Astra"
	case "gpt-6-sol":
		return "GPT-6 Sol"
	case "gpt-6-luna":
		return "GPT-6 Luna"
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

// catalogLoader adapts the ambient ListModels call into the lazy loader the
// Validate*With functions take, so the ctx-based entry points stay one line.
func catalogLoader(ctx context.Context, providerType string, cmd Command) func() (Catalog, error) {
	return func() (Catalog, error) { return ListModels(ctx, providerType, cmd) }
}

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
//   - omp: fails closed for the same reason by a different route — see
//     ThinkingRequiresExplicitModel.
//
// Only a catalog discovery verified may reject a level. A fallback or empty
// catalog (see Catalog.Verified) answers with errUnverifiedCatalog, which the
// daemon treats like any lookup failure: the saved level goes to the CLI
// unchanged. The one thing still enforced there is the binary's own effort
// vocabulary (Catalog.CLIThinkingLevels) — a Claude CLI without `--effort`
// rejects the launch outright, whatever model it would have run.
//
// The lookup goes through ListModels so it sees the *current* CLI
// catalog (including dynamic discovery for codex), not just a static
// map. The function is intentionally pure of HTTP concerns so the
// daemon's pre-execution guard and the server's UpdateAgent gate can
// share the same source of truth.
func ValidateThinkingLevel(ctx context.Context, providerType string, cmd Command, model, value string) (bool, error) {
	return ValidateThinkingLevelWith(catalogLoader(ctx, providerType, cmd), providerType, model, value)
}

// ValidateThinkingLevelWith is ValidateThinkingLevel over a caller-supplied
// catalog loader. loadCatalog is invoked at most once, and only when the answer
// genuinely depends on the catalog — the guards below settle their cases
// without it.
//
// The daemon passes a loader memoized for the whole task so model
// qualification and both capability checks share one discovery round. That
// matters because discovery is a CLI subprocess with a 15-30s ceiling and
// cachedDiscovery deliberately does not memoize an empty or fallback result
// (#3729, MUL-5549), so each read costs the ceiling again on a logged-out or
// timing-out runtime (MUL-6471 review).
func ValidateThinkingLevelWith(loadCatalog func() (Catalog, error), providerType, model, value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	// Empty-model fail-closed, checked BEFORE the catalog load so the outcome is
	// deterministic even when discovery would error. That ordering is the whole
	// point: on a lookup error the daemon passes the level through to the CLI
	// (see its thinking_level guard), which is exactly what must not happen for
	// a provider whose effective model we cannot know.
	if model == "" && ThinkingRequiresExplicitModel(providerType) {
		return false, nil
	}
	catalog, err := loadCatalog()
	if err != nil {
		return false, err
	}
	if catalog.CLIThinkingLevels != nil && !slices.Contains(catalog.CLIThinkingLevels, value) {
		return false, nil
	}
	if !catalog.Verified() {
		return false, fmt.Errorf("%w; cannot validate %s thinking level %q", errUnverifiedCatalog, providerType, value)
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
			// opencode has no single default model, so it accepts a level any
			// advertised model supports. Providers that instead require a pinned
			// model never reach here — they were already rejected above.
			if providerType == "opencode" {
				return anyModelSupportsThinkingValue(models, value), nil
			}
			return false, nil
		}
	}
	for _, m := range models {
		// Normalise the catalog side too, not just the requested model. Claude
		// discovery reports what the CLI would really run, and that includes
		// the context-window tag (`claude-opus-5[1m]`), while target has
		// already had it stripped. Comparing raw IDs would miss every tagged
		// entry and fail the level closed, silently dropping the user's
		// --effort (MUL-6961).
		if modelIDForCapabilityLookup(providerType, m.ID) != target {
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

// errUnverifiedCatalog is what the capability checks answer when the catalog
// they were handed is not the runtime's own (see Catalog.Verified). It is an
// error rather than a "no" on purpose: the daemon passes a value it could not
// check through to the CLI instead of discarding the user's saved choice.
var errUnverifiedCatalog = errors.New("model discovery did not return the runtime's own catalog")

// ThinkingRequiresExplicitModel reports whether a provider refuses to carry an
// effort unless a model is pinned, because its empty-model resolution happens
// somewhere Multica cannot observe:
//
//   - codex: the effective model comes from the local config.toml and can be any
//     installed model, so borrowing the catalog's Default entry would green-light
//     levels the configured model may not support (MUL-4347).
//   - omp: its `models --json` catalog marks no default at all and sorts by
//     provider/id, so no entry here is the one that would run. At task time omp
//     resolves its own default role model and clamps the requested level to what
//     THAT model supports, so a level validated against any other entry is not
//     the level that runs (MUL-7412).
//
// Both are checked before any catalog read, so discovery failing cannot turn
// into "pass the level through".
func ThinkingRequiresExplicitModel(providerType string) bool {
	switch providerType {
	case "codex", "omp":
		return true
	}
	return false
}

// ThinkingLevelRejectedWithoutModel reports whether the API should refuse to
// STORE an effort that has no pinned model, instead of storing it and leaving
// the daemon to drop it at launch.
//
// Deliberately narrower than ThinkingRequiresExplicitModel. codex shares the
// execution constraint but predates this check: agents out there already hold an
// effort alongside an empty model, and 400ing that combination would block
// unrelated edits to them, so codex stays grandfathered and the daemon keeps
// dropping the level. omp has no such history — persisting an effort at all was
// impossible before MUL-7412 — so it is strict from the start and the invalid
// combination never reaches storage.
func ThinkingLevelRejectedWithoutModel(providerType string) bool {
	return providerType == "omp"
}

// ValidateServiceTier reports whether value is advertised by the current
// Codex catalog for the explicit model. An empty value is always valid and
// means "inherit runtime configuration". Codex's "default" sentinel is valid
// only when the daemon's installed CLI reports support for explicit standard
// routing. An empty Codex model otherwise fails closed because its effective
// model comes from config.toml and may not support the requested tier.
func ValidateServiceTier(ctx context.Context, providerType string, cmd Command, model, value string) (bool, error) {
	return ValidateServiceTierWith(catalogLoader(ctx, providerType, cmd), providerType, model, value)
}

// ValidateServiceTierWith is ValidateServiceTier over a caller-supplied
// catalog loader. See ValidateThinkingLevelWith for why the daemon needs one.
func ValidateServiceTierWith(loadCatalog func() (Catalog, error), providerType, model, value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	if providerType != "codex" {
		return false, nil
	}
	if value != codexStandardServiceTier && model == "" {
		return false, nil
	}
	catalog, err := loadCatalog()
	if err != nil {
		return false, err
	}
	// Explicit standard routing is a CLI-version capability, not a per-model
	// one: discoverCodexCatalog stamps it on every entry, fallback entries
	// included, from the installed version alone. Resolve it before the
	// unverified-catalog pass-through below: an old CLI must never receive
	// "default" just because discovery failed.
	if value == codexStandardServiceTier {
		for _, candidate := range catalog.Models {
			if candidate.SupportsExplicitStandardServiceTier {
				return true, nil
			}
		}
		return false, nil
	}
	if !catalog.Verified() {
		return false, fmt.Errorf("%w; cannot validate %s service tier %q", errUnverifiedCatalog, providerType, value)
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
	// omp (Oh-My-Pi) dispatches to the pi backend (see BuiltinRuntimes), so it
	// inherits pi's fixed CLI vocabulary; discoverOmpModels narrows it to each
	// model's advertised efforts before execution. `auto` is deliberately absent
	// even though omp's --thinking accepts it — see ompThinkingFromCatalogEntry
	// (MUL-7412).
	"omp": {
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
	"codex": true,
	"dsh":   true,
	// Grok advertises each model's effort catalog through session/new, so the
	// server does not maintain a provider-wide fixed enum. The daemon applies
	// the selected effort with `--effort`, not session/set_config_option.
	"grok":     true,
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
	// dim (dimcode 0.3.10+): session/new advertises thought_level.
	"dim": true,
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
