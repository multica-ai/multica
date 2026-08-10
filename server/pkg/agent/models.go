package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Model describes a single LLM model exposed by an agent provider.
// The dropdown groups by Provider when the ID uses the
// `provider/model` form (e.g. "openai/gpt-4o" from opencode).
// Default is a *display* hint: the UI badges the entry the
// runtime advertises as its preferred pick (e.g. Claude Code's
// shipped default, or hermes' currentModelId). It has no effect
// at execution time — when agent.model is empty the daemon passes
// "" to the backend so each provider's own CLI resolves its own
// default, which is always closer to what the user's account /
// environment actually supports than a static guess here.
type Model struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Provider     string             `json:"provider,omitempty"`
	Default      bool               `json:"default,omitempty"`
	ServiceTiers []ModelServiceTier `json:"service_tiers,omitempty"`
	// Thinking advertises the runtime's reasoning/effort catalog for this
	// model. nil means the runtime/model has no thinking-level control
	// (or the daemon couldn't discover one); the UI hides its picker. The
	// catalog is per-model because Codex's `codex debug models` is itself
	// per-model and Claude's `--effort` superset has known per-model gaps
	// (`xhigh` is Opus-only, `max` is session-only). See MUL-2339.
	Thinking *ModelThinking `json:"thinking,omitempty"`
}

// ModelServiceTier is one runtime-native execution tier advertised for a
// model. ID is sent back to the provider protocol unchanged; Name and
// Description are display copy owned by the runtime catalog.
type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ModelThinking carries the per-model reasoning/effort catalog
// surfaced by an agent runtime. Values are runtime-native — Codex can emit
// "none|minimal|low|medium|high|xhigh|max|ultra"; Claude emits
// "low|medium|high|xhigh|max"; Pi emits
// "off|minimal|low|medium|high|xhigh|max". The frontend renders
// SupportedLevels as-is so what users see matches each CLI's own UI.
type ModelThinking struct {
	SupportedLevels []ThinkingLevel `json:"supported_levels"`
	// DefaultLevel is the value the runtime picks when no override is
	// provided. Empty means "the runtime picks, we don't know" — the
	// UI shows "Default" as a generic option.
	DefaultLevel string `json:"default_level,omitempty"`
}

// ThinkingLevel is one entry in a ModelThinking.SupportedLevels list.
// Value is the literal token passed to the CLI (Claude `--effort <value>`
// or Codex `model_reasoning_effort=<value>`); Label is a display string;
// Description is optional helper copy lifted from the upstream catalog
// when available (Codex's `description` field).
type ThinkingLevel struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Catalog is the outcome of one model-discovery round: the models to show,
// plus whether they were actually discovered.
type Catalog struct {
	Models []Model
	// Fallback reports that discovery did not succeed and Models is a static
	// stand-in rather than the runtime's real catalog.
	//
	// Several providers (codebuddy, copilot, cursor, grok) answer a failed
	// discovery with a baked-in list so the picker still has something to
	// show. That is a reasonable UI affordance and a terrible thing to
	// persist: it is not what the runtime actually supports, and for
	// codebuddy the static IDs and the real ones do not overlap at all, so
	// every pick is an ID the CLI rejects. Callers must therefore never
	// treat a fallback catalog as authoritative — in particular it must not
	// enter the server's day-scale model-catalog cache, which would pin one
	// transient failure as the answer for 24h (MUL-5549).
	Fallback bool
}

// discovered adapts a plain `([]Model, error)` discovery function to Catalog
// for providers that have no static fallback — for them a failure is already
// reported as an empty list or an error, which downstream guards handle.
func discovered(models []Model, err error) (Catalog, error) {
	return Catalog{Models: models}, err
}

// modelCache memoizes dynamic discovery calls so repeated UI loads
// don't re-shell the agent CLI. Entries expire after cacheTTL.
type modelCacheEntry struct {
	models    []Model
	expiresAt time.Time
}

var (
	modelCacheMu sync.Mutex
	modelCache   = map[string]modelCacheEntry{}
)

const modelCacheTTL = 60 * time.Second

// ListModels returns the models supported by the given agent provider.
// For providers with a known static catalog it returns the baked-in
// list; for providers with a CLI discovery mechanism (codex, opencode,
// pi, openclaw) it shells out with caching and falls back where the
// provider has a safe static catalog.
//
// For claude, codex, opencode, pi, and kimi, the catalog is augmented with
// per-model thinking-level options discovered from the local CLI. Codex
// discovery failures fall back to a model + thinking snapshot; providers
// without a safe fallback leave Thinking nil, which makes the UI hide the
// thinking picker.
//
// executablePath lets the caller point at a non-default binary; pass
// "" to use the provider's default name on PATH.
func ListModels(ctx context.Context, providerType, executablePath string) (Catalog, error) {
	// Built-in runtime identities (e.g. "omp") declare their model discovery
	// strategy in the descriptor. Resolve generically before the protocol-
	// family switch so no runtime-specific case is needed below. When the
	// descriptor has no ModelDiscovery strategy, return an empty catalog
	// (not the family's default) — running a semantically incompatible
	// discovery command (e.g. omp rejecting --list-models) is worse than
	// degrading to manual entry.
	if desc, ok := BuiltinRuntimeByID(providerType); ok {
		if desc.ModelDiscovery != nil {
			return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
				return discovered(desc.ModelDiscovery(ctx, executablePath))
			})
		}
		return Catalog{Models: []Model{}}, nil
	}
	switch providerType {
	case "claude":
		models := claudeStaticModels()
		annotateClaudeThinking(ctx, models, executablePath)
		// Claude's catalog is static by design, not by failure: there is no
		// discovery step to fall back from, so this is authoritative.
		return Catalog{Models: models}, nil
	case "codex":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverCodexModels(ctx, executablePath), nil)
		})
	case "antigravity":
		// agy 1.0.6 added a `--model` flag plus an `agy models` catalog
		// command (MUL-3125). Enumerate it on demand like the other
		// dynamic-discovery backends.
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverAntigravityModels(ctx, executablePath))
		})
	case "traecli":
		// Official TRAE CLI is ACP-native: it returns its model catalog from
		// session/new. Enumerate it on demand like the other ACP backends
		// (requires a logged-in traecli; falls back to manual entry on error).
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverTraecliModels(ctx, executablePath))
		})
	case "cursor":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discoverCursorModels(ctx, executablePath)
		})
	case "copilot":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discoverCopilotModels(ctx, executablePath)
		})
	case "hermes":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverHermesModels(ctx, executablePath))
		})
	case "kimi":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverKimiModels(ctx, executablePath))
		})
	case "reasonix":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverReasonixModels(ctx, executablePath))
		})
	case "kiro":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverKiroModels(ctx, executablePath))
		})
	case "qoder", "qoderclicn":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverQoderModels(ctx, executablePath, qoderDefaultBinary(providerType)))
		})
	case "opencode":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverOpenCodeModels(ctx, executablePath))
		})
	case "deveco":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverDevecoModels(ctx, executablePath))
		})
	case "pi":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverPiModels(ctx, executablePath))
		})
	case "openclaw":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discovered(discoverOpenclawAgents(ctx, executablePath))
		})
	case "codebuddy":
		// discoverCodebuddyModels owns the thinking annotation too, so the one
		// `--help` capture feeds both catalogs. Annotating out here would run
		// the command a second time (MUL-5549).
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discoverCodebuddyModels(ctx, executablePath)
		})
	case "qwen":
		// Qwen Code has no account-independent headless model catalog. An
		// empty list keeps the runtime default and manual model entry available
		// without advertising a Token-Plan-specific model to other accounts.
		return Catalog{Models: []Model{}}, nil
	case "qwenpaw":
		// QwenPaw's model selection is unsupported (session/set_model
		// persists to agent scope, not session scope), so there is no
		// consumer for a discovered catalog. Return an empty list to
		// avoid spawning an ACP subprocess that has no effect. If upstream
		// makes model selection session-scoped, restore a discovery helper
		// here modelled on discoverTraecliModels.
		return Catalog{Models: []Model{}}, nil
	case "grok":
		// xAI Grok Build is ACP-native (`grok agent stdio`); model catalog
		// comes from session/new. Falls back to a small static list so the
		// UI picker stays usable offline / unauthenticated.
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() (Catalog, error) {
			return discoverGrokModels(ctx, executablePath)
		})
	default:
		return Catalog{}, fmt.Errorf("unknown agent type: %q", providerType)
	}
}

// ModelSelectionSupported reports whether setting `agent.model` has
// any effect for the given provider. Every built-in provider now honours
// `opts.Model` end-to-end — Hermes routes it through the ACP
// `session/set_model` RPC before each prompt; Claude / Codex / Cursor /
// Gemini / Copilot / Kimi / Reasonix / Kiro / OpenCode / OpenClaw / Pi / Antigravity
// pass it via flag or session config (Antigravity gained `--model` in agy
// 1.0.6 — MUL-3125).
//
// The hook is retained — rather than inlining `true` at the call sites — so
// a model-less runtime can opt out in one place, which makes the UI
// render a disabled "Managed by runtime" picker instead of an empty
// dropdown plus a silently-ignored manual-entry field.
func ModelSelectionSupported(providerType string) bool {
	switch providerType {
	case "qwenpaw":
		// QwenPaw's `session/set_model` persists to agent.json at the agent
		// scope, not the session scope. Calling it would mutate the user's
		// shared, persistent agent config. Model override is therefore
		// unsupported — the runtime uses whatever model is configured in
		// the agent profile. If QwenPaw makes model selection session-scoped
		// upstream, this can be reverted to `true`.
		return false
	default:
		return true
	}
}

// ModelKnownIncompatibleWithProvider reports whether a saved model is a known
// mismatch for a target runtime provider. For first-party providers with
// maintained static catalogs, compatibility is exact: the model must be one of
// the IDs that runtime advertises. Unknown/custom model strings still return
// false because the UI and CLI allow manual entries and the server should not
// erase values it cannot confidently classify.
func ModelKnownIncompatibleWithProvider(providerType, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}

	accepted, ok := acceptedModelIDsForProvider(providerType)
	if !ok {
		return false
	}
	if accepted[model] {
		return false
	}
	return isRuntimeSpecificModelID(model)
}

