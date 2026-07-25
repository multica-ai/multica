"use client";

import { useMemo, useState } from "react";
import { BarChart3, ChevronRight, AlertCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CompactNumberFlow,
  CurrencyNumberFlow,
} from "@multica/ui/components/ui/number-flow";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { RuntimeUsage, AgentRuntime } from "@multica/core/types";
import {
  runtimeUsageOptions,
  runtimeUsageByAgentOptions,
} from "@multica/core/runtimes/queries";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import {
  formatTokens,
  estimateCost,
  estimateCacheSavings,
  aggregateByDate,
  aggregateByWeek,
  aggregateCostByAgent,
  aggregateCostByModel,
  collectUnmappedModels,
  pctChange,
  sliceWindow,
  type CostByKey,
} from "../utils";
import { KpiCard } from "./shared";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  DailyCostChart,
  DailyTokensChart,
  WeeklyCostChart,
  WeeklyTokensChart,
  ActivityHeatmap,
} from "./charts";
import { CustomPricingDialog } from "./custom-pricing-dialog";
import { useT } from "../../i18n";

// Single source of truth for the period selector. KPIs, the When-chart, the
// Cost-by tabs, and the CSV export all read from the same `days` value so
// the labels ("· 30D") and the data slice never disagree.
//
// `dims` declares which dimensions each range is allowed in. 7 days at the
// weekly grain is one bar, so 7d is daily-only; 180d is weekly-only because
// 180 daily bars are visually unreadable.
const TIME_RANGES = [
  { label: "7d", days: 7, dims: ["daily"] as const },
  { label: "30d", days: 30, dims: ["daily", "weekly"] as const },
  { label: "90d", days: 90, dims: ["daily", "weekly"] as const },
  { label: "180d", days: 180, dims: ["weekly"] as const },
] as const;

type TimeRange = (typeof TIME_RANGES)[number]["days"];
type WhenTab = "daily" | "weekly" | "heatmap";

// Default time range per dimension. Switching dimensions resets the period
// to its default rather than keeping a now-invalid value.
const DEFAULT_DAYS_BY_DIM: Record<Exclude<WhenTab, "heatmap">, TimeRange> = {
  daily: 30,
  weekly: 90,
};

function rangesForDim(dim: Exclude<WhenTab, "heatmap">) {
  return TIME_RANGES.filter((r) =>
    (r.dims as readonly string[]).includes(dim),
  );
}

// ---------------------------------------------------------------------------
// Local segmented control. shadcn's Tabs is wired for full tab pages with
// keyboard nav and ARIA semantics that a compact toolbar pill doesn't need.
// Visual: light-grey track + white "raised" active pill.
// ---------------------------------------------------------------------------

