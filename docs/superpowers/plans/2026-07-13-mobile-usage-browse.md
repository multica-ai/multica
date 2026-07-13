# Mobile Usage Browse (More page) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only workspace "Usage" page to mobile's More list — project filter, daily/weekly + period controls, 4 KPI cards, a metric-toggling trend chart, and a per-agent leaderboard — mirroring `packages/views/dashboard/components/dashboard-page.tsx`'s exact numbers.

**Architecture:** Business logic (cost estimation + aggregation) is mirrored, not imported, into two new `apps/mobile/lib/` files, since mobile cannot import from `packages/views`. A new `apps/mobile/data/queries/usage.ts` adds 4 read endpoints following the existing query-factory pattern. The screen itself (`apps/mobile/app/(app)/[workspace]/more/usage.tsx`) is built incrementally across 3 tasks (shell+KPIs, chart, leaderboard) on top of a new charting dependency (`react-native-gifted-charts`).

**Tech Stack:** Expo/React Native, TanStack Query, `@react-native-segmented-control/segmented-control`, `@expo/react-native-action-sheet`, `react-native-gifted-charts` (new), `react-native-svg` (already installed), vitest.

## Global Constraints

- Mirror `DashboardPage`'s exact behavior and numbers — this is a read-only port, not a redesign.
- Mobile package boundary: only `import type` from `@multica/core/types` and pure functions from `@multica/core` — never `packages/views`.
- Every `queryFn` must destructure and forward `{ signal }` (apps/mobile/CLAUDE.md Lesson 4 — `grep -n "queryFn: () =>" apps/mobile/data/queries/` must return zero matches).
- No custom-pricing override, no per-model/per-agent "cost by" tabs, no per-runtime `UsageSection` (surface A) — all out of scope per the approved design spec.
- Code comments in English; Chinese UI copy follows `apps/docs/content/docs/developers/conventions.mdx` and mirrors the existing web/desktop `packages/views/locales/{en,zh-Hans}/usage.json` wording where the same concept is being expressed.
- Reference spec: `docs/superpowers/specs/2026-07-13-mobile-usage-browse-design.md`.

---

## Task 1: Usage display logic — pricing + dashboard aggregation

**Files:**
- Create: `apps/mobile/lib/usage-pricing.ts`
- Create: `apps/mobile/lib/usage-pricing.test.ts`
- Create: `apps/mobile/lib/usage-display.ts`
- Create: `apps/mobile/lib/usage-display.test.ts`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces (for Tasks 3–5):
  - From `usage-pricing.ts`: `Priceable` (type), `CostBreakdown` (type), `estimateCost(usage: Priceable): number`, `estimateCostBreakdown(usage: Priceable): CostBreakdown`, `formatTokens(n: number): string`, `fmtMoney(n: number): string`.
  - From `usage-display.ts`: `DailyTokenData`, `DailyCostStack`, `DashboardTokenTotals`, `AgentCostRow`, `AgentDashboardRow`, `DailyTimeData`, `DailyTasksData`, `WeeklyTokenData`, `WeeklyCostStackData`, `WeeklyTimeData`, `WeeklyTasksData` (all types); `aggregateDailyCost(usage: DashboardUsageDaily[]): DailyCostStack[]`, `aggregateDailyTokens(usage: DashboardUsageDaily[]): DailyTokenData[]`, `computeDailyTotals(usage: DashboardUsageDaily[]): DashboardTokenTotals`, `aggregateAgentTokens(rows: DashboardUsageByAgent[]): AgentCostRow[]`, `mergeAgentDashboardRows(tokenRows: AgentCostRow[], runTimeRows: DashboardAgentRunTime[]): AgentDashboardRow[]`, `DELETED_AGENTS_ROW_ID` (string constant), `bucketUnknownAgentRows(rows: AgentDashboardRow[], knownAgentIds: ReadonlySet<string> | null): AgentDashboardRow[]`, `aggregateByWeek(usage, tz, weekCount): { weeklyTokens: WeeklyTokenData[]; weeklyCostStack: WeeklyCostStackData[] }`, `aggregateWeeklyTime(rows: DashboardRunTimeDaily[], tz: string, weekCount: number): WeeklyTimeData[]`, `aggregateWeeklyTasks(rows: DashboardRunTimeDaily[], tz: string, weekCount: number): WeeklyTasksData[]`, `aggregateDailyTime(rows: DashboardRunTimeDaily[]): DailyTimeData[]`, `aggregateDailyTasks(rows: DashboardRunTimeDaily[]): DailyTasksData[]`, `formatDuration(seconds: number, lessThanMinuteLabel: string): string`, `todayIso(tz: string): string`, `addDaysIso(iso: string, days: number): string`.

### Step 1: Write `usage-pricing.test.ts`

- [ ] Create `apps/mobile/lib/usage-pricing.test.ts`:

```ts
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
```

### Step 2: Run test to verify it fails

Run: `cd apps/mobile && pnpm test -- usage-pricing`
Expected: FAIL — `Cannot find module './usage-pricing'`

### Step 3: Write `usage-pricing.ts`

- [ ] Create `apps/mobile/lib/usage-pricing.ts`:

