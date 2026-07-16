// The set of runtime providers whose backend reads `agent.mcp_config` and
// forwards MCP servers to the underlying CLI. The MCP config tab is hidden
// for every other provider so a user can't save a value the runtime will
// silently ignore. Keep this list in sync with the backends in
// `server/pkg/agent/` that read `ExecOptions.McpConfig`, plus providers whose
// per-task preparers in `server/internal/daemon/execenv/` materialise MCP
// config for CLIs that do not receive it through ExecOptions.
const MCP_SUPPORTED_PROVIDERS = new Set([
  "claude",
  "codebuddy",
  "codex",
  "cursor",
  "grok",
  "hermes",
  "kimi",
  "kiro",
  "opencode",
  "openclaw",
  "qoder",
  "traecli",
]);

export function providerSupportsMcpConfig(provider: string | undefined | null): boolean {
  if (!provider) return false;
  return MCP_SUPPORTED_PROVIDERS.has(provider);
}