func acceptedModelIDsForProvider(providerType string) (map[string]bool, bool) {
	switch {
	case providerType == "claude":
		return modelIDSet(claudeStaticModels()), true
	case providerType == "codex":
		return modelIDSet(codexStaticModels()), true
	default:
		return nil, false
	}
}

func modelIDSet(models []Model) map[string]bool {
	out := make(map[string]bool, len(models))
	for _, m := range models {
		out[m.ID] = true
	}
	return out
}

func isRuntimeSpecificModelID(model string) bool {
	if strings.Contains(model, "/") {
		return true
	}
	return modelHasKnownPrefix(model) ||
		modelIDSet(claudeStaticModels())[model] ||
		modelIDSet(codexStaticModels())[model]
}

func modelHasKnownPrefix(model string) bool {
	return strings.HasPrefix(model, "claude-") ||
		strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "gemini-") ||
		strings.HasPrefix(model, "auto-gemini-") ||
		isOpenAIReasoningSeriesID(model)
}

// cachedDiscovery invokes fn and caches the result for modelCacheTTL.
// Callers build the key with discoveryCacheKey so two runtimes of the same
// protocol family never share one memo entry when they enumerate different
// binaries.
func cachedDiscovery(key string, fn func() (Catalog, error)) (Catalog, error) {
	modelCacheMu.Lock()
	if entry, ok := modelCache[key]; ok && time.Now().Before(entry.expiresAt) {
		out := entry.models
		modelCacheMu.Unlock()
		return Catalog{Models: out}, nil
	}
	modelCacheMu.Unlock()

	catalog, err := fn()
	if err != nil {
		return Catalog{}, err
	}

	// Don't cache an empty result. Zero models is almost always a transient
	// failure (discovery CLI timeout, not-logged-in, network blip) rather than
	// a runtime that genuinely has no models; caching it would keep the picker
	// blank for the full TTL even after the cause clears. Skipping the cache
	// lets the next request retry immediately. See #3729.
	//
	// A fallback catalog is the same situation wearing a disguise: non-empty,
	// but only because discovery failed and the provider substituted a static
	// list. Caching it would hold the stand-in for the full TTL and stop the
	// next request from retrying, which is exactly the recovery path we want
	// to keep open (MUL-5549).
	if len(catalog.Models) == 0 || catalog.Fallback {
		return catalog, nil
	}

	modelCacheMu.Lock()
	modelCache[key] = modelCacheEntry{models: catalog.Models, expiresAt: time.Now().Add(modelCacheTTL)}
	modelCacheMu.Unlock()
	return catalog, nil
}

// discoveryCacheKey scopes a discovery memo to the binary it enumerated.
// A custom runtime profile (MUL-3284) runs a different executable under a
// built-in protocol family, so a provider-only key would let a built-in
// runtime and a same-family profile runtime on one host serve each other's
// catalog for the TTL — the same mismatch the daemon's path resolution
// fixes, reintroduced at the cache layer (MUL-5789). An empty path keeps
// the bare provider key, which is what a built-in on PATH resolves to.
func discoveryCacheKey(providerType, executablePath string) string {
	if executablePath == "" {
		return providerType
	}
	return providerType + ":" + executablePath
}

// ── Static catalogs ──

// claudeStaticModels reflects the Claude Code CLI's accepted --model
// values. Keep this list short and current; stale entries here
// mislead users more than they help. Default = Sonnet because it's
// the everyday workhorse (Opus is reserved for advisor-style flows).
func claudeStaticModels() []Model {
	return []Model{
		{ID: "claude-sonnet-5", Label: "Claude Sonnet 5", Provider: "anthropic"},
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6", Provider: "anthropic", Default: true},
		{ID: "claude-fable-5", Label: "Claude Fable 5", Provider: "anthropic"},
		{ID: "claude-opus-5", Label: "Claude Opus 5", Provider: "anthropic"},
		{ID: "claude-opus-4-8", Label: "Claude Opus 4.8", Provider: "anthropic"},
		{ID: "claude-opus-4-7", Label: "Claude Opus 4.7", Provider: "anthropic"},
		{ID: "claude-haiku-4-5-20251001", Label: "Claude Haiku 4.5", Provider: "anthropic"},
		{ID: "claude-opus-4-6", Label: "Claude Opus 4.6", Provider: "anthropic"},
		{ID: "claude-sonnet-4-5", Label: "Claude Sonnet 4.5", Provider: "anthropic"},
	}
}

// codexStaticModels is the fallback for Codex versions older than 0.122.0
// and for failed/malformed `codex debug models --bundled` calls. Keep it in
// sync with the visible entries in the newest locally verified bundled
// catalog, plus still-common models from older Codex releases. Each entry
// carries its own reasoning catalog so old/offline CLIs retain the same model
// + thinking picker contract as dynamic discovery. Service tiers are
// intentionally NOT guessed here: they are runtime/version/account-sensitive,
// so a discovery failure hides the speed picker and fails the override closed.
func codexStaticModels() []Model {
	// `Default` here is NOT a user-facing "default model" badge — the picker
	// stopped rendering that (Multica follows the CLI config when the model is
	// unset). It only marks the current flagship for the "default must track
	// the latest release" catalog guard
	// (TestCodexStaticModelsMatchVerifiedFallbackCatalog,
	// multica#2009). It is deliberately NOT used to validate effort for an
	// empty (follow-CLI-config) model: that config can resolve to any model,
	// so ValidateThinkingLevel fails an empty codex model closed rather than
	// borrowing this entry's catalog (which alone advertises `ultra`) — see
	// ValidateThinkingLevel and MUL-4347. Keep exactly one entry flagged.
	standardThinking := func(defaultLevel string, includeMax, includeUltra bool) *ModelThinking {
		levels := []ThinkingLevel{
			{Value: "low", Label: "Low", Description: "Fast responses with lighter reasoning"},
			{Value: "medium", Label: "Medium", Description: "Balances speed and reasoning depth for everyday tasks"},
			{Value: "high", Label: "High", Description: "Greater reasoning depth for complex problems"},
			{Value: "xhigh", Label: "Extra high", Description: "Extra high reasoning depth for complex problems"},
		}
		if includeMax {
			levels = append(levels, ThinkingLevel{Value: "max", Label: "Max", Description: "Maximum reasoning depth for the hardest problems"})
		}
		if includeUltra {
			levels = append(levels, ThinkingLevel{Value: "ultra", Label: "Ultra", Description: "Maximum reasoning with automatic task delegation"})
		}
		return &ModelThinking{DefaultLevel: defaultLevel, SupportedLevels: levels}
	}
	gpt52Thinking := func() *ModelThinking {
		return &ModelThinking{
			DefaultLevel: "medium",
			SupportedLevels: []ThinkingLevel{
				{Value: "low", Label: "Low", Description: "Balances speed with some reasoning; useful for straightforward queries and short explanations"},
				{Value: "medium", Label: "Medium", Description: "Provides a solid balance of reasoning depth and latency for general-purpose tasks"},
				{Value: "high", Label: "High", Description: "Maximizes reasoning depth for complex or ambiguous problems"},
				{Value: "xhigh", Label: "Extra high", Description: "Extra high reasoning for complex problems"},
			},
		}
	}
	return []Model{
		{ID: "gpt-5.6-sol", Label: "GPT-5.6-Sol", Provider: "openai", Default: true, Thinking: standardThinking("low", true, true)},
		{ID: "gpt-5.6-terra", Label: "GPT-5.6-Terra", Provider: "openai", Thinking: standardThinking("medium", true, true)},
		{ID: "gpt-5.6-luna", Label: "GPT-5.6-Luna", Provider: "openai", Thinking: standardThinking("medium", true, false)},
		{ID: "gpt-5.5", Label: "GPT-5.5", Provider: "openai", Thinking: standardThinking("medium", false, false)},
		{ID: "gpt-5.4", Label: "GPT-5.4", Provider: "openai", Thinking: standardThinking("medium", false, false)},
		{ID: "gpt-5.4-mini", Label: "GPT-5.4-Mini", Provider: "openai", Thinking: standardThinking("medium", false, false)},
		{ID: "gpt-5.3-codex", Label: "GPT-5.3-Codex", Provider: "openai", Thinking: standardThinking("medium", false, false)},
		{ID: "gpt-5.2", Label: "GPT-5.2", Provider: "openai", Thinking: gpt52Thinking()},
	}
}

// discoverTraecliModels spins up a throwaway `traecli acp serve --yolo` process
// and parses the model catalog traecli returns from session/new (same shape as
// Kiro/Qoder). The official TRAE CLI must be logged in for the catalog to be
// non-empty; on any failure the caller falls back to the manual-entry field.
func discoverTraecliModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "traecli",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-traecli-discovery-",
		acpArgs:      []string{"acp", "serve", "--yolo"},
	})
}

// cursorStaticModels is a minimal fallback used when
// `cursor-agent --list-models` isn't available (binary missing,
// offline, etc). The real catalog is fetched dynamically because
// Cursor's model IDs shift (e.g. `composer-2-fast`,
// `claude-4.6-sonnet-medium`, `gemini-3.1-pro`) and any static
// list we ship goes stale fast.
func cursorStaticModels() []Model {
	return []Model{
		{ID: "auto", Label: "Auto", Provider: "cursor", Default: true},
	}
}

// copilotStaticModels — fallback used when GitHub Copilot CLI is
// missing on PATH or the user hasn't logged in. Normal operation
// goes through discoverCopilotModels(), which speaks ACP to the
// CLI and gets the live catalog (including which IDs the user's
// account actually has access to). This list is just a safety net
// so the UI dropdown still has reasonable options when the live
// query fails.
//
// Source: https://docs.github.com/en/copilot/reference/ai-models/supported-models
// IDs use the dotted form `copilot --model <id>` actually accepts.
func copilotStaticModels() []Model {
	return []Model{
		// OpenAI
		{ID: "gpt-5.5", Label: "GPT-5.5", Provider: "openai"},
		{ID: "gpt-5.4", Label: "GPT-5.4", Provider: "openai"},
		{ID: "gpt-5.4-mini", Label: "GPT-5.4 mini", Provider: "openai"},
		{ID: "gpt-5.3-codex", Label: "GPT-5.3-Codex", Provider: "openai"},
		{ID: "gpt-5.2-codex", Label: "GPT-5.2-Codex", Provider: "openai"},
		{ID: "gpt-5.2", Label: "GPT-5.2", Provider: "openai"},
		{ID: "gpt-5-mini", Label: "GPT-5 mini", Provider: "openai"},
		{ID: "gpt-4.1", Label: "GPT-4.1", Provider: "openai"},
		// Anthropic
		{ID: "claude-opus-4.7", Label: "Claude Opus 4.7", Provider: "anthropic"},
		{ID: "claude-sonnet-4.6", Label: "Claude Sonnet 4.6", Provider: "anthropic"},
		{ID: "claude-sonnet-4.5", Label: "Claude Sonnet 4.5", Provider: "anthropic"},
		{ID: "claude-haiku-4.5", Label: "Claude Haiku 4.5", Provider: "anthropic"},
	}
}