```ts
/**
 * Cost-estimation math for the mobile Usage page. Mirrors (does not
 * import — apps/mobile/CLAUDE.md forbids importing packages/views)
 * packages/views/runtimes/utils.ts's pricing section. Behavioral parity
 * requires this stay numerically identical to that file; when it changes
 * on web, sync this file. There is no custom-pricing override on mobile
 * (see the design spec's Non-goals) — an unmapped model always estimates
 * to $0 here, where web falls back to a per-browser Zustand override.
 *
 * Pricing per million tokens (USD). Sources, each authoritative for the
 * rows tagged under it — keep in sync when providers release new models
 * or adjust prices.
 *
 *   Anthropic: https://platform.claude.com/docs/en/about-claude/pricing
 *   OpenAI:    https://openai.com/api/pricing
 *   DeepSeek:  https://api-docs.deepseek.com/quick_start/pricing
 *   Moonshot:  https://www.kimi.com/resources/kimi-k2-6-pricing
 *   Zhipu:     https://docs.z.ai/guides/overview/pricing
 */
const MODEL_PRICING: Record<
  string,
  { input: number; output: number; cacheRead: number; cacheWrite: number }
> = {
  "claude-sonnet-5":     { input: 2,    output: 10,   cacheRead: 0.20, cacheWrite: 2.50 },
  "claude-fable-5":     { input: 10,   output: 50,   cacheRead: 1.00, cacheWrite: 12.50 },
  "claude-haiku-4-5":   { input: 1,    output: 5,    cacheRead: 0.10, cacheWrite: 1.25 },
  "claude-sonnet-4-5":  { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },
  "claude-sonnet-4-6":  { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },
  "claude-opus-4-5":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-6":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-7":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-8":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },

  "claude-opus-4-1":    { input: 15,   output: 75,   cacheRead: 1.50, cacheWrite: 18.75 },
  "claude-opus-4":      { input: 15,   output: 75,   cacheRead: 1.50, cacheWrite: 18.75 },

  "claude-sonnet-4":    { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },

  "claude-haiku-3-5":   { input: 0.80, output: 4,    cacheRead: 0.08, cacheWrite: 1.00 },

  "gpt-5.6-sol":        { input: 5,    output: 30,   cacheRead: 0.50,  cacheWrite: 6.25 },
  "gpt-5.6-terra":      { input: 2.50, output: 15,   cacheRead: 0.25,  cacheWrite: 3.125 },
  "gpt-5.6-luna":       { input: 1,    output: 6,    cacheRead: 0.10,  cacheWrite: 1.25 },
  "gpt-5.5":            { input: 5,    output: 30,   cacheRead: 0.50,  cacheWrite: 5 },
  "gpt-5.4-mini":       { input: 0.75, output: 4.50, cacheRead: 0.075, cacheWrite: 0.75 },
  "gpt-5.4":            { input: 2.50, output: 15,   cacheRead: 0.25,  cacheWrite: 2.50 },
  "gpt-5.3-codex":      { input: 1.75, output: 14,   cacheRead: 0.175, cacheWrite: 1.75 },

  "gpt-5-codex":        { input: 1.25, output: 10,   cacheRead: 0.125, cacheWrite: 1.25 },
  "gpt-5-mini":         { input: 0.25, output: 2,    cacheRead: 0.025, cacheWrite: 0.25 },
  "gpt-5-nano":         { input: 0.05, output: 0.40, cacheRead: 0.005, cacheWrite: 0.05 },
  "gpt-5":              { input: 1.25, output: 10,   cacheRead: 0.125, cacheWrite: 1.25 },

  "o3-mini":            { input: 1.10, output: 4.40, cacheRead: 0.55,  cacheWrite: 1.10 },
  "o3":                 { input: 2,    output: 8,    cacheRead: 0.50,  cacheWrite: 2 },
  "o4-mini":            { input: 1.10, output: 4.40, cacheRead: 0.275, cacheWrite: 1.10 },

  "gpt-4o-mini":        { input: 0.15, output: 0.60, cacheRead: 0.075, cacheWrite: 0.15 },
  "gpt-4o":             { input: 2.50, output: 10,   cacheRead: 1.25,  cacheWrite: 2.50 },

  "deepseek-v4-flash":  { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },
  "deepseek-v4-pro":    { input: 1.74, output: 3.48, cacheRead: 0.0145, cacheWrite: 1.74 },
  "deepseek-chat":      { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },
  "deepseek-reasoner":  { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },

  "kimi-k2.6":          { input: 0.95, output: 4.00, cacheRead: 0.16,   cacheWrite: 0.95 },

  "glm-5.1":            { input: 1.4,  output: 4.4,  cacheRead: 0.26,   cacheWrite: 1.4 },
  "glm-5":              { input: 1.0,  output: 3.2,  cacheRead: 0.2,    cacheWrite: 1.0 },
  "glm-5-turbo":        { input: 1.2,  output: 4.0,  cacheRead: 0.24,   cacheWrite: 1.2 },
  "glm-4.7":            { input: 0.6,  output: 2.2,  cacheRead: 0.11,   cacheWrite: 0.6 },
  "glm-4.7-flashx":     { input: 0.07, output: 0.4,  cacheRead: 0.01,   cacheWrite: 0.07 },
  "glm-4.7-flash":      { input: 0,    output: 0,    cacheRead: 0,      cacheWrite: 0 },
  "glm-4.6":            { input: 0.6,  output: 2.2,  cacheRead: 0.11,   cacheWrite: 0.6 },
  "glm-4.5":            { input: 0.6,  output: 2.2,  cacheRead: 0.11,   cacheWrite: 0.6 },
  "glm-4.5-x":          { input: 2.2,  output: 8.9,  cacheRead: 0.45,   cacheWrite: 2.2 },
  "glm-4.5-air":        { input: 0.2,  output: 1.1,  cacheRead: 0.03,   cacheWrite: 0.2 },
  "glm-4.5-airx":       { input: 1.1,  output: 4.5,  cacheRead: 0.22,   cacheWrite: 1.1 },
  "glm-4.5-flash":      { input: 0,    output: 0,    cacheRead: 0,      cacheWrite: 0 },

  "cursor/auto":              { input: 1.25, output: 6,    cacheRead: 0.25,   cacheWrite: 0 },
  "cursor/composer-2.5-fast": { input: 3,    output: 15,   cacheRead: 0.5,    cacheWrite: 0 },
  "cursor/composer-2.5":      { input: 0.5,  output: 2.5,  cacheRead: 0.2,    cacheWrite: 0 },
  "cursor/composer-2-fast":   { input: 1.5,  output: 7.5,  cacheRead: 0.35,   cacheWrite: 0 },
  "cursor/composer-2":        { input: 0.5,  output: 2.5,  cacheRead: 0.2,    cacheWrite: 0 },
  "cursor/composer-1.5":      { input: 3.5,  output: 17.5, cacheRead: 0.35,   cacheWrite: 0 },
  "cursor/composer-1":        { input: 1.25, output: 10,   cacheRead: 0.125,  cacheWrite: 0 },
  "cursor":                   { input: 3,    output: 15,   cacheRead: 0.5,    cacheWrite: 0 },
};

// Anything carrying per-model token totals can be priced. `provider` is
// optional so callers with provider-less rows still type-check; when
// present it disambiguates generic model ids during pricing.
export type Priceable = {
  model: string;
  provider?: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
};

export interface CostBreakdown {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

// Canonical provider token for keying: trimmed + lowercased.
function normalizeProvider(provider?: string): string {
  return provider?.trim().toLowerCase() ?? "";
}

// Provider-qualify a key, skipping the prefix when the key already carries
// this provider.
function qualify(provider: string, key: string): string {
  return key.startsWith(`${provider}/`) ? key : `${provider}/${key}`;
}

// Generate the lookup candidates for a model string, in priority order:
// raw string first, then canonicalized forms (strip provider prefix,
// Anthropic dot<->dash, strip trailing date snapshot, strip trailing
// `[1m]` context tag), deduped.
const canonicalCandidatesCache = new Map<string, string[]>();
function canonicalCandidates(model: string): string[] {
  const cached = canonicalCandidatesCache.get(model);
  if (cached) return cached;
  const seen = new Set<string>();
  const out: string[] = [];
  const push = (s: string) => {
    if (!s || seen.has(s)) return;
    seen.add(s);
    out.push(s);
  };
  const stripDate = (s: string) =>
    s.replace(/-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$/, "");
  const stripProvider = (s: string) => {
    const i = s.indexOf("/");
    return i > 0 && /^[a-z][a-z0-9_-]*$/i.test(s.slice(0, i)) ? s.slice(i + 1) : s;
  };
  const canonAnthropic = (s: string) =>
    s.startsWith("claude-") ? s.replace(/\./g, "-") : s;
  const stripContextTag = (s: string) => s.replace(/\[[^\]]+\]$/, "");

  const raw = model;
  const noProvider = stripProvider(raw);
  const dashed = canonAnthropic(noProvider);
  const noTag = stripContextTag(dashed);

  push(raw);
  push(noProvider);
  push(dashed);
  push(noTag);
  push(stripDate(raw));
  push(stripDate(noProvider));
  push(stripDate(dashed));
  push(stripDate(noTag));
  canonicalCandidatesCache.set(model, out);
  return out;
}

// Lookup keys for a (model, provider) pair: every canonical candidate
// `${provider}/`-qualified first (when a provider is known), then the
// bare candidates.
function pricingCandidates(model: string, provider?: string): string[] {
  const base = canonicalCandidates(model);
  const p = normalizeProvider(provider);
  if (!p) return base;
  return [...base.map((c) => qualify(p, c)), ...base];
}

// Resolve a model string to its pricing tier. No custom-pricing fallback
// on mobile (see file header) — unmapped models simply return undefined.
function resolvePricing(model: string, provider?: string) {
  if (!model) return undefined;
  const candidates = pricingCandidates(model, provider);
  for (const candidate of candidates) {
    const hit = MODEL_PRICING[candidate];
    if (hit) return hit;
  }
  return undefined;
}

export function estimateCost(usage: Priceable): number {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) return 0;
  return (
    (usage.input_tokens * pricing.input +
      usage.output_tokens * pricing.output +
      usage.cache_read_tokens * pricing.cacheRead +
      usage.cache_write_tokens * pricing.cacheWrite) /
    1_000_000
  );
}

export function estimateCostBreakdown(usage: Priceable): CostBreakdown {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) {
    return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
  }
  return {
    input: (usage.input_tokens * pricing.input) / 1_000_000,
    output: (usage.output_tokens * pricing.output) / 1_000_000,
    cacheRead: (usage.cache_read_tokens * pricing.cacheRead) / 1_000_000,
    cacheWrite: (usage.cache_write_tokens * pricing.cacheWrite) / 1_000_000,
  };
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return m % 1 < 0.05 ? `${Math.round(m)}M` : `${m.toFixed(1)}M`;
  }
  if (n >= 1_000) {
    const k = n / 1_000;
    return k % 1 < 0.05 ? `${Math.round(k)}K` : `${k.toFixed(1)}K`;
  }
  return n.toLocaleString();
}

export function fmtMoney(n: number): string {
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
}
```

### Step 4: Run test to verify it passes

Run: `cd apps/mobile && pnpm test -- usage-pricing`
Expected: PASS (all cases green)

### Step 5: Write `usage-display.test.ts`

- [ ] Create `apps/mobile/lib/usage-display.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  aggregateAgentTokens,
  bucketUnknownAgentRows,
  computeDailyTotals,
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  mergeAgentDashboardRows,
} from "./usage-display";

describe("computeDailyTotals", () => {
  it("sums tokens, cost, and task count across all rows", () => {
    const totals = computeDailyTotals([
      {
        date: "2026-07-01",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 2,
      },
      {
        date: "2026-07-02",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 3,
      },
    ]);
    expect(totals.input).toBe(2_000_000);
    expect(totals.cost).toBeCloseTo(6, 5); // 2M x $3/1M
    expect(totals.taskCount).toBe(5);
  });
});

describe("aggregateAgentTokens", () => {
  it("folds per-(agent, model) rows into one row per agent, sorted cost desc", () => {
    const rows = aggregateAgentTokens([
      {
        agent_id: "agent-a",
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
      {
        agent_id: "agent-b",
        provider: "anthropic",
        model: "claude-opus-4-7",
        input_tokens: 1_000_000,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        task_count: 1,
      },
    ]);
    expect(rows.map((r) => r.agentId)).toEqual(["agent-b", "agent-a"]); // opus ($5) > sonnet ($3)
  });
});

describe("mergeAgentDashboardRows", () => {
  it("prefers the run-time rollup's task count over the token rollup's", () => {
    const merged = mergeAgentDashboardRows(
      [{ agentId: "agent-a", tokens: 1000, cost: 1, taskCount: 5 }],
      [{ agent_id: "agent-a", total_seconds: 120, task_count: 3, failed_count: 0 }],
    );
    expect(merged[0]).toMatchObject({
      agentId: "agent-a",
      tokens: 1000,
      cost: 1,
      seconds: 120,
      taskCount: 3, // from run-time rollup, not the token rollup's 5
    });
  });

  it("includes agents with run-time rows but zero tokens (errored before producing usage)", () => {
    const merged = mergeAgentDashboardRows(
      [],
      [{ agent_id: "agent-a", total_seconds: 5, task_count: 1, failed_count: 1 }],
    );
    expect(merged).toEqual([
      { agentId: "agent-a", tokens: 0, cost: 0, seconds: 5, taskCount: 1 },
    ]);
  });
});

describe("bucketUnknownAgentRows", () => {
  it("folds rows for hard-deleted agents into one synthetic row", () => {
    const rows = [
      { agentId: "known-agent", tokens: 100, cost: 1, seconds: 10, taskCount: 1 },
      { agentId: "deleted-agent", tokens: 50, cost: 0.5, seconds: 5, taskCount: 1 },
    ];
    const bucketed = bucketUnknownAgentRows(rows, new Set(["known-agent"]));
    expect(bucketed).toHaveLength(2);
    expect(bucketed[1]).toEqual({
      agentId: DELETED_AGENTS_ROW_ID,
      tokens: 50,
      cost: 0.5,
      seconds: 0,
      taskCount: 0,
    });
  });

  it("passes rows through untouched while the agent list is still loading (knownAgentIds is null)", () => {
    const rows = [{ agentId: "any-agent", tokens: 1, cost: 1, seconds: 1, taskCount: 1 }];
    expect(bucketUnknownAgentRows(rows, null)).toBe(rows);
  });

  it("returns the rows unchanged when nothing is deleted", () => {
    const rows = [{ agentId: "known-agent", tokens: 1, cost: 1, seconds: 1, taskCount: 1 }];
    expect(bucketUnknownAgentRows(rows, new Set(["known-agent"]))).toEqual(rows);
  });
});

describe("formatDuration", () => {
  it("shows the less-than-a-minute label for sub-second durations", () => {
    expect(formatDuration(0.4, "<1m")).toBe("<1m");
  });

  it("formats sub-minute durations in seconds", () => {
    expect(formatDuration(45, "<1m")).toBe("45s");
  });

  it("formats sub-hour durations as minutes and seconds", () => {
    expect(formatDuration(90, "<1m")).toBe("1m 30s");
  });

  it("drops the seconds segment when it's exactly zero", () => {
    expect(formatDuration(120, "<1m")).toBe("2m");
  });

  it("formats multi-hour durations as hours and minutes", () => {
    expect(formatDuration(3720, "<1m")).toBe("1h 2m");
  });

  it("formats multi-day durations as days and hours", () => {
    expect(formatDuration(90000, "<1m")).toBe("1d 1h");
  });
});
```

