// @vitest-environment node

import { describe, expect, it } from "vitest";

import { providerSupportsMcpConfig } from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("accepts a provider whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
  });
  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig("pi")).toBe(false);
    // ZeroClaw's ACP server never reads `params.mcpServers` — MCP lives in
    // ZeroClaw's own config-dir, so a value saved here could not be honoured.
    expect(providerSupportsMcpConfig("zeroclaw")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });

  it.each([
    [undefined, false],
    [{}, false],
    [{ managed_mcp: true }, true],
    [{ managed_mcp: false }, false],
    [{ managed_mcp: "true" }, false],
    [{ managed_mcp: 1 }, false],
    [{ managed_mcp: null }, false],
  ])("requires Pi's detected managed MCP capability: %j", (metadata, expected) => {
    expect(providerSupportsMcpConfig("pi", metadata)).toBe(expected);
  });

  it("keeps other providers independent of Pi's detected capability", () => {
    expect(providerSupportsMcpConfig("claude", { managed_mcp: false })).toBe(true);
    expect(providerSupportsMcpConfig("copilot", { managed_mcp: true })).toBe(false);
  });
});
