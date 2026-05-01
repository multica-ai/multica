import { describe, expect, it } from "vitest";
import { compileProfile } from "./compile";
import { buildDefaultProfile } from "./presets";
import { COMPILED_PROMPT_TOKEN_CAP } from "./schema";
import { estimateTokens, formatContextPercent } from "./tokens";

describe("estimateTokens", () => {
  it("always marks results as approximate", () => {
    expect(estimateTokens("hello").approximate).toBe(true);
  });

  it("returns at least 1 token for non-empty text", () => {
    expect(estimateTokens("a").tokens).toBeGreaterThanOrEqual(1);
  });

  it("scales linearly with text length", () => {
    const small = estimateTokens("x".repeat(33));
    const large = estimateTokens("x".repeat(330));
    expect(large.tokens).toBeGreaterThan(small.tokens * 8);
  });

  it("default profiles all stay under the 200-token cap", () => {
    for (const persona of ["utalmodig", "ekspert", "grundig", "larling"] as const) {
      const profile = buildDefaultProfile(persona);
      const compiled = compileProfile(profile, { displayName: "Jens" });
      const { tokens } = estimateTokens(compiled, profile.language);
      expect(tokens, `persona ${persona}`).toBeLessThan(COMPILED_PROMPT_TOKEN_CAP);
    }
  });
});

describe("formatContextPercent", () => {
  it("renders a small fraction with floor label", () => {
    expect(formatContextPercent(0.00005)).toBe("<0.01%");
  });

  it("renders a normal fraction with two decimals", () => {
    expect(formatContextPercent(0.0123)).toBe("1.23%");
  });
});
