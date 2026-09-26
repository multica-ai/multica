package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const cursorCLIConfigFile = "cli-config.json"

// splitCursorContextModel splits a Cursor model carrying a context-window tag
// (`grok-4.7[500k]`, the same `[<n>k|m]` modifier Claude uses for
// `claude-opus-5[1m]`) into Cursor's model id and context value. Cursor has no
// CLI flag or model id for its long windows: they are only selectable through
// cli-config.json (`selectedModel.parameters` / `modelParameters[<id>]` context
// entry plus `maxMode`), so a tagged model is routed through a per-run config.
func splitCursorContextModel(model string) (id, contextValue string, ok bool) {
	loc := claudeContextWindowTagRe.FindStringIndex(model)
	if loc == nil || loc[0] == 0 {
		return "", "", false
	}
	return model[:loc[0]], model[loc[0]+1 : loc[1]-1], true
}

// cursorSourceConfigDir returns the directory holding the user's
// cli-config.json, following Cursor's documented lookup: CURSOR_CONFIG_DIR,
// then $XDG_CONFIG_HOME/cursor on Linux/BSD, then ~/.cursor.
func cursorSourceConfigDir(env map[string]string) (string, error) {
	getenv := func(key string) string {
		if v, ok := env[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
	var candidates []string
	if dir := getenv("CURSOR_CONFIG_DIR"); dir != "" {
		candidates = append(candidates, dir)
	}
	if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		candidates = append(candidates, filepath.Join(xdg, "cursor"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".cursor"))
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, cursorCLIConfigFile)); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no Cursor %s found (checked %s); run cursor-agent once to create it", cursorCLIConfigFile, strings.Join(candidates, ", "))
}

// prepareCursorContextConfigDir builds a per-run CURSOR_CONFIG_DIR under
// parent: every entry of the user's config dir is symlinked (shared state such
// as mcp.json, rules and skills stays live), except cli-config.json, which is
// copied fresh and rewritten to select modelID at contextValue with MAX mode.
// Cursor rewrites cli-config.json during a run (caches), so the copy is
// discarded afterwards and the user's own file is never modified. The caller
// removes the returned directory when the run ends.
func prepareCursorContextConfigDir(parent, sourceDir, modelID, contextValue string) (string, error) {
	data, err := os.ReadFile(filepath.Join(sourceDir, cursorCLIConfigFile))
	if err != nil {
		return "", fmt.Errorf("read cursor %s: %w", cursorCLIConfigFile, err)
	}
	rewritten, err := rewriteCursorCLIConfig(data, modelID, contextValue)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return "", fmt.Errorf("read cursor config dir: %w", err)
	}
	dir, err := os.MkdirTemp(parent, "multica-cursor-config-")
	if err != nil {
		return "", fmt.Errorf("create cursor config dir: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == cursorCLIConfigFile {
			continue
		}
		if err := os.Symlink(filepath.Join(sourceDir, entry.Name()), filepath.Join(dir, entry.Name())); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("link cursor config entry %s: %w", entry.Name(), err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, cursorCLIConfigFile), rewritten, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write cursor %s: %w", cursorCLIConfigFile, err)
	}
	return dir, nil
}

// rewriteCursorCLIConfig returns cli-config.json with modelID selected at the
// given context window and MAX mode on. Every other key (auth, caches,
// permissions, other models' parameters) is preserved verbatim.
func rewriteCursorCLIConfig(data []byte, modelID, contextValue string) ([]byte, error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse cursor %s: %w", cursorCLIConfigFile, err)
	}
	if cfg == nil {
		return nil, errors.New("cursor " + cursorCLIConfigFile + " is not a JSON object")
	}

	var modelParams map[string][]map[string]any
	if raw, ok := cfg["modelParameters"]; ok {
		if err := json.Unmarshal(raw, &modelParams); err != nil {
			return nil, fmt.Errorf("parse cursor modelParameters: %w", err)
		}
	}
	if modelParams == nil {
		modelParams = map[string][]map[string]any{}
	}
	// Keep the user's other parameters for this model (effort, fast, ...) and
	// replace only the context entry.
	params := []map[string]any{{"id": "context", "value": contextValue}}
	for _, p := range modelParams[modelID] {
		if p["id"] != "context" {
			params = append(params, p)
		}
	}
	modelParams[modelID] = params

	var model map[string]any
	if raw, ok := cfg["model"]; ok {
		_ = json.Unmarshal(raw, &model)
	}
	if model == nil || model["modelId"] != modelID {
		model = map[string]any{"modelId": modelID, "displayModelId": modelID, "displayName": modelID, "displayNameShort": modelID, "aliases": []string{}}
	}
	model["maxMode"] = true

	updates := map[string]any{
		"modelParameters":        modelParams,
		"selectedModel":          map[string]any{"modelId": modelID, "parameters": params},
		"model":                  model,
		"maxMode":                true,
		"hasChangedDefaultModel": true,
	}
	for key, value := range updates {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode cursor %s: %w", key, err)
		}
		cfg[key] = raw
	}
	return json.MarshalIndent(cfg, "", "  ")
}
