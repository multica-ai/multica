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
// claude and codex backends so the daemon can advertise them to the
// UI without hard-coding (and getting wrong) what's installed locally.
//
// MUL-2339: we deliberately do not flatten Claude's `low|medium|high|
// xhigh|max` and Codex's `none|minimal|low|medium|high|xhigh` onto a
// shared enum — what users pick must round-trip exactly through each
// CLI's own value vocabulary.

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
// parsing fails it returns the static fallback rather than nothing so
// callers can still render a usable picker.
func claudeEffortSuperset(ctx context.Context, executablePath string) []string {
	cmd := exec.CommandContext(ctx, executablePath, "--help")
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return append([]string(nil), claudeStaticEffortFallback...)
	}
	parsed := parseClaudeEffortHelp(string(out))
	if len(parsed) == 0 {
		// Help format drifted — fall back to the last known good
		// superset rather than the conservative subset, so newer
		// levels are still offered until we hand-edit the fallback.
		return append([]string(nil), claudeStaticEffortFullSuperset...)
	}
	return parsed
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
// `codex debug models` is the structured discovery hook Elon's review
// flagged. It returns the per-model reasoning catalog directly,
// including the model's documented default. We prefer this over the
// older config-error probe trick because:
//   1. It gives us per-model subsets without hand-maintained tables.
//   2. The schema is stable across CLI versions (Codex 0.131.0+).
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
// On older Codex versions / failures, the picker just disappears for
// that model rather than offering a wrong list.

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
}

// codexDebugModelsResponse mirrors the JSON shape emitted by
// `codex debug models` (Codex 0.131.0+). Only the fields we
// consume are typed; unknown keys are ignored.
type codexDebugModelsResponse struct {
	Models []struct {
		Slug                    string `json:"slug"`
		DefaultReasoningLevel   string `json:"default_reasoning_level"`
		SupportedReasoningLevel []struct {
			Effort      string `json:"effort"`
			Description string `json:"description"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

// annotateCodexThinking decorates each model entry with its reasoning
// catalog. Models the CLI doesn't know about (older codex install,
// brand-new ID we haven't shipped) get Thinking=nil — the UI hides
// the picker for those rows rather than guessing.
func annotateCodexThinking(ctx context.Context, models []Model, executablePath string) {
	mapping := loadCodexThinkingByModel(ctx, executablePath)
	for i := range models {
		if t, ok := mapping[models[i].ID]; ok && t != nil {
			models[i].Thinking = t
		}
	}
}

func loadCodexThinkingByModel(ctx context.Context, executablePath string) map[string]*ModelThinking {
	if executablePath == "" {
		executablePath = "codex"
	}
	version, _ := DetectVersion(ctx, executablePath)
	key := thinkingCacheKey{provider: "codex", executablePath: executablePath, cliVersion: version}
	if cached, ok := thinkingCacheGet(key); ok {
		return cached
	}

	raw, err := runCodexDebugModels(ctx, executablePath)
	if err != nil {
		// Cache the empty result so repeated UI polls don't re-shell
		// the missing binary; TTL eventually retries.
		thinkingCachePut(key, map[string]*ModelThinking{})
		return map[string]*ModelThinking{}
	}
	parsed := parseCodexDebugModels(raw)
	thinkingCachePut(key, parsed)
	return parsed
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

// parseCodexDebugModels takes the JSON payload from `codex debug
// models` and projects it into a per-model thinking catalog.
// Returns an empty map (never nil) so callers can compose safely
// without nil-checking the result.
func parseCodexDebugModels(raw []byte) map[string]*ModelThinking {
	out := map[string]*ModelThinking{}
	var resp codexDebugModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return out
	}
	for _, m := range resp.Models {
		if m.Slug == "" || len(m.SupportedReasoningLevel) == 0 {
			continue
		}
		levels := make([]ThinkingLevel, 0, len(m.SupportedReasoningLevel))
		for _, lvl := range m.SupportedReasoningLevel {
			if lvl.Effort == "" {
				continue
			}
			label, ok := codexEffortLabel[lvl.Effort]
			if !ok {
				label = strings.Title(lvl.Effort) //nolint:staticcheck
			}
			levels = append(levels, ThinkingLevel{
				Value:       lvl.Effort,
				Label:       label,
				Description: lvl.Description,
			})
		}
		if len(levels) == 0 {
			continue
		}
		out[m.Slug] = &ModelThinking{
			SupportedLevels: levels,
			DefaultLevel:    m.DefaultReasoningLevel,
		}
	}
	return out
}

// ── Shared validation ────────────────────────────────────────────────

// ValidateThinkingLevel reports whether `value` is in the supported
// catalog for the given (provider, model) pair. Empty value is always
// valid — it means "use the runtime default".
//
// Empty model is treated as "use the provider's default model"; we
// resolve it through ListModels so the daemon's pre-execution guard
// behaves the same whether the agent picked an explicit model or
// inherited the runtime default. Without this, a default-model task
// with a valid thinking_level would be rejected on the grounds that
// the empty string is not in the catalog — exactly the misjudgement
// Elon flagged in the PR1 review.
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

// providerThinkingEnums is the server-side accept-list for each runtime's
// reasoning-effort vocabulary. The server doesn't have local CLI binaries,
// so it cannot do per-model discovery the way the daemon can; what it CAN
// do is reject values that are not in any version of the provider's enum
// at all. Per-model gaps (e.g. user sets `xhigh` while the chosen model
// only supports up to `high`) surface as a daemon-side task failure with
// a clear error, not a server-side 400 — that split is intentional so the
// API behaviour stays consistent (always-400 on literal-invalid, never
// auto-clear on combination-invalid). See MUL-2339 review notes.
//
// Keep these lists permissive: they're a "is this a known token in this
// runtime's universe" check, not an "is this the right level for this
// model" check. Adding a new level upstream means adding it here too so
// users can persist it before the next discovery refresh.
var providerThinkingEnums = map[string]map[string]bool{
	"claude": {
		"low":    true,
		"medium": true,
		"high":   true,
		"xhigh":  true,
		"max":    true,
	},
	"codex": {
		"none":    true,
		"minimal": true,
		"low":     true,
		"medium":  true,
		"high":    true,
		"xhigh":   true,
	},
}

// IsKnownThinkingValue reports whether `value` is a recognised effort
// token for the given provider. Empty string is always accepted (means
// "use runtime default"). Unknown providers (no thinking concept) accept
// only empty.
//
// This is the cheap synchronous gate the server uses on CreateAgent /
// UpdateAgent. Unlike ValidateThinkingLevel it does NOT consult the live
// catalog or per-model subset.
func IsKnownThinkingValue(providerType, value string) bool {
	if value == "" {
		return true
	}
	enum, ok := providerThinkingEnums[providerType]
	if !ok {
		// Variant providers (codebuddy, claude-internal, codex-internal,
		// gemini-internal) inherit their family's enum.
		enum, ok = providerThinkingEnums[ProtocolFamily(providerType)]
	}
	if !ok {
		return false
	}
	return enum[value]
}
