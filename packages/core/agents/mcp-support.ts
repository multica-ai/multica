// The set of runtime providers whose backend reads `agent.mcp_config` and
// forwards MCP servers to the underlying CLI. The MCP config tab is hidden
// for every other provider so a user can't save a value the runtime will
// silently ignore. Keep this list in sync with the backends in
// `server/pkg/agent/` that read `ExecOptions.McpConfig`, plus the OpenClaw
// per-task wrapper preparer in `server/internal/daemon/execenv/` which
// materialises `mcp.servers` into the synthesised config rather than going
// through ExecOptions.
const MCP_SUPPORTED_PROVIDERS = new Set([
  "claude",
  "codex",
  "hermes",
  "kimi",
  "kiro",
  "openclaw",
]);

export function providerSupportsMcpConfig(provider: string | undefined | null): boolean {
  if (!provider) return false;
  return MCP_SUPPORTED_PROVIDERS.has(provider);
}
