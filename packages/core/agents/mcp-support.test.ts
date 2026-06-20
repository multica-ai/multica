import { describe, expect, it } from "vitest";

import {
  mcpSupportKind,
  providerSupportsMcpConfig,
  toolPlaneSupported,
} from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("is true for native MCP providers", () => {
    for (const p of ["claude", "codex", "dirge", "hermes", "kimi", "kiro", "opencode", "openclaw"]) {
      expect(providerSupportsMcpConfig(p)).toBe(true);
    }
  });

  it("is false for non-MCP providers and nullish input", () => {
    expect(providerSupportsMcpConfig("gemini")).toBe(false);
    // pi is adapter-backed, not native, so the native mcp_config tab stays hidden.
    expect(providerSupportsMcpConfig("pi")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});

describe("mcpSupportKind", () => {
  it("classifies native providers", () => {
    expect(mcpSupportKind("claude")).toBe("native");
    expect(mcpSupportKind("dirge")).toBe("native");
    expect(mcpSupportKind("openclaw")).toBe("native");
  });

  it("classifies unsupported providers as none", () => {
    expect(mcpSupportKind("gemini")).toBe("none");
    expect(mcpSupportKind(undefined)).toBe("none");
  });

  it("classifies pi as adapter-backed", () => {
    expect(mcpSupportKind("pi")).toBe("adapter");
  });
});

describe("toolPlaneSupported", () => {
  it("covers native and adapter-backed providers", () => {
    expect(toolPlaneSupported("codex")).toBe(true);
    expect(toolPlaneSupported("pi")).toBe(true);
    expect(toolPlaneSupported("gemini")).toBe(false);
  });
});
