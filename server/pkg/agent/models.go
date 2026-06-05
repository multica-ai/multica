package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
	Default  bool   `json:"default,omitempty"`
	// Thinking advertises the runtime's reasoning/effort catalog for this
	// model. nil means the runtime/model has no thinking-level control
	// (or the daemon couldn't discover one); the UI hides its picker. The
	// catalog is per-model because Codex's `codex debug models` is itself
	// per-model and Claude's `--effort` superset has known per-model gaps
	// (`xhigh` is Opus-only, `max` is session-only). See MUL-2339.
	Thinking *ModelThinking `json:"thinking,omitempty"`
}

// ModelThinking carries the per-model reasoning/effort catalog
// surfaced by an agent runtime. Values are runtime-native — Codex
// emits "none|minimal|low|medium|high|xhigh"; Claude emits
// "low|medium|high|xhigh|max". The frontend renders SupportedLevels
// as-is so what users see matches each CLI's own UI.
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
// list; for providers with a CLI discovery mechanism (opencode, pi,
// openclaw) it shells out with caching and falls back to the static
// list on failure.
//
// For claude, codex, and opencode, the catalog is augmented with per-model
// thinking-level options discovered from the local CLI. Discovery failures
// silently leave Thinking == nil on each entry, which the UI treats as
// "no picker for this model" rather than blocking model selection.
//
// executablePath lets the caller point at a non-default binary; pass
// "" to use the provider's default name on PATH.
func ListModels(ctx context.Context, providerType, executablePath string) ([]Model, error) {
	switch providerType {
	case "claude":
		models := claudeStaticModels()
		annotateClaudeThinking(ctx, models, executablePath)
		return models, nil
	case "codex":
		models := codexStaticModels()
		annotateCodexThinking(ctx, models, executablePath)
		return models, nil
	case "gemini":
		return geminiStaticModels(), nil
	case "antigravity":
		// Antigravity CLI (`agy`) does not expose a `--model` flag today;
		// model selection lives in the user's Antigravity settings and is
		// communicated to the backend internally by the CLI itself. Return
		// an empty catalog so the daemon's model_list endpoint succeeds
		// without populating a misleading dropdown.
		return []Model{}, nil
	case "cursor":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverCursorModels(ctx, executablePath)
		})
	case "copilot":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverCopilotModels(ctx, executablePath)
		})
	case "hermes":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverHermesModels(ctx, executablePath)
		})
	case "kimi":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverKimiModels(ctx, executablePath)
		})
	case "kiro":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverKiroModels(ctx, executablePath)
		})
	case "opencode":
		return cachedDiscovery(discoveryCacheKey(providerType, executablePath), func() ([]Model, error) {
			return discoverOpenCodeModels(ctx, executablePath)
		})
	case "pi":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverPiModels(ctx, executablePath)
		})
	case "openclaw":
		return cachedDiscovery(providerType, func() ([]Model, error) {
			return discoverOpenclawAgents(ctx, executablePath)
		})
	default:
		return nil, fmt.Errorf("unknown agent type: %q", providerType)
	}
}

// ModelSelectionSupported reports whether setting `agent.model` has
// any effect for the given provider. Most providers honour `opts.Model`
// end-to-end — Hermes routes it through the ACP `session/set_model` RPC
// before each prompt, Claude / Codex / Cursor / Gemini / Copilot / Kimi /
// Kiro / OpenCode / OpenClaw / Pi pass it via flag or session config.
//
// Antigravity is the lone exception: `agy` has no `--model` flag today,
// and the backend in antigravity.go deliberately drops opts.Model on the
// floor. Returning false here makes the UI render a disabled
// "Managed by runtime" picker instead of an empty dropdown plus a
// silently-ignored manual-entry field.
func ModelSelectionSupported(providerType string) bool {
	if providerType == "antigravity" {
		return false
	}
	return true
}

// cachedDiscovery invokes fn and caches the result for modelCacheTTL.
// The cache is keyed on providerType only; callers that need to
// distinguish discovery by host/user should include that in the key
// if we ever introduce such a mode.
func cachedDiscovery(key string, fn func() ([]Model, error)) ([]Model, error) {
	modelCacheMu.Lock()
	if entry, ok := modelCache[key]; ok && time.Now().Before(entry.expiresAt) {
		out := entry.models
		modelCacheMu.Unlock()
		return out, nil
	}
	modelCacheMu.Unlock()

	models, err := fn()
	if err != nil {
		return nil, err
	}

	modelCacheMu.Lock()
	modelCache[key] = modelCacheEntry{models: models, expiresAt: time.Now().Add(modelCacheTTL)}
	modelCacheMu.Unlock()
	return models, nil
}

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
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6", Provider: "anthropic", Default: true},
		{ID: "claude-opus-4-8", Label: "Claude Opus 4.8", Provider: "anthropic"},
		{ID: "claude-opus-4-7", Label: "Claude Opus 4.7", Provider: "anthropic"},
		{ID: "claude-haiku-4-5-20251001", Label: "Claude Haiku 4.5", Provider: "anthropic"},
		{ID: "claude-opus-4-6", Label: "Claude Opus 4.6", Provider: "anthropic"},
		{ID: "claude-sonnet-4-5", Label: "Claude Sonnet 4.5", Provider: "anthropic"},
	}
}

