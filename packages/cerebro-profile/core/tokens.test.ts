import { describe, expect, it } from "vitest";
import { compileProfile } from "./compile";
import { buildDefaultProfile } from "./presets";
import { COMPILED_PROMPT_TOKEN_CAP } from "./schema";
import {
  estimateTokens,
  estimateTokensFromLength,
  formatContextPercent,
} from "./tokens";

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

describe("estimateTokensFromLength", () => {
  // FIR-3212: the prompt snapshot header knows the recorded prompt's size as a
  // byte count, never as text — the stored view is redacted, so its length is
  // not the length that was actually sent. Estimating from the recorded length
  // keeps the token figure consistent with the byte figure beside it.
  it("agrees with estimateTokens for text of the same length", () => {
    const text = "x".repeat(380);
    expect(estimateTokensFromLength(text.length, "en").tokens).toBe(
      estimateTokens(text, "en").tokens,
    );
  });

  it("always marks results as approximate", () => {
    expect(estimateTokensFromLength(1000, "en").approximate).toBe(true);
  });

  it("reports zero tokens for an empty prompt rather than inventing one", () => {
    expect(estimateTokensFromLength(0, "en").tokens).toBe(0);
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
