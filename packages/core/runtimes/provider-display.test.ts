import { describe, expect, it } from "vitest";
import { displayProviderName } from "./provider-display";

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