### Step 6: Run test to verify it fails

Run: `cd apps/mobile && pnpm test -- usage-display`
Expected: FAIL — `Cannot find module './usage-display'`

### Step 7: Write `usage-display.ts`

- [ ] Create `apps/mobile/lib/usage-display.ts`:

```ts
/**
 * Workspace-Usage-page aggregation. Mirrors (does not import)
 * packages/views/dashboard/utils.ts, plus the shared date/week helpers
 * from packages/views/runtimes/utils.ts that both surfaces reuse
 * (aggregateByWeek, todayIso, addDaysIso, weekStartIso, formatShortDate).
 * Behavioral parity requires this stay numerically identical to those
 * files; when either changes on web, sync this file.
 *
 * Deliberately NOT ported (out of scope — see the design spec's
 * Non-goals): sliceWindow, aggregateByDate, estimateCacheSavings,
 * isModelPriced/modelGroupingKey/aggregateCostByAgent/aggregateCostByModel
 * — all of these are used only by the per-runtime UsageSection (surface
 * A), never by DashboardPage. Do not add them here without re-verifying
 * they're actually needed.
 */
import type {
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
} from "@multica/core/types";
import { estimateCost, estimateCostBreakdown } from "./usage-pricing";

// ---------------------------------------------------------------------------
// Calendar helpers — all date math runs on YYYY-MM-DD strings in the
// viewing timezone. Pure string/UTC math so DST transitions never shift
// a result by an hour into a neighbouring day.
// ---------------------------------------------------------------------------

export function todayIso(tz: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: tz,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
}

export function addDaysIso(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  dt.setUTCDate(dt.getUTCDate() + days);
  return dt.toISOString().slice(0, 10);
}

// Monday-of-week as YYYY-MM-DD (ISO 8601 week-start).
function weekStartIso(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  const day = dt.getUTCDay(); // 0 = Sun, 1 = Mon, ..., 6 = Sat
  const offset = (day + 6) % 7; // distance back to Monday
  dt.setUTCDate(dt.getUTCDate() - offset);
  return dt.toISOString().slice(0, 10);
}

// "May 12" — short month/day for a YYYY-MM-DD string.
function formatShortDate(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  return dt.toLocaleString("en", { month: "short", day: "numeric", timeZone: "UTC" });
}

function diffDaysIso(from: string, to: string): number {
  const [y1, m1, d1] = from.split("-").map(Number);
  const [y2, m2, d2] = to.split("-").map(Number);
  const a = Date.UTC(y1 ?? 1970, (m1 ?? 1) - 1, d1 ?? 1);
  const b = Date.UTC(y2 ?? 1970, (m2 ?? 1) - 1, d2 ?? 1);
  return Math.round((b - a) / 86_400_000);
}

function formatDateLabel(d: string): string {
  const date = new Date(d + "T00:00:00");
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

// ---------------------------------------------------------------------------
// Daily aggregations
// ---------------------------------------------------------------------------

export interface DailyCostStack {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export function aggregateDailyCost(usage: DashboardUsageDaily[]): DailyCostStack[] {
  const map = new Map<string, { input: number; output: number; cacheWrite: number }>();
  for (const u of usage) {
    const b = estimateCostBreakdown(u);
    const entry = map.get(u.date) ?? { input: 0, output: 0, cacheWrite: 0 };
    entry.input += b.input;
    entry.output += b.output;
    entry.cacheWrite += b.cacheWrite;
    map.set(u.date, entry);
  }
  const round = (n: number) => Math.round(n * 100) / 100;
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, s]) => {
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return { date, label: formatDateLabel(date), input, output, cacheWrite, total: round(input + output + cacheWrite) };
    });
}

export interface DailyTokenData {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export function aggregateDailyTokens(usage: DashboardUsageDaily[]): DailyTokenData[] {
  const map = new Map<string, { input: number; output: number; cacheRead: number; cacheWrite: number }>();
  for (const u of usage) {
    const entry = map.get(u.date) ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
    entry.input += u.input_tokens;
    entry.output += u.output_tokens;
    entry.cacheRead += u.cache_read_tokens;
    entry.cacheWrite += u.cache_write_tokens;
    map.set(u.date, entry);
  }
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, t]) => ({ date, label: formatDateLabel(date), ...t }));
}

export interface DailyTimeData {
  date: string;
  label: string;
  totalSeconds: number;
}

export function aggregateDailyTime(rows: DashboardRunTimeDaily[]): DailyTimeData[] {
  return rows
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => ({ date: r.date, label: formatDateLabel(r.date), totalSeconds: r.total_seconds }));
}

export interface DailyTasksData {
  date: string;
  label: string;
  completed: number;
  failed: number;
}

export function aggregateDailyTasks(rows: DashboardRunTimeDaily[]): DailyTasksData[] {
  return rows
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => {
      const failed = r.failed_count;
      const completed = Math.max(0, r.task_count - failed);
      return { date: r.date, label: formatDateLabel(r.date), completed, failed };
    });
}

export interface DashboardTokenTotals {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  taskCount: number;
}

export function computeDailyTotals(usage: DashboardUsageDaily[]): DashboardTokenTotals {
  return usage.reduce<DashboardTokenTotals>(
    (acc, u) => ({
      input: acc.input + u.input_tokens,
      output: acc.output + u.output_tokens,
      cacheRead: acc.cacheRead + u.cache_read_tokens,
      cacheWrite: acc.cacheWrite + u.cache_write_tokens,
      cost: acc.cost + estimateCost(u),
      taskCount: acc.taskCount + u.task_count,
    }),
    { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, taskCount: 0 },
  );
}

// ---------------------------------------------------------------------------
// Per-agent aggregations
// ---------------------------------------------------------------------------

export interface AgentCostRow {
  agentId: string;
  tokens: number;
  cost: number;
  taskCount: number;
}

export function aggregateAgentTokens(rows: DashboardUsageByAgent[]): AgentCostRow[] {
  const map = new Map<string, AgentCostRow>();
  for (const r of rows) {
    const entry = map.get(r.agent_id) ?? { agentId: r.agent_id, tokens: 0, cost: 0, taskCount: 0 };
    entry.tokens += r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    entry.taskCount += r.task_count;
    map.set(r.agent_id, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

export interface AgentDashboardRow {
  agentId: string;
  tokens: number;
  cost: number;
  seconds: number;
  taskCount: number;
}

// taskCount comes from runTimeRows when available (a true per-agent
// distinct count); the token rollup double-counts a task that spans
// multiple models, so it's only used as a fallback for agents with no
// terminal run yet. Sorted by cost desc, then run time desc.
export function mergeAgentDashboardRows(
  tokenRows: AgentCostRow[],
  runTimeRows: DashboardAgentRunTime[],
): AgentDashboardRow[] {
  const runTimeByAgent = new Map(runTimeRows.map((r) => [r.agent_id, r] as const));
  const merged = new Map<string, AgentDashboardRow>();
  for (const r of tokenRows) {
    const rt = runTimeByAgent.get(r.agentId);
    merged.set(r.agentId, {
      agentId: r.agentId,
      tokens: r.tokens,
      cost: r.cost,
      seconds: rt?.total_seconds ?? 0,
      taskCount: rt ? rt.task_count : r.taskCount,
    });
  }
  for (const r of runTimeRows) {
    if (merged.has(r.agent_id)) continue;
    merged.set(r.agent_id, { agentId: r.agent_id, tokens: 0, cost: 0, seconds: r.total_seconds, taskCount: r.task_count });
  }
  return Array.from(merged.values()).toSorted((a, b) => {
    if (b.cost !== a.cost) return b.cost - a.cost;
    return b.seconds - a.seconds;
  });
}

// Synthetic agentId for the row that aggregates all hard-deleted agents.
export const DELETED_AGENTS_ROW_ID = "__deleted_agents__";

// Fold usage rows whose agent no longer exists into one aggregated
// "Deleted agents" row instead of dropping them (dropping would make
// sum(visible rows) != KPI total). knownAgentIds is null while the agent
// list is still loading — pass rows through untouched in that case.
export function bucketUnknownAgentRows(
  rows: AgentDashboardRow[],
  knownAgentIds: ReadonlySet<string> | null,
): AgentDashboardRow[] {
  if (!knownAgentIds) return rows;
  const known: AgentDashboardRow[] = [];
  const bucket: AgentDashboardRow = { agentId: DELETED_AGENTS_ROW_ID, tokens: 0, cost: 0, seconds: 0, taskCount: 0 };
  let hasDeleted = false;
  for (const r of rows) {
    if (knownAgentIds.has(r.agentId)) {
      known.push(r);
      continue;
    }
    hasDeleted = true;
    bucket.tokens += r.tokens;
    bucket.cost += r.cost;
  }
  return hasDeleted ? [...known, bucket] : known;
}

// ---------------------------------------------------------------------------
// Weekly aggregations
// ---------------------------------------------------------------------------

interface WeekShell {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
}

function buildWeekShells(tz: string, weekCount: number): WeekShell[] {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);
  const shells: WeekShell[] = [];
  for (let i = 0; i < count; i++) {
    const weekStart = addDaysIso(firstWeekStart, i * 7);
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    const clampedToday = today < weekStart ? weekStart : today < weekEnd ? today : weekEnd;
    const elapsed = Math.min(7, Math.max(1, diffDaysIso(weekStart, clampedToday) + 1));
    shells.push({
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} - ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsed : 7,
    });
  }
  return shells;
}

export interface WeeklyTimeData extends WeekShell {
  totalSeconds: number;
}

export function aggregateWeeklyTime(rows: DashboardRunTimeDaily[], tz: string, weekCount: number): WeeklyTimeData[] {
  const shells = buildWeekShells(tz, weekCount);
  const totals = new Map<string, number>();
  for (const shell of shells) totals.set(shell.weekStart, 0);
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    if (!totals.has(wkStart)) continue;
    totals.set(wkStart, (totals.get(wkStart) ?? 0) + r.total_seconds);
  }
  return shells.map((s) => ({ ...s, totalSeconds: totals.get(s.weekStart) ?? 0 }));
}

export interface WeeklyTasksData extends WeekShell {
  completed: number;
  failed: number;
}

export function aggregateWeeklyTasks(rows: DashboardRunTimeDaily[], tz: string, weekCount: number): WeeklyTasksData[] {
  const shells = buildWeekShells(tz, weekCount);
  const buckets = new Map<string, { completed: number; failed: number }>();
  for (const shell of shells) buckets.set(shell.weekStart, { completed: 0, failed: 0 });
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    const bucket = buckets.get(wkStart);
    if (!bucket) continue;
    const failed = r.failed_count;
    const completed = Math.max(0, r.task_count - failed);
    bucket.completed += completed;
    bucket.failed += failed;
  }
  return shells.map((s) => {
    const b = buckets.get(s.weekStart) ?? { completed: 0, failed: 0 };
    return { ...s, completed: b.completed, failed: b.failed };
  });
}

type WeeklyAggregable = {
  date: string;
  model: string;
  provider?: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
};

export interface WeeklyTokenData extends WeekShell {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface WeeklyCostStackData extends WeekShell {
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export function aggregateByWeek(
  usage: readonly WeeklyAggregable[],
  tz: string,
  weekCount: number,
): { weeklyTokens: WeeklyTokenData[]; weeklyCostStack: WeeklyCostStackData[] } {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);

  type TokenAgg = { weekStart: string; input: number; output: number; cacheRead: number; cacheWrite: number };
  const tokenMap = new Map<string, TokenAgg>();
  const stackMap = new Map<string, { input: number; output: number; cacheWrite: number }>();

  for (let i = 0; i < count; i++) {
    const wkStart = addDaysIso(firstWeekStart, i * 7);
    tokenMap.set(wkStart, { weekStart: wkStart, input: 0, output: 0, cacheRead: 0, cacheWrite: 0 });
    stackMap.set(wkStart, { input: 0, output: 0, cacheWrite: 0 });
  }

  for (const u of usage) {
    const wkStart = weekStartIso(u.date);
    if (wkStart < firstWeekStart || wkStart > currentWeekStart) continue;
    const tokens = tokenMap.get(wkStart);
    if (!tokens) continue;
    tokens.input += u.input_tokens;
    tokens.output += u.output_tokens;
    tokens.cacheRead += u.cache_read_tokens;
    tokens.cacheWrite += u.cache_write_tokens;

    const breakdown = estimateCostBreakdown(u);
    const stack = stackMap.get(wkStart);
    if (!stack) continue;
    stack.input += breakdown.input;
    stack.output += breakdown.output;
    stack.cacheWrite += breakdown.cacheWrite;
  }

  const decorate = (weekStart: string): WeekShell => {
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    const elapsedDays = Math.min(
      7,
      Math.max(1, diffDaysIso(weekStart, today < weekStart ? weekStart : today < weekEnd ? today : weekEnd) + 1),
    );
    return {
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} - ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsedDays : 7,
    };
  };

  const weeklyTokens: WeeklyTokenData[] = Array.from(tokenMap.values())
    .toSorted((a, b) => a.weekStart.localeCompare(b.weekStart))
    .map((t) => ({ ...decorate(t.weekStart), input: t.input, output: t.output, cacheRead: t.cacheRead, cacheWrite: t.cacheWrite }));

  const weeklyCostStack: WeeklyCostStackData[] = Array.from(stackMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([weekStart, s]) => {
      const round = (n: number) => Math.round(n * 100) / 100;
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return { ...decorate(weekStart), input, output, cacheWrite, total: round(input + output + cacheWrite) };
    });

  return { weeklyTokens, weeklyCostStack };
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// Compact human duration: "1h 23m" / "12m 30s" / "45s" / "<1m".
export function formatDuration(seconds: number, lessThanMinuteLabel: string): string {
  if (seconds < 0 || !Number.isFinite(seconds)) return lessThanMinuteLabel;
  if (seconds < 60) {
    if (seconds < 1) return lessThanMinuteLabel;
    return `${Math.round(seconds)}s`;
  }
  const totalMinutes = Math.floor(seconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  if (hours === 0) {
    const secs = Math.floor(seconds) % 60;
    return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
  }
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const h = hours % 24;
    return h > 0 ? `${days}d ${h}h` : `${days}d`;
  }
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}
```