// inferCopilotProvider tags Copilot model IDs with a vendor name so
// the UI can group them. The Copilot CLI's ACP `availableModels`
// payload exposes only `modelId`/`name`; the vendor is implicit in
// the prefix. Returning "" leaves the entry ungrouped, which
// matches what other ACP discovery paths (hermes/kimi) do for
// non-prefixed IDs.
//
// The OpenAI reasoning series (`o1`, `o3`, `o3-mini`, `o4-mini`,
// future `o5`/`o6`/…) is matched by the generic `o<digit>…`
// pattern so we don't have to chase every new generation.
func inferCopilotProvider(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "gpt-") || isOpenAIReasoningSeriesID(modelID):
		return "openai"
	case strings.HasPrefix(modelID, "claude-"):
		return "anthropic"
	case strings.HasPrefix(modelID, "gemini-"):
		return "google"
	case strings.HasPrefix(modelID, "grok-"):
		return "xai"
	default:
		return ""
	}
}

// isOpenAIReasoningSeriesID matches IDs in OpenAI's `o`-prefixed
// reasoning family: lowercase `o` followed by at least one digit
// and then either end-of-string or a `-` separator (e.g. `o3`,
// `o3-mini`, `o4-mini-high`). Avoids false positives like
// `opus-…` or random IDs that happen to start with `o`.
func isOpenAIReasoningSeriesID(id string) bool {
	if len(id) < 2 || id[0] != 'o' {
		return false
	}
	i := 1
	for i < len(id) && id[i] >= '0' && id[i] <= '9' {
		i++
	}
	if i == 1 {
		return false
	}
	return i == len(id) || id[i] == '-'
}

// ── Dynamic discovery ──

// discoverOpenCodeModels runs `opencode models --verbose` and parses its
// output. The CLI prints `provider/model` rows, followed by JSON metadata
// when verbose mode is enabled; we emit IDs verbatim so what the user sees
// matches what `--model` accepts, and project any model `variants` into the
// thinking-level picker because OpenCode's `run --variant` flag is its
// provider-specific reasoning-effort surface.
// On any failure (CLI missing, parse error, timeout) we fall back to
// an empty list so the creatable UI still works.
func discoverOpenCodeModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "opencode"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	// Newer opencode (1.15+) syncs its hosted free-model catalog over the
	// network on `opencode models`, which can take ~6s; the previous 5s cap
	// timed out and returned an empty list, so the runtime showed online but
	// the model picker was empty. See multica-ai/multica#3627.
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "models", "--verbose")
	hideAgentWindow(cmd)
	// Parse whatever the verbose command printed, even on a non-zero exit — a
	// stale config entry can make `opencode models` exit non-zero while still
	// listing the resolvable catalog (mirrors the pi path; see #3729/#3627).
	out, _ := cmd.Output()
	models := parseOpenCodeModels(string(out))
	if len(models) == 0 {
		// Verbose yielded nothing usable (unsupported flag, error text, or an
		// empty list). Retry the plain command, which omits the per-model JSON
		// but still prints the IDs.
		cmd = exec.CommandContext(runCtx, executablePath, "models")
		hideAgentWindow(cmd)
		out, _ = cmd.Output()
		models = parseOpenCodeModels(string(out))
	}
	if len(models) == 0 {
		return []Model{}, nil
	}
	return models, nil
}

// parseOpenCodeModels accepts the `opencode models` text output and
// extracts IDs. Non-verbose output is one `provider/model` row per line.
// Verbose output appends a pretty-printed JSON object after each ID; when
// that object contains `variants`, each enabled variant becomes a thinking
// level that the backend later passes through `opencode run --variant`.
func parseOpenCodeModels(output string) []Model {
	lines := strings.Split(output, "\n")
	var models []Model
	indexByID := map[string]int{}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		id := parseOpenCodeModelIDLine(line)
		if id == "" {
			continue
		}
		idx, seen := indexByID[id]
		if !seen {
			provider := ""
			if slash := strings.Index(id, "/"); slash > 0 {
				provider = id[:slash]
			}
			idx = len(models)
			indexByID[id] = idx
			models = append(models, Model{ID: id, Label: id, Provider: provider})
		}

		next := i + 1
		for next < len(lines) && strings.TrimSpace(lines[next]) == "" {
			next++
		}
		if next >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[next]), "{") {
			continue
		}
		raw, resumeAt := collectOpenCodeModelJSON(lines, next)
		if json.Valid(raw) {
			annotateOpenCodeModelMetadata(&models[idx], raw)
		}
		i = resumeAt - 1
	}
	return models
}

func parseOpenCodeModelIDLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	id := fields[0]
	if strings.HasPrefix(id, `"`) || strings.HasPrefix(id, "{") || strings.HasPrefix(id, "[") {
		return ""
	}
	if !strings.Contains(id, "/") {
		return ""
	}
	// Skip header rows such as PROVIDER/MODEL.
	if id == strings.ToUpper(id) {
		return ""
	}
	return id
}

func collectOpenCodeModelJSON(lines []string, start int) ([]byte, int) {
	var b strings.Builder
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if i > start && parseOpenCodeModelIDLine(line) != "" {
			return []byte(b.String()), i
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(lines[i])
		if json.Valid([]byte(b.String())) {
			return []byte(b.String()), i + 1
		}
	}
	return []byte(b.String()), len(lines)
}

type opencodeModelMetadata struct {
	Reasoning bool                            `json:"reasoning"`
	Variants  map[string]opencodeModelVariant `json:"variants"`
}

type opencodeModelVariant struct {
	Disabled        bool            `json:"disabled"`
	ReasoningEffort string          `json:"reasoningEffort"`
	Thinking        json.RawMessage `json:"thinking"`
}

var opencodeVariantLabel = map[string]string{
	"none":    "None",
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
}

var opencodeVariantOrder = map[string]int{
	"none":    0,
	"minimal": 1,
	"low":     2,
	"medium":  3,
	"high":    4,
	"xhigh":   5,
	"max":     6,
}

func annotateOpenCodeModelMetadata(model *Model, raw []byte) {
	var meta opencodeModelMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	if !meta.Reasoning && !openCodeVariantsLookReasoning(meta.Variants) {
		return
	}
	levels := openCodeThinkingLevelsFromVariants(meta.Variants)
	if len(levels) == 0 {
		return
	}
	model.Thinking = &ModelThinking{SupportedLevels: levels}
}

func openCodeVariantsLookReasoning(variants map[string]opencodeModelVariant) bool {
	for name, variant := range variants {
		if _, known := opencodeVariantOrder[name]; known {
			return true
		}
		if variant.ReasoningEffort != "" || len(variant.Thinking) > 0 {
			return true
		}
	}
	return false
}

