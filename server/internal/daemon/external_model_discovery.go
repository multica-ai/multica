package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// ── CLI-based model discovery ──────────────────────────────────────────

// DiscoveredModels holds the result of a dynamic model discovery call.
type DiscoveredModels struct {
	Models  []agent.Model
	Pricing map[string]RuntimePricing
}

// discoverExternalModels is the unified entry point called by handleModelList
// when a manifest declares `models_discovery`. It dispatches to CLI or ACP
// discovery based on the config's Method field, then reports the result.
func (d *Daemon) discoverExternalModels(ctx context.Context, rt Runtime, requestID string, entry AgentEntry) {
	disc := entry.ModelsDiscovery
	if disc == nil {
		// Should not happen — caller checks, but defensive.
		d.reportExternalModels(ctx, rt, requestID, entry.Models)
		return
	}

	var result *DiscoveredModels
	var err error

	switch disc.Method {
	case "cli":
		result, err = d.discoverExternalCLIModels(ctx, entry)
		if err != nil && isCodeBuddyEntry(entry) {
			productResult, productErr := d.discoverCodeBuddyProductModels(entry)
			if productErr == nil && productResult != nil && len(productResult.Models) > 0 {
				if d.logger != nil {
					d.logger.Info("external cli model discovery fell back to CodeBuddy product catalog",
						"provider", entry.Provider, "method", disc.Method, "error", err)
				}
				result = productResult
				err = nil
			} else if productErr != nil {
				err = fmt.Errorf("%w; codebuddy product catalog fallback: %v", err, productErr)
			}
		}
	case "acp":
		result, err = d.discoverExternalACPModels(ctx, entry)
	case "codebuddy-product":
		result, err = d.discoverCodeBuddyProductModels(entry)
	default:
		err = fmt.Errorf("unsupported models_discovery.method %q", disc.Method)
	}

	if err != nil || result == nil || len(result.Models) == 0 {
		if err != nil {
			d.logger.Warn("external model discovery failed, falling back to static",
				"provider", entry.Provider, "method", disc.Method, "error", err)
		}
		// Fallback to static models array.
		if len(entry.Models) > 0 {
			d.reportExternalModels(ctx, rt, requestID, entry.Models)
		} else {
			d.reportModelListResult(ctx, rt, requestID, map[string]any{
				"status": "completed", "models": []any{}, "supported": true,
			})
		}
		return
	}

	// Update in-memory pricing from discovery result.
	if len(result.Pricing) > 0 {
		if entry.Pricing == nil {
			entry.Pricing = make(map[string]RuntimePricing)
		}
		for k, v := range result.Pricing {
			entry.Pricing[k] = v
		}
	}

	// Convert to wire format and report.
	d.reportDiscoveredModels(ctx, rt, requestID, result)
}