### Step 8: Run test to verify it passes

Run: `cd apps/mobile && pnpm test -- usage-display`
Expected: PASS (all cases green)

### Step 9: Typecheck and lint

Run: `cd apps/mobile && pnpm typecheck && pnpm lint`
Expected: no new errors (0 errors; existing pre-existing warnings unrelated to these files are fine)

### Step 10: Commit

```bash
git add apps/mobile/lib/usage-pricing.ts apps/mobile/lib/usage-pricing.test.ts apps/mobile/lib/usage-display.ts apps/mobile/lib/usage-display.test.ts
git commit -m "feat(mobile): port Usage page cost-estimation and aggregation logic"
```

---

## Task 2: Data layer — viewing timezone + API + query options

**Files:**
- Create: `apps/mobile/lib/use-viewing-timezone.ts`
- Modify: `apps/mobile/data/api.ts`
- Create: `apps/mobile/data/queries/usage.ts`

**Interfaces:**
- Consumes: nothing from Task 1 (independent).
- Produces (for Tasks 3-5): `useViewingTimezone(): string`; `usageKeys` (factory); `dashboardUsageDailyOptions(wsId, days, projectId, tz)`, `dashboardUsageByAgentOptions(wsId, days, projectId, tz)`, `dashboardAgentRunTimeOptions(wsId, days, projectId, tz)`, `dashboardRunTimeDailyOptions(wsId, days, projectId, tz)` (each a `queryOptions(...)` factory, resolving to `DashboardUsageDaily[]` / `DashboardUsageByAgent[]` / `DashboardAgentRunTime[]` / `DashboardRunTimeDaily[]` respectively).

### Step 1: Write `use-viewing-timezone.ts`

- [ ] Create `apps/mobile/lib/use-viewing-timezone.ts`:

```ts
/**
 * Mirrors packages/views/common/use-viewing-timezone.ts's fallback chain:
 * stored user preference, else device-detected, else UTC. Behavioral
 * parity requires the Usage page slice the same "today" boundary as web
 * for the same account, so this must not fall back to a different
 * default.
 */
import * as Localization from "expo-localization";
import { useAuthStore } from "@/data/auth-store";

export function useViewingTimezone(): string {
  const stored = useAuthStore((s) => s.user?.timezone ?? null);
  if (stored && stored.trim() !== "") return stored;
  return Localization.getCalendars()[0]?.timeZone ?? "UTC";
}
```

### Step 2: Add the 4 Usage API methods to `api.ts`

- [ ] In `apps/mobile/data/api.ts`, add near the other read methods (following the `searchProjects` pattern — build `URLSearchParams`, call `this.fetch<unknown>`, then `parseWithFallback`):