function Segmented<T extends string | number>({
  value,
  onChange,
  options,
  disabled,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { label: string; value: T }[];
  disabled?: boolean;
}) {
  return (
    <div
      className={`inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5 ${
        disabled ? "opacity-50" : ""
      }`}
    >
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          disabled={disabled}
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2.5 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed ${
            o.value === value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function fmtMoney(n: number): string {
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
}

// ---------------------------------------------------------------------------
// Top-level orchestrator. Owns the time window, fetches a 180-day usage
// cache once, slices it into "current" / "prior" windows for delta math,
// and threads everything into the four visual blocks below.
//
// 180 days (vs the older 90) is sized for the Heatmap tab — it shows 26
// weeks (~6 months) so the long view actually looks long. The 7d/30d/90d
// period selector slices client-side; the prior-window delta on the Cost
// KPI also benefits from having extra history available.
// ---------------------------------------------------------------------------

export function UsageSection({ runtime }: { runtime: AgentRuntime }) {
  const { t, i18n } = useT("runtimes");
  const runtimeId = runtime.id;
  // Reports render in the viewer's timezone — the backend slices the UTC
  // hourly rollup on the same `tz` we pass here, so every frontend window
  // calculation shares one axis with the server.
  const tz = useViewingTimezone();
  const { data: usage = [], isLoading: loading } = useQuery(
    runtimeUsageOptions(runtimeId, 180, tz),
  );
  const [dim, setDim] = useState<Exclude<WhenTab, "heatmap">>("daily");
  const [days, setDays] = useState<TimeRange>(30);
  // Subscribe so the KPI cards (which call estimateCost at render-time, not
  // through a memo) re-evaluate when the user saves a custom rate. The
  // aggregate sub-components (WhenChart, CostByBlock, ActivityHeatmap) each
  // subscribe on their own and pass pricings as a memo dep there.
  useCustomPricingStore((s) => s.pricings);

  if (loading) return <UsageSkeleton />;
  if (usage.length === 0) return <UsageEmpty />;

  // Slice the cached 180-day window into the user's selected sub-window AND
  // the immediately prior window of equal length. The KPI delta ("+18% vs
  // prev") then compares like-for-like ranges instead of "this period vs
  // all of history". Tz-aware so the cutoff lands on the same calendar
  // boundary the backend used when bucketing rows.
  const { filtered, prevFiltered } = sliceWindow(usage, days, tz);

  const allowedRanges = rangesForDim(dim);
  const handleDimChange = (next: Exclude<WhenTab, "heatmap">) => {
    setDim(next);
    const stillAllowed = (rangesForDim(next) as readonly { days: number }[]).some(
      (r) => r.days === days,
    );
    if (!stillAllowed) {
      setDays(DEFAULT_DAYS_BY_DIM[next]);
    }
  };
  const totals = computeTotals(filtered);
  const prevTotals = computeTotals(prevFiltered);

  const tokensTotal =
    totals.input + totals.output + totals.cacheRead + totals.cacheWrite;
  const cacheableTokens = totals.input + totals.cacheRead;
  const cacheHitRate =
    cacheableTokens > 0 ? Math.round((totals.cacheRead / cacheableTokens) * 100) : 0;

  const costDelta = pctChange(totals.cost, prevTotals.cost);
  const locales = i18n.resolvedLanguage ?? i18n.language;

  return (
    <div className="space-y-5">
      {/* Page-wide period selector. Lives at the top because it controls
          basically everything below: the KPI numbers and labels, the
          daily / weekly chart window, and the cost-by aggregations. The
          Heatmap tab is the only sub-view that ignores it (always shows
          26 weeks), and its tab disables this control to telegraph that. */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="text-xs uppercase tracking-wider text-muted-foreground">
            {t(($) => $.usage.dimension_label)}
          </span>
          <Segmented
            value={dim}
            onChange={handleDimChange}
            options={
              [
                { label: t(($) => $.usage.when_tab_daily), value: "daily" },
                { label: t(($) => $.usage.when_tab_weekly), value: "weekly" },
              ] as const
            }
          />
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs uppercase tracking-wider text-muted-foreground">
            {t(($) => $.usage.period_label)}
          </span>
          <Segmented
            value={days}
            onChange={setDays}
            options={allowedRanges.map((r) => ({
              label: r.label,
              value: r.days,
            }))}
          />
        </div>
      </div>

      {/* Pricing bar. Sits above the KPI grid so a *partial* unmapping
          (some priced + some unpriced models in the same window) still has
          a visible entry point into the manual-pricing dialog — otherwise
          the chart would render normally and the unmapped tokens would silently
          contribute $0 to totals. Stays reachable once every model is priced
          if the user has saved overrides, so those rates remain editable. */}
      <CustomPricingBar usage={filtered} />

      <div className="grid grid-cols-3 divide-x rounded-lg border bg-card">
        <KpiCard
          label={t(($) => $.usage.kpi_cost_label, { days })}
          value={
            <CurrencyNumberFlow
              value={totals.cost}
              locales={locales}
              aria-label={fmtMoney(totals.cost)}
            />
          }
          hint={
            costDelta == null ? undefined : (
              <span
                className={
                  costDelta > 0
                    ? "text-warning"
                    : costDelta < 0
                      ? "text-success"
                      : ""
                }
              >
                {t(($) => $.usage.kpi_cost_delta, {
                  sign: costDelta > 0 ? "+" : "",
                  pct: costDelta,
                })}
              </span>
            )
          }
        />
        <KpiCard
          label={t(($) => $.usage.kpi_cache_label, { days })}
          value={
            <CurrencyNumberFlow
              value={totals.cacheSavings}
              locales={locales}
              aria-label={fmtMoney(totals.cacheSavings)}
            />
          }
          accent={totals.cacheSavings > 0 ? "success" : "default"}
          hint={
            <span>
              {t(($) => $.usage.kpi_cache_hint, {
                pct: cacheHitRate,
                reads: formatTokens(totals.cacheRead),
              })}
            </span>
          }
        />
        <KpiCard
          label={t(($) => $.usage.kpi_tokens_label, { days })}
          value={
            <CompactNumberFlow
              value={tokensTotal}
              locales={locales}
              aria-label={formatTokens(tokensTotal)}
            />
          }
          hint={
            <span>
              {t(($) => $.usage.kpi_tokens_hint, {
                input: formatTokens(totals.input),
                output: formatTokens(totals.output),
              })}
            </span>
          }
        />
      </div>

      {/* Layer 2 — WHEN chart. Dimension (Daily / Weekly) is owned by the
          parent so the period selector at the top can react to it. The
          Heatmap is an independent toggle inside this card — it ignores
          the period selector by design (it's a fixed 26-week long-view). */}
      <WhenChart
        usage={usage}
        filtered={filtered}
        days={days}
        dim={dim}
        tz={tz}
      />

      {/* Layer 3 — WHO/WHAT burned the spend. */}
      <CostByBlock runtimeId={runtimeId} days={days} usage={filtered} tz={tz} />

      {/* Layer 4 — Folded raw view. The Heatmap used to live here too; it
          was promoted into the WHEN chart's toggle, leaving only the
          breakdown table behind. */}
      <FoldedRow usage={filtered} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// WhenChart — answers "WHEN was this runtime spending money?".
// Dimension (Daily / Weekly) is owned by the parent so the page-level
// period selector can react to it. Heatmap is an independent view toggled
// inside this card; it ignores both the dimension and the period selector,
// always showing the long 26-week view.
// ---------------------------------------------------------------------------

type Dim = Exclude<WhenTab, "heatmap">;
type DailyMetric = "cost" | "tokens";

function WhenChart({
  usage,
  filtered,
  days,
  dim,
  tz,
}: {
  usage: RuntimeUsage[];
  filtered: RuntimeUsage[];
  days: TimeRange;
  dim: Dim;
  tz: string;
}) {
  const { t } = useT("runtimes");
  // Heatmap is the "independent" sibling — toggled here, not part of the
  // page-level dimension segmented (per the RFC).
  const [showHeatmap, setShowHeatmap] = useState(false);
  // Daily and Weekly share a Cost-vs-Tokens metric toggle.
  const [chartMetric, setChartMetric] = useState<DailyMetric>("cost");
  // Memo dep — the aggregates below run `estimateCost`, which now consults
  // the user override store. Without listing pricings here the memos cache
  // pre-override totals when query data hasn't changed.
  const pricings = useCustomPricingStore((s) => s.pricings);

  const { dailyCostStack, dailyTokens } = useMemo(
    () => aggregateByDate(filtered),
    [filtered, pricings],
  );
  // Weekly aggregation builds exactly N trailing calendar weeks anchored at
  // today (in the runtime tz). Buckets are pre-zeroed inside aggregateByWeek
  // so weeks with no usage render as empty bars; rows outside the window are
  // dropped. This avoids the earlier bug where slicing on a sparse 180-day
  // aggregate surfaced old populated weeks instead of in-range empty ones.
  const weekCount = Math.max(1, Math.ceil(days / 7));
  const { weeklyTokens, weeklyCostStack } = useMemo(
    () => aggregateByWeek(usage, tz, weekCount),
    [usage, tz, weekCount, pricings],
  );

  const metricToggleVisible = !showHeatmap;
  const legendIncludesCacheRead = !showHeatmap && chartMetric === "tokens";

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h4 className="text-sm font-semibold">{t(($) => $.usage.when_title)}</h4>
          {/* Cost / Tokens metric toggle — only meaningful when the chart
              actually has two series-types to switch between. */}
          {metricToggleVisible && (
            <Segmented
              value={chartMetric}
              onChange={setChartMetric}
              options={
                [
                  { label: t(($) => $.usage.daily_metric_cost), value: "cost" },
                  { label: t(($) => $.usage.daily_metric_tokens), value: "tokens" },
                ] as const
              }
            />
          )}
          {/* Heatmap toggle — independent of the page-level dim segmented.
              "On" puts the card into a fixed 26-week long-view that ignores
              the period selector. */}
          <button
            type="button"
            onClick={() => setShowHeatmap((v) => !v)}
            className={`rounded-md border px-2.5 py-1 text-xs font-medium transition-colors ${
              showHeatmap
                ? "border-foreground bg-foreground text-background"
                : "border-border text-muted-foreground hover:text-foreground"
            }`}
            aria-pressed={showHeatmap}
          >
            {t(($) => $.usage.when_tab_heatmap)}
          </button>
        </div>
        {!showHeatmap && (
          <ChartLegend includeCacheRead={legendIncludesCacheRead} />
        )}
      </div>

      {showHeatmap && (
        <p className="mb-2 text-center text-xs text-muted-foreground">
          {t(($) => $.usage.heatmap_caption)}
        </p>
      )}

      <div className="min-h-[260px]">
        {showHeatmap ? (
          <ActivityHeatmap usage={usage} tz={tz} />
        ) : dim === "daily" ? (
          <DailyTab
            metric={chartMetric}
            costData={dailyCostStack}
            tokensData={dailyTokens}
            usage={filtered}
          />
        ) : (
          <WeeklyTab
            metric={chartMetric}
            costData={weeklyCostStack}
            tokensData={weeklyTokens}
            usage={filtered}
          />
        )}
      </div>
    </div>
  );
}

function DailyTab({
  metric,
  costData,
  tokensData,
  usage,
}: {
  metric: DailyMetric;
  costData: Parameters<typeof DailyCostChart>[0]["data"];
  tokensData: Parameters<typeof DailyTokensChart>[0]["data"];
  usage: RuntimeUsage[];
}) {
  if (metric === "tokens") {
    // Token chart fires its own empty state: if no tokens were recorded the
    // chart is genuinely empty (independent of pricing — unmapped models
    // still contribute raw token counts).
    const totalTokens = tokensData.reduce(
      (s, d) => s + d.input + d.output + d.cacheRead + d.cacheWrite,
      0,
    );
    if (totalTokens === 0) return <EmptyChartState usage={usage} />;
    return <DailyTokensChart data={tokensData} />;
  }
  const totalCost = costData.reduce((s, d) => s + d.total, 0);
  if (totalCost === 0) return <EmptyChartState usage={usage} />;
  return <DailyCostChart data={costData} />;
}

function WeeklyTab({
  metric,
  costData,
  tokensData,
  usage,
}: {
  metric: DailyMetric;
  costData: Parameters<typeof WeeklyCostChart>[0]["data"];
  tokensData: Parameters<typeof WeeklyTokensChart>[0]["data"];
  usage: RuntimeUsage[];
}) {
  if (metric === "tokens") {
    const totalTokens = tokensData.reduce(
      (s, d) => s + d.input + d.output + d.cacheRead + d.cacheWrite,
      0,
    );
    if (totalTokens === 0) return <EmptyChartState usage={usage} />;
    return <WeeklyTokensChart data={tokensData} />;
  }
  const totalCost = costData.reduce((s, d) => s + d.total, 0);
  if (totalCost === 0) return <EmptyChartState usage={usage} />;
  return <WeeklyCostChart data={costData} />;
}

// ---------------------------------------------------------------------------
// EmptyChartState — drop-in replacement for "the chart would render empty".
// Two cases worth distinguishing:
//   1. No tokens at all → "no usage" (genuinely nothing happened).
//   2. Tokens present but cost is $0 → almost always means the model name
//      reported by the daemon isn't in our pricing table. List the offenders
//      so a developer can update MODEL_PRICING in one go.
// ---------------------------------------------------------------------------

function EmptyChartState({ usage }: { usage: RuntimeUsage[] }) {
  const { t } = useT("runtimes");
  const hasTokens = usage.some(
    (u) =>
      u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens >
      0,
  );
  const unmapped = collectUnmappedModels(usage);

  return (
    <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
      <BarChart3 className="h-5 w-5 text-muted-foreground/50" />
      {!hasTokens ? (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.usage.empty_no_usage)}
        </p>
      ) : unmapped.length > 0 ? (
        // CTA lives in the page-level UnmappedPricingNotice above. Keep the
        // chart-area copy descriptive only so the two surfaces don't bicker.
        <>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.usage.empty_pricing_missing)}
          </p>
          <p className="font-mono text-[11px] text-foreground">
            {unmapped.join(", ")}
          </p>
          <p className="text-[11px] text-muted-foreground/70">
            {t(($) => $.usage.empty_pricing_hint)}
          </p>
        </>
      ) : (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.usage.empty_zero_cost)}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// CustomPricingBar — the only entry point into the custom-pricing dialog,
// rendered above the KPI grid. Two states:
//
//   1. The window contains unpriced models → warning banner. Covers the
//      partial-unmapping case where the chart still renders (so
//      EmptyChartState never fires) but some tokens silently contribute $0.
//   2. Everything resolves, but the user has saved overrides → a quiet row
//      that still offers the dialog. Gating the whole bar on "something is
//      unmapped" used to hide it the moment the last override was saved:
//      the override made the model resolve, the banner disappeared, and the
//      saved rates could no longer be corrected or removed from the UI.
//
// Hidden entirely when nothing is unmapped and nothing is overridden.
// ---------------------------------------------------------------------------

function CustomPricingBar({ usage }: { usage: RuntimeUsage[] }) {
  const { t } = useT("runtimes");
  const [dialogOpen, setDialogOpen] = useState(false);
  // Boolean (not the object) so the selector stays referentially stable.
  const hasOverrides = useCustomPricingStore(
    (s) => Object.keys(s.pricings).length > 0,
  );
  const unmapped = collectUnmappedModels(usage);
  if (unmapped.length === 0 && !hasOverrides) return null;

  const hasGap = unmapped.length > 0;
  return (
    <div
      role={hasGap ? "alert" : undefined}
      className={cn(
        "flex flex-wrap items-center gap-3 rounded-lg border px-3 py-2 text-xs",
        hasGap ? "border-warning/30 bg-warning/10" : "bg-muted/20",
      )}
    >
      {hasGap ? (
        <>
          <AlertCircle className="h-4 w-4 shrink-0 text-warning" />
          <div className="min-w-0 flex-1 space-y-0.5">
            <p className="text-foreground">
              {t(($) => $.usage.unmapped_notice, { count: unmapped.length })}
            </p>
            <p className="truncate font-mono text-[11px] text-muted-foreground">
              {unmapped.join(", ")}
            </p>
          </div>
        </>
      ) : (
        <p className="min-w-0 flex-1 text-muted-foreground">
          {t(($) => $.usage.custom_pricing.active_notice)}
        </p>
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => setDialogOpen(true)}
      >
        {hasGap
          ? t(($) => $.usage.custom_pricing.open_button)
          : t(($) => $.usage.custom_pricing.edit_button)}
      </Button>
      <CustomPricingDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        unmappedModels={unmapped}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Chart legend — three coloured dots + labels, rendered in WhenChart's
// header so the chart body keeps its full vertical real estate.
// ---------------------------------------------------------------------------

function ChartLegend({ includeCacheRead = false }: { includeCacheRead?: boolean }) {
  const { t } = useT("runtimes");
  // Token-stack mode adds a cache-read pip between output and cache-write to
  // match the four-segment stack of DailyTokensChart. The cost chart drops
  // cache-read because at typical pricing it'd be ~0 px tall in the stack.
  const items = [
    { label: t(($) => $.usage.legend_input), color: "var(--color-chart-1)" },
    { label: t(($) => $.usage.legend_output), color: "var(--color-chart-2)" },
    ...(includeCacheRead
      ? [{ label: t(($) => $.usage.legend_cache_read), color: "var(--color-chart-4)" }]
      : []),
    { label: t(($) => $.usage.legend_cache_write), color: "var(--color-chart-3)" },
  ];
  return (
    <div className="flex items-center gap-3 text-xs text-muted-foreground">
      {items.map((it) => (
        <span key={it.label} className="inline-flex items-center gap-1.5">
          <span
            className="h-2 w-2 rounded-sm"
            style={{ background: it.color }}
          />
          {it.label}
        </span>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Cost-by block: two-tab attribution view (by agent / by model).
// ---------------------------------------------------------------------------

function CostByBlock({
  runtimeId,
  days,
  usage,
  tz,
}: {
  runtimeId: string;
  days: number;
  usage: RuntimeUsage[];
  tz: string;
}) {
  const { t } = useT("runtimes");
  const [tab, setTab] = useState<"agent" | "model">("agent");
  // Memo dep — same reason as WhenChart: aggregateCostBy{Agent,Model} call
  // estimateCost, which now reads the override store.
  const pricings = useCustomPricingStore((s) => s.pricings);

  // by-agent is server-side aggregation (fetched lazily on tab activation).
  // by-model derives from the daily cache the parent already has — free.
  const { data: byAgentRows = [] } = useQuery({
    ...runtimeUsageByAgentOptions(runtimeId, days, tz),
    enabled: tab === "agent",
  });

  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const byAgent = useMemo(
    () => aggregateCostByAgent(byAgentRows),
    [byAgentRows, pricings],
  );
  const byModel = useMemo(
    () => aggregateCostByModel(usage),
    [usage, pricings],
  );

  const caption =
    tab === "agent"
      ? t(($) => $.usage.cost_by_caption_agent, { count: byAgent.length })
      : t(($) => $.usage.cost_by_caption_model, { count: byModel.length });

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-3">
        <div className="flex items-center gap-3">
          <h4 className="text-sm font-semibold">
            {tab === "agent"
              ? t(($) => $.usage.cost_by_title_agent)
              : t(($) => $.usage.cost_by_title_model)}
          </h4>
          <Segmented
            value={tab}
            onChange={setTab}
            options={
              [
                { label: t(($) => $.usage.cost_by_tab_agent), value: "agent" },
                { label: t(($) => $.usage.cost_by_tab_model), value: "model" },
              ] as const
            }
          />
        </div>
        <span className="text-xs text-muted-foreground">{caption}</span>
      </div>
      <div className="pt-4">
        {tab === "agent" && (
          <CostByList
            rows={byAgent}
            renderKey={(key) => {
              const agent = agents.find((a) => a.id === key);
              return (
                <div className="flex min-w-0 items-center gap-2">
                  <ActorAvatar actorType="agent" actorId={key} size="md" enableHoverCard />
                  <span className="cursor-pointer truncate text-sm font-medium">
                    {agent?.name ?? key}
                  </span>
                </div>
              );
            }}
          />
        )}
        {tab === "model" && (
          <CostByList
            rows={byModel}
            renderKey={(key) => (
              <span className="truncate font-mono text-xs text-foreground">
                {key}
              </span>
            )}
          />
        )}
      </div>
    </div>
  );
}

// Generic horizontal-bar list shared by both Cost-by tabs. Each row scales
// its bar relative to the heaviest row in the set, so the visual ranking
// is always 0..max and the biggest spender visually fills the column.
function CostByList({
  rows,
  renderKey,
  emptyHint,
}: {
  rows: CostByKey[];
  renderKey: (key: string) => React.ReactNode;
  emptyHint?: string;
}) {
  const { t } = useT("runtimes");
  if (rows.length === 0) {
    return (
      <p className="py-4 text-center text-xs text-muted-foreground">
        {emptyHint ?? t(($) => $.usage.empty_no_usage)}
      </p>
    );
  }
  const maxCost = rows.reduce((m, r) => Math.max(m, r.cost), 0);
  return (
    <div className="space-y-2">
      {rows.map((row) => {
        const pct = maxCost > 0 ? (row.cost / maxCost) * 100 : 0;
        return (
          <div
            key={row.key}
            className="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)_5rem_5rem] items-center gap-3 py-1"
          >
            <div className="min-w-0">{renderKey(row.key)}</div>
            <div className="relative h-2 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-chart-1"
                style={{ width: `${pct}%` }}
              />
            </div>
            <div className="text-right text-xs tabular-nums text-muted-foreground">
              {formatTokens(row.tokens)}
            </div>
            <div className="text-right text-sm font-medium tabular-nums">
              ${row.cost.toFixed(2)}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Folded row — single chevron-toggle link revealing the raw breakdown
// table. The Activity heatmap used to live here too; it was promoted to a
// WhenChart toggle, leaving only the breakdown table behind.
// ---------------------------------------------------------------------------

function FoldedRow({ usage }: { usage: RuntimeUsage[] }) {
  const { t } = useT("runtimes");
  const [open, setOpen] = useState(false);
  return (
    <div className="border-t pt-3">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronRight
          className={`h-3 w-3 transition-transform ${open ? "rotate-90" : ""}`}
        />
        {t(($) => $.usage.daily_breakdown_toggle)}
      </button>
      {open && (
        <div className="mt-3 rounded-md border p-4">
          <DailyBreakdownTable usage={usage} />
        </div>
      )}
    </div>
  );
}

function DailyBreakdownTable({ usage }: { usage: RuntimeUsage[] }) {
  const { t } = useT("runtimes");
  const byDate = new Map<string, RuntimeUsage[]>();
  for (const u of usage) {
    const existing = byDate.get(u.date) ?? [];
    existing.push(u);
    byDate.set(u.date, existing);
  }
  return (
    <div className="rounded-lg border">
      <div className="grid grid-cols-[100px_1fr_80px_80px_80px_80px] gap-2 border-b px-3 py-2 text-xs font-medium text-muted-foreground">
        <div>{t(($) => $.usage.table_date)}</div>
        <div>{t(($) => $.usage.table_model)}</div>
        <div className="text-right">{t(($) => $.usage.table_input)}</div>
        <div className="text-right">{t(($) => $.usage.table_output)}</div>
        <div className="text-right">{t(($) => $.usage.table_cache_r)}</div>
        <div className="text-right">{t(($) => $.usage.table_cache_w)}</div>
      </div>
      <div className="max-h-64 overflow-y-auto divide-y">
        {[...byDate.entries()].map(([date, rows]) =>
          rows.map((row, i) => (
            <div
              key={`${date}-${row.model}-${i}`}
              className="grid grid-cols-[100px_1fr_80px_80px_80px_80px] gap-2 px-3 py-1.5 text-xs"
            >
              <div className="text-muted-foreground">{date}</div>
              <div className="truncate font-mono">{row.model}</div>
              <div className="text-right tabular-nums">
                {formatTokens(row.input_tokens)}
              </div>
              <div className="text-right tabular-nums">
                {formatTokens(row.output_tokens)}
              </div>
              <div className="text-right tabular-nums">
                {formatTokens(row.cache_read_tokens)}
              </div>
              <div className="text-right tabular-nums">
                {formatTokens(row.cache_write_tokens)}
              </div>
            </div>
          )),
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Loading + empty states
// ---------------------------------------------------------------------------

function UsageSkeleton() {
  return (
    <div className="space-y-5">
      <Skeleton className="h-28 rounded-lg" />
      <Skeleton className="h-56 rounded-lg" />
      <Skeleton className="h-32" />
    </div>
  );
}

function UsageEmpty() {
  const { t } = useT("runtimes");
  return (
    <div className="flex flex-col items-center rounded-lg border border-dashed py-8">
      <BarChart3 className="h-5 w-5 text-muted-foreground/40" />
      <p className="mt-2 text-xs text-muted-foreground">
        {t(($) => $.usage.no_data)}
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface UsageTotals {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  cacheSavings: number;
}

function computeTotals(rows: RuntimeUsage[]): UsageTotals {
  return rows.reduce<UsageTotals>(
    (acc, u) => ({
      input: acc.input + u.input_tokens,
      output: acc.output + u.output_tokens,
      cacheRead: acc.cacheRead + u.cache_read_tokens,
      cacheWrite: acc.cacheWrite + u.cache_write_tokens,
      cost: acc.cost + estimateCost(u),
      cacheSavings: acc.cacheSavings + estimateCacheSavings(u),
    }),
    { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, cacheSavings: 0 },
  );
}