// discoverExternalCLIModels shells out to the runtime CLI with the
// manifest-declared discovery args and parses the JSON output.
func (d *Daemon) discoverExternalCLIModels(ctx context.Context, entry AgentEntry) (*DiscoveredModels, error) {
	disc := entry.ModelsDiscovery
	if disc.CLI == nil || len(disc.CLI.Args) == 0 {
		return nil, fmt.Errorf("models_discovery.cli.args is empty")
	}

	// Check cache first.
	cacheKey := "ext-cli:" + entry.ManifestID
	if cached := modelDiscoveryCache.get(cacheKey); cached != nil {
		return cached, nil
	}

	timeout := time.Duration(disc.CLI.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	discCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(discCtx, entry.Path, disc.CLI.Args...)
	hideAgentWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cli model discovery %q %v: %w", entry.Path, disc.CLI.Args, err)
	}

	result, err := ParseCLIModelsJSON(out)
	if err != nil {
		return nil, fmt.Errorf("parse cli model discovery output: %w", err)
	}

	// Cache successful non-empty result.
	ttl := time.Duration(disc.CacheTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if len(result.Models) > 0 {
		modelDiscoveryCache.set(cacheKey, result, ttl)
	}

	return result, nil
}

// discoverExternalACPModels performs an ACP session/new handshake to
// extract the models list from the response. This mirrors what the
// hermes/kimi/kiro built-in backends do.
func (d *Daemon) discoverExternalACPModels(ctx context.Context, entry AgentEntry) (*DiscoveredModels, error) {
	disc := entry.ModelsDiscovery

	cacheKey := "ext-acp:" + entry.ManifestID
	if cached := modelDiscoveryCache.get(cacheKey); cached != nil {
		return cached, nil
	}

	timeout := 15 * time.Second
	if disc.CacheTTLSeconds > 0 {
		// Use cache TTL as a rough proxy for acceptable wait time.
		timeout = time.Duration(disc.CacheTTLSeconds) * time.Second
		if timeout > 30*time.Second {
			timeout = 30 * time.Second
		}
	}

	discCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := DiscoverACPModelsExternal(discCtx, entry.Path, entry.ACPArgs)
	if err != nil {
		return nil, err
	}

	ttl := time.Duration(disc.CacheTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if len(result.Models) > 0 {
		modelDiscoveryCache.set(cacheKey, result, ttl)
	}

	return result, nil
}

// reportDiscoveredModels converts DiscoveredModels to the standard wire
// format and reports them (same shape as reportExternalModels but with
// richer thinking/pricing data from CLI JSON output).
func (d *Daemon) reportDiscoveredModels(ctx context.Context, rt Runtime, requestID string, result *DiscoveredModels) {
	type thinkingLevelWire struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	}
	type modelThinkingWire struct {
		SupportedLevels []thinkingLevelWire `json:"supported_levels"`
		DefaultLevel    string              `json:"default_level,omitempty"`
	}
	type modelWire struct {
		ID       string             `json:"id"`
		Label    string             `json:"label"`
		Provider string             `json:"provider,omitempty"`
		Default  bool               `json:"default,omitempty"`
		Thinking *modelThinkingWire `json:"thinking,omitempty"`
	}

	wire := make([]modelWire, 0, len(result.Models))
	for _, m := range result.Models {
		entry := modelWire{
			ID:       m.ID,
			Label:    m.Label,
			Provider: m.Provider,
			Default:  m.Default,
		}
		if m.Thinking != nil {
			levels := make([]thinkingLevelWire, 0, len(m.Thinking.SupportedLevels))
			for _, lvl := range m.Thinking.SupportedLevels {
				levels = append(levels, thinkingLevelWire{
					Value:       lvl.Value,
					Label:       lvl.Label,
					Description: lvl.Description,
				})
			}
			entry.Thinking = &modelThinkingWire{
				SupportedLevels: levels,
				DefaultLevel:    m.Thinking.DefaultLevel,
			}
		}
		wire = append(wire, entry)
	}

	payload := map[string]any{
		"status":    "completed",
		"models":    wire,
		"supported": true,
	}
	if len(result.Pricing) > 0 {
		payload["pricing"] = result.Pricing
	}
	d.reportModelListResult(ctx, rt, requestID, payload)
}

// ── JSON parsing for CLI model discovery ───────────────────────────────

// CLIModelsOutput is the expected JSON structure from a CLI model
// discovery command (e.g. `<cli> --list-models --format json`).
type CLIModelsOutput struct {
	Models []CLIModelEntry `json:"models"`
}

type CLIModelEntry struct {
	ID       string            `json:"id"`
	Label    string            `json:"label,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Default  bool              `json:"default,omitempty"`
	Thinking *CLIModelThinking `json:"thinking,omitempty"`
	Pricing  *RuntimePricing   `json:"pricing,omitempty"`
}

type CLIModelThinking struct {
	SupportedLevels []CLIThinkingLevel `json:"supported_levels,omitempty"`
	DefaultLevel    string             `json:"default_level,omitempty"`
}

type CLIThinkingLevel struct {
	Value       string `json:"value"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// ParseCLIModelsJSON parses the JSON output from a CLI model discovery
// command and returns structured models + pricing. Exported for testing.
func ParseCLIModelsJSON(data []byte) (*DiscoveredModels, error) {
	var output CLIModelsOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("unmarshal cli models json: %w", err)
	}
	if len(output.Models) == 0 {
		return &DiscoveredModels{}, nil
	}

	models := make([]agent.Model, 0, len(output.Models))
	pricing := make(map[string]RuntimePricing)

	for _, m := range output.Models {
		am := agent.Model{
			ID:       m.ID,
			Label:    m.Label,
			Provider: m.Provider,
			Default:  m.Default,
		}
		if am.Label == "" {
			am.Label = am.ID
		}
		if m.Thinking != nil {
			levels := make([]agent.ThinkingLevel, 0, len(m.Thinking.SupportedLevels))
			for _, lvl := range m.Thinking.SupportedLevels {
				label := lvl.Label
				if label == "" {
					label = lvl.Value
				}
				levels = append(levels, agent.ThinkingLevel{
					Value:       lvl.Value,
					Label:       label,
					Description: lvl.Description,
				})
			}
			am.Thinking = &agent.ModelThinking{
				SupportedLevels: levels,
				DefaultLevel:    m.Thinking.DefaultLevel,
			}
		}
		models = append(models, am)
		if m.Pricing != nil {
			pricing[m.ID] = *m.Pricing
		}
	}

	return &DiscoveredModels{Models: models, Pricing: pricing}, nil
}

// ── ACP-based model discovery for external runtimes ────────────────────

// DiscoverACPModelsExternal performs a lightweight ACP handshake
// (initialize → session/new) to extract the available models from the
// session/new response. It mirrors the built-in hermes/kimi discovery
// path but is generic enough for any external ACP runtime.
func DiscoverACPModelsExternal(ctx context.Context, execPath string, acpArgs []string) (*DiscoveredModels, error) {
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("acp discovery: %w", err)
	}

	cmd := exec.CommandContext(ctx, execPath, acpArgs...)
	hideAgentWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil // discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start acp discovery: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	writeJSON := func(payload any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		_, err = stdin.Write(data)
		return err
	}

	decoder := json.NewDecoder(stdout)
	readResponse := func() (map[string]any, error) {
		for {
			var frame map[string]any
			if err := decoder.Decode(&frame); err != nil {
				return nil, err
			}
			if _, hasID := frame["id"]; hasID {
				if errField, ok := frame["error"]; ok {
					return nil, fmt.Errorf("acp rpc error: %v", errField)
				}
				return frame, nil
			}
		}
	}

	// initialize
	if err := writeJSON(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "1.0",
			"clientInfo":      map[string]string{"name": "multica-model-discovery", "version": "0.1.0"},
			"capabilities":    map[string]any{},
		},
	}); err != nil {
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	if _, err := readResponse(); err != nil {
		return nil, fmt.Errorf("acp initialize response: %w", err)
	}

	// session/new
	if err := writeJSON(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "session/new",
		"params": map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("acp session/new: %w", err)
	}
	resp, err := readResponse()
	if err != nil {
		return nil, fmt.Errorf("acp session/new response: %w", err)
	}

	// Extract models from response.result.models.availableModels
	return parseACPSessionNewModelsExternal(resp)
}

