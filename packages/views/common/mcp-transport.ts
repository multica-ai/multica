/** User-facing protocol names shared by every MCP inventory and editor. */
export function mcpTransportLabel(transport: string): string {
  const normalized = transport.trim().toLowerCase();
  switch (normalized) {
    case "local":
    case "stdio":
      return "STDIO";
    case "remote":
    case "http":
    case "streamable-http":
      return "Streamable HTTP";
    case "sse":
      return "SSE";
    default:
      return transport.trim() || "Unknown";
  }
}