```ts
  async getDashboardUsageDaily(
    params: { days: number; project_id?: string | null; tz: string },
    opts?: { signal?: AbortSignal },
  ): Promise<DashboardUsageDaily[]> {
    const search = new URLSearchParams();
    search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/dashboard/usage/daily?${search}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, DashboardUsageDailyListSchema, [], {
      endpoint: "GET /api/dashboard/usage/daily",
    });
  }

  async getDashboardUsageByAgent(
    params: { days: number; project_id?: string | null; tz: string },
    opts?: { signal?: AbortSignal },
  ): Promise<DashboardUsageByAgent[]> {
    const search = new URLSearchParams();
    search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/dashboard/usage/by-agent?${search}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, DashboardUsageByAgentListSchema, [], {
      endpoint: "GET /api/dashboard/usage/by-agent",
    });
  }

  async getDashboardAgentRunTime(
    params: { days: number; project_id?: string | null; tz: string },
    opts?: { signal?: AbortSignal },
  ): Promise<DashboardAgentRunTime[]> {
    const search = new URLSearchParams();
    search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/dashboard/agent-runtime?${search}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, DashboardAgentRunTimeListSchema, [], {
      endpoint: "GET /api/dashboard/agent-runtime",
    });
  }

  async getDashboardRunTimeDaily(
    params: { days: number; project_id?: string | null; tz: string },
    opts?: { signal?: AbortSignal },
  ): Promise<DashboardRunTimeDaily[]> {
    const search = new URLSearchParams();
    search.set("days", String(params.days));
    if (params.project_id) search.set("project_id", params.project_id);
    search.set("tz", params.tz);
    const raw = await this.fetch<unknown>(
      `/api/dashboard/runtime/daily?${search}`,
      { signal: opts?.signal },
    );
    return parseWithFallback(raw, DashboardRunTimeDailyListSchema, [], {
      endpoint: "GET /api/dashboard/runtime/daily",
    });
  }
```

- [ ] Add the matching imports near the top of `apps/mobile/data/api.ts`, alongside the file's existing `@multica/core/api/schemas` and `@multica/core/types` imports:

```ts
import {
  DashboardUsageDailyListSchema,
  DashboardUsageByAgentListSchema,
  DashboardAgentRunTimeListSchema,
  DashboardRunTimeDailyListSchema,
} from "@multica/core/api/schemas";
import type {
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
} from "@multica/core/types";
```

(If `apps/mobile/data/api.ts` already imports from `@multica/core/api/schemas` or `@multica/core/types` elsewhere in the file, merge these into the existing import statements instead of duplicating them.)

### Step 3: Write `apps/mobile/data/queries/usage.ts`

- [ ] Create `apps/mobile/data/queries/usage.ts`:

```ts
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const usageKeys = {
  all: (wsId: string | null) => ["usage", wsId] as const,
  daily: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "daily", days, projectId, tz] as const,
  byAgent: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "by-agent", days, projectId, tz] as const,
  agentRuntime: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "agent-runtime", days, projectId, tz] as const,
  runtimeDaily: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "runtime-daily", days, projectId, tz] as const,
};

// 60s: matches web's dashboardKeys STALE_TIME (packages/core/dashboard/queries.ts) —
// the server rolls up usage on a 5-min cadence, so sub-minute refetches
// would just repeat the same numbers.
const STALE_TIME = 60 * 1000;

export const dashboardUsageDailyOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.daily(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardUsageDaily({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });

export const dashboardUsageByAgentOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.byAgent(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardUsageByAgent({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });

export const dashboardAgentRunTimeOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.agentRuntime(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardAgentRunTime({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });

export const dashboardRunTimeDailyOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.runtimeDaily(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardRunTimeDaily({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
```

### Step 4: Typecheck and lint

Run: `cd apps/mobile && pnpm typecheck && pnpm lint`
Expected: no new errors. If `expo-localization`'s `getCalendars` type isn't recognized, confirm the installed `expo-localization` version (already `~55.0.16` per Task-4-adjacent work earlier this branch) exports it — it does, `getCalendars()` has shipped since SDK 51.

### Step 5: Verify no bare `queryFn: () =>` slipped in

Run: `grep -n "queryFn: () =>" apps/mobile/data/queries/usage.ts`
Expected: no output (every `queryFn` above destructures `{ signal }`)

### Step 6: Commit

```bash
git add apps/mobile/lib/use-viewing-timezone.ts apps/mobile/data/api.ts apps/mobile/data/queries/usage.ts
git commit -m "feat(mobile): add Usage page data layer (viewing tz + 4 dashboard endpoints)"
```

---

## Task 3: Charting dependency + nav entry + screen shell + controls + KPI row

**Files:**
- Modify: `apps/mobile/package.json` (+ `apps/mobile/app.config.ts` if the install step requires a plugin entry, mirroring the pattern used for `expo-image` earlier this branch)
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`
- Create: `apps/mobile/locales/en/usage.json`
- Create: `apps/mobile/locales/zh-Hans/usage.json`
- Modify: `apps/mobile/locales/index.ts`
- Create: `apps/mobile/app/(app)/[workspace]/more/usage.tsx`

**Interfaces:**
- Consumes: `usageKeys`/`dashboardUsage*Options` (Task 2), `computeDailyTotals`/`formatDuration`/`fmtMoney`/`formatTokens` (Tasks 1-2), `useViewingTimezone` (Task 2), `projectListOptions` (existing, `apps/mobile/data/queries/projects.ts`), `agentListOptions` (existing, `apps/mobile/data/queries/agents.ts`).
- Produces (for Tasks 4-5): the `more/usage.tsx` screen's internal `dim`/`days`/`projectId` state and the already-fetched `dailyUsage`/`byAgentUsage`/`runTimeRows`/`runTimeDailyRows` arrays, ready for Task 4 (chart) and Task 5 (leaderboard) to extend in place. `components/usage/usage-stat-card.tsx` exporting `UsageStatCard({ label, value, hint }: { label: string; value: string; hint?: string })`.

### Step 1: Install the charting dependency

Run: `cd apps/mobile && npx expo install react-native-gifted-charts expo-linear-gradient`

Expected: `apps/mobile/package.json` gains `"react-native-gifted-charts": "^1.4.77"` (or whatever the installed SDK-compatible patch resolves to) and `"expo-linear-gradient": "~<sdk-55-version>"`. If the install step errors trying to auto-register a config plugin (as happened earlier this branch for `expo-image`, because `app.config.ts` is a dynamic TS config the CLI can't auto-write to), add the plugin manually:

- [ ] In `apps/mobile/app.config.ts`, add `"expo-linear-gradient"` to the `plugins` array (any position; match the existing bare-string entries like `"expo-secure-store"`).

### Step 2: Confirm the install

Run: `cd apps/mobile && npx expo install --check`
Expected: `Dependencies are up to date` (or only pre-existing unrelated mismatches, if any existed before this task)

### Step 3: Add i18n namespace

- [ ] Create `apps/mobile/locales/en/usage.json`:

```json
{
  "list": {
    "header_title": "Usage"
  },
  "subtitle": "Token spend and agent activity across this workspace.",
  "filter": {
    "all_projects": "All projects"
  },
  "kpi": {
    "cost_label": "Cost · {{days}}D",
    "tokens_label": "Tokens · {{days}}D",
    "tokens_hint": "Input {{input}} · Output {{output}}",
    "run_time_label": "Run time · {{days}}D",
    "run_time_hint": "Across {{tasks}} tasks",
    "tasks_label": "Tasks · {{days}}D",
    "tasks_hint": "{{failed}} failed"
  },
  "dim": {
    "daily": "Daily",
    "weekly": "Weekly"
  },
  "metric": {
    "cost": "Cost",
    "tokens": "Tokens",
    "time": "Time",
    "tasks": "Tasks"
  },
  "leaderboard": {
    "title": "Leaderboard",
    "caption": "{{count}} agents",
    "caption_with_deleted": "{{count}} agents · {{deleted}} deleted",
    "deleted_agents": "Deleted agents",
    "sort_tokens": "Tokens",
    "sort_cost": "Cost",
    "sort_time": "Time",
    "sort_tasks": "Tasks"
  },
  "empty": {
    "title": "No usage yet",
    "body": "Once agents start running tasks here, their token spend and run time will appear in this view."
  },
  "error": {
    "load_prefix": "Failed to load usage data:",
    "unknown": "Unknown error",
    "retry": "Retry"
  },
  "duration": {
    "less_than_minute": "<1m"
  }
}
```

- [ ] Create `apps/mobile/locales/zh-Hans/usage.json`:

```json
{
  "list": {
    "header_title": "用量"
  },
  "subtitle": "查看当前工作区的 token 消耗和智能体运行情况。",
  "filter": {
    "all_projects": "全部项目"
  },
  "kpi": {
    "cost_label": "费用 · {{days}}天",
    "tokens_label": "Token · {{days}}天",
    "tokens_hint": "输入 {{input}} · 输出 {{output}}",
    "run_time_label": "运行时长 · {{days}}天",
    "run_time_hint": "共 {{tasks}} 个任务",
    "tasks_label": "任务数 · {{days}}天",
    "tasks_hint": "失败 {{failed}} 个"
  },
  "dim": {
    "daily": "按天",
    "weekly": "按周"
  },
  "metric": {
    "cost": "费用",
    "tokens": "Token",
    "time": "运行时长",
    "tasks": "任务数"
  },
  "leaderboard": {
    "title": "排行榜",
    "caption": "{{count}} 个智能体",
    "caption_with_deleted": "{{count}} 个智能体 · {{deleted}} 个已删除",
    "deleted_agents": "已删除的智能体",
    "sort_tokens": "Token",
    "sort_cost": "费用",
    "sort_time": "运行时长",
    "sort_tasks": "任务数"
  },
  "empty": {
    "title": "暂无消耗数据",
    "body": "当智能体在这里开始执行任务后，它们的 token 消耗和运行时长将出现在此处。"
  },
  "error": {
    "load_prefix": "加载用量数据失败：",
    "unknown": "未知错误",
    "retry": "重试"
  },
  "duration": {
    "less_than_minute": "<1分钟"
  }
}
```

- [ ] In `apps/mobile/locales/index.ts`, add the import + registration for both languages, alphabetically after `skills` and before `workspace`:

```ts
import enUsage from "./en/usage.json";
// ...
import zhHansUsage from "./zh-Hans/usage.json";
```

And in each `RESOURCES` block:

```ts
    skills: enSkills,
    usage: enUsage,
    workspace: enWorkspace,