func openCodeThinkingLevelsFromVariants(variants map[string]opencodeModelVariant) []ThinkingLevel {
	if len(variants) == 0 {
		return nil
	}
	values := make([]string, 0, len(variants))
	for value, variant := range variants {
		if value == "" || variant.Disabled {
			continue
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		left, leftKnown := opencodeVariantOrder[values[i]]
		right, rightKnown := opencodeVariantOrder[values[j]]
		if leftKnown && rightKnown {
			return left < right
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return values[i] < values[j]
	})
	levels := make([]ThinkingLevel, 0, len(values))
	for _, value := range values {
		label, ok := opencodeVariantLabel[value]
		if !ok {
			label = strings.Title(strings.ReplaceAll(value, "-", " ")) //nolint:staticcheck
		}
		levels = append(levels, ThinkingLevel{Value: value, Label: label})
	}
	return levels
}

var piThinkingLevelLabels = map[string]string{
	"off":     "Off",
	"minimal": "Minimal",
	"low":     "Low",
	"medium":  "Medium",
	"high":    "High",
	"xhigh":   "Extra high",
	"max":     "Max",
}

var piThinkingLevelOrder = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// Keep Pi discovery within the established 15-second window while reserving a
// real opportunity for the compatibility fallback when RPC hangs.
const (
	piRPCDiscoveryTimeout   = 7 * time.Second
	piTableDiscoveryTimeout = 8 * time.Second
)

type piRPCModel struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Provider         string             `json:"provider"`
	Reasoning        bool               `json:"reasoning"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
}

type piRPCState struct {
	Model         *piRPCModel `json:"model"`
	ThinkingLevel string      `json:"thinkingLevel"`
}

type piRPCResponse struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

// discoverPiModels asks Pi's RPC API for its machine-readable model catalog.
// The RPC model objects carry the exact per-model reasoning metadata that the
// human-readable `--list-models` table reduces to a yes/no column. Older Pi
// versions and pi-family forks may not implement RPC, so discovery falls back
// to the existing table parser without advertising a guessed thinking catalog.
func discoverPiModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverPiModelsWithin(ctx, executablePath, piRPCDiscoveryTimeout, piTableDiscoveryTimeout)
}

func discoverPiModelsWithin(ctx context.Context, executablePath string, rpcTimeout, tableTimeout time.Duration) ([]Model, error) {
	if executablePath == "" {
		executablePath = "pi"
	}
	lookedUp, err := exec.LookPath(executablePath)
	if err != nil {
		return []Model{}, nil
	}
	// Split the established 15-second discovery budget so an RPC surface that
	// accepts the mode but never answers cannot starve the compatibility table
	// fallback. Both phase contexts still inherit caller cancellation.
	rpcCtx, rpcCancel := context.WithTimeout(ctx, rpcTimeout)
	models, ok := discoverPiModelsRPC(rpcCtx, executablePath, lookedUp)
	rpcCancel()
	if ok {
		return models, nil
	}

	tableCtx, tableCancel := context.WithTimeout(ctx, tableTimeout)
	defer tableCancel()
	return discoverPiModelsTable(tableCtx, executablePath)
}

// discoverPiModelsRPC starts a short-lived Pi RPC session and requests both
// the available models and current state. The state identifies the model Pi
// will choose when Multica omits --model; its thinking level is the runtime's
// effective default for that selected model.
func discoverPiModelsRPC(ctx context.Context, executablePath, lookedUp string) ([]Model, bool) {
	args := []string{
		"--mode", "rpc",
		"--no-session",
		// Extensions stay enabled because Pi extensions can register providers
		// and models; disabling them would make RPC discovery disagree with the
		// catalog used by real task execution.
		"--no-skills",
		"--no-prompt-templates",
		"--no-context-files",
	}
	argv0, cmdArgs := choosePiInvocation(executablePath, lookedUp, args, slog.Default())
	cmd := exec.CommandContext(ctx, argv0, cmdArgs...)
	hideAgentWindow(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, false
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, false
	}

	encoder := json.NewEncoder(stdin)
	requests := []map[string]string{
		{"id": "multica-state", "type": "get_state"},
		{"id": "multica-models", "type": "get_available_models"},
	}
	for _, request := range requests {
		if err := encoder.Encode(request); err != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return nil, false
		}
	}

	var (
		rawModels  []piRPCModel
		state      piRPCState
		modelsDone bool
		stateDone  bool
	)
	scanner := newAgentStreamScanner(stdout)
	for scanner.Scan() {
		var response piRPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil || response.Type != "response" {
			continue
		}
		switch {
		case response.ID == "multica-state" || response.Command == "get_state":
			stateDone = true
			if response.Success {
				_ = json.Unmarshal(response.Data, &state)
			}
		case response.ID == "multica-models" || response.Command == "get_available_models":
			modelsDone = true
			if response.Success {
				var payload struct {
					Models []piRPCModel `json:"models"`
				}
				if err := json.Unmarshal(response.Data, &payload); err == nil {
					rawModels = payload.Models
				}
			}
		}
		if modelsDone && stateDone {
			break
		}
	}

	// Pi's RPC loop waits for more commands while stdin remains open. EOF ends
	// the throwaway session cleanly; the context remains a safety net for an
	// older/forked CLI that ignores EOF or never completes both responses.
	_ = stdin.Close()
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return nil, false
	}
	if !modelsDone || len(rawModels) == 0 {
		return nil, false
	}
	return piModelsFromRPC(rawModels, state), true
}

func piModelsFromRPC(rawModels []piRPCModel, state piRPCState) []Model {
	defaultID := ""
	if state.Model != nil && state.Model.Provider != "" && state.Model.ID != "" {
		defaultID = state.Model.Provider + "/" + state.Model.ID
	}

	models := make([]Model, 0, len(rawModels))
	seen := make(map[string]bool, len(rawModels))
	for _, raw := range rawModels {
		provider := strings.TrimSpace(raw.Provider)
		modelID := strings.TrimSpace(raw.ID)
		if provider == "" || modelID == "" {
			continue
		}
		id := provider + "/" + modelID
		if seen[id] {
			continue
		}
		seen[id] = true

		model := Model{
			ID:       id,
			Label:    id,
			Provider: provider,
			Default:  id == defaultID,
			Thinking: piThinkingFromRPCModel(raw),
		}
		if model.Default && model.Thinking != nil && piThinkingSupports(model.Thinking, state.ThinkingLevel) {
			model.Thinking.DefaultLevel = state.ThinkingLevel
		}
		models = append(models, model)
	}
	return models
}

// piThinkingFromRPCModel follows Pi's getSupportedThinkingLevels(model) for
// reasoning-capable models: off..high are available unless explicitly mapped
// to null; xhigh/max require an explicit non-null mapping. Pi reports only
// "off" for reasoning=false; Multica intentionally hides that no-op picker.
func piThinkingFromRPCModel(model piRPCModel) *ModelThinking {
	if !model.Reasoning {
		return nil
	}
	levels := make([]ThinkingLevel, 0, len(piThinkingLevelOrder))
	for _, value := range piThinkingLevelOrder {
		mapped, present := model.ThinkingLevelMap[value]
		if present && mapped == nil {
			continue
		}
		if (value == "xhigh" || value == "max") && !present {
			continue
		}
		levels = append(levels, ThinkingLevel{Value: value, Label: piThinkingLevelLabels[value]})
	}
	if len(levels) == 0 {
		return nil
	}
	return &ModelThinking{SupportedLevels: levels}
}

func piThinkingSupports(thinking *ModelThinking, value string) bool {
	if thinking == nil || value == "" {
		return false
	}
	for _, level := range thinking.SupportedLevels {
		if level.Value == value {
			return true
		}
	}
	return false
}

// discoverPiModelsTable runs the legacy human-readable catalog command.
// Older pi versions print the list to stderr; newer versions use stdout. We
// capture both and parse whichever is non-empty. The fallback intentionally
// leaves Thinking nil because the table exposes only a yes/no capability bit.
func discoverPiModelsTable(ctx context.Context, executablePath string) ([]Model, error) {
	// Newer pi fetches its catalog from each configured provider over the
	// network, so discovery time scales with provider count — a multi-provider
	// setup measured ~4.6-4.8s, right at the old 5s cap. When jitter pushed it
	// over, the daemon killed the command before it printed anything and the
	// model picker came back empty while the runtime stayed online. 15s matches
	// the opencode discovery cap (see #3729, same class as #3627).
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "--list-models")
	hideAgentWindow(cmd)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil && len(stdout) == 0 && stderr.Len() == 0 {
		return []Model{}, nil
	}
	text := string(stdout)
	if strings.TrimSpace(text) == "" {
		text = stderr.String()
	}
	return parsePiModels(text), nil
}

// parsePiModels accepts the `pi --list-models` output. Pi historically
// emitted `provider:model` per line and now emits a multi-column table
// (`provider  model  context …`); both shapes are normalized to
// `provider/model` to match opencode/UI conventions. The case-insensitive
// `provider` token in column 0 is treated as the table header and skipped.
func parsePiModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// pi interleaves human-readable diagnostics with the catalog when an
		// agent config references stale patterns — e.g.
		//   Warning: No models match pattern "opencode-go/mimo-v2-omni"
		// Skip them before field-splitting; otherwise prose tokens are coined
		// into bogus models like `No/models` or `Warning/`. See #3729.
		if isPiDiscoveryNoise(line) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		if strings.EqualFold(first, "provider") {
			continue
		}
		var id string
		if strings.ContainsAny(first, ":/") {
			// Legacy `provider:model` format — normalize colon to slash.
			// Restricted to this branch so a model name with a `:` in
			// the table format's column 1 is not silently rewritten.
			id = strings.Replace(first, ":", "/", 1)
		} else if len(fields) >= 2 {
			id = first + "/" + fields[1]
		} else {
			continue
		}
		// A real id has a non-empty provider and model on both sides of the
		// slash. Drop anything that doesn't (e.g. a stray `something:` token),
		// a cheap structural backstop on top of the diagnostic filter above.
		if slash := strings.Index(id, "/"); slash <= 0 || slash == len(id)-1 {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		provider := ""
		if i := strings.Index(id, "/"); i > 0 {
			provider = id[:i]
		}
		models = append(models, Model{ID: id, Label: id, Provider: provider})
	}
	return models
}

// isPiDiscoveryNoise reports whether a `pi --list-models` line is a diagnostic
// message rather than a catalog row. pi prints these alongside the table when
// an agent config references stale provider/model patterns, e.g.
//
//	Warning: No models match pattern "opencode-go/mimo-v2-omni"
//
// The `Warning:` prefix is not guaranteed across versions, so the unmatched-
// pattern message is also matched on its own. These are prose, not
// `provider model` rows; without skipping them the field splitter coins bogus
// models like `No/models`. See #3729.
//
// A pi-family custom runtime profile (MUL-3284) can point at a fork that has
// no `--list-models` at all. Those exit non-zero printing usage text —
//
//	Error: unknown flag: --list-models
//	Run `omp --help` for available flags.
//
// — and the second line has no diagnostic prefix, so it used to be coined into
// a `Run/`omp` model. Usage hints are matched on their own markers (backtick-
// quoted commands, `--help`, `usage:`, unknown flag/command) so the picker
// comes back empty and the UI falls back to manual entry instead of offering
// garbage IDs. Deliberately narrow: catalog rows are `provider/model` or
// `provider model …` tokens, none of which carry these markers. The parse-on-
// non-zero-exit behaviour from #3729 is untouched — older pi really does print
// its catalog to stderr while exiting non-zero. See #4482.
func isPiDiscoveryNoise(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "no models match pattern") {
		return true
	}
	if strings.HasPrefix(lower, "warning:") ||
		strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "info:") {
		return true
	}
	return strings.Contains(line, "`") ||
		strings.Contains(lower, "--help") ||
		strings.Contains(lower, "usage:") ||
		strings.Contains(lower, "unknown flag") ||
		strings.Contains(lower, "unknown command")
}

// discoverOmpModels runs `omp models --json` and parses the JSON catalog.
// omp (oh-my-pi) rejects `--list-models` — a pi flag it never adopted, and it
// exits non-zero on it — so its native discovery is `omp models --json`, which
// prints a `{"models":[...]}` object; parseOmpModels documents the entry shape.
// An empty catalog (an omp with no provider credentials configured prints
// `{"models":[]}`) or a non-zero exit (binary missing, omp too old) falls back
// to an empty list so the UI degrades to manual entry instead of erroring.
func discoverOmpModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "omp"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "models", "--json")
	hideAgentWindow(cmd)
	stdout, err := cmd.Output()
	if err != nil || len(stdout) == 0 {
		return []Model{}, nil
	}
	return parseOmpModels(stdout)
}

