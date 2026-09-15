import { describe, expect, it } from "vitest";

import { providerSupportsMcpConfig } from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("accepts a provider whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
    expect(providerSupportsMcpConfig("pi")).toBe(true);
  });
  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    // ZeroClaw's ACP server never reads `params.mcpServers` — MCP lives in
    // ZeroClaw's own config-dir, so a value saved here could not be honoured.
    expect(providerSupportsMcpConfig("zeroclaw")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
