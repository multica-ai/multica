import { describe, expect, it } from "vitest";

import {
  mcpSupportKind,
  providerSupportsMcpConfig,
  toolPlaneSupported,
} from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("is true for native MCP providers", () => {
    for (const p of ["claude", "codex", "hermes", "kimi", "kiro", "opencode", "openclaw"]) {
      expect(providerSupportsMcpConfig(p)).toBe(true);
    }
  });

  it("is false for non-MCP providers and nullish input", () => {
    expect(providerSupportsMcpConfig("gemini")).toBe(false);
    expect(providerSupportsMcpConfig("pi")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});

describe("mcpSupportKind", () => {
  it("classifies native providers", () => {
    expect(mcpSupportKind("claude")).toBe("native");
    expect(mcpSupportKind("openclaw")).toBe("native");
  });

  it("classifies unsupported providers as none", () => {
    expect(mcpSupportKind("gemini")).toBe("none");
    expect(mcpSupportKind(undefined)).toBe("none");
  });

  it("pi is currently none (adapter integration not yet wired)", () => {
    expect(mcpSupportKind("pi")).toBe("none");
  });
});

describe("toolPlaneSupported", () => {
  it("tracks native support today", () => {
    expect(toolPlaneSupported("codex")).toBe(true);
    expect(toolPlaneSupported("gemini")).toBe(false);
    expect(toolPlaneSupported("pi")).toBe(false);
  });
});