// parseOmpModels parses the JSON output from `omp models --json`. omp emits
// an object wrapper with a `models` array — NOT a bare top-level array:
//
//	{"models":[{"provider":"anthropic","id":"claude-sonnet-5","selector":"anthropic/claude-sonnet-5","name":"Claude Sonnet 5",...}]}
//
// The persistable Model.ID is the selector (provider/id), matching the
// convention parsePiModels uses: buildPiArgs splits Model.ID on "/" and
// emits --provider + --model. Using the bare id would drop --provider and
// let omp's internal provider priority ranking pick a different backend
// for the same model id. Provider is kept for UI grouping. The dedup key
// is the selector when present, falling back to provider/id.
func parseOmpModels(data []byte) ([]Model, error) {
	var wrapper struct {
		Models []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Selector string `json:"selector"`
			Name     string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return []Model{}, nil
	}
	var models []Model
	seen := map[string]bool{}
	for _, e := range wrapper.Models {
		bareID := strings.TrimSpace(e.ID)
		if bareID == "" {
			continue
		}
		provider := strings.TrimSpace(e.Provider)
		// Model.ID is the qualified selector (provider/id) so buildPiArgs
		// emits both --provider and --model. When selector is absent, fall
		// back to provider/id; when provider is also absent, the bare id is
		// the only form available.
		selector := strings.TrimSpace(e.Selector)
		if selector == "" {
			if provider != "" {
				selector = provider + "/" + bareID
			} else {
				selector = bareID
			}
		}
		if seen[selector] {
			continue
		}
		seen[selector] = true
		label := strings.TrimSpace(e.Name)
		if label == "" {
			label = selector
		}
		models = append(models, Model{ID: selector, Label: label, Provider: provider})
	}
	return models, nil
}

// discoverHermesModels spins up a throwaway `hermes acp` process,
// drives just enough of the protocol to receive the model list
// advertised in the `session/new` response, and shuts it down. The
// list and the `current` flag both come from hermes' own
// `_build_model_state` so whatever ~/.hermes/config.yaml resolves
// to at runtime is exactly what the UI shows.
//
// Failure modes (hermes missing, no credentials, config resolution
// error) all return an empty list so the UI falls back to the
// creatable manual-entry input instead of blocking the form.
func discoverHermesModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "hermes",
		clientName:   "multica-model-discovery",
		extraEnv:     []string{"HERMES_YOLO_MODE=1"},
		tmpdirPrefix: "multica-hermes-discovery-",
	})
}

// discoverKimiModels combines Kimi's ACP model catalog with the structured
// per-model effort data from `kimi provider list --json`. The provider catalog
// is the only authoritative source for per-model supportEfforts/defaultEffort,
// because a session/new response only ever describes the single model that
// session was created with.
//
// Reading the effort catalog out of the session instead does not work, even
// though Kimi (0.33.0) does refresh the thinking option after
// session/set_model. The refreshed option arrives as a `session/update`
// notification (`sessionUpdate: "config_option_update"`), and the requestACP
// helper this file shares matches responses by id and drops notifications.
// Consuming that notification would not be enough either: the option list Kimi
// sends after switching to a low/high/max model still carries the previous
// model's currentValue as a trailing entry (`[low, high, max, on]`), so using
// it as a catalog would render a phantom level. supportEfforts has no such
// residue.
//
// The ACP side is unchanged: Kimi ≤0.28 returns a `models` block
// (`availableModels`/`currentModelId`) and 0.29 moved the same catalog into
// `configOptions` (MUL-5239); the shared parser still accepts both, so the
// discovery path stays identical. See parseACPConfigOptionModels.
//
// Effort selection is gated on the CLI build, because provider-list alone
// cannot tell whether the runtime can act on what it advertises. Verified
// against real binaries: 0.28.1 reports supportEfforts [low high max] for K3
// exactly like 0.33.0 does, but its ACP only implements the on/off toggle, so
// set_config_option("max") returns success while confirming "on". Gating on the
// session's `thinking` config id cannot separate the two — 0.28.1 advertises
// that id too. The version is the only honest signal, and the initialize
// response already carries it, so this costs no extra process.
//
// Above that, support is decided per model and nothing else: a model that
// provider-list does not list, or lists without efforts, keeps Thinking nil,
// which hides the control for that model alone.
//
// Failure modes (kimi missing, not logged in, config error) return an empty
// list so the UI falls back to manual entry.
func discoverKimiModels(ctx context.Context, executablePath string) ([]Model, error) {
	var acpVersion string
	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "kimi",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-kimi-discovery-",
		inspectInit: func(initResult json.RawMessage) {
			acpVersion = acpAgentInfoVersion(initResult)
		},
	})
	if err != nil || len(models) == 0 {
		return models, err
	}
	if !kimiSupportsThinkingEfforts(acpVersion) {
		// Not an error and not worth a Warn: an older CLI still lists models and
		// runs tasks, it just cannot be told which effort to use. Model discovery
		// re-runs on every cache miss, so a Warn here would repeat forever.
		slog.Debug("kimi CLI predates ACP effort selection; hiding thinking controls",
			"detected_version", acpVersion,
			"required_version", kimiMinThinkingEffortVersion,
		)
		return models, nil
	}

	perModel, err := discoverKimiProviderThinking(ctx, executablePath)
	if err != nil {
		// Do not include err or command output here: provider JSON can contain
		// credentials. The fixed reason explains why thinking controls are hidden
		// without leaking host or account data. Debug, not Warn: model discovery
		// re-runs on every cache miss, so an older CLI would log this forever.
		slog.Debug("kimi per-model thinking discovery unavailable; hiding thinking controls",
			"reason", "provider_list_unavailable",
		)
		return models, nil
	}
	for i := range models {
		if thinking, ok := perModel[models[i].ID]; ok {
			models[i].Thinking = thinking
		}
	}
	return models, nil
}

// kimiMinThinkingEffortVersion is the first Kimi Code CLI whose ACP surface
// applies an effort level instead of a plain on/off toggle (upstream 0.29.0,
// released 2026-07-22). Below it, `session/set_config_option` accepts
// "low"/"high"/"max" without error and leaves the session on "on", so
// advertising those levels would promise something the runtime cannot deliver.
const kimiMinThinkingEffortVersion = "0.29.0"

// kimiSupportsThinkingEfforts reports whether an ACP-reported Kimi version can
// act on an effort level. An empty or unparsable version answers no: a build we
// cannot identify (a fork, a wrapper, a future format) is not one we can
// promise effort selection for, and hiding the picker only costs that build a
// control it may not honour anyway. Tasks still run either way.
func kimiSupportsThinkingEfforts(version string) bool {
	detected, err := parseSemver(strings.TrimSpace(version))
	if err != nil {
		return false
	}
	minimum, err := parseSemver(kimiMinThinkingEffortVersion)
	if err != nil {
		return false
	}
	return !detected.lessThan(minimum)
}

// acpAgentInfoVersion pulls `agentInfo.version` out of an ACP initialize
// result. ACP agents report their own build here (Kimi sends
// {"name":"Kimi Code CLI","version":"0.33.0"}); an agent that omits it yields
// "", which every caller must treat as "unknown", never as "new enough".
func acpAgentInfoVersion(raw json.RawMessage) string {
	var response struct {
		AgentInfo struct {
			Version string `json:"version"`
		} `json:"agentInfo"`
		AgentInfoSnake struct {
			Version string `json:"version"`
		} `json:"agent_info"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return ""
	}
	if version := strings.TrimSpace(response.AgentInfo.Version); version != "" {
		return version
	}
	return strings.TrimSpace(response.AgentInfoSnake.Version)
}

func discoverKimiProviderThinking(ctx context.Context, executablePath string) (map[string]*ModelThinking, error) {
	if executablePath == "" {
		executablePath = "kimi"
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, executablePath, "provider", "list", "--json")
	hideAgentWindow(cmd)
	cmd.Stderr = io.Discard
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kimi provider list: %w", err)
	}
	return parseKimiProviderThinking(raw)
}

type kimiProviderModel struct {
	SupportEfforts      []string `json:"supportEfforts"`
	SupportEffortsSnake []string `json:"support_efforts"`
	DefaultEffort       string   `json:"defaultEffort"`
	DefaultEffortSnake  string   `json:"default_effort"`
}

func parseKimiProviderThinking(raw []byte) (map[string]*ModelThinking, error) {
	var response struct {
		Models map[string]kimiProviderModel `json:"models"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("parse kimi provider catalog: %w", err)
	}
	if len(response.Models) == 0 {
		return nil, fmt.Errorf("kimi provider catalog contained no models")
	}

	result := make(map[string]*ModelThinking, len(response.Models))
	for modelID, model := range response.Models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		efforts := model.SupportEfforts
		if len(efforts) == 0 {
			efforts = model.SupportEffortsSnake
		}
		seen := make(map[string]bool, len(efforts))
		levels := make([]ThinkingLevel, 0, len(efforts))
		for _, rawEffort := range efforts {
			effort := strings.TrimSpace(rawEffort)
			if effort == "" || seen[effort] || !isValidDynamicThinkingValue(effort) {
				continue
			}
			seen[effort] = true
			levels = append(levels, ThinkingLevel{
				Value: effort,
				Label: kimiThinkingLabel(effort),
			})
		}
		if len(levels) == 0 {
			continue
		}

		defaultEffort := strings.TrimSpace(model.DefaultEffort)
		if defaultEffort == "" {
			defaultEffort = strings.TrimSpace(model.DefaultEffortSnake)
		}
		if !seen[defaultEffort] {
			defaultEffort = ""
		}
		result[modelID] = &ModelThinking{
			SupportedLevels: levels,
			DefaultLevel:    defaultEffort,
		}
	}
	return result, nil
}

func kimiThinkingLabel(value string) string {
	switch value {
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	case "high":
		return "High"
	case "max":
		return "Max"
	default:
		return strings.Title(value) //nolint:staticcheck
	}
}

type acpConfigOptionState struct {
	ID                string `json:"id"`
	CurrentValue      string `json:"currentValue"`
	CurrentValueSnake string `json:"current_value"`
}

