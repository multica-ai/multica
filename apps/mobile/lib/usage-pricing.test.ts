import { describe, expect, it } from "vitest";
import { estimateCost, estimateCostBreakdown, fmtMoney, formatTokens } from "./usage-pricing";

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
    // 1M x $3 input + 1M x $15 output = $18.
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
    // 1M x $1.25 + 1M x $10 + 2M x $0.125 = $11.50.
    expect(cost).toBeCloseTo(11.5, 5);
  });

  it("strips dated snapshots before resolving (gpt-5-2025-08-07 -> gpt-5)", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "gpt-5-2025-08-07",
      input_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(1.25, 5);
  });

  it("prices a Copilot session reporting claude-opus-4.7 at the official Opus rate", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-opus-4.7",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
  });

  it("prices Claude Fable 5 at the Mythos-class tier", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-fable-5",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 1_000_000,
      cache_write_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(10 + 50 + 1 + 12.5, 5);
  });

  it("prices Claude Sonnet 5 at Anthropic's intro $2 / $10 tier", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-sonnet-5",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 1_000_000,
      cache_write_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(2 + 10 + 0.2 + 2.5, 5);
  });

  it("prices the provider-prefixed Anthropic form (anthropic/claude-sonnet-4.6)", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "anthropic/claude-sonnet-4.6",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(3 + 15, 5);
  });

  it("prices the dated dotted Anthropic form (claude-haiku-4.5-20251001)", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-haiku-4.5-20251001",
      input_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(1, 5);
  });

  it("prices the full provider+dotted+dated form (anthropic/claude-opus-4.7-20251001)", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "anthropic/claude-opus-4.7-20251001",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
  });

  it("prices the 1M-context Anthropic tag form (claude-opus-4-7[1m]) at the standard Opus tier", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-opus-4-7[1m]",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
  });

  it("prices each dotted Codex catalog SKU at its own tier, not gpt-5", () => {
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

  it("prices the gpt-5.6 series per OpenAI's official cache-aware rates", () => {
    const cases = [
      { model: "gpt-5.6-sol", input: 5, cacheRead: 0.5, cacheWrite: 6.25, output: 30, total: 41.75 },
      { model: "gpt-5.6-terra", input: 2.5, cacheRead: 0.25, cacheWrite: 3.125, output: 15, total: 20.875 },
      { model: "gpt-5.6-luna", input: 1, cacheRead: 0.1, cacheWrite: 1.25, output: 6, total: 8.35 },
    ];
    for (const c of cases) {
      const breakdown = estimateCostBreakdown({
        ...zeroUsage,
        model: c.model,
        input_tokens: 1_000_000,
        cache_read_tokens: 1_000_000,
        cache_write_tokens: 1_000_000,
        output_tokens: 1_000_000,
      });
      expect(breakdown.input).toBeCloseTo(c.input, 5);
      expect(breakdown.cacheRead).toBeCloseTo(c.cacheRead, 5);
      expect(breakdown.cacheWrite).toBeCloseTo(c.cacheWrite, 5);
      expect(breakdown.output).toBeCloseTo(c.output, 5);
      expect(
        estimateCost({
          ...zeroUsage,
          model: c.model,
          input_tokens: 1_000_000,
          cache_read_tokens: 1_000_000,
          cache_write_tokens: 1_000_000,
          output_tokens: 1_000_000,
        }),
      ).toBeCloseTo(c.total, 5);
    }
  });

  it("returns 0 for a catalog SKU without a published price (gpt-5.5-mini)", () => {
    expect(
      estimateCost({ ...zeroUsage, model: "gpt-5.5-mini", input_tokens: 1_000_000 }),
    ).toBe(0);
  });

  it("returns 0 for a hypothetical future variant instead of inheriting a relative's price", () => {
    expect(
      estimateCost({ ...zeroUsage, model: "gpt-5.99-codex", input_tokens: 1_000_000 }),
    ).toBe(0);
  });

  it("returns 0 for a genuinely unknown model", () => {
    expect(
      estimateCost({ ...zeroUsage, model: "totally-made-up-model", input_tokens: 1_000_000 }),
    ).toBe(0);
  });

  it("prices Cursor Composer rows at the published rates without cache-write spend", () => {
    const costWithAllTokenTypes = (model: string) =>
      estimateCost({
        ...zeroUsage,
        provider: "cursor",
        model,
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
        cache_read_tokens: 1_000_000,
        cache_write_tokens: 1_000_000,
      });

    expect(costWithAllTokenTypes("auto")).toBeCloseTo(1.25 + 6 + 0.25, 5);
    expect(costWithAllTokenTypes("composer-2.5-fast")).toBeCloseTo(3 + 15 + 0.5, 5);
    expect(costWithAllTokenTypes("composer-2.5")).toBeCloseTo(0.5 + 2.5 + 0.2, 5);
    expect(costWithAllTokenTypes("composer-2-fast")).toBeCloseTo(1.5 + 7.5 + 0.35, 5);
    expect(costWithAllTokenTypes("composer-2")).toBeCloseTo(0.5 + 2.5 + 0.2, 5);
    expect(costWithAllTokenTypes("composer-1.5")).toBeCloseTo(3.5 + 17.5 + 0.35, 5);
    expect(costWithAllTokenTypes("composer-1")).toBeCloseTo(1.25 + 10 + 0.125, 5);
    expect(costWithAllTokenTypes("cursor")).toBeCloseTo(3 + 15 + 0.5, 5);
  });

  it("scopes the generic auto id by provider so collisions don't borrow a price", () => {
    const auto = (provider?: string) =>
      estimateCost({ ...zeroUsage, provider, model: "auto", input_tokens: 1_000_000 });

    expect(auto("cursor")).toBeCloseTo(1.25, 5);
    expect(auto("acme")).toBe(0);
    expect(auto(undefined)).toBe(0);
  });

  it("recognises provider-prefixed forms emitted by OpenRouter-style runtimes", () => {
    expect(
      estimateCost({ ...zeroUsage, model: "deepseek/deepseek-v4-flash", input_tokens: 1_000_000 }),
    ).toBeGreaterThan(0);
    expect(
      estimateCost({ ...zeroUsage, model: "moonshotai/kimi-k2.6", input_tokens: 1_000_000 }),
    ).toBeGreaterThan(0);
    expect(
      estimateCost({ ...zeroUsage, model: "zhipuai/glm-5.1", input_tokens: 1_000_000 }),
    ).toBeGreaterThan(0);
  });

  // The Chinese-model rates below are spot-checked against the official price
  // sheets cited in usage-pricing.ts's MODEL_PRICING header comment.
  it("prices deepseek-v4-flash at the official $0.14/$0.28 with ~50x cache-hit discount", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: "deepseek-v4-flash",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(0.14 + 0.28 + 0.0028, 5);
  });

  it("prices the deepseek-chat / deepseek-reasoner aliases at the same rate as deepseek-v4-flash", () => {
    const flash = estimateCost({
      ...zeroUsage,
      model: "deepseek-v4-flash",
      input_tokens: 1_000_000,
    });
    expect(
      estimateCost({ ...zeroUsage, model: "deepseek-chat", input_tokens: 1_000_000 }),
    ).toBeCloseTo(flash, 5);
    expect(
      estimateCost({ ...zeroUsage, model: "deepseek-reasoner", input_tokens: 1_000_000 }),
    ).toBeCloseTo(flash, 5);
  });

  it("prices kimi-k2.6 at the official $0.95 / $4.00 tier", () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: "kimi-k2.6",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(4.95, 5);
  });

  it("prices glm-5.1 at the official $1.4 / $4.4 tier", () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: "glm-5.1",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(1.4 + 4.4, 5);
  });

  it("prices glm-4.5-flash at the official Free tier ($0)", () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: "glm-4.5-flash",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBe(0);
  });
});

describe("formatTokens", () => {
  it("formats under 1000 as a plain locale string", () => {
    expect(formatTokens(999)).toBe("999");
  });

  it("formats thousands with a K suffix", () => {
    expect(formatTokens(1500)).toBe("1.5K");
  });

  it("formats millions with an M suffix", () => {
    expect(formatTokens(2_400_000)).toBe("2.4M");
  });
});

describe("fmtMoney", () => {
  it("shows 2 decimal places under $100", () => {
    expect(fmtMoney(4.5)).toBe("$4.50");
  });

  it("rounds to whole dollars at $100 and above", () => {
    expect(fmtMoney(123.45)).toBe("$123");
  });
});
