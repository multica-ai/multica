// Providers with unconditional support for forwarding `agent.mcp_config`
// to the underlying CLI. Pi is checked separately against the runtime
// capability reported by its daemon, because it requires an enabled adapter.
// Keep this list in sync with the backends in
// `server/pkg/agent/` that read `ExecOptions.McpConfig`, plus providers whose
// per-task preparers in `server/internal/daemon/execenv/` materialise MCP
// config for CLIs that do not receive it through ExecOptions.
const MCP_SUPPORTED_PROVIDERS = new Set([
  "claude",
  "codebuddy",
  "codearts",
  "codex",
  "cursor",
  "grok",
  "hermes",
  "kimi",
  "reasonix",
  "dsh",
  "kiro",
  "opencode",
  "openclaw",
  "qoder",
  "qoderclicn",
  "qwen",
  "qwenpaw",
  "mcode",
  "traecli",
  "dim",
  "omp",
]);

export function providerSupportsMcpConfig(
  provider: string | undefined | null,
  metadata?: Record<string, unknown> | null,
): boolean {
  if (!provider) return false;
  // Pi requires a compatible, enabled MCP extension on this runtime instance.
  if (provider === "pi") return metadata?.managed_mcp === true;
  return MCP_SUPPORTED_PROVIDERS.has(provider);
}
