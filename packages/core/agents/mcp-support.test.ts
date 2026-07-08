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
    expect(providerSupportsMcpConfig("kiro")).toBe(true);
    expect(providerSupportsMcpConfig("opencode")).toBe(true);
    expect(providerSupportsMcpConfig("openclaw")).toBe(true);
    expect(providerSupportsMcpConfig("qoder")).toBe(true);
    expect(providerSupportsMcpConfig("traecli")).toBe(true);
  });

  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig("pi")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
