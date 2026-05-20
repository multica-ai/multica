import { describe, expect, it } from "vitest";
import { displayProviderName, displayRuntimeName } from "./provider-display";

describe("displayProviderName", () => {
  it("keeps AGY user-facing while preserving the gemini provider key", () => {
    expect(displayProviderName("gemini")).toBe("AGY");
    expect(displayProviderName("Gemini")).toBe("AGY");
  });

  it("capitalizes ordinary provider keys", () => {
    expect(displayProviderName("claude")).toBe("Claude");
    expect(displayProviderName("codex")).toBe("Codex");
  });

  it("falls back for empty provider values", () => {
    expect(displayProviderName("")).toBe("Runtime");
    expect(displayProviderName(null)).toBe("Runtime");
  });
});

describe("displayRuntimeName", () => {
  it("normalizes existing Gemini runtime names for AGY-backed runtimes", () => {
    expect(displayRuntimeName("Gemini (mini)", "gemini")).toBe("AGY (mini)");
    expect(displayRuntimeName("gemini", "gemini")).toBe("AGY");
  });

  it("does not rewrite model-like names for other providers", () => {
    expect(displayRuntimeName("Gemini evaluator", "codex")).toBe("Gemini evaluator");
  });
});
