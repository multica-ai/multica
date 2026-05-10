import { describe, it, expect } from "vitest";

import { collectUnmappedModels, estimateCost, isModelPriced } from "./utils";

const zeroUsage = {
  input_tokens: 0,
  output_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
};

describe("estimateCost", () => {
  it("prices the canonical Anthropic Sonnet 4.6 SKU", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-sonnet-4-6",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    // 1M × $3 input + 1M × $15 output = $18.
    expect(cost).toBeCloseTo(18, 5);
  });

  it("prices a Codex CLI session reporting gpt-5-codex", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "gpt-5-codex",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 2_000_000,
    });
    // 1M × $1.25 + 1M × $10 + 2M × $0.125 = $11.50.
    expect(cost).toBeCloseTo(11.5, 5);
  });

  it("strips dated snapshots before resolving (gpt-5-2025-08-07 → gpt-5)", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "gpt-5-2025-08-07",
      input_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(1.25, 5);
  });

  it("prices each dotted Codex catalog SKU at its own tier, not gpt-5", () => {
    // Every dotted minor version is priced independently. The resolver does
    // exact-match-after-date-strip (no startsWith fallback), so each row
    // must exist on its own.
    expect(
      estimateCost({ ...zeroUsage, model: "gpt-5.5", input_tokens: 1_000_000 }),
    ).toBeCloseTo(5, 5);
    expect(
      estimateCost({ ...zeroUsage, model: "gpt-5.4", output_tokens: 1_000_000 }),
    ).toBeCloseTo(15, 5);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "gpt-5.4-mini",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(0.75 + 4.5, 5);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "gpt-5.3-codex",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(1.75 + 14, 5);
  });

  it("flags catalog SKUs without a published price (gpt-5.5-mini) as unmapped", () => {
    // `gpt-5.5-mini` is in the Codex catalog but OpenAI hasn't published a
    // public rate. We refuse to absorb it into `gpt-5.5` — the diagnostic
    // surfaces it instead so the team knows to add an explicit row.
    expect(isModelPriced("gpt-5.5-mini")).toBe(false);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "gpt-5.5-mini",
        input_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it("flags hypothetical future variants as unmapped instead of inheriting a relative's price", () => {
    // No exact match → unmapped. Covers both dotted families (`gpt-5.99-codex`)
    // and unknown sub-variants (`gpt-5-foo`); both must miss rather than
    // silently inherit `gpt-5` pricing.
    expect(isModelPriced("gpt-5.99-codex")).toBe(false);
    expect(isModelPriced("gpt-5-foo")).toBe(false);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "gpt-5.99-codex",
        input_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it("returns 0 for a genuinely unknown model so the UI can flag it", () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: "totally-made-up-model",
        input_tokens: 1_000_000,
      }),
    ).toBe(0);
  });
});

describe("isModelPriced", () => {
  it("recognises both Claude and Codex/GPT families", () => {
    expect(isModelPriced("claude-sonnet-4-6")).toBe(true);
    expect(isModelPriced("gpt-5-codex")).toBe(true);
    expect(isModelPriced("gpt-5-mini")).toBe(true);
    expect(isModelPriced("o3")).toBe(true);
    expect(isModelPriced("totally-made-up-model")).toBe(false);
  });
});

describe("collectUnmappedModels", () => {
  it("only surfaces names that miss every pricing tier", () => {
    const rows = [
      { ...zeroUsage, model: "claude-sonnet-4-6" },
      { ...zeroUsage, model: "gpt-5-codex" },
      { ...zeroUsage, model: "fictional-model-x" },
    ];
    expect(collectUnmappedModels(rows)).toEqual(["fictional-model-x"]);
  });
});