// parseACPSessionNewModelsExternal extracts models + pricing from the
// ACP session/new response. The expected shape is:
//
//	{ "result": { "models": { "availableModels": [...] } } }
//
// Each availableModel may have an optional "pricing" object.
func parseACPSessionNewModelsExternal(resp map[string]any) (*DiscoveredModels, error) {
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		return &DiscoveredModels{}, nil
	}
	modelsBlock, _ := result["models"].(map[string]any)
	if modelsBlock == nil {
		return &DiscoveredModels{}, nil
	}
	available, _ := modelsBlock["availableModels"].([]any)
	if len(available) == 0 {
		return &DiscoveredModels{}, nil
	}

	models := make([]agent.Model, 0, len(available))
	pricing := make(map[string]RuntimePricing)

	for _, raw := range available {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["modelId"].(string)
		if id == "" {
			id, _ = m["id"].(string)
		}
		if id == "" {
			continue
		}
		label, _ := m["name"].(string)
		if label == "" {
			label, _ = m["label"].(string)
		}
		if label == "" {
			label = id
		}
		isDefault, _ := m["default"].(bool)

		am := agent.Model{ID: id, Label: label, Default: isDefault}

		// Parse thinking levels if present.
		if thinkingRaw, ok := m["thinking"].(map[string]any); ok {
			if levels, ok := thinkingRaw["supported_levels"].([]any); ok && len(levels) > 0 {
				tl := make([]agent.ThinkingLevel, 0, len(levels))
				for _, lRaw := range levels {
					lm, _ := lRaw.(map[string]any)
					if lm == nil {
						continue
					}
					val, _ := lm["value"].(string)
					lab, _ := lm["label"].(string)
					if lab == "" {
						lab = val
					}
					desc, _ := lm["description"].(string)
					tl = append(tl, agent.ThinkingLevel{Value: val, Label: lab, Description: desc})
				}
				defLvl, _ := thinkingRaw["default_level"].(string)
				am.Thinking = &agent.ModelThinking{SupportedLevels: tl, DefaultLevel: defLvl}
			}
		}

		models = append(models, am)

		// Parse optional pricing.
		if pRaw, ok := m["pricing"].(map[string]any); ok {
			p := RuntimePricing{}
			if v, ok := pRaw["input"].(float64); ok {
				p.Input = v
			}
			if v, ok := pRaw["output"].(float64); ok {
				p.Output = v
			}
			if v, ok := pRaw["cacheRead"].(float64); ok {
				p.CacheRead = v
			}
			if v, ok := pRaw["cacheWrite"].(float64); ok {
				p.CacheWrite = v
			}
			if p.Input > 0 || p.Output > 0 {
				pricing[id] = p
			}
		}
	}

	return &DiscoveredModels{Models: models, Pricing: pricing}, nil
}

// ── Model discovery cache ──────────────────────────────────────────────

type modelCacheEntry struct {
	result  *DiscoveredModels
	expires time.Time
}

type modelCache struct {
	mu    sync.RWMutex
	store map[string]*modelCacheEntry
}

var modelDiscoveryCache = &modelCache{store: make(map[string]*modelCacheEntry)}

func (c *modelCache) get(key string) *DiscoveredModels {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.store[key]
	if !ok || time.Now().After(e.expires) {
		return nil
	}
	return e.result
}

func (c *modelCache) set(key string, result *DiscoveredModels, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = &modelCacheEntry{result: result, expires: time.Now().Add(ttl)}
}

// hideAgentWindow is defined in the agent package; we need a local
// forward declaration since it's package-internal there. On Windows
// it sets the CREATE_NO_WINDOW flag; on other platforms it's a no-op.
// This is already imported via the proc_*.go files in the agent package;
// for the daemon package we use exec.Cmd directly without the flag
// since discovery processes are short-lived and don't need window hiding.
// The function is referenced here for documentation; actual daemon
// subprocess management doesn't need it.
func hideAgentWindow(_ *exec.Cmd) {
	// Placeholder — the daemon's discovery subprocess doesn't need
	// window hiding. The real hideAgentWindow lives in pkg/agent.
}
