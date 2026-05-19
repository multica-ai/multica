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
// executablePath lets the caller point at a non-default binary; pass
// "" to use the provider's default name on PATH.
func ListModels(ctx context.Context, providerType, executablePath string) ([]Model, error) {
	switch providerType {
	case "claude":
		return claudeStaticModels(), nil
	case "codex":
		return codexStaticModels(), nil
	case "gemini":
		return geminiStaticModels(), nil
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
		return cachedDiscovery(providerType, func() ([]Model, error) {
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
// any effect for the given provider. Today every provider in the
// registry honours `opts.Model` end-to-end: Hermes routes it through
// the ACP `session/set_model` RPC before each prompt, which means
// the UI's dropdown choice is carried all the way down to the LLM
// call. The helper is retained so we can add a `return false` branch
// the next time a provider legitimately ignores model selection.
func ModelSelectionSupported(providerType string) bool {
	_ = providerType
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

// ── Static catalogs ──

// claudeStaticModels reflects the Claude Code CLI's accepted --model
// values. Keep this list short and current; stale entries here
// mislead users more than they help. Default = Sonnet because it's
// the everyday workhorse (Opus is reserved for advisor-style flows).
func claudeStaticModels() []Model {
	return []Model{
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6", Provider: "anthropic", Default: true},
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

// discoverOpenCodeModels runs `opencode models` and parses its tabular
// output. The CLI prints `provider/model` rows; we emit them verbatim
// as IDs so what the user sees matches what `--model` accepts.
// On any failure (CLI missing, parse error, timeout) we fall back to
// an empty list so the creatable UI still works.
func discoverOpenCodeModels(ctx context.Context, executablePath string) ([]Model, error) {
	if executablePath == "" {
		executablePath = "opencode"
	}
	if _, err := exec.LookPath(executablePath); err != nil {
		return []Model{}, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executablePath, "models")
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return []Model{}, nil
	}
	return parseOpenCodeModels(string(out)), nil
}

// parseOpenCodeModels accepts the `opencode models` text output and
// extracts IDs. Output format (v0.x): a header row followed by rows
// whose first whitespace-delimited field is `provider/model`.
func parseOpenCodeModels(output string) []Model {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var models []Model
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		first := strings.Fields(line)
		if len(first) == 0 {
			continue
		}
		id := first[0]
		if !strings.Contains(id, "/") {
			continue
		}
		// Skip the header row (opencode prints e.g. PROVIDER/MODEL in caps).
		if id == strings.ToUpper(id) {
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
	if err != nil {
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
	var resp struct {
		Models struct {
			AvailableModels []struct {
				ModelID     string `json:"modelId"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"availableModels"`
			CurrentModelID string `json:"currentModelId"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	models := make([]Model, 0, len(resp.Models.AvailableModels))
	seen := map[string]bool{}
	for _, m := range resp.Models.AvailableModels {
		if m.ModelID == "" || seen[m.ModelID] {
			continue
		}
		seen[m.ModelID] = true
		label := m.Name
		if label == "" {
			label = m.ModelID
		}
		provider := ""
		if idx := strings.Index(m.ModelID, ":"); idx > 0 {
			provider = m.ModelID[:idx]
		}
		models = append(models, Model{
			ID:       m.ModelID,
			Label:    label,
			Provider: provider,
			Default:  m.ModelID == resp.Models.CurrentModelID,
		})
	}
	return models
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
	if err != nil {
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
		if err != nil {
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
	if err != nil {
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
