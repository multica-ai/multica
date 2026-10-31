import { describe, expect, it } from "vitest";

import { providerSupportsMcpConfig } from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("accepts a provider whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
    expect(providerSupportsMcpConfig("devin")).toBe(true);
    // ZCode's session/create and session/resume both take params.mcpServers,
    // and cold resume must re-inject them (design doc §2.6).
    expect(providerSupportsMcpConfig("zcode")).toBe(true);
  });
  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig("pi")).toBe(true);
    // ZeroClaw's ACP server never reads `params.mcpServers` — MCP lives in
    // ZeroClaw's own config-dir, so a value saved here could not be honoured.
    expect(providerSupportsMcpConfig("zeroclaw")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
