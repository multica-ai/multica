package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dettoolsServerName is the MCP server key the daemon injects. A user-defined
// server with the same name is left untouched (the merge stays additive).
const dettoolsServerName = "multica-tools"

// dettoolsExecOptionsProviders lists the providers that receive the
// deterministic tool server through ExecOptions.McpConfig (the --mcp-config /
// config.toml injection path). Phase 1 ships claude only; codex follows in
// Phase 2 (its backend already reads ExecOptions.McpConfig, so adding it here is
// a one-line change once validated).
var dettoolsExecOptionsProviders = map[string]bool{
	"claude": true,
}

// injectDeterministicTools returns agentCfg with the daemon-managed deterministic
// tool MCP server merged in, when the tool plane is enabled and the provider
// reaches it via ExecOptions. The merge is additive and fail-open: on any error
// it logs and returns the original config so a tool-plane problem never blocks a
// task launch.
func (d *Daemon) injectDeterministicTools(agentCfg json.RawMessage, provider, workDir string, logger *slog.Logger) json.RawMessage {
	if !d.cfg.DetTools.Enabled || !dettoolsExecOptionsProviders[provider] {
		return agentCfg
	}
	selfBin, err := os.Executable()
	if err != nil {
		logger.Warn("dettools: cannot resolve daemon binary; skipping tool plane injection", "error", err)
		return agentCfg
	}
	if resolved, rerr := filepath.EvalSymlinks(selfBin); rerr == nil {
		selfBin = resolved
	}
	merged, err := buildEffectiveMcpConfig(agentCfg, selfBin, workDir, d.cfg.DetTools)
	if err != nil {
		logger.Warn("dettools: merge failed; launching without tool plane", "error", err)
		return agentCfg
	}
	logger.Info("dettools: injected deterministic tool server",
		"provider", provider,
		"tools", d.cfg.DetTools.AllowedTools,
	)
	return merged
}

// buildEffectiveMcpConfig merges the deterministic tool server into a Claude-style
// mcp_config (`{"mcpServers": {...}}`). It preserves all existing top-level keys
// and user-defined servers; if a server already named dettoolsServerName exists,
// the original config is returned unchanged so user intent always wins.
func buildEffectiveMcpConfig(agentCfg json.RawMessage, selfBin, workDir string, cfg DetToolsConfig) (json.RawMessage, error) {
	root := map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(agentCfg))) > 0 {
		if err := json.Unmarshal(agentCfg, &root); err != nil {
			return nil, fmt.Errorf("parse agent mcp_config: %w", err)
		}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, fmt.Errorf("parse mcpServers: %w", err)
		}
	}
	if _, exists := servers[dettoolsServerName]; exists {
		return agentCfg, nil
	}

	entry := map[string]any{
		"command": selfBin,
		"args":    []string{"mcp-tools", "serve"},
		"env":     dettoolsServerEnv(workDir, cfg),
	}
	entryRaw, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	servers[dettoolsServerName] = entryRaw

	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	root["mcpServers"] = serversRaw
	return json.Marshal(root)
}

// dettoolsServerEnv builds the MULTICA_DETTOOLS_* environment the agent CLI
// passes to the spawned MCP server process. WorkDir is pinned explicitly rather
// than relying on the CLI to inherit cwd into the child.
func dettoolsServerEnv(workDir string, cfg DetToolsConfig) map[string]string {
	env := map[string]string{
		"MULTICA_DETTOOLS_WORKDIR":       workDir,
		"MULTICA_DETTOOLS_TIMEOUT":       cfg.Timeout.String(),
		"MULTICA_DETTOOLS_ALLOW_NETWORK": strconv.FormatBool(cfg.AllowNetwork),
		"MULTICA_DETTOOLS_ARTIFACT_DIR":  cfg.ArtifactDir,
	}
	if len(cfg.AllowedTools) > 0 {
		env["MULTICA_DETTOOLS_ALLOWED"] = strings.Join(cfg.AllowedTools, ",")
	}
	return env
}
