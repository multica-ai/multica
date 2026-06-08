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
// deterministic tool server through ExecOptions.McpConfig. They all consume the
// same Claude-style {"mcpServers":{name:{command,args,env}}} shape and support a
// stdio command server:
//   - claude:        temp file via --mcp-config
//   - codex:         daemon-managed [mcp_servers.*] block in config.toml
//   - opencode:      translated into OPENCODE_CONFIG_CONTENT (type:"local")
//   - hermes/kimi/kiro: translated into the ACP session mcpServers array
//     (stdio entries always pass the runtime's transport-capability filter)
var dettoolsExecOptionsProviders = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
	"hermes":   true,
	"kimi":     true,
	"kiro":     true,
}

// agentDetToolsProfile is the per-agent deterministic-tool policy, read from the
// agent's runtime_config under the "deterministic_tools" key. An agent may only
// narrow the daemon's allowlist (role-based access), never widen it.
type agentDetToolsProfile struct {
	AllowedTools []string `json:"allowed_tools"`
	DeniedTools  []string `json:"denied_tools"`
}

// injectExecOptionsTools merges the deterministic tool server into agentCfg for
// providers that consume MCP via ExecOptions.McpConfig. workDir is the known
// task working directory, pinned into the server env.
func (d *Daemon) injectExecOptionsTools(agentCfg json.RawMessage, provider, workDir string, runtimeConfig json.RawMessage, logger *slog.Logger) json.RawMessage {
	if !d.cfg.DetTools.Enabled || !dettoolsExecOptionsProviders[provider] {
		return agentCfg
	}
	return d.mergeDetTools(agentCfg, provider, workDir, runtimeConfig, logger)
}

// injectExecenvTools merges the deterministic tool server into agentCfg for
// OpenClaw, whose backend materializes mcp.servers from McpConfig during
// execenv.Prepare (before the work dir exists). The work dir is therefore left
// empty: the tool server falls back to the cwd OpenClaw spawns it with, which is
// the pinned task workspace.
func (d *Daemon) injectExecenvTools(agentCfg json.RawMessage, provider string, runtimeConfig json.RawMessage, logger *slog.Logger) json.RawMessage {
	if !d.cfg.DetTools.Enabled || provider != "openclaw" {
		return agentCfg
	}
	return d.mergeDetTools(agentCfg, provider, "", runtimeConfig, logger)
}

// mergeDetTools computes the effective tool allowlist for the agent and merges
// the deterministic server into agentCfg. Fail-open: any error logs and returns
// the original config so a tool-plane problem never blocks a task launch.
func (d *Daemon) mergeDetTools(agentCfg json.RawMessage, provider, workDir string, runtimeConfig json.RawMessage, logger *slog.Logger) json.RawMessage {
	effective := computeEffectiveAllowed(d.cfg.DetTools, runtimeConfig)
	if len(effective) == 0 {
		logger.Info("dettools: no tools enabled for this agent after policy; skipping injection", "provider", provider)
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
	merged, err := buildEffectiveMcpConfig(agentCfg, selfBin, workDir, d.cfg.DetTools, effective)
	if err != nil {
		logger.Warn("dettools: merge failed; launching without tool plane", "error", err)
		return agentCfg
	}
	logger.Info("dettools: injected deterministic tool server", "provider", provider, "tools", effective)
	return merged
}

// computeEffectiveAllowed resolves the tool allowlist for one agent: start from
// the daemon allowlist, intersect with the agent's allowed_tools when set (the
// agent can only narrow), then drop anything in the daemon or agent denylist.
func computeEffectiveAllowed(cfg DetToolsConfig, runtimeConfig json.RawMessage) []string {
	base := cfg.AllowedTools
	profile := parseAgentDetToolsProfile(runtimeConfig)
	if len(profile.AllowedTools) > 0 {
		allowed := toSet(profile.AllowedTools)
		base = filterSlice(base, func(t string) bool { return allowed[t] })
	}
	denied := toSet(cfg.DeniedTools)
	for _, t := range profile.DeniedTools {
		denied[t] = true
	}
	return filterSlice(base, func(t string) bool { return !denied[t] })
}

func parseAgentDetToolsProfile(runtimeConfig json.RawMessage) agentDetToolsProfile {
	var p agentDetToolsProfile
	if len(strings.TrimSpace(string(runtimeConfig))) == 0 {
		return p
	}
	var wrapper struct {
		DeterministicTools agentDetToolsProfile `json:"deterministic_tools"`
	}
	if err := json.Unmarshal(runtimeConfig, &wrapper); err == nil {
		p = wrapper.DeterministicTools
	}
	return p
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, i := range items {
		set[i] = true
	}
	return set
}

func filterSlice(items []string, keep func(string) bool) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		if keep(i) {
			out = append(out, i)
		}
	}
	return out
}

// buildEffectiveMcpConfig merges the deterministic tool server into a Claude-style
// mcp_config (`{"mcpServers": {...}}`). It preserves all existing top-level keys
// and user-defined servers; if a server already named dettoolsServerName exists,
// the original config is returned unchanged so user intent always wins. allowed
// is the per-agent effective tool list passed to the server.
func buildEffectiveMcpConfig(agentCfg json.RawMessage, selfBin, workDir string, cfg DetToolsConfig, allowed []string) (json.RawMessage, error) {
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
		"env":     dettoolsServerEnv(workDir, cfg, allowed),
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
// passes to the spawned MCP server process. workDir is pinned explicitly when
// known; when empty (OpenClaw path) it is omitted so the server falls back to
// its spawned cwd. allowed is the resolved per-agent tool list.
func dettoolsServerEnv(workDir string, cfg DetToolsConfig, allowed []string) map[string]string {
	env := map[string]string{
		"MULTICA_DETTOOLS_TIMEOUT":       cfg.Timeout.String(),
		"MULTICA_DETTOOLS_ALLOW_NETWORK": strconv.FormatBool(cfg.AllowNetwork),
		"MULTICA_DETTOOLS_ARTIFACT_DIR":  cfg.ArtifactDir,
		"MULTICA_DETTOOLS_ALLOWED":       strings.Join(allowed, ","),
	}
	if workDir != "" {
		env["MULTICA_DETTOOLS_WORKDIR"] = workDir
	}
	return env
}
