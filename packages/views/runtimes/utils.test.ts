import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import type { AgentRuntime, RuntimeUsage } from "@multica/core/types";

import {
  addDaysIso,
  aggregateByWeek,
  aggregateCostByModel,
  collectUnmappedModels,
  computeCostInWindow,
  estimateCost,
  isModelPriced,
  isSelfHealingRuntime,
  sliceWindow,
  todayIso,
  weekStartIso,
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

describe("isSelfHealingRuntime", () => {
  function makeRuntime(overrides: Partial<AgentRuntime>): AgentRuntime {
    return {
      id: "rt-1",
      workspace_id: "ws-1",
      daemon_id: null,
      name: "rt",
      runtime_mode: "local",
      provider: "claude",
      launch_header: "",
      status: "online",
      device_info: "",
      metadata: {},
      owner_id: null,
      visibility: "private",
      last_seen_at: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      ...overrides,
    };
  }

  it("flags an online local runtime as self-healing", () => {
    expect(
      isSelfHealingRuntime(
        makeRuntime({ runtime_mode: "local", status: "online" }),
      ),
    ).toBe(true);
  });

  it("treats an offline local runtime as safe to delete", () => {
    // Daemon isn't running, so the server-side delete is final — no
    // re-registration race to worry about.
    expect(
      isSelfHealingRuntime(
        makeRuntime({ runtime_mode: "local", status: "offline" }),
      ),
    ).toBe(false);
  });

  it("treats cloud runtimes as safe to delete regardless of status", () => {
    // Cloud workers are managed by Fleet, not a self-restarting local daemon.
    expect(
      isSelfHealingRuntime(
        makeRuntime({ runtime_mode: "cloud", status: "online" }),
      ),
    ).toBe(false);
    expect(
      isSelfHealingRuntime(
        makeRuntime({ runtime_mode: "cloud", status: "offline" }),
      ),
    ).toBe(false);
  });
});

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

  it("prices the 1M-context Anthropic tag form (claude-opus-4-7[1m]) at the standard Opus tier", () => {
    // Claude Code reports the 1M-context beta as `claude-opus-4-7[1m]`.
    // Anthropic prices it at the standard Opus rate for prompts ≤200K
    // input tokens (with a 2× surcharge above that, which we can't see
    // from aggregated daily totals). Strip the bracketed context tag so
    // the tokens still land in the cost total at standard pricing —
    // mild under-estimate, but the alternative was excluding them
    // entirely (the bug this fixes).
    const cost = estimateCost({
      ...zeroUsage,
      model: "claude-opus-4-7[1m]",
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
    expect(isModelPriced("claude-opus-4-7[1m]")).toBe(true);
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

  // The Chinese-model rates below are spot-checked against the literal
  // numbers on the three official price sheets cited in MODEL_PRICING's
  // header comment. Pinning them in tests is what catches a future edit
  // that copies a price from a near-named neighbour by accident — the
  // mistake the previous attempt (PR #3170, closed) made.
  it("prices deepseek-v4-flash at the official $0.14/$0.28 with ~50× cache-hit discount", () => {
    // 1M input × $0.14 + 1M output × $0.28 + 1M cache read × $0.0028 = $0.4228.
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
    // The DeepSeek docs explicitly route both legacy names to v4-flash —
    // they must hit the same numbers, not the older $0.27/$1.10 tier.
    const flash = estimateCost({
      ...zeroUsage,
      model: "deepseek-v4-flash",
      input_tokens: 1_000_000,
    });
    expect(
      estimateCost({
        ...zeroUsage,
        model: "deepseek-chat",
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(flash, 5);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "deepseek-reasoner",
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(flash, 5);
  });

  it("prices kimi-k2.6 at the official $0.95 / $4.00 tier (not the K2 tier)", () => {
    // Moonshot's K2.6 page is the only authoritative source today; K2.6 is
    // explicitly NOT priced like K2. 1M input × $0.95 + 1M output × $4.00 = $4.95.
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
    // z.ai currently ships Free tiers for the *-flash family; $0 is the
    // literal price on the page, not a placeholder. Anything non-zero
    // here would mean we mis-copied a paid SKU's number into the row.
    expect(isModelPriced("glm-4.5-flash")).toBe(true);
    expect(isModelPriced("glm-4.7-flash")).toBe(true);
    expect(
      estimateCost({
        ...zeroUsage,
        model: "glm-4.5-flash",
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it("recognises the provider-prefixed forms emitted by OpenRouter-style runtimes", () => {
    // opencode + OpenRouter route IDs through as `<provider>/<model>`.
    // canonicalCandidates strips the prefix; without this the rows above
    // would only fire on bare IDs and the dashboard would still show
    // $0.00 for the runtime that actually triggered this work.
    expect(isModelPriced("deepseek/deepseek-v4-flash")).toBe(true);
    expect(isModelPriced("moonshotai/kimi-k2.6")).toBe(true);
    expect(isModelPriced("zhipuai/glm-5.1")).toBe(true);
    expect(isModelPriced("zhipuai/glm-4.5-air")).toBe(true);
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

// ---------------------------------------------------------------------------
// Calendar helpers + weekly aggregation. All of these run on YYYY-MM-DD
// strings (the wire shape of RuntimeUsage.date) and on a runtime-supplied
// IANA timezone — the host browser's tz should never affect the result.
// ---------------------------------------------------------------------------

describe("weekStartIso", () => {
  it("returns the Monday of the same ISO week", () => {
    // 2026-05-19 is a Tuesday → Monday is 2026-05-18.
    expect(weekStartIso("2026-05-19")).toBe("2026-05-18");
  });

  it("treats Monday as the start of its own week (idempotent)", () => {
    expect(weekStartIso("2026-05-18")).toBe("2026-05-18");
  });

  it("rolls Sunday back to the previous Monday", () => {
    // 2026-05-17 is a Sunday → Monday is 2026-05-11.
    expect(weekStartIso("2026-05-17")).toBe("2026-05-11");
  });

  it("crosses month and year boundaries", () => {
    // 2026-01-03 is a Saturday → Monday is 2025-12-29.
    expect(weekStartIso("2026-01-03")).toBe("2025-12-29");
  });
});

describe("addDaysIso", () => {
  it("adds across month boundary", () => {
    expect(addDaysIso("2026-05-30", 3)).toBe("2026-06-02");
  });

  it("subtracts across year boundary", () => {
    expect(addDaysIso("2026-01-02", -5)).toBe("2025-12-28");
  });
});

describe("todayIso", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("uses the runtime's timezone, not the host's, to decide today", () => {
    // 2026-05-19 16:00 UTC. In Asia/Shanghai (UTC+8) it's already 2026-05-20.
    // In America/Los_Angeles (UTC-7 on this date) it's still 2026-05-19.
    vi.setSystemTime(new Date("2026-05-19T16:00:00Z"));
    expect(todayIso("Asia/Shanghai")).toBe("2026-05-20");
    expect(todayIso("America/Los_Angeles")).toBe("2026-05-19");
    expect(todayIso("UTC")).toBe("2026-05-19");
  });
});

describe("sliceWindow (timezone-aware)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function makeUsage(date: string): RuntimeUsage {
    return {
      runtime_id: "r",
      date,
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: 0,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
  }

  it("cuts the current window at today-in-tz, not today-in-host-utc", () => {
    // Host clock is 2026-05-19 23:00 UTC → still May 19 in UTC, May 20 in Shanghai.
    // A daily-usage row dated 2026-05-20 (the runtime's "today" in Shanghai)
    // should be included in the current window when tz=Asia/Shanghai.
    vi.setSystemTime(new Date("2026-05-19T23:00:00Z"));
    const usage = [
      makeUsage("2026-05-13"),
      makeUsage("2026-05-19"),
      makeUsage("2026-05-20"),
    ];
    const { filtered } = sliceWindow(usage, 7, "Asia/Shanghai");
    expect(filtered.map((u) => u.date)).toEqual([
      "2026-05-13",
      "2026-05-19",
      "2026-05-20",
    ]);
  });

  it("returns the immediately prior window of equal length", () => {
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const usage = [
      makeUsage("2026-05-01"),
      makeUsage("2026-05-08"),
      makeUsage("2026-05-15"),
      makeUsage("2026-05-19"),
    ];
    const { filtered, prevFiltered } = sliceWindow(usage, 7, "UTC");
    expect(filtered.map((u) => u.date)).toEqual(["2026-05-15", "2026-05-19"]);
    expect(prevFiltered.map((u) => u.date)).toEqual(["2026-05-08"]);
  });
});

describe("aggregateByWeek", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function makeUsage(
    date: string,
    input: number,
    output: number,
  ): RuntimeUsage {
    return {
      runtime_id: "r",
      date,
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: input,
      output_tokens: output,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
  }

  it("groups daily rows into Mon-anchored ISO weeks", () => {
    // 2026-05-24 is Sunday, so the calendar week containing "today" is
    // Mon=05-18..Sun=05-24. With weekCount=2 the window covers weeks
    // 2026-05-11 and 2026-05-18 — exactly the two weeks the rows fall in.
    vi.setSystemTime(new Date("2026-05-24T12:00:00Z"));
    // 2026-05-11 is Mon; 2026-05-17 is Sun (same week).
    // 2026-05-18 is Mon (next week).
    const rows = [
      makeUsage("2026-05-11", 1_000_000, 0),
      makeUsage("2026-05-17", 0, 1_000_000),
      makeUsage("2026-05-18", 2_000_000, 0),
    ];
    const { weeklyTokens } = aggregateByWeek(rows, "UTC", 2);
    expect(weeklyTokens).toHaveLength(2);
    expect(weeklyTokens[0]).toMatchObject({
      weekStart: "2026-05-11",
      weekEnd: "2026-05-17",
      input: 1_000_000,
      output: 1_000_000,
      partial: false,
      daysCovered: 7,
    });
    expect(weeklyTokens[1]).toMatchObject({
      weekStart: "2026-05-18",
      weekEnd: "2026-05-24",
      input: 2_000_000,
      partial: false,
      daysCovered: 7,
    });
  });

  it("flags the in-progress week as partial with days-elapsed count", () => {
    // 2026-05-20 is a Wednesday (Mon=05-18, Sun=05-24).
    vi.setSystemTime(new Date("2026-05-20T08:00:00Z"));
    const rows = [makeUsage("2026-05-18", 1_000_000, 0)];
    const { weeklyTokens } = aggregateByWeek(rows, "UTC", 1);
    expect(weeklyTokens[0]).toMatchObject({
      weekStart: "2026-05-18",
      weekEnd: "2026-05-24",
      partial: true,
      daysCovered: 3, // Mon, Tue, Wed
    });
  });

  it("sums costs per week using the model pricing table", () => {
    // 2026-05-17 sits in the calendar week of 2026-05-11..2026-05-17, so
    // weekCount=1 anchors the window on that same week.
    vi.setSystemTime(new Date("2026-05-17T12:00:00Z"));
    // 1M input × $3 + 1M output × $15 = $18 per row. Two rows in the same
    // week (Mon + Wed) → $36 weekly total.
    const rows = [
      makeUsage("2026-05-11", 1_000_000, 1_000_000),
      makeUsage("2026-05-13", 1_000_000, 1_000_000),
    ];
    const { weeklyCostStack } = aggregateByWeek(rows, "UTC", 1);
    expect(weeklyCostStack).toHaveLength(1);
    expect(weeklyCostStack[0]?.total).toBeCloseTo(36, 2);
  });

  it("emits trailing calendar weeks pinned to today, dropping older populated weeks", () => {
    // Regression for MUL-2382 weekly window scoping:
    // before the fix, aggregateByWeek built buckets only for weeks that had
    // data and the caller did `.slice(-weekCount)`. With sparse data (an old
    // populated week far outside the selected window plus an empty stretch
    // closer to today), that slice would surface the OLD populated week
    // instead of the trailing in-window weeks. The chart should now show
    // exactly the trailing calendar weeks, with the empty in-range weeks
    // present as zero-valued buckets rather than disappearing.
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    // 30-day window @ 2026-05-19 → 5 trailing weeks (Mon=04-20, 04-27,
    // 05-04, 05-11, 05-18). 2026-04-13 (Mon) is one week earlier — outside
    // the window. No data in any of the 5 in-range weeks.
    const rows = [makeUsage("2026-04-13", 1_000_000, 1_000_000)];
    const { weeklyTokens, weeklyCostStack } = aggregateByWeek(rows, "UTC", 5);

    expect(weeklyTokens.map((w) => w.weekStart)).toEqual([
      "2026-04-20",
      "2026-04-27",
      "2026-05-04",
      "2026-05-11",
      "2026-05-18",
    ]);
    // Every in-range week is empty — the old populated week was dropped.
    for (const w of weeklyTokens) {
      expect(w.input).toBe(0);
      expect(w.output).toBe(0);
      expect(w.cacheRead).toBe(0);
      expect(w.cacheWrite).toBe(0);
    }
    for (const w of weeklyCostStack) {
      expect(w.total).toBe(0);
    }
  });

  it("keeps in-window weeks empty when nearby data sits inside the window", () => {
    // Sparse-but-in-range case: only the oldest in-window week has data;
    // the remaining trailing weeks must render as empty buckets, not be
    // collapsed to a single populated bar.
    vi.setSystemTime(new Date("2026-05-19T12:00:00Z"));
    const rows = [makeUsage("2026-04-22", 1_000_000, 1_000_000)]; // week of 04-20
    const { weeklyTokens } = aggregateByWeek(rows, "UTC", 5);
    expect(weeklyTokens).toHaveLength(5);
    expect(weeklyTokens[0]).toMatchObject({
      weekStart: "2026-04-20",
      input: 1_000_000,
      output: 1_000_000,
    });
    for (const w of weeklyTokens.slice(1)) {
      expect(w.input).toBe(0);
      expect(w.output).toBe(0);
    }
  });
});

// computeCostInWindow drives the runtime-list cost cell and its ↑/↓ delta.
// The `tz` argument was inserted as the THIRD positional parameter (before
// `offsetDays`) in the timezone-architecture RFC — a positional-arg slip
// here is otherwise silent, so the window math, the end-exclusive boundary,
// the offset shift, and the tz-of-"today" all need explicit coverage.
describe("computeCostInWindow", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  // claude-sonnet-4-6 is priced at $3 / 1M input tokens, so a row with
  // 1M input tokens contributes exactly $3.
  function priced(date: string, inputTokens: number): RuntimeUsage {
    return {
      runtime_id: "r",
      date,
      provider: "anthropic",
      model: "claude-sonnet-4-6",
      input_tokens: inputTokens,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
  }

  it("sums cost over the trailing daysBack window, end-exclusive of today", () => {
    // 2026-05-19 23:00 UTC is already 2026-05-20 in Asia/Shanghai, so
    // "today" is 2026-05-20 and the 7-day window is [2026-05-13, 2026-05-20).
    vi.setSystemTime(new Date("2026-05-19T23:00:00Z"));
    const rows = [
      priced("2026-05-12", 1_000_000), // before window — excluded
      priced("2026-05-13", 1_000_000), // window start — included
      priced("2026-05-19", 1_000_000), // included
      priced("2026-05-20", 1_000_000), // today — excluded (end-exclusive)
    ];
    expect(computeCostInWindow(rows, 7, "Asia/Shanghai")).toBeCloseTo(6, 5);
  });

  it("offsetDays shifts the window back to the prior period", () => {
    // today = 2026-05-20; offsetDays=7, daysBack=7 → window [05-06, 05-13).
    vi.setSystemTime(new Date("2026-05-20T12:00:00Z"));
    const rows = [
      priced("2026-05-05", 1_000_000), // before prior window — excluded
      priced("2026-05-06", 1_000_000), // prior window start — included
      priced("2026-05-12", 1_000_000), // included
      priced("2026-05-13", 1_000_000), // in the current window, not prior — excluded
    ];
    expect(computeCostInWindow(rows, 7, "UTC", 7)).toBeCloseTo(6, 5);
  });

  it("reads 'today' in the supplied tz, not the host clock", () => {
    // Host clock is 2026-05-19 in UTC but already 2026-05-20 in Shanghai.
    // A row dated 2026-05-19 falls inside the 1-day window only when the
    // tz pushes "today" forward to 2026-05-20.
    vi.setSystemTime(new Date("2026-05-19T20:00:00Z"));
    const rows = [priced("2026-05-19", 1_000_000)];
    expect(computeCostInWindow(rows, 1, "UTC")).toBe(0); // today=05-19, window [05-18,05-19)
    expect(computeCostInWindow(rows, 1, "Asia/Shanghai")).toBeCloseTo(3, 5);
  });

  it("returns 0 for an unpriced model rather than NaN", () => {
    vi.setSystemTime(new Date("2026-05-20T12:00:00Z"));
    const rows: RuntimeUsage[] = [
      { ...priced("2026-05-19", 1_000_000), model: "totally-made-up-model" },
    ];
    expect(computeCostInWindow(rows, 7, "UTC")).toBe(0);
  });

  it("returns 0 for an empty row set", () => {
    vi.setSystemTime(new Date("2026-05-20T12:00:00Z"));
    expect(computeCostInWindow([], 7, "UTC")).toBe(0);
  });
});
