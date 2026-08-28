import { describe, expect, it } from "vitest";
import { providerSupportsMcpConfig } from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("supports OmniRoute's native MCP adapter", () => {
    expect(providerSupportsMcpConfig("omniroute")).toBe(true);
  });

  it("does not advertise MCP for providers without an adapter", () => {
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
