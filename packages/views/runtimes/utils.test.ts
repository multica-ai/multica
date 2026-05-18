import { describe, it, expect, afterEach } from "vitest";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";

import {
  aggregateCostByModel,
  collectUnmappedModels,
  estimateCost,
  isModelPriced,
} from "./utils";

afterEach(() => {
  // Reset overrides so tests don't bleed pricing state into one another.
  useCustomPricingStore.setState({ pricings: {} });
});

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

  it("prices a Copilot session reporting claude-opus-4.7 at the official Opus rate", () => {
    // Copilot's `meta.agentMeta.model` is `claude-opus-4.7` (dotted). We
    // canonicalize to the dashed catalog key so it hits the maintained $5/$25
    // tier instead of falling through to the custom-pricing dialog.
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-opus-4.7",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
  });

  it("prices the provider-prefixed Anthropic form (anthropic/claude-sonnet-4.6)", () => {
    // openclaw / opencode emit `<provider>/<model>`. Same SKU as the
    // bare form, must hit the same rate.
    const cost = estimateCost({
      ...zeroUsage,
      model: "anthropic/claude-sonnet-4.6",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(3 + 15, 5);
  });

  it("prices the dated dotted Anthropic form (claude-haiku-4.5-20251001)", () => {
    // Belt-and-braces: combine all three tolerances (provider prefix not
    // present, but dot→dash + date strip both apply).
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-haiku-4.5-20251001",
      input_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(1, 5);
  });

  it("prices the full provider+dotted+dated form (anthropic/claude-opus-4.7-20251001)", () => {
    // All three normalization steps must compose: strip `anthropic/`,
    // dot→dash on the Claude ID, and trim the date stamp. Pins the
    // combined path so a future change to candidate ordering can't
    // silently drop one tolerance.
    const cost = estimateCost({
      ...zeroUsage,
      model: "anthropic/claude-opus-4.7-20251001",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
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

  it("recognises dotted Anthropic IDs as the same SKU as their dashed canonical form", () => {
    // GitHub Copilot reports Claude models with dots (`claude-opus-4.7`)
    // while Anthropic's own CLIs use dashes (`claude-opus-4-7`). Both must
    // hit the same catalog row, otherwise Copilot-routed usage gets bucketed
    // as "unmapped" and the user has to type the price in by hand.
    expect(isModelPriced("claude-haiku-4.5")).toBe(true);
    expect(isModelPriced("claude-sonnet-4.5")).toBe(true);
    expect(isModelPriced("claude-sonnet-4.6")).toBe(true);
    expect(isModelPriced("claude-opus-4.5")).toBe(true);
    expect(isModelPriced("claude-opus-4.6")).toBe(true);
    expect(isModelPriced("claude-opus-4.7")).toBe(true);
  });

  it("recognises provider-prefixed Anthropic IDs (openclaw / opencode form)", () => {
    // openclaw / opencode emit `<provider>/<model>` in `meta.agentMeta.model`.
    // The provider prefix is routing metadata, not part of the SKU.
    expect(isModelPriced("anthropic/claude-opus-4.7")).toBe(true);
    expect(isModelPriced("anthropic/claude-sonnet-4-6")).toBe(true);
  });

  it("still rejects OpenAI dotted variants that don't have their own row", () => {
    // The Anthropic dot→dash normalization is scoped to `claude-*` IDs.
    // For OpenAI the separator is semantic — `gpt-5.4` is a different SKU
    // from a hypothetical `gpt-5-4` — and `gpt-5.5-mini` must still surface
    // as unmapped because OpenAI hasn't published its rate.
    expect(isModelPriced("gpt-5.5-mini")).toBe(false);
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

describe("user-supplied custom pricing", () => {
  it("prices a model the maintained catalog doesn't ship", () => {
    useCustomPricingStore.getState().setCustomPricing("gpt-5.5-mini", {
      input: 1,
      output: 4,
      cacheRead: 0.1,
      cacheWrite: 1,
    });
    expect(isModelPriced("gpt-5.5-mini")).toBe(true);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "gpt-5.5-mini",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(5, 5);
  });

  it("does NOT shadow the maintained catalog when both define the same model", () => {
    // Catalog wins so a user can't accidentally over-charge themselves for
    // a model we already track (and so a stale local override doesn't
    // silently disagree with what the dashboard shows everyone else).
    useCustomPricingStore.getState().setCustomPricing("claude-sonnet-4-6", {
      input: 999,
      output: 999,
      cacheRead: 999,
      cacheWrite: 999,
    });
    expect(
      estimateCost({
        ...zeroUsage,
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(3, 5); // maintained input rate, not the 999 override
  });

  it("falls back to a stripped dated snapshot in the custom store", () => {
    useCustomPricingStore.getState().setCustomPricing("brand-new-model", {
      input: 2,
      output: 8,
      cacheRead: 0.2,
      cacheWrite: 2,
    });
    expect(
      estimateCost({
        ...zeroUsage,
        model: "brand-new-model-2026-04-01",
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(2, 5);
  });

  it("removeCustomPricing clears the override", () => {
    const store = useCustomPricingStore.getState();
    store.setCustomPricing("gpt-5.5-mini", {
      input: 1,
      output: 4,
      cacheRead: 0.1,
      cacheWrite: 1,
    });
    expect(isModelPriced("gpt-5.5-mini")).toBe(true);
    useCustomPricingStore.getState().removeCustomPricing("gpt-5.5-mini");
    expect(isModelPriced("gpt-5.5-mini")).toBe(false);
  });

  it("priced + unpriced models in the same window produce a mixed-cost aggregate", () => {
    // The partial-unmapping case: chart renders normally because some
    // models are priced, but the unmapped ones silently contribute $0 if
    // we don't surface them. Confirm aggregateCostByModel exposes both
    // sides so the UI can show a notice for the gap.
    const rows = [
      {
        ...zeroUsage,
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        date: "2026-01-01",
        provider: "anthropic",
        agent_count: 1,
      },
      {
        ...zeroUsage,
        model: "fictional-model-x",
        input_tokens: 1_000_000,
        date: "2026-01-01",
        provider: "fictional",
        agent_count: 1,
      },
    ];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const byModel = aggregateCostByModel(rows as any);
    const sonnet = byModel.find((r) => r.key === "claude-sonnet-4-6");
    const fictional = byModel.find((r) => r.key === "fictional-model-x");
    expect(sonnet?.cost).toBeCloseTo(3, 5);
    expect(fictional?.cost).toBe(0);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect(collectUnmappedModels(rows as any)).toEqual(["fictional-model-x"]);
  });

  it("aggregateCostByModel reflects a newly-saved custom price on re-call with the same input", () => {
    // Regression for the memo-dependency bug GPT-Boy flagged: aggregate
    // helpers must give different answers before vs after a price save,
    // otherwise child components (WhenChart / CostByBlock / ActivityHeatmap)
    // that memo on query data alone keep showing pre-save totals.
    const rows = [
      {
        ...zeroUsage,
        model: "fictional-model-x",
        input_tokens: 1_000_000,
        date: "2026-01-01",
        provider: "fictional",
        agent_count: 1,
      },
    ];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const before = aggregateCostByModel(rows as any);
    expect(before[0]?.cost).toBe(0);

    useCustomPricingStore.getState().setCustomPricing("fictional-model-x", {
      input: 2,
      output: 8,
      cacheRead: 0.2,
      cacheWrite: 2,
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const after = aggregateCostByModel(rows as any);
    expect(after[0]?.cost).toBeCloseTo(2, 5);
  });
});
