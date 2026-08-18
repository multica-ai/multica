// This is a generated mirror of `server/pkg/agent/provider_capabilities.go`.
// The Go provider-capability tests parse this list and fail on drift, because
// the frontend package cannot import the server's Go table at runtime.
//
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
  "reasonix",
  "kiro",
  "opencode",
  "openclaw",
  "qoder",
  "qoderclicn",
  "qwen",
  "qwenpaw",
  "traecli",
]);

export function providerSupportsMcpConfig(provider: string | undefined | null): boolean {
  if (!provider) return false;
  return MCP_SUPPORTED_PROVIDERS.has(provider);
}