```

```ts
    skills: zhHansSkills,
    usage: zhHansUsage,
    workspace: zhHansWorkspace,
```

### Step 4: Add the nav entry

- [ ] In `apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`, add a `NavRow` right after the Runtimes row (still inside the same `SectionGroup`):

```tsx
          <NavRow
            onPress={() => slug && router.push(`/${slug}/more/usage`)}
            chevronColor={mutedFg}
            title={t("more_page.nav.usage")}
          />
```

- [ ] Add the matching key to `apps/mobile/locales/en/workspace.json` and `apps/mobile/locales/zh-Hans/workspace.json`, in the `more_page.nav` object, after `runtimes`:

  en: `"usage": "Usage"`
  zh-Hans: `"usage": "用量"`

### Step 5: Register the pushed route

- [ ] In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, add a `tUsage` translation hook alongside the existing per-namespace hooks:

```tsx
  const { t: tUsage } = useTranslation("usage");
```

- [ ] Add the `Stack.Screen` registration, right after `more/runtimes`:

```tsx
        <Stack.Screen
          name="more/usage"
          options={{
            title: tUsage("list.header_title"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

### Step 6: Write the `UsageStatCard` component

- [ ] Create `apps/mobile/components/usage/usage-stat-card.tsx`:

```tsx
import { View } from "react-native";
import { Text } from "@/components/ui/text";

export function UsageStatCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <View className="flex-1 min-w-[45%] gap-2 p-4">
      <Text className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </Text>
      <Text className="text-2xl font-semibold text-foreground tabular-nums">
        {value}
      </Text>
      {hint ? (
        <Text className="text-xs text-muted-foreground">{hint}</Text>
      ) : null}
    </View>
  );
}
```

### Step 7: Write the screen shell — `more/usage.tsx`

- [ ] Create `apps/mobile/app/(app)/[workspace]/more/usage.tsx`:

```tsx
/**
 * Workspace Usage page. Mirrors packages/views/dashboard/components/
 * dashboard-page.tsx's DashboardPage (the "Usage" nav page — not the
 * per-runtime UsageSection, which is out of scope). See
 * docs/superpowers/specs/2026-07-13-mobile-usage-browse-design.md.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useActionSheet } from "@expo/react-native-action-sheet";
import SegmentedControl from "@react-native-segmented-control/segmented-control";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { UsageStatCard } from "@/components/usage/usage-stat-card";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewingTimezone } from "@/lib/use-viewing-timezone";
import { projectListOptions } from "@/data/queries/projects";
import { agentListOptions } from "@/data/queries/agents";
import {
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardUsageDailyOptions,
} from "@/data/queries/usage";
import { addDaysIso, computeDailyTotals, formatDuration, todayIso } from "@/lib/usage-display";
import { fmtMoney, formatTokens } from "@/lib/usage-pricing";

type Dim = "daily" | "weekly";

// Legal periods per dimension + default-on-switch, confirmed against
// packages/views/dashboard/components/dashboard-page.tsx's
// TIME_RANGES / DEFAULT_DAYS_BY_DIM.
const TIME_RANGES = [
  { label: "1d", days: 1, dims: ["daily"] as const },
  { label: "7d", days: 7, dims: ["daily"] as const },
  { label: "30d", days: 30, dims: ["daily", "weekly"] as const },
  { label: "90d", days: 90, dims: ["daily", "weekly"] as const },
  { label: "180d", days: 180, dims: ["weekly"] as const },
] as const;
const DEFAULT_DAYS_BY_DIM: Record<Dim, number> = { daily: 30, weekly: 90 };
const ALL_PROJECTS = "__all__";

function rangesForDim(dim: Dim) {
  return TIME_RANGES.filter((r) => (r.dims as readonly Dim[]).includes(dim));
}

export default function UsagePage() {
  const { t } = useTranslation("usage");
  const { t: tCommon } = useTranslation("common");
  const { showActionSheetWithOptions } = useActionSheet();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const viewTZ = useViewingTimezone();

  const [dim, setDim] = useState<Dim>("daily");
  const [days, setDays] = useState<number>(30);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);

  const allowedRanges = rangesForDim(dim);
  const handleDimChange = (next: Dim) => {
    setDim(next);
    const stillAllowed = rangesForDim(next).some((r) => r.days === days);
    if (!stillAllowed) setDays(DEFAULT_DAYS_BY_DIM[next]);
  };

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const projectId = useMemo(() => {
    if (projectValue === ALL_PROJECTS) return null;
    return projects.some((p) => p.id === projectValue) ? projectValue : null;
  }, [projectValue, projects]);

  const selectedProjectTitle =
    projectValue === ALL_PROJECTS
      ? t("filter.all_projects")
      : (projects.find((p) => p.id === projectValue)?.title ?? t("filter.all_projects"));

  const openProjectPicker = () => {
    const options = [t("filter.all_projects"), ...projects.map((p) => p.title), tCommon("cancel")];
    showActionSheetWithOptions(
      { options, cancelButtonIndex: options.length - 1 },
      (index) => {
        if (index == null || index === options.length - 1) return;
        setProjectValue(index === 0 ? ALL_PROJECTS : projects[index - 1].id);
      },
    );
  };

  // The weekly chart paints ceil(days / 7) trailing calendar weeks. In the
  // worst case (today = Sunday) the leftmost Monday sits weekCount*7-1
  // days back, so over-fetch the per-date queries to cover the full first
  // week — mirrors DashboardPage's chartFetchDays exactly.
  const weekCount = Math.max(1, Math.ceil(days / 7));
  const chartFetchDays = dim === "weekly" ? weekCount * 7 : days;

  const dailyQuery = useQuery(dashboardUsageDailyOptions(wsId, chartFetchDays, projectId, viewTZ));
  const byAgentQuery = useQuery(dashboardUsageByAgentOptions(wsId, days, projectId, viewTZ));
  const runTimeQuery = useQuery(dashboardAgentRunTimeOptions(wsId, days, projectId, viewTZ));
  const runTimeDailyQuery = useQuery(dashboardRunTimeDailyOptions(wsId, chartFetchDays, projectId, viewTZ));

  const isLoading =
    dailyQuery.isLoading || byAgentQuery.isLoading || runTimeQuery.isLoading || runTimeDailyQuery.isLoading;
  const error = dailyQuery.error ?? byAgentQuery.error ?? runTimeQuery.error ?? runTimeDailyQuery.error;
  const refetchAll = () => {
    dailyQuery.refetch();
    byAgentQuery.refetch();
    runTimeQuery.refetch();
    runTimeDailyQuery.refetch();
  };

  const dailyUsage = dailyQuery.data ?? [];
  const byAgentUsage = byAgentQuery.data ?? [];
  const runTimeRows = runTimeQuery.data ?? [];
  const runTimeDailyRows = runTimeDailyQuery.data ?? [];

  // Client-side day-window re-slice, mirroring DashboardPage's
  // dailyCutoffIso — dailyQuery/runTimeDailyQuery are over-fetched to
  // chartFetchDays for the weekly chart, so the KPI totals must re-slice
  // back down to the advertised `days` window.
  const dailyCutoffIso = useMemo(
    () => addDaysIso(todayIso(viewTZ), -(days - 1)),
    [days, viewTZ],
  );
  const dailyUsageInWindow = useMemo(
    () => dailyUsage.filter((u) => u.date >= dailyCutoffIso),
    [dailyUsage, dailyCutoffIso],
  );

  const totals = useMemo(() => computeDailyTotals(dailyUsageInWindow), [dailyUsageInWindow]);
  const runTimeTotals = useMemo(() => {
    let totalSeconds = 0;
    let taskCount = 0;
    let failedCount = 0;
    for (const r of runTimeRows) {
      totalSeconds += r.total_seconds;
      taskCount += r.task_count;
      failedCount += r.failed_count;
    }
    return { totalSeconds, taskCount, failedCount };
  }, [runTimeRows]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("error.load_prefix")} {error instanceof Error ? error.message : t("error.unknown")}
          </Text>
          <Button variant="outline" onPress={refetchAll}>
            <Text>{t("error.retry")}</Text>
          </Button>
        </View>
      ) : (
        <ScrollView contentContainerClassName="pb-6">
          <View className="flex-row flex-wrap items-center gap-2 px-4 pt-4">
            <Button variant="outline" size="sm" onPress={openProjectPicker}>
              <Text numberOfLines={1}>{selectedProjectTitle}</Text>
            </Button>
            <SegmentedControl
              values={[t("dim.daily"), t("dim.weekly")]}
              selectedIndex={dim === "daily" ? 0 : 1}
              onChange={(e) =>
                handleDimChange(e.nativeEvent.selectedSegmentIndex === 0 ? "daily" : "weekly")
              }
              style={{ width: 140 }}
            />
            <SegmentedControl
              values={allowedRanges.map((r) => r.label)}
              selectedIndex={Math.max(0, allowedRanges.findIndex((r) => r.days === days))}
              onChange={(e) => setDays(allowedRanges[e.nativeEvent.selectedSegmentIndex].days)}
              style={{ width: allowedRanges.length * 46 }}
            />
          </View>

          <View className="flex-row flex-wrap px-2 pt-2">
            <UsageStatCard label={t("kpi.cost_label", { days })} value={fmtMoney(totals.cost)} />
            <UsageStatCard
              label={t("kpi.tokens_label", { days })}
              value={formatTokens(totals.input + totals.output + totals.cacheRead + totals.cacheWrite)}
              hint={t("kpi.tokens_hint", { input: formatTokens(totals.input), output: formatTokens(totals.output) })}
            />
            <UsageStatCard
              label={t("kpi.run_time_label", { days })}
              value={formatDuration(runTimeTotals.totalSeconds, t("duration.less_than_minute"))}
              hint={t("kpi.run_time_hint", { tasks: runTimeTotals.taskCount })}
            />
            <UsageStatCard
              label={t("kpi.tasks_label", { days })}
              value={String(runTimeTotals.taskCount)}
              hint={t("kpi.tasks_hint", { failed: runTimeTotals.failedCount })}
            />
          </View>
        </ScrollView>
      )}
    </SafeAreaView>
  );
}
```

### Step 8: Typecheck and lint

Run: `cd apps/mobile && pnpm typecheck && pnpm lint`
Expected: no new errors.

### Step 9: Manual check (no automated test for a screen shell)

Run the app (`cd apps/mobile && npx expo start --dev-client`), reload on device, tap Usage on the More page. Confirm: project picker action sheet opens and filters the KPI numbers; Daily/Weekly segmented control switches and legal periods update the period control's options; KPI cards show plausible non-placeholder numbers.

### Step 10: Commit

```bash
git add apps/mobile/package.json apps/mobile/app.config.ts apps/mobile/locales apps/mobile/app/\(app\)/\[workspace\]/\(tabs\)/more.tsx apps/mobile/app/\(app\)/\[workspace\]/_layout.tsx apps/mobile/components/usage apps/mobile/app/\(app\)/\[workspace\]/more/usage.tsx
git commit -m "feat(mobile): add Usage page nav entry, controls, and KPI row"
```

(If `expo-linear-gradient` didn't require a manual `app.config.ts` edit in Step 1, drop that path from the `git add` list above.)

---

## Task 4: Trend chart

**Files:**
- Modify: `apps/mobile/app/(app)/[workspace]/more/usage.tsx`

**Interfaces:**
- Consumes: `aggregateDailyCost`, `aggregateDailyTokens`, `aggregateDailyTime`, `aggregateDailyTasks`, `aggregateByWeek`, `aggregateWeeklyTime`, `aggregateWeeklyTasks` (Task 1), the screen's existing `dailyUsageInWindow`/`runTimeDailyRows`/`dim`/`days`/`viewTZ`/`weekCount` state (Task 3).
- Produces: nothing consumed by later tasks — Task 5 (leaderboard) is independent of the chart.

### Step 1: Extend the screen with per-metric aggregation + the chart

- [ ] In `apps/mobile/app/(app)/[workspace]/more/usage.tsx`, add the metric state, the daily/weekly aggregations for all 4 metrics, and the `BarChart` rendering. Add `import { BarChart } from "react-native-gifted-charts";` as a new import line, and extend the existing `@/lib/usage-display` import (added in Task 3) with these additional named imports rather than writing a second `from "@/lib/usage-display"` line:

```tsx
import { BarChart } from "react-native-gifted-charts";
import {
  aggregateByWeek,
  aggregateDailyCost,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
} from "@/lib/usage-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
```

- [ ] Add this state + these `useMemo`s inside `UsagePage`, after the existing `runTimeTotals` block:

```tsx
  type Metric = "cost" | "tokens" | "time" | "tasks";
  const [metric, setMetric] = useState<Metric>("tokens");

  const dailyCost = useMemo(() => aggregateDailyCost(dailyUsageInWindow), [dailyUsageInWindow]);
  const dailyTokens = useMemo(() => aggregateDailyTokens(dailyUsageInWindow), [dailyUsageInWindow]);
  const runTimeDailyInWindow = useMemo(
    () => runTimeDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [runTimeDailyRows, dailyCutoffIso],
  );
  const dailyTime = useMemo(() => aggregateDailyTime(runTimeDailyInWindow), [runTimeDailyInWindow]);
  const dailyTasks = useMemo(() => aggregateDailyTasks(runTimeDailyInWindow), [runTimeDailyInWindow]);

  // Weekly aggregates use the raw over-fetched series (chartFetchDays),
  // NOT the windowed one — mirrors DashboardPage exactly, so the leftmost
  // week isn't truncated.
  const weekly = useMemo(() => aggregateByWeek(dailyUsage, viewTZ, weekCount), [dailyUsage, viewTZ, weekCount]);
  const weeklyTime = useMemo(
    () => aggregateWeeklyTime(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );
  const weeklyTasks = useMemo(
    () => aggregateWeeklyTasks(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );

  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const lessThanMinuteLabel = t("duration.less_than_minute");

  // Per-metric chart data. Cost/Tokens are stacked series (input/output/
  // cache-write for Cost — cache-read excluded; input/output/cache-read/
  // cache-write for Tokens); Time is a single unstacked series; Tasks is
  // a stacked 2-series (completed/failed) — mirrors DashboardPage's
  // DailyCostChart/DailyTokensChart/DailyTimeChart/DailyTasksChart (and
  // their Weekly siblings) exactly, per
  // packages/views/runtimes/components/charts/*.
  const chartBarData = useMemo(() => {
    const stackColors = {
      input: theme.chart1,
      output: theme.chart2,
      cacheRead: theme.chart4,
      cacheWrite: theme.chart3,
      completed: theme.chart1,
      failed: theme.chart5,
      single: theme.chart1,
    };
    const stacked = (
      rows: { label: string }[],
      segments: Array<{ key: string; color: string }>,
      getValue: (row: any, key: string) => number,
    ) =>
      rows.map((row) => ({
        label: row.label,
        stacks: segments.map((s) => ({ value: getValue(row, s.key), color: s.color })),
      }));

    if (metric === "cost") {
      const rows = dim === "weekly" ? weekly.weeklyCostStack : dailyCost;
      return stacked(rows, [
        { key: "input", color: stackColors.input },
        { key: "output", color: stackColors.output },
        { key: "cacheWrite", color: stackColors.cacheWrite },
      ], (r, k) => r[k]);
    }
    if (metric === "tokens") {
      const rows = dim === "weekly" ? weekly.weeklyTokens : dailyTokens;
      return stacked(rows, [
        { key: "input", color: stackColors.input },
        { key: "output", color: stackColors.output },
        { key: "cacheRead", color: stackColors.cacheRead },
        { key: "cacheWrite", color: stackColors.cacheWrite },
      ], (r, k) => r[k]);
    }
    if (metric === "time") {
      const rows = dim === "weekly" ? weeklyTime : dailyTime;
      return rows.map((row) => ({ label: row.label, value: row.totalSeconds, frontColor: stackColors.single }));
    }
    const rows = dim === "weekly" ? weeklyTasks : dailyTasks;
    return stacked(rows, [
      { key: "completed", color: stackColors.completed },
      { key: "failed", color: stackColors.failed },
    ], (r, k) => r[k]);
  }, [metric, dim, dailyCost, dailyTokens, dailyTime, dailyTasks, weekly, weeklyTime, weeklyTasks, theme]);
```

- [ ] Add the metric-toggle + chart JSX, right after the KPI row's closing `</View>` (still inside the `<ScrollView>`):

```tsx
          <View className="px-4 pt-4">
            <SegmentedControl
              values={[t("metric.tokens"), t("metric.cost"), t("metric.time"), t("metric.tasks")]}
              selectedIndex={["tokens", "cost", "time", "tasks"].indexOf(metric)}
              onChange={(e) =>
                setMetric((["tokens", "cost", "time", "tasks"] as const)[e.nativeEvent.selectedSegmentIndex])
              }
            />
            <View className="pt-3">
              {chartBarData.length === 0 ? (
                <Text className="text-sm text-muted-foreground text-center py-8">{t("empty.title")}</Text>
              ) : metric === "time" ? (
                <BarChart
                  data={chartBarData as { label: string; value: number; frontColor: string }[]}
                  height={180}
                  barWidth={dim === "weekly" ? 24 : 12}
                  spacing={dim === "weekly" ? 16 : 6}
                  noOfSections={4}
                  yAxisTextStyle={{ color: theme.mutedForeground }}
                  xAxisLabelTextStyle={{ color: theme.mutedForeground, fontSize: 10 }}
                  formatYLabel={(v: string) => formatDuration(Number(v), lessThanMinuteLabel)}
                />
              ) : (
                <BarChart
                  stackData={chartBarData as { label: string; stacks: { value: number; color: string }[] }[]}
                  height={180}
                  barWidth={dim === "weekly" ? 24 : 12}
                  spacing={dim === "weekly" ? 16 : 6}
                  noOfSections={4}
                  yAxisTextStyle={{ color: theme.mutedForeground }}
                  xAxisLabelTextStyle={{ color: theme.mutedForeground, fontSize: 10 }}
                  formatYLabel={(v: string) =>
                    metric === "cost" ? fmtMoney(Number(v)) : metric === "tokens" ? formatTokens(Number(v)) : v
                  }
                />
              )}
            </View>
          </View>
```

**Note for the implementer:** `react-native-gifted-charts`' `BarChart` takes either flat `data` (single-series, each item `{value, frontColor}`) or `stackData` (multi-series, each item `{stacks: [{value, color}]}`) — confirm both prop names against the installed version's type definitions (`node_modules/react-native-gifted-charts/src/BarChart/types.ts` or wherever its `.d.ts` lands) before wiring, since exact prop names can shift between minor versions; adjust the JSX above to match if they differ from what's written here.

### Step 2: Typecheck and lint

Run: `cd apps/mobile && pnpm typecheck && pnpm lint`
Expected: no new errors. If `react-native-gifted-charts`' types don't line up with the `stacked`/`chartBarData` helper's loose typing, tighten the helper's return type to match the library's actual `BarChart` prop types rather than reaching for `any` beyond the one `getValue: (row: any, key: string) => number` helper signature above (which is fine — it's a private, same-file helper consumed immediately, not a public API).

### Step 3: Manual check

On device: switch the metric toggle across all 4 options in both Daily and Weekly dimension; confirm Tokens/Cost render stacked segments, Time renders a single bar series, Tasks renders a 2-segment stack; confirm switching Daily/Weekly re-renders with the correct bucket count (days vs weeks); confirm an empty window (e.g. a brand-new workspace, or a period where the data genuinely has 0 rows) shows the empty-state text instead of a blank chart.

### Step 4: Commit

```bash
git add apps/mobile/app/\(app\)/\[workspace\]/more/usage.tsx
git commit -m "feat(mobile): add Usage page trend chart (4-metric toggle)"
```

---

## Task 5: Leaderboard

**Files:**
- Modify: `apps/mobile/app/(app)/[workspace]/more/usage.tsx`

**Interfaces:**
- Consumes: `aggregateAgentTokens`, `mergeAgentDashboardRows`, `bucketUnknownAgentRows`, `DELETED_AGENTS_ROW_ID` (Task 1), the screen's `byAgentUsage`/`runTimeRows`/`agents` (Task 3).
- Produces: nothing (final screen piece).

### Step 1: Add the leaderboard section

- [ ] In `apps/mobile/app/(app)/[workspace]/more/usage.tsx`, add `import { ActorAvatar } from "@/components/ui/actor-avatar";` and `import { Ionicons } from "@expo/vector-icons";` as new import lines, and extend the existing `@/lib/usage-display` import (already carrying the Task 3/4 names) with these additional named imports rather than writing a third `from "@/lib/usage-display"` line:

```tsx
import { ActorAvatar } from "@/components/ui/actor-avatar";
import {
  aggregateAgentTokens,
  bucketUnknownAgentRows,
  DELETED_AGENTS_ROW_ID,
  mergeAgentDashboardRows,
} from "@/lib/usage-display";
import { Ionicons } from "@expo/vector-icons";
```

- [ ] Add this state + these `useMemo`s inside `UsagePage`, after the chart-related block from Task 4:

```tsx
  type LeaderboardSort = "tokens" | "cost" | "time" | "tasks";
  const [sortBy, setSortBy] = useState<LeaderboardSort>("tokens");

  const agentTokenRows = useMemo(() => aggregateAgentTokens(byAgentUsage), [byAgentUsage]);
  const agentRows = useMemo(
    () => mergeAgentDashboardRows(agentTokenRows, runTimeRows),
    [agentTokenRows, runTimeRows],
  );
  const knownAgentIds = useMemo(() => new Set(agents.map((a) => a.id)), [agents]);
  const visibleAgentRows = useMemo(
    () => bucketUnknownAgentRows(agentRows, knownAgentIds),
    [agentRows, knownAgentIds],
  );
  const deletedAgentCount = useMemo(
    () => agentRows.filter((r) => !knownAgentIds.has(r.agentId)).length,
    [agentRows, knownAgentIds],
  );

  const SORT_METRIC: Record<LeaderboardSort, (r: (typeof visibleAgentRows)[number]) => number> = {
    tokens: (r) => r.tokens,
    cost: (r) => r.cost,
    time: (r) => r.seconds,
    tasks: (r) => r.taskCount,
  };
  const sortedRows = useMemo(() => {
    const metricFn = SORT_METRIC[sortBy];
    return [...visibleAgentRows].sort((a, b) => metricFn(b) - metricFn(a));
  }, [visibleAgentRows, sortBy]);
  const maxValue = useMemo(() => {
    const metricFn = SORT_METRIC[sortBy];
    return sortedRows.reduce((m, r) => Math.max(m, metricFn(r)), 0);
  }, [sortedRows, sortBy]);
```

- [ ] Add the leaderboard JSX at the end of the `<ScrollView>`, right after the trend-chart `</View>` from Task 4:

```tsx
          <View className="mt-4 border-t border-border">
            <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
              <Text className="text-sm font-semibold text-foreground">{t("leaderboard.title")}</Text>
              <Text className="text-xs text-muted-foreground">
                {deletedAgentCount > 0
                  ? t("leaderboard.caption_with_deleted", {
                      count: visibleAgentRows.length - 1,
                      deleted: deletedAgentCount,
                    })
                  : t("leaderboard.caption", { count: visibleAgentRows.length })}
              </Text>
            </View>
            <SegmentedControl
              values={[t("leaderboard.sort_tokens"), t("leaderboard.sort_cost"), t("leaderboard.sort_time"), t("leaderboard.sort_tasks")]}
              selectedIndex={(["tokens", "cost", "time", "tasks"] as const).indexOf(sortBy)}
              onChange={(e) =>
                setSortBy((["tokens", "cost", "time", "tasks"] as const)[e.nativeEvent.selectedSegmentIndex])
              }
              style={{ marginHorizontal: 16, marginBottom: 8 }}
            />
            {sortedRows.length === 0 ? (
              <Text className="text-sm text-muted-foreground text-center py-6">{t("empty.title")}</Text>
            ) : (
              sortedRows.map((row) => {
                const isDeletedBucket = row.agentId === DELETED_AGENTS_ROW_ID;
                const value = SORT_METRIC[sortBy](row);
                const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
                return (
                  <View key={row.agentId} className="flex-row items-center gap-3 px-4 py-2.5">
                    {isDeletedBucket ? (
                      <View className="h-6 w-6 items-center justify-center rounded-full bg-muted">
                        <Ionicons name="trash-outline" size={14} color={theme.mutedForeground} />
                      </View>
                    ) : (
                      <ActorAvatar type="agent" id={row.agentId} size={24} />
                    )}
                    <View className="flex-1 gap-1">
                      <Text
                        numberOfLines={1}
                        className={isDeletedBucket ? "text-sm italic text-muted-foreground" : "text-sm font-medium text-foreground"}
                      >
                        {isDeletedBucket ? t("leaderboard.deleted_agents") : (agents.find((a) => a.id === row.agentId)?.name ?? row.agentId)}
                      </Text>
                      <View className="h-1.5 rounded-full bg-muted overflow-hidden">
                        <View style={{ width: `${pct}%` }} className="h-full rounded-full bg-primary" />
                      </View>
                    </View>
                    <Text className="text-xs tabular-nums text-muted-foreground w-16 text-right">
                      {formatTokens(row.tokens)}
                    </Text>
                    <Text className="text-xs tabular-nums text-muted-foreground w-14 text-right">
                      {fmtMoney(row.cost)}
                    </Text>
                  </View>
                );
              })
            )}
          </View>
```

(The row only shows Tokens + Cost as trailing numeric columns, not all 4 metrics side-by-side like desktop's wider grid — phone width doesn't fit 4 numeric columns plus name/avatar/bar legibly. The active sort's column is what the `pct` bar visualizes; the two shown numbers give enough context regardless of which metric is sorted. This is an intentional mobile-only layout divergence, not a data gap — Time/Tasks are still fully computed in `sortedRows`, just not rendered as extra columns to keep the row scannable at phone width.)

### Step 2: Typecheck and lint

Run: `cd apps/mobile && pnpm typecheck && pnpm lint`
Expected: no new errors.

### Step 3: Manual check

On device: confirm the leaderboard lists agents sorted by Tokens by default; tapping each sort chip re-sorts and the progress bars re-scale; if the workspace has any hard-deleted agent with historical activity, confirm a "Deleted agents" row appears with a trash icon instead of an avatar and participates in sorting like any other row; confirm the row count in the caption matches `visibleAgentRows.length` (or `length - 1` when a deleted-agents bucket is present).

### Step 4: Commit

```bash
git add apps/mobile/app/\(app\)/\[workspace\]/more/usage.tsx
git commit -m "feat(mobile): add Usage page leaderboard"
```

---

## Task 6: Manual bilingual verification

**Files:** none (verification only).

**Interfaces:** N/A.

### Step 1: Run full verification suite

```bash
cd apps/mobile
pnpm typecheck
pnpm lint
pnpm test
```
Expected: 0 errors on typecheck/lint (pre-existing unrelated warnings are fine); all vitest suites pass, including `usage-pricing.test.ts`, `usage-display.test.ts`, and the locale parity test (confirms `en`/`zh-Hans` `usage.json` and `workspace.json` key sets match).

### Step 2: Manual pass — English

On device (English locale): tap Usage on the More page. Confirm:
- Project filter opens an action sheet, "All projects" + every real project listed; picking one re-filters KPIs/chart/leaderboard (refetch, not stale numbers).
- Daily/Weekly toggle and period toggle behave per the legal-period table; switching dimension resets period only when the current value isn't legal in the new dimension.
- KPI cards show Cost (no hint), Tokens (input/output hint), Run time (task-count hint), Tasks (failed-count hint) — cross-check the actual numbers against the same workspace's desktop Usage page for the same project/period selection.
- Trend chart switches cleanly across Tokens/Cost/Time/Tasks in both dimensions.
- Leaderboard sorts correctly per chip, shows a "Deleted agents" row only when applicable, and its rows' Tokens+Cost values are internally consistent with the KPI totals (the sum of all leaderboard token/cost values should equal the KPI Tokens/Cost card, since `bucketUnknownAgentRows` keeps that reconciled).

### Step 3: Manual pass — Chinese (zh-Hans)

Switch the device/app locale to Chinese, repeat the same walkthrough. Confirm the "用量" nav label, all KPI/chart/leaderboard copy, and the empty/error states render correctly with no missing-key fallback text (e.g. no bare `usage.kpi.cost_label`-style raw keys visible on screen).

### Step 4: Report

No commit for this task — it's a verification pass. If any step surfaces a real bug, fix it as a follow-up commit (not part of this plan) and re-run Steps 1-3 before considering the round done.