func codexStaticModels() []Model {
	return []Model{
		{ID: "gpt-5.5", Label: "GPT-5.5", Provider: "openai", Default: true},
		{ID: "gpt-5.5-mini", Label: "GPT-5.5 mini", Provider: "openai"},
		{ID: "gpt-5.4", Label: "GPT-5.4", Provider: "openai"},
		{ID: "gpt-5.4-mini", Label: "GPT-5.4 mini", Provider: "openai"},
		{ID: "gpt-5.3-codex", Label: "GPT-5.3 Codex", Provider: "openai"},
		{ID: "gpt-5", Label: "GPT-5", Provider: "openai"},
		{ID: "o3", Label: "o3", Provider: "openai"},
		{ID: "o3-mini", Label: "o3-mini", Provider: "openai"},
	}
}

// geminiStaticModels lists the values we pass via `gemini -m`. Gemini
// CLI has no `models list` subcommand, so dynamic discovery isn't
// possible; the next best thing is to expose the CLI's own aliases
// (auto / pro / flash / flash-lite and the `auto-gemini-*` family)
// alongside a few explicit version pins. Aliases track whatever the
// installed CLI considers current (see `resolveModel` in the CLI's
// packages/core/src/config/models.ts), so new Gemini releases light
// up without a Multica redeploy. Default is `auto` to match Google's
// recommendation — the CLI picks Pro vs Flash per task and falls back
// when quota is exhausted.
func geminiStaticModels() []Model {
	return []Model{
		{ID: "auto", Label: "Auto (Gemini 3)", Provider: "google", Default: true},
		{ID: "auto-gemini-2.5", Label: "Auto (Gemini 2.5)", Provider: "google"},
		{ID: "pro", Label: "Pro", Provider: "google"},
		{ID: "flash", Label: "Flash", Provider: "google"},
		{ID: "flash-lite", Label: "Flash Lite", Provider: "google"},
		{ID: "gemini-3-pro-preview", Label: "Gemini 3 Pro (preview)", Provider: "google"},
		{ID: "gemini-3-flash-preview", Label: "Gemini 3 Flash (preview)", Provider: "google"},
		{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro", Provider: "google"},
		{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash", Provider: "google"},
		{ID: "gemini-2.5-flash-lite", Label: "Gemini 2.5 Flash Lite", Provider: "google"},
	}
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

// discoverPiModels runs `pi --list-models` and parses its output.
// Older pi versions print the list to stderr; newer versions use
// stdout. We capture both and parse whichever is non-empty.
func discoverPiModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "pi"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
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
func isPiDiscoveryNoise(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "no models match pattern") {
		return true
	}
	return strings.HasPrefix(lower, "warning:") ||
		strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "info:")
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

// discoverKimiModels spins up a throwaway `kimi acp` process and
// drives the same minimal ACP handshake as Hermes to surface the
// model catalog advertised by Kimi's `session/new` response. Kimi's
// ACPServer.new_session returns a `models` block of the same shape
// (`availableModels`/`currentModelId`) so the parsing path is shared.
//
// Failure modes (kimi missing, not logged in, config error) all
// return an empty list so the UI falls back to manual entry.
func discoverKimiModels(ctx context.Context, executablePath string) ([]Model, error) {
	return discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "kimi",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-kimi-discovery-",
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
func discoverCopilotModels(ctx context.Context, executablePath string) ([]Model, error) {
	models, err := discoverACPModels(ctx, executablePath, acpDiscoveryProvider{
		defaultBin:   "copilot",
		clientName:   "multica-model-discovery",
		tmpdirPrefix: "multica-copilot-discovery-",
		acpArgs:      []string{"--acp"},
	})
	if err != nil || len(models) == 0 {
		return copilotStaticModels(), nil
	}
	for i := range models {
		if models[i].Provider == "" {
			models[i].Provider = inferCopilotProvider(models[i].ID)
		}
	}
	return models, nil
}

// acpDiscoveryProvider configures how discoverACPModels launches an
// ACP-speaking agent CLI. The shared helper drives every CLI in
// the same way (initialize → session/new → parse models block) — the
// per-provider differences are which binary to spawn, which env
// vars suppress interactive prompts during init, what argv puts
// the binary into ACP server mode (most use `acp`, Copilot uses
// `--acp`), and what to label temporary work directories so they're
// easy to identify in logs.
type acpDiscoveryProvider struct {
	defaultBin   string
	clientName   string
	extraEnv     []string
	tmpdirPrefix string
	// acpArgs is the argv passed to the binary to start it in ACP
	// server mode. Defaults to []string{"acp"} when nil/empty.
	acpArgs []string
}

// discoverACPModels runs the ACP handshake for any agent CLI that
// implements the standard `initialize` + `session/new` flow and
// advertises its model catalog in the response under
// `models.availableModels` / `models.currentModelId`. This covers
// Hermes and Kimi today; future ACP backends can plug in by adding
// an acpDiscoveryProvider entry instead of duplicating the loop.
func discoverACPModels(ctx context.Context, executablePath string, p acpDiscoveryProvider) ([]Model, error) {
	if executablePath == "" {
		executablePath = p.defaultBin
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmdArgs := p.acpArgs
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"acp"}
	}
	cmd := exec.CommandContext(runCtx, executablePath, cmdArgs...)
	hideAgentWindow(cmd)
	if len(p.extraEnv) > 0 {
		cmd.Env = append(os.Environ(), p.extraEnv...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return []Model{}, nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return []Model{}, nil
	}
	// Discard stderr; noisy logs here don't help us and we don't
	// want them bleeding into the daemon log every 60s.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return []Model{}, nil
	}
	// Ensure the child process is always reaped.
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	writeACP := func(id int, method string, params map[string]any) error {
		msg := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
			"params":  params,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		_, err = stdin.Write(data)
		return err
	}

	// Send initialize + session/new.
	if err := writeACP(1, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]any{"name": p.clientName, "version": "0.1.0"},
		"clientCapabilities": map[string]any{},
	}); err != nil {
		return []Model{}, nil
	}

	// session/new requires a valid cwd — use a temp directory we
	// clean up afterwards, not the daemon's workdir (which might
	// be in the middle of another task's worktree).
	tmp, err := os.MkdirTemp("", p.tmpdirPrefix)
	if err != nil {
		return []Model{}, nil
	}
	defer os.RemoveAll(tmp)

	if err := writeACP(2, "session/new", map[string]any{
		"cwd":        tmp,
		"mcpServers": []any{},
	}); err != nil {
		return []Model{}, nil
	}

	// Read responses until we see the one for id=2 (session/new).
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	deadline := time.After(12 * time.Second)
	done := make(chan []Model, 1)
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var env struct {
				ID     json.Number     `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				continue
			}
			if env.ID.String() != "2" || len(env.Result) == 0 {
				continue
			}
			done <- parseACPSessionNewModels(env.Result)
			return
		}
	}()

	select {
	case models := <-done:
		if models == nil {
			return []Model{}, nil
		}
		return models, nil
	case <-deadline:
		return []Model{}, nil
	case <-runCtx.Done():
		return []Model{}, nil
	}
}

// parseACPSessionNewModels extracts the model catalog from an ACP
// `session/new` response. Both Hermes and Kimi (and any other ACP
// agent that follows the standard schema) emit:
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
		label := acpModelLabel(m.Name, modelID)
		provider := ""
		if idx := strings.Index(modelID, ":"); idx > 0 {
			provider = modelID[:idx]
		}
		models = append(models, Model{
			ID:       modelID,
			Label:    label,
			Provider: provider,
			Default:  modelID == currentModelID,
		})
	}
	return models
}

func acpModelLabel(name, modelID string) string {
	label := strings.TrimSpace(name)
	if label == "" || strings.EqualFold(label, "unknown") {
		return modelID
	}
	return label
}

// discoverCursorModels runs `cursor-agent --list-models` and parses
// the `id - Label` rows. Cursor's catalog changes often and ships
// many variants of the same base model (thinking / fast / max
// suffixes) — static baking would be obsolete within weeks. On any
// failure we fall back to the minimal static catalog so the UI
// stays usable when cursor-agent isn't installed on the daemon host.
func discoverCursorModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "cursor-agent"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return cursorStaticModels(), nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "--list-models")
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return cursorStaticModels(), nil
	}
	models := parseCursorModels(string(out))
	if len(models) == 0 {
		return cursorStaticModels(), nil
	}
	return models, nil
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
