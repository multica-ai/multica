import { describe, expect, it } from "vitest";

import { providerSupportsMcpConfig } from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("matches providers whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
    expect(providerSupportsMcpConfig("codebuddy")).toBe(true);
    expect(providerSupportsMcpConfig("codex")).toBe(true);
    expect(providerSupportsMcpConfig("cursor")).toBe(true);
    expect(providerSupportsMcpConfig("hermes")).toBe(true);
    expect(providerSupportsMcpConfig("kimi")).toBe(true);
    expect(providerSupportsMcpConfig("reasonix")).toBe(true);
    expect(providerSupportsMcpConfig("dsh")).toBe(true);
    expect(providerSupportsMcpConfig("kiro")).toBe(true);
    expect(providerSupportsMcpConfig("opencode")).toBe(true);
    expect(providerSupportsMcpConfig("openclaw")).toBe(true);
    expect(providerSupportsMcpConfig("qoder")).toBe(true);
    expect(providerSupportsMcpConfig("qoderclicn")).toBe(true);
    expect(providerSupportsMcpConfig("qwen")).toBe(true);
    expect(providerSupportsMcpConfig("qwenpaw")).toBe(true);
    expect(providerSupportsMcpConfig("traecli")).toBe(true);
    expect(providerSupportsMcpConfig("grok")).toBe(true);
    expect(providerSupportsMcpConfig("dim")).toBe(true);
    expect(providerSupportsMcpConfig("mcode")).toBe(true);
    expect(providerSupportsMcpConfig("omp")).toBe(true);
  });

  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    // Pi ships without MCP by design: upstream's README states "No MCP." and
    // directs users to extensions instead, so there is no config file Multica
    // could write that pi would read. Only its omp fork consumes mcp_config.
    expect(providerSupportsMcpConfig("pi")).toBe(false);
    // ZeroClaw's ACP server never reads `params.mcpServers` — MCP lives in
    // ZeroClaw's own config-dir, so a value saved here could not be honoured.
    expect(providerSupportsMcpConfig("zeroclaw")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