// findACPConfigOption returns one exact config-id match from an ACP response.
// Config ids are protocol identifiers, not display labels: category-only or
// case-insensitive matches would advertise a control that execution cannot set.
// Both camelCase and snake_case field spellings are accepted because ACP agents
// in the wild emit both.
func findACPConfigOption(raw json.RawMessage, configID string) (acpConfigOptionState, bool) {
	var response struct {
		ConfigOptions      []acpConfigOptionState `json:"configOptions"`
		ConfigOptionsSnake []acpConfigOptionState `json:"config_options"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return acpConfigOptionState{}, false
	}
	options := append(response.ConfigOptions, response.ConfigOptionsSnake...)
	for _, option := range options {
		if option.ID == configID {
			return option, true
		}
	}
	return acpConfigOptionState{}, false
}

func acpConfigOptionCurrentValue(raw json.RawMessage, configID string) (string, bool) {
	option, ok := findACPConfigOption(raw, configID)
	if !ok {
		return "", false
	}
	value := strings.TrimSpace(option.CurrentValue)
	if value == "" {
		value = strings.TrimSpace(option.CurrentValueSnake)
	}
	return value, value != ""
}

// discoverReasonixModels drives a short Reasonix ACP session and parses the
// model configOptions advertised by session/new. Authentication and provider
// configuration remain owned by `reasonix setup`; discovery failure therefore
// falls back to manual model entry like the other ACP runtimes.
func discoverReasonixModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:       "reasonix",
		clientName:       "multica-model-discovery",
		acpArgs:          reasonixACPLaunchArgs(),
		tmpdirPrefix:     "multica-reasonix-discovery-",
		isolatedStateEnv: "REASONIX_STATE_HOME",
	})
}

// discoverKiroModels spins up a throwaway `kiro-cli acp` process and parses
// the models block Kiro returns from session/new.
func discoverKiroModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "kiro-cli",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-kiro-discovery-",
	})
}

// discoverCopilotModels spins up `copilot --acp` and reads the
// `availableModels` block from session/new. The catalog is keyed
// off the user's GitHub account, so this is the only way to know
// which IDs they actually have access to (Pro vs Pro+ vs
// Enterprise vs evaluation models).
//
// Falls back to copilotStaticModels() when the binary is missing
// or when the ACP handshake fails (auth missing, network down,
// etc.) so the UI dropdown always has something to show.
//
// We also tag each entry with a vendor in the Provider field —
// the Copilot ACP payload doesn't include one, but the UI groups
// by Provider, so deriving it from the ID prefix keeps OpenAI /
// Anthropic / Gemini sections distinct.
//
// No extra env or permission flags are needed: discovery only
// drives `initialize` + `session/new`, neither of which triggers
// a tool-permission prompt — the model catalog is part of the
// session/new response itself.
func discoverCopilotModels(ctx context.Context, executablePath string) (Catalog, error) {
	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "copilot",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-copilot-discovery-",
		acpArgs:      []string{"--acp"},
	})
	if err != nil || len(models) == 0 {
		return Catalog{Models: copilotStaticModels(), Fallback: true}, nil
	}
	for i := range models {
		if models[i].Provider == "" {
			models[i].Provider = inferCopilotProvider(models[i].ID)
		}
	}
	return Catalog{Models: models}, nil
}

// discoverQoderModels spins up a Qoder CLI binary with `--yolo --acp` and
// parses models from session/new.
func discoverQoderModels(ctx context.Context, executablePath, defaultBin string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   defaultBin,
		clientName:   "multica-model-discovery",
		acpArgs:      []string{"--yolo", "--acp"},
		tmpdirPrefix: "multica-qoder-discovery-",
	})
}

// acpDiscoveryProvider configures how discoverACPModels launches an
// ACP-speaking agent CLI. The shared helper drives every CLI in
// the same way (initialize → optional authenticate → session/new → parse
// models block) — the
// per-provider differences are which binary to spawn, which env
// vars suppress interactive prompts during init, what argv puts
// the binary into ACP server mode (most use `acp`, Copilot uses
// `--acp`), and what to label temporary work directories so they're
// easy to identify in logs.
type acpDiscoveryProvider struct {
	defaultBin       string
	clientName       string
	extraEnv         []string
	tmpdirPrefix     string
	isolatedStateEnv string
	// acpArgs is the argv passed to the binary to start it in ACP
	// server mode. Defaults to []string{"acp"} when nil/empty.
	acpArgs []string
	// selectAuthMethod inspects the initialize response and child environment
	// after initialize succeeds. A non-nil selector must return one advertised
	// method id; its error aborts discovery before any session operation.
	selectAuthMethod func(json.RawMessage, []string) (string, error)
	// strictErrors surfaces stage-specific handshake errors to the caller.
	// Legacy discovery providers keep their empty-list behavior; Grok enables
	// this so it can log the actual fallback reason.
	strictErrors bool
	// annotate receives the parsed catalog plus the raw session/new result so a
	// provider can enrich models from parts of the response the shared parser
	// ignores. CodeBuddy uses it to read its effort catalog out of the same
	// handshake, which is why it needs no separate discovery call at all.
	annotate func([]Model, json.RawMessage)
	// inspectInit receives the raw initialize result before any session is
	// created. It is for reading capability facts the handshake already
	// carries — Kimi reads `agentInfo.version` to gate a feature on the CLI
	// build — so a provider does not have to spend a second process asking the
	// binary what it is. It cannot fail discovery: a provider that learns
	// nothing useful here must degrade, not abort.
	inspectInit func(json.RawMessage)
}

// discoverACPModels runs the ACP handshake for any agent CLI that
// implements the standard `initialize` + `session/new` flow and
// advertises its model catalog in the response under
// `models.availableModels` / `models.currentModelId`, or under the
// newer `configOptions` list. Provider-specific `launchArgs` select
// ACP mode (e.g. `acp` vs `--acp`).
func discoverACPModels(ctx context.Context, executablePath string, p acpDiscoveryProvider) ([]Model, error) {
	fail := func(stage string, err error) ([]Model, error) {
		if p.strictErrors {
			return nil, fmt.Errorf("ACP model discovery %s failed: %w", stage, err)
		}
		return []Model{}, nil
	}
	if executablePath == "" {
		executablePath = p.defaultBin
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return fail("executable lookup", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var isolatedStateDir string
	if p.isolatedStateEnv != "" {
		var err error
		isolatedStateDir, err = os.MkdirTemp("", p.tmpdirPrefix+"state-")
		if err != nil {
			return fail("temporary state", err)
		}
		defer os.RemoveAll(isolatedStateDir)
	}

	cmdArgs := p.acpArgs
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"acp"}
	}
	cmd := exec.CommandContext(runCtx, executablePath, cmdArgs...)
	hideAgentWindow(cmd)
	childEnv := append(os.Environ(), p.extraEnv...)
	if isolatedStateDir != "" {
		childEnv = replaceEnvValue(childEnv, p.isolatedStateEnv, isolatedStateDir)
	}
	cmd.Env = childEnv
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail("stdin setup", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fail("stdout setup", err)
	}
	// Discard stderr; noisy logs here don't help us and we don't
	// want them bleeding into the daemon log every 60s.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fail("process start", err)
	}
	// Ensure the child process is always reaped.
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	nextID := 1
	requestACP := func(method string, params map[string]any) (json.RawMessage, error) {
		id := nextID
		nextID++
		msg := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
			"params":  params,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return nil, err
		}
		data = append(data, '\n')
		if _, err = stdin.Write(data); err != nil {
			return nil, err
		}

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var env struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil || string(env.ID) != fmt.Sprint(id) {
				continue
			}
			if len(env.Error) > 0 && string(env.Error) != "null" {
				var rpcErr struct {
					Code    int             `json:"code"`
					Message string          `json:"message"`
					Data    json.RawMessage `json:"data"`
				}
				_ = json.Unmarshal(env.Error, &rpcErr)
				detail := ""
				if len(rpcErr.Data) > 0 && string(rpcErr.Data) != "null" {
					if err := json.Unmarshal(rpcErr.Data, &detail); err != nil {
						detail = strings.TrimSpace(string(rpcErr.Data))
					}
				}
				return nil, &acpRPCError{Method: method, Code: rpcErr.Code, Message: rpcErr.Message, Data: detail}
			}
			if len(env.Result) == 0 {
				return nil, fmt.Errorf("response contained neither result nor error")
			}
			return env.Result, nil
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if err := runCtx.Err(); err != nil {
			return nil, err
		}
		return nil, io.ErrUnexpectedEOF
	}

	// Each stage is response-driven. In particular, do not guess Grok's auth
	// method or enqueue session/new before initialize/authenticate succeeds.
	initResult, err := requestACP("initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]any{"name": p.clientName, "version": "0.1.0"},
		"clientCapabilities": map[string]any{},
	})
	if err != nil {
		return fail("initialize", err)
	}
	if p.inspectInit != nil {
		p.inspectInit(initResult)
	}

	// session/new requires a valid cwd — use a temp directory we
	// clean up afterwards, not the daemon's workdir (which might
	// be in the middle of another task's worktree).
	tmp, err := os.MkdirTemp("", p.tmpdirPrefix)
	if err != nil {
		return fail("temporary cwd", err)
	}
	defer os.RemoveAll(tmp)

	if p.selectAuthMethod != nil {
		methodID, err := p.selectAuthMethod(initResult, childEnv)
		if err != nil {
			return fail("auth method selection", err)
		}
		if _, err := requestACP("authenticate", map[string]any{
			"methodId": methodID,
			"_meta":    map[string]any{"headless": true},
		}); err != nil {
			return fail(fmt.Sprintf("authenticate (%s)", methodID), err)
		}
	}

	sessionResult, err := requestACP("session/new", map[string]any{
		"cwd":        tmp,
		"mcpServers": []any{},
	})
	if err != nil {
		return fail("session/new", err)
	}
	models := parseACPSessionNewModels(sessionResult)
	if len(models) == 0 {
		// session/new succeeded but carried no catalog we recognise. This
		// is what upstream schema drift looks like from here (MUL-5239:
		// kimi 0.29 moved the catalog from `models` to `configOptions`),
		// and without this line it is indistinguishable from "the CLI
		// really has no models". Log the top-level keys only — never the
		// response body, which carries session ids and account-shaped data.
		slog.Debug("ACP model discovery found no models in session/new response",
			"binary", executablePath,
			"result_keys", strings.Join(acpResultTopLevelKeys(sessionResult), ","),
		)
	}
	if models == nil {
		return fail("session/new model parsing", fmt.Errorf("response contained no model catalog"))
	}
	if err := runCtx.Err(); err != nil {
		return fail("completion", err)
	}
	if p.annotate != nil {
		p.annotate(models, sessionResult)
	}
	return models, nil
}

func replaceEnvValue(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, key) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, key+"="+value)
}

// parseACPSessionNewModels extracts the model catalog from an ACP
// `session/new` response. Hermes and older Kimi (and any other ACP
// agent that follows that schema) emit:
//
//	{
//	  "sessionId": "...",
//	  "models": {
//	    "availableModels": [
//	      {"modelId": "...", "name": "...", "description": "..."}
//	    ],
//	    "currentModelId": "..."
//	  }
//	}
//
// Newer agents advertise the same catalog through the ACP
// `configOptions` list instead — see parseACPConfigOptionModels. Both
// shapes are accepted: the `models` block wins when present, and
// `configOptions` is consulted only when it yields nothing, so no
// existing provider changes behaviour.
//
// Returns nil (not an empty slice) when the payload is missing so
// the caller can distinguish "parsed with no models" (valid but
// empty catalog) from "couldn't find the structure at all".
func parseACPSessionNewModels(raw json.RawMessage) []Model {
	type acpModelInfo struct {
		ModelID      string `json:"modelId"`
		ModelIDSnake string `json:"model_id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
	}
	var resp struct {
		Models struct {
			AvailableModels      []acpModelInfo `json:"availableModels"`
			AvailableModelsSnake []acpModelInfo `json:"available_models"`
			CurrentModelID       string         `json:"currentModelId"`
			CurrentModelIDSnake  string         `json:"current_model_id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	availableModels := resp.Models.AvailableModels
	if len(availableModels) == 0 && resp.Models.AvailableModelsSnake != nil {
		availableModels = resp.Models.AvailableModelsSnake
	}
	currentModelID := strings.TrimSpace(resp.Models.CurrentModelID)
	if currentModelID == "" {
		currentModelID = strings.TrimSpace(resp.Models.CurrentModelIDSnake)
	}
	models := make([]Model, 0, len(availableModels))
	seen := map[string]bool{}
	for _, m := range availableModels {
		modelID := strings.TrimSpace(m.ModelID)
		if modelID == "" {
			modelID = strings.TrimSpace(m.ModelIDSnake)
		}
		if modelID == "" || seen[modelID] {
			continue
		}
		seen[modelID] = true
		models = append(models, acpModelEntry(modelID, m.Name, currentModelID))
	}
	if len(models) > 0 {
		return models
	}
	if fromConfig := parseACPConfigOptionModels(raw); len(fromConfig) > 0 {
		return fromConfig
	}
	return models
}

// parseACPConfigOptionModels extracts the model catalog from the ACP
// `configOptions` list returned by `session/new`. Kimi Code 0.29 dropped
// the top-level `models` block in favour of this shape (MUL-5239):
//
//	{
//	  "sessionId": "...",
//	  "configOptions": [
//	    {
//	      "type": "select", "id": "model", "name": "Model", "category": "model",
//	      "currentValue": "kimi-code/k3",
//	      "options": [
//	        {"value": "kimi-code/k3", "name": "K3"},
//	        {"value": "kimi-code/kimi-for-coding", "name": "K2.7 Coding"}
//	      ]
//	    },
//	    {"id": "thinking", "category": "thought_level", ...}
//	  ]
//	}
//
// A config option counts as the model picker when its `id` or `category`
// is "model" (case-insensitive). Every other option — thinking level in
// particular — is deliberately ignored: those are separate product
// surfaces, and mapping them into the model dropdown would offer values
// `session/set_model` cannot honour. `currentValue` marks the default.
//
// Returns nil when no model option is present, so the caller can keep
// whatever the `models` block produced.
func parseACPConfigOptionModels(raw json.RawMessage) []Model {
	type acpConfigChoice struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	}
	type acpConfigOption struct {
		ID                string            `json:"id"`
		Category          string            `json:"category"`
		CurrentValue      string            `json:"currentValue"`
		CurrentValueSnake string            `json:"current_value"`
		Options           []acpConfigChoice `json:"options"`
	}
	var resp struct {
		ConfigOptions      []acpConfigOption `json:"configOptions"`
		ConfigOptionsSnake []acpConfigOption `json:"config_options"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	configOptions := resp.ConfigOptions
	if len(configOptions) == 0 {
		configOptions = resp.ConfigOptionsSnake
	}
	for _, opt := range configOptions {
		if !strings.EqualFold(strings.TrimSpace(opt.ID), "model") &&
			!strings.EqualFold(strings.TrimSpace(opt.Category), "model") {
			continue
		}
		currentValue := strings.TrimSpace(opt.CurrentValue)
		if currentValue == "" {
			currentValue = strings.TrimSpace(opt.CurrentValueSnake)
		}
		models := make([]Model, 0, len(opt.Options))
		seen := map[string]bool{}
		for _, choice := range opt.Options {
			modelID := strings.TrimSpace(choice.Value)
			if modelID == "" || seen[modelID] {
				continue
			}
			seen[modelID] = true
			models = append(models, acpModelEntry(modelID, choice.Name, currentValue))
		}
		if len(models) > 0 {
			return models
		}
	}
	return nil
}

// acpModelEntry builds one dropdown entry from an ACP-advertised model id
// and its display name. Provider is derived from the `provider:model` form
// only — ids like kimi's `kimi-code/k3` carry no colon and stay ungrouped,
// which matches how the UI renders a flat catalog.
func acpModelEntry(modelID, name, currentModelID string) Model {
	provider := ""
	if idx := strings.Index(modelID, ":"); idx > 0 {
		provider = modelID[:idx]
	}
	return Model{
		ID:       modelID,
		Label:    acpModelLabel(name, modelID),
		Provider: provider,
		Default:  modelID == currentModelID,
	}
}

// acpResultTopLevelKeys returns the sorted top-level object keys of an ACP
// result. Keys only, never values: enough to tell `configOptions` drift from
// a genuinely empty catalog in daemon.log without logging session ids or any
// other content from the upstream response.
func acpResultTopLevelKeys(raw json.RawMessage) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func acpModelLabel(name, modelID string) string {
	label := strings.TrimSpace(name)
	if label == "" || strings.EqualFold(label, "unknown") {
		return modelID
	}
	return label
}

// discoverAntigravityModels runs `agy models` and returns the catalog the
// installed Antigravity CLI advertises (one display name per line).
//
// Unlike cursor / pi / opencode there is deliberately NO static fallback.
// agy's `--model` takes the exact human display string (e.g.
// "Claude Opus 4.6 (Thinking)") and silently no-ops on any value it doesn't
// recognise — empty output, exit 0 — so a guessed static list would risk
// offering a model the installed CLI can't honour, turning a typo into a
// "successful" empty run. On any discovery failure we return an empty
// catalog instead; agent.model stays unset and agy resolves its own
// default. cachedDiscovery never caches empty results, so this retries on
// the next request once the cause clears.
func discoverAntigravityModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "agy"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return nil, nil
	}
	// `agy models` is a local enumeration (no network round-trip), so a
	// short cap is plenty; keep it generous enough to absorb cold starts.
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "models")
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, nil
	}
	return parseAntigravityModels(string(out)), nil
}

