import { describe, it, expect } from "vitest";
import type { RuntimeUsage } from "@multica/core/types";
import {
  collectUnmappedModels,
  estimateCost,
  estimateCostBreakdown,
  isModelPriced,
} from "./utils";

// Build a one-million-token usage row so estimateCost output equals the
// per-MTok rate directly — makes pricing assertions readable.
function usage(overrides: Partial<RuntimeUsage>): RuntimeUsage {
  return {
    runtime_id: "rt-1",
    date: "2026-04-01",
    provider: "",
    model: "",
    input_tokens: 0,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_write_tokens: 0,
    ...overrides,
  };
}

const ONE_M = 1_000_000;

describe("resolvePricing / isModelPriced", () => {
  it("matches full provider/model keys exactly", () => {
    expect(isModelPriced("anthropic/claude-sonnet-4-5")).toBe(true);
  });

  it("matches models with trailing date suffix via startsWith on date-stripped name", () => {
    expect(isModelPriced("anthropic/claude-sonnet-4-5-20250929")).toBe(true);
  });

  it("matches a date-suffixed `provider/model-YYYYMMDD` form", () => {
    expect(isModelPriced("anthropic/claude-sonnet-4-5-20250929")).toBe(true);
    expect(isModelPriced("openai/gpt-4o-2024-08-06")).toBe(true);
  });

  it("matches popular OpenAI models reported by OpenCode", () => {
    for (const m of [
      "openai/gpt-4o",
      "openai/gpt-4o-mini",
      "openai/gpt-4.1",
      "openai/o1",
      "openai/o3-mini",
      "openai/o4-mini",
    ]) {
      expect(isModelPriced(m), m).toBe(true);
    }
  });

  it("matches popular Google Gemini models reported by OpenCode", () => {
    for (const m of [
      "google/gemini-2.5-pro",
      "google/gemini-2.5-flash",
      "google/gemini-2.0-flash",
    ]) {
      expect(isModelPriced(m), m).toBe(true);
    }
  });

  it("returns false for xAI and DeepSeek models (not in pricing table)", () => {
    for (const m of [
      "xai/grok-4",
      "xai/grok-3-mini",
      "deepseek/deepseek-chat",
      "deepseek/deepseek-reasoner",
    ]) {
      expect(isModelPriced(m), m).toBe(false);
    }
  });

  it("returns undefined / false for genuinely unknown provider/model", () => {
    expect(isModelPriced("madeup/totally-not-a-real-model")).toBe(false);
    expect(isModelPriced("openai/this-is-not-a-real-model-xyzzy")).toBe(false);
  });

  it("treats empty strings as unknown", () => {
    expect(isModelPriced("")).toBe(false);
  });

  it("matches bare model names without provider prefix", () => {
    for (const m of [
      "gpt-4o",
      "gpt-4o-mini",
      "claude-sonnet-4-5",
      "claude-opus-4-1",
      "gemini-2.5-pro",
    ]) {
      expect(isModelPriced(m), m).toBe(true);
    }
  });

  it("matches bare models with date suffix", () => {
    expect(isModelPriced("claude-opus-4-1-20260105")).toBe(true);
    expect(isModelPriced("gpt-4o-2024-08-06")).toBe(true);
  });

  it("matches provider-prefixed models with date suffix", () => {
    expect(isModelPriced("anthropic/claude-opus-4-1-20260105")).toBe(true);
    expect(isModelPriced("openai/gpt-4o-2024-08-06")).toBe(true);
  });
});

