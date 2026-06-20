// The set of runtime providers whose backend reads `agent.mcp_config` and
// forwards MCP servers to the underlying CLI. The MCP config tab is hidden
// for every other provider so a user can't save a value the runtime will
// silently ignore. Keep this list in sync with the backends in
// `server/pkg/agent/` that read `ExecOptions.McpConfig`, plus providers whose
// per-task preparers in `server/internal/daemon/execenv/` materialise MCP
// config for CLIs that do not receive it through ExecOptions.
const MCP_SUPPORTED_PROVIDERS = new Set([
  "claude",
  "codex",
  "cursor",
  "dirge",
  "hermes",
  "kimi",
  "kiro",
  "opencode",
  "openclaw",
]);

export function providerSupportsMcpConfig(provider: string | undefined | null): boolean {
  if (!provider) return false;
  return MCP_SUPPORTED_PROVIDERS.has(provider);
}

// How a provider reaches MCP / the deterministic tool plane:
// - "native"  — the runtime consumes mcp_config directly (the set above).
// - "adapter" — the runtime needs a bridge to reach MCP servers. Pi has no
//   native MCP and reaches the tool plane through pi-mcp-adapter; the daemon
//   path is opt-in (MULTICA_DETTOOLS_PI_ADAPTER) and fail-open, so actual
//   availability still depends on the adapter being installed at runtime.
// - "none"    — no MCP support.
export type McpSupportKind = "native" | "adapter" | "none";

const ADAPTER_MCP_PROVIDERS = new Set<string>(["pi"]);

export function mcpSupportKind(provider: string | undefined | null): McpSupportKind {
  if (!provider) return "none";
  if (MCP_SUPPORTED_PROVIDERS.has(provider)) return "native";
  if (ADAPTER_MCP_PROVIDERS.has(provider)) return "adapter";
  return "none";
}

// toolPlaneSupported reports whether a provider can reach the deterministic tool
// plane by any path (native or adapter). UI affordances for the tool plane key
// on this rather than on native MCP alone, so adapter-backed providers light up
// once their integration is complete.
export function toolPlaneSupported(provider: string | undefined | null): boolean {
  return mcpSupportKind(provider) !== "none";
}