// parseAntigravityModels turns `agy models` output — one model display name
// per line — into Model entries. The display string IS the value `--model`
// expects, so ID and Label are identical and the daemon ships opts.Model
// verbatim. Blank and duplicate lines are skipped.
func parseAntigravityModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, Model{
			ID:       name,
			Label:    name,
			Provider: "antigravity",
		})
	}
	return models
}

// discoverGrokModels spins up `grok agent --always-approve stdio` and parses
// the model catalog from session/new (same shape as Kiro/Qoder/Trae). Requires
// an authenticated Grok CLI; on any failure falls back to grokStaticModels.
func discoverGrokModels(ctx context.Context, executablePath string) (Catalog, error) {
	// Match the daemon's runtime launch: `--no-auto-update` (global) so a
	// background update check can't stall discovery. Auth is selected only
	// after initialize returns the methods this installed CLI actually offers.
	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "grok",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-grok-discovery-",
		acpArgs:      []string{"--no-auto-update", "agent", "--always-approve", "stdio"},
		selectAuthMethod: func(initResult json.RawMessage, childEnv []string) (string, error) {
			return selectGrokAuthMethod(extractACPAuthMethods(initResult), envHasNonEmpty(childEnv, "XAI_API_KEY"))
		},
		strictErrors: true,
	})
	if err != nil || len(models) == 0 {
		if err != nil {
			slog.Debug("grok model discovery fell back to static catalog", "error", err)
		}
		return Catalog{Models: grokStaticModels(), Fallback: true}, nil
	}
	for i := range models {
		if models[i].Provider == "" {
			models[i].Provider = "xai"
		}
	}
	annotateGrokThinking(models)
	return Catalog{Models: models}, nil
}

// grokStaticModels is the offline fallback catalog for the Grok Build CLI.
// IDs match a typical signed-in `session/new` / `grok models` listing.
func grokStaticModels() []Model {
	models := []Model{
		{ID: "grok-4.5", Label: "Grok 4.5", Provider: "xai", Default: true},
		{ID: "grok-composer-2.5-fast", Label: "Grok Composer 2.5 Fast", Provider: "xai"},
	}
	annotateGrokThinking(models)
	return models
}

// annotateGrokThinking attaches only capabilities confirmed by xAI's
// per-model reasoning documentation. session/new does not advertise effort
// catalogs, so unknown and composer models deliberately keep Thinking nil
// instead of exposing values that may fail at runtime.
func annotateGrokThinking(models []Model) {
	for i := range models {
		if models[i].ID != "grok-4.5" {
			continue
		}
		models[i].Thinking = &ModelThinking{SupportedLevels: []ThinkingLevel{
			{Value: "low", Label: "Low"},
			{Value: "medium", Label: "Medium"},
			{Value: "high", Label: "High"},
		}}
	}
}

// discoverCursorModels runs `cursor-agent --list-models` and parses
// the `id - Label` rows. Cursor's catalog changes often and ships
// many variants of the same base model (thinking / fast / max
// suffixes) — static baking would be obsolete within weeks. On any
// failure we fall back to the minimal static catalog so the UI
// stays usable when cursor-agent isn't installed on the daemon host.
func discoverCursorModels(ctx context.Context, executablePath string) (Catalog, error) {
	if executablePath == "" {
		executablePath = "cursor-agent"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return Catalog{Models: cursorStaticModels(), Fallback: true}, nil
	}
	// 15s to match the other network-backed discovery paths (pi/opencode/ACP);
	// cursor-agent fetches its frequently-changing catalog, so a tight cap can
	// time out and fall back to the minimal static list. See #3729.
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "--list-models")
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return Catalog{Models: cursorStaticModels(), Fallback: true}, nil
	}
	models := parseCursorModels(string(out))
	if len(models) == 0 {
		return Catalog{Models: cursorStaticModels(), Fallback: true}, nil
	}
	return Catalog{Models: models}, nil
}