describe("estimateCost — OpenCode provider/model parity", () => {
  // The "Anthropic-via-OpenCode" parity case from the acceptance criteria:
  // routing the same model through OpenCode must not change billing.
  it("returns accurate cost for anthropic/claude-sonnet-4-5", () => {
    const tokens = {
      input_tokens: 1_000_000,
      output_tokens: 500_000,
      cache_read_tokens: 200_000,
      cache_write_tokens: 50_000,
    };
    const cost = estimateCost(usage({ model: "anthropic/claude-sonnet-4-5", ...tokens }));
    expect(cost).toBeGreaterThan(0);
  });

  it("returns a non-zero, accurate cost for openai/gpt-4o", () => {
    // OpenAI gpt-4o published rates (per 1M tokens):
    //   input  $2.50
    //   output $10.00
    // 1M input + 1M output should be exactly $12.50 — assert tightly so any
    // accidental decimal-point shift in MODEL_PRICING fails this test.
    const cost = estimateCost(
      usage({
        model: "openai/gpt-4o",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(cost).toBeCloseTo(12.5, 6);
  });

  it("returns a non-zero, accurate cost for google/gemini-2.5-pro", () => {
    // Google Gemini 2.5 Pro (≤200K-context tier, the OpenCode default):
    //   input  $1.25
    //   output $10.00
    const cost = estimateCost(
      usage({
        model: "google/gemini-2.5-pro",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(cost).toBeCloseTo(11.25, 6);
  });

  it("deepseek/deepseek-chat is not priced (not in pricing table)", () => {
    const cost = estimateCost(
      usage({
        model: "deepseek/deepseek-chat",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(cost).toBe(0);
  });

  it("matches Anthropic's `claude-3-5-haiku-latest` id format", () => {
    // Anthropic's actual model ids use `claude-3-5-haiku-…`, not
    // `claude-haiku-3-5-…`. Earlier the table keyed off the latter and
    // returned $0 for OpenCode's `anthropic/claude-3-5-haiku-latest`.
    const cost = estimateCost(
      usage({
        model: "anthropic/claude-3-5-haiku-latest",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(cost).toBeCloseTo(0.8 + 4, 6);
  });

  it("resolves the gpt-4o family to each entry's specific price", () => {
    // Three sibling entries in the snapshot share the `gpt-4o` prefix but
    // carry different prices. The resolver must hit the *exact* bare key
    // for each one rather than greedily falling through a startsWith
    // match — otherwise `gpt-4o-2024-05-13` would resolve to `gpt-4o`'s
    // price (alphabetically first) instead of its own.
    const bareGpt4o = estimateCost(
      usage({
        model: "gpt-4o",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(bareGpt4o).toBeCloseTo(2.5 + 10, 6); // openai/gpt-4o → $2.50 / $10

    const gpt4oMini = estimateCost(
      usage({
        model: "gpt-4o-mini",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(gpt4oMini).toBeCloseTo(0.15 + 0.6, 6); // openai/gpt-4o-mini → $0.15 / $0.60

    const gpt4oDated = estimateCost(
      usage({
        model: "gpt-4o-2024-05-13",
        input_tokens: ONE_M,
        output_tokens: ONE_M,
      }),
    );
    expect(gpt4oDated).toBeCloseTo(5 + 15, 6); // openai/gpt-4o-2024-05-13 → $5 / $15
  });

  it("breakdown sums match the total cost", () => {
    const u = usage({
      model: "openai/gpt-4o-mini",
      input_tokens: 750_000,
      output_tokens: 250_000,
      cache_read_tokens: 100_000,
      cache_write_tokens: 50_000,
    });
    const total = estimateCost(u);
    const b = estimateCostBreakdown(u);
    expect(b.input + b.output + b.cacheRead + b.cacheWrite).toBeCloseTo(total, 10);
    expect(total).toBeGreaterThan(0);
  });
});

describe("collectUnmappedModels", () => {
  it("returns an empty list for supported providers (excludes xAI and DeepSeek)", () => {
    const rows = [
      usage({ model: "anthropic/claude-sonnet-4-5", input_tokens: 1 }),
      usage({ model: "openai/gpt-4o", input_tokens: 1 }),
      usage({ model: "google/gemini-2.5-pro", input_tokens: 1 }),
    ];
    expect(collectUnmappedModels(rows)).toEqual([]);
  });

  it("still flags genuinely unknown models", () => {
    const rows = [
      usage({ model: "anthropic/claude-sonnet-4-5", input_tokens: 1 }),
      usage({ model: "noprovider/madeup-model-xyzzy", input_tokens: 1 }),
    ];
    expect(collectUnmappedModels(rows)).toEqual(["noprovider/madeup-model-xyzzy"]);
  });
});