// parseCursorModels extracts model IDs from `cursor-agent --list-models`.
// Output format (as of cursor-agent 2026.04):
//
//	Available models
//	<blank>
//	auto - Auto
//	composer-2-fast - Composer 2 Fast (current, default)
//	composer-2 - Composer 2
//	…
//
// The model tagged `(default)` is surfaced as Default=true so the
// UI badge points at cursor's own recommendation rather than a
// hard-coded guess from our catalog.
func parseCursorModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Row format: "<id> - <label>". Skip the "Available models" header.
		idx := strings.Index(line, " - ")
		if idx <= 0 {
			continue
		}
		id := strings.TrimSpace(line[:idx])
		label := strings.TrimSpace(line[idx+3:])
		if !isOpenclawIdentifier(id) {
			// Reuse the identifier guard — cursor IDs are in the
			// same character set (alnum + `-./_`), so anything
			// that fails it is either malformed or a header line.
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		isDefault := strings.Contains(label, "default")
		// Strip the "(current, default)" suffix from the display
		// label since we surface that through the Default flag.
		if paren := strings.Index(label, "("); paren > 0 {
			label = strings.TrimSpace(label[:paren])
		}
		if label == "" {
			label = id
		}
		models = append(models, Model{
			ID:       id,
			Label:    label,
			Provider: "cursor",
			Default:  isDefault,
		})
	}
	return models
}

// discoverOpenclawAgents enumerates the pre-registered OpenClaw
// agents (which is where model selection actually lives in the
// OpenClaw world — each agent is bound to a model at `agents add`
// time). It tries structured JSON output first, falling back to a
// conservative text parser that rejects TUI decoration and section
// headers. On any ambiguity we return an empty list and let the
// creatable dropdown handle manual entry — a silently-wrong
// enumeration would be worse than none.
func discoverOpenclawAgents(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "openclaw"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Try JSON modes first. Different openclaw builds expose the
	// flag under different names; trying a couple is cheap.
	for _, jsonArgs := range [][]string{
		{"agents", "list", "--json"},
		{"agents", "list", "--output", "json"},
		{"agents", "list", "-o", "json"},
	} {
		cmd := exec.CommandContext(runCtx, executablePath, jsonArgs...)
		hideAgentWindow(cmd)
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			continue
		}
		if models, ok := parseOpenclawAgentsJSON(out); ok {
			return models, nil
		}
	}

	// Text fallback. Be strict — the default output is a decorated
	// banner with box-drawing and section headers, and picking up
	// the wrong tokens produces nonsense entries like "Identity:".
	cmd := exec.CommandContext(runCtx, executablePath, "agents", "list")
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return []Model{}, nil
	}
	return parseOpenclawAgents(string(out)), nil
}

// openclawAgentEntry is the shape parseOpenclawAgentsJSON expects
// from `openclaw agents list --json`. `id` is the routing key
// passed to `openclaw agent --agent <id>`; `name` is the human
// display label set via `openclaw agents set-identity --name` and
// is only used to enrich the dropdown label. The two are not
// interchangeable — see openclawEntriesToModels for the mapping.
// Older openclaw versions may emit only `name`; in that case we
// fall back to using it as the id for backward compatibility.
// `model` is optional and only used to enrich the dropdown label.
type openclawAgentEntry struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Model string `json:"model"`
}

// parseOpenclawAgentsJSON accepts `openclaw agents list --json`-style
// output. It handles two common shapes: a top-level array, or an
// object with an `agents` key whose value is an array. Returns
// ok=false if the input isn't valid JSON in either shape.
func parseOpenclawAgentsJSON(raw []byte) ([]Model, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false
	}

	var flat []openclawAgentEntry
	if err := json.Unmarshal(raw, &flat); err == nil {
		return openclawEntriesToModels(flat), true
	}

	var wrapped struct {
		Agents []openclawAgentEntry `json:"agents"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Agents != nil {
		return openclawEntriesToModels(wrapped.Agents), true
	}

	return nil, false
}

func openclawEntriesToModels(entries []openclawAgentEntry) []Model {
	models := make([]Model, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		// Use ID as the model identifier because openclaw resolves
		// --agent by id, not by display name. Names may contain spaces
		// (e.g. "Sub2API OPS") which openclaw's normalizeAgentId would
		// mangle into a different string ("sub2api-ops"), causing a
		// lookup miss and "no parseable output" errors.
		id := e.ID
		if id == "" {
			id = e.Name
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		displayName := e.Name
		if displayName == "" {
			displayName = id
		}
		label := displayName
		if e.Model != "" {
			label = displayName + " (" + e.Model + ")"
		}
		models = append(models, Model{ID: id, Label: label, Provider: "openclaw"})
	}
	return models
}

// parseOpenclawAgents extracts agent names from the text output of
// `openclaw agents list`. The default CLI output is a decorated
// banner — section headers ending in `:`, box-drawing characters,
// and single-character icons — so we only accept lines that look
// like a proper `<name> <model>` row: at least two whitespace-
// separated tokens, both made of safe identifier characters, and
// neither ending in `:`. Anything else is discarded to avoid
// surfacing "Identity:" or `◇` as selectable models.
func parseOpenclawAgents(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, model := fields[0], fields[1]
		if !isOpenclawIdentifier(name) || !isOpenclawIdentifier(model) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, Model{
			ID:       name,
			Label:    name + " (" + model + ")",
			Provider: "openclaw",
		})
	}
	return models
}

// isOpenclawIdentifier reports whether s looks like a valid
// agent-name or model-id token: starts with a letter, contains only
// identifier-safe characters, and isn't a section header
// (trailing colon). Rejects TUI decoration like `│`, `╭`, `◇`, `|`.
func isOpenclawIdentifier(s string) bool {
	if s == "" || strings.HasSuffix(s, ":") {
		return false
	}
	first := s[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return false
		}
	}
	return true
}

// ── CodeBuddy model discovery ──

// discoverCodebuddyModels asks CodeBuddy for its catalog over ACP
// (`codebuddy --acp`), the same handshake Copilot / Kimi / Reasonix / Kiro / Qoder / Grok /
// TRAE already use. `session/new` answers with a structured catalog under
// `models.availableModels` plus a `currentModelId`, which is what the shared
// parseACPSessionNewModels reads.
//
// This replaces scraping the `--model` line out of `codebuddy --help` (MUL-5549).
// The help text carried IDs and nothing else, so labels had to be guessed from
// the ID and produced names CodeBuddy does not use ("Kimi K3 1" for what the CLI
// calls Kimi-K3, "Deepseek V3 2 Volc" for DeepSeek-V3.2), the default model was
// a "first entry wins" guess rather than the advertised currentModelId, and the
// effort catalog needed a second regex over the same output. Measured against
// CodeBuddy 2.130.0 the handshake is also markedly faster than --help — which
// matters because that command is slow enough to have been the prime suspect for
// the timeouts behind #6180.
//
// Falls back to the static catalog (marked Fallback, so it can never be cached
// as authoritative) when the handshake fails — including the not-logged-in case,
// where session/new may legitimately refuse.
func discoverCodebuddyModels(ctx context.Context, executablePath string) (Catalog, error) {
	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "codebuddy",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-codebuddy-discovery-",
		acpArgs:      []string{"--acp"},
		strictErrors: true,
		annotate:     annotateCodebuddyThinkingFromACP,
	})
	if err != nil || len(models) == 0 {
		if err != nil {
			slog.Debug("codebuddy model discovery fell back to static catalog", "error", err)
		}
		return codebuddyFallbackCatalog(), nil
	}
	// Same post-pass Copilot runs: the ACP payload carries no vendor, and
	// acpModelEntry can only recover one from a `vendor:model` id. CodeBuddy's
	// ids are bare (`glm-5.2`, `kimi-k3-1`), so without this every model lands
	// in one unlabelled group instead of the Zhipu / Kimi / DeepSeek sections
	// the picker renders from Provider.
	for i := range models {
		if models[i].Provider == "" {
			models[i].Provider = codebuddyModelProvider(models[i].ID)
		}
	}
	return Catalog{Models: models}, nil
}

// codebuddyModelProvider infers a vendor from a CodeBuddy model ID prefix.
// CodeBuddy aggregates several vendors under its own account, and neither the
// ACP catalog nor the static fallback carries a vendor field, so the ID prefix
// is the only signal available for grouping the picker.
func codebuddyModelProvider(id string) string {
	switch {
	case strings.HasPrefix(id, "claude-"):
		return "anthropic"
	case strings.HasPrefix(id, "gemini-"):
		return "google"
	case strings.HasPrefix(id, "gpt-"):
		return "openai"
	case strings.HasPrefix(id, "glm-"):
		return "zhipu"
	case strings.HasPrefix(id, "minimax-"):
		return "minimax"
	case strings.HasPrefix(id, "kimi-"):
		return "kimi"
	case len(id) >= 3 && id[0] == 'h' && id[1] == 'y' && id[2] >= '0' && id[2] <= '9':
		return "hunyuan"
	case strings.HasPrefix(id, "deepseek-"):
		return "deepseek"
	default:
		return ""
	}
}

// codebuddyFallbackCatalog is the static stand-in for a failed discovery. It
// applies the static effort levels locally rather than probing again: whatever
// broke the ACP handshake (binary missing, CLI not logged in, timeout) would
// break a second attempt too.
func codebuddyFallbackCatalog() Catalog {
	models := codebuddyStaticModels()
	applyCodebuddyStaticThinking(models)
	return Catalog{Models: models, Fallback: true}
}

// codebuddyStaticModels is the fallback catalog when ACP discovery fails
// (binary missing, CLI not logged in, handshake timeout).
//
// These IDs do not overlap CodeBuddy's real catalog at all, so this list is a
// last-resort affordance to keep the picker usable, never an answer. It is
// always returned marked Fallback so it cannot be cached as authoritative
// (MUL-5549).
func codebuddyStaticModels() []Model {
	return []Model{
		{ID: "claude-sonnet-4.6", Label: "Claude Sonnet 4.6", Provider: "anthropic", Default: true},
		{ID: "claude-opus-4.7", Label: "Claude Opus 4.7", Provider: "anthropic"},
		{ID: "gemini-3.1-pro", Label: "Gemini 3.1 Pro", Provider: "google"},
		{ID: "gpt-5.5", Label: "GPT 5.5", Provider: "openai"},
		{ID: "deepseek-v3-2-volc-ioa", Label: "Deepseek V3 2 Volc IOA", Provider: "deepseek"},
	}
}
