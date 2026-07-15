"use client";

import { useEffect, useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight } from "lucide-react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  analyticsQueryOptions,
  type AnalyticsDimension,
  type AnalyticsGrain,
  type AnalyticsMetric,
  type AnalyticsOperator,
  type AnalyticsQuery,
  type AnalyticsQueryResult,
} from "@multica/cerebro-usage";
import {
  filtersToSearchParams,
  removeAnalyticsFilterValue,
  toggleAnalyticsFilter,
  type AnalyticsFilter,
} from "../../core/analytics";
import { useDashboardStore } from "../../core/store";
import { timeRangeToDays } from "../../core/types";
import { RunDetailDrawer, type RunRow } from "./run-detail-drawer";
import { RunsToolbar } from "./runs-toolbar";

type Result = AnalyticsQueryResult | undefined;
type HeatGrain = Extract<AnalyticsGrain, "hour" | "day" | "month">;

const DAY_MS = 86_400_000;

function num(value: unknown): number {
  return typeof value === "number" ? value : Number(value) || 0;
}
function str(value: unknown): string | null {
  if (value == null || value === "") return null;
  return String(value);
}
function dollars(cents: unknown): string {
  return `$${(num(cents) / 100).toLocaleString("en", { maximumFractionDigits: 0 })}`;
}
function compact(value: unknown): string {
  const n = num(value);
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}m`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toLocaleString("en");
}
function pct(rate: unknown): string {
  return `${Math.round(num(rate) * 100)}%`;
}
function passClass(rate: number): string {
  return rate >= 0.85 ? "text-emerald-600" : rate >= 0.7 ? "text-amber-600" : "text-rose-600";
}

// Runs control room. Bespoke layout for the FIR-2996 Dashboard "Runs" tab,
// built directly on the canonical analytics engine so every panel shares the
// same filters, time range and drill-down. Clicking any cell filters the whole
// view; clicking a run row opens the debug/trace rail.
export function RunsControlRoom({ workspaceId, filters, onFiltersChange, onNewVisual, onRunPanelOpen }: { workspaceId: string; filters: AnalyticsFilter[]; onFiltersChange: React.Dispatch<React.SetStateAction<AnalyticsFilter[]>>; onNewVisual: () => void; onRunPanelOpen?: () => void }) {
  const range = useDashboardStore((s) => s.range);
  const setRange = useDashboardStore((s) => s.setRange);
  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", []);
  const [heatGrain, setHeatGrain] = useState<HeatGrain>("hour");
  const [search, setSearch] = useState("");
  const [runsCursors, setRunsCursors] = useState<string[]>([]);
  const [panelCursors, setPanelCursors] = useState<Record<string, string[]>>({});
  const [selectedRun, setSelectedRun] = useState<RunRow | null>(null);
  const [compactLayout, setCompactLayout] = useState(false);

  // Time-range floor, recomputed only when the range changes so query keys stay
  // stable across renders. Every panel query inherits this filter.
  const sinceFilter = useMemo<{ dimension: AnalyticsDimension; operator: AnalyticsOperator; values: string[] }>(() => {
    const since = new Date(Date.now() - timeRangeToDays(range) * DAY_MS).toISOString();
    return { dimension: "time", operator: "gte", values: [since] };
  }, [range]);

  useEffect(() => {
    const params = filtersToSearchParams(filters, new URLSearchParams(window.location.search));
    const search = params.toString();
    window.history.replaceState(null, "", `${window.location.pathname}${search ? `?${search}` : ""}`);
  }, [filters]);

  const baseFilters = useMemo(() => [...filters, sinceFilter], [filters, sinceFilter]);

  const runsCursor = runsCursors.at(-1);
  const panelPage = (key: string, limit: number) => ({
    limit,
    ...(panelCursors[key]?.at(-1) ? { cursor: panelCursors[key]!.at(-1) } : {}),
  });
  const build = (
    metrics: AnalyticsMetric[],
    dimensions: AnalyticsDimension[],
    extra: Partial<AnalyticsQuery> = {},
  ): AnalyticsQuery => ({
    population: "all",
    metrics,
    dimensions,
    grain: "none",
    filters: baseFilters,
    page: { limit: 12 },
    timezone,
    ...extra,
  });

  const queries = useQueries({
    queries: [
      // 0 KPI totals
      analyticsQueryOptions(workspaceId, build(
        ["runs", "input_tokens", "output_tokens", "cost_cents", "missing_cost_runs", "saved_cents", "quality_pass_rate", "skill_invocations"],
        [],
        { page: { limit: 1 } },
      )),
      // 1 activity heatmap
      analyticsQueryOptions(workspaceId, build(["runs"], ["time"], {
        grain: heatGrain,
        page: panelPage("activity", heatGrain === "hour" ? 168 : heatGrain === "day" ? 62 : 12),
        sort: [{ field: "time", direction: "asc" }],
      })),
      // 2 runs & savings over time
      analyticsQueryOptions(workspaceId, build(["runs", "saved_cents"], ["time"], {
        grain: "day",
        page: panelPage("time-series", 62),
        sort: [{ field: "time", direction: "asc" }],
      })),
      // 3 by model
      analyticsQueryOptions(workspaceId, build(["runs"], ["model"], { page: panelPage("model", 6) })),
      // 4 by outcome
      analyticsQueryOptions(workspaceId, build(["runs"], ["status"], { page: panelPage("outcome", 6) })),
      // 5 usage by source
      analyticsQueryOptions(workspaceId, build(["runs"], ["source"], { page: panelPage("sources", 8) })),
      // 6 people x source
      analyticsQueryOptions(workspaceId, build(["runs"], ["person", "source"], { page: panelPage("people-source", 60) })),
      // 7 provider / model / skill chips
      analyticsQueryOptions(workspaceId, build(["runs", "skill_invocations"], ["provider", "model", "skill"], { page: panelPage("chips", 12) })),
      // 8 people x project
      analyticsQueryOptions(workspaceId, build(["runs", "cost_cents", "quality_pass_rate"], ["person", "project"], { page: panelPage("people-project", 200) })),
      // 9 quality observations by category
      analyticsQueryOptions(workspaceId, build(["runs", "quality_pass_rate"], ["quality_category"], { page: panelPage("quality", 8) })),
      // 10 blocking categories by type
      analyticsQueryOptions(workspaceId, build(["runs"], ["quality_type"], { page: panelPage("blocking", 8) })),
      // 11 matching runs
      analyticsQueryOptions(workspaceId, build(
        ["cost_cents", "skill_invocations", "input_tokens", "output_tokens", "saved_cents", "duration_seconds"],
        ["run", "time", "person", "source", "runtime", "provider", "model", "status", "cost_kind", "reference_label", "debug_link", "trace"],
        { page: { limit: 25, ...(runsCursor ? { cursor: runsCursor } : {}) }, sort: [{ field: "time", direction: "desc" }] },
      )),
    ],
  });

  const [kpi, heat, timeSeries, byModel, byOutcome, bySource, peopleSource, chips, peopleProject, qualityObs, blocking, runsResult] =
    queries.map((q) => q.data) as Result[];
  const loading = queries.some((q) => q.isLoading);

  const onFilter = (dimension: AnalyticsDimension, value: string, operator: "in" | "not_in" = "in") => {
    if (!value) return;
    onFiltersChange((current) => toggleAnalyticsFilter(current, dimension, value, operator));
    setRunsCursors([]);
    setPanelCursors({});
  };
  const onRemoveFilter = (dimension: AnalyticsDimension, value: string, operator: AnalyticsOperator) => {
    onFiltersChange((current) => removeAnalyticsFilterValue(current, dimension, value, operator));
    setRunsCursors([]);
    setPanelCursors({});
  };
  const onTimeBucketFilter = (value: string) => {
    const start = new Date(value);
    if (Number.isNaN(start.getTime())) return;
    const end = new Date(start);
    if (heatGrain === "hour") end.setHours(end.getHours() + 1);
    else if (heatGrain === "month") end.setMonth(end.getMonth() + 1);
    else end.setDate(end.getDate() + 1);
    onFiltersChange((current) => [
      ...current.filter((filter) => !(filter.dimension === "time" && (filter.operator === "gte" || filter.operator === "lte"))),
      { dimension: "time", operator: "gte", values: [start.toISOString()] },
      { dimension: "time", operator: "lte", values: [end.toISOString()] },
    ]);
    setRunsCursors([]);
    setPanelCursors({});
  };

  const nextPanel = (key: string, result: Result) => {
    if (!result?.next_cursor) return;
    setPanelCursors((current) => ({
      ...current,
      [key]: [...(current[key] ?? []), result.next_cursor as string],
    }));
  };
  const previousPanel = (key: string) => {
    setPanelCursors((current) => ({
      ...current,
      [key]: (current[key] ?? []).slice(0, -1),
    }));
  };

  const kpiRow = (kpi?.rows ?? [])[0] ?? {};
  const gatePass = num(kpiRow.quality_pass_rate);

  return (
    <div className="flex min-h-0 flex-1">
      <div className={`min-w-0 flex-1 p-6 ${compactLayout ? "space-y-2" : "space-y-3"}`}>
        <RunsToolbar
          range={range}
          onRangeChange={setRange}
          filters={filters}
          onAddFilter={onFilter}
          onRemoveFilter={onRemoveFilter}
          onClear={() => { onFiltersChange([]); setRunsCursors([]); setPanelCursors({}); }}
          onCustomize={() => setCompactLayout((compact) => !compact)}
          onNewVisual={() => {
            setSelectedRun(null);
            onNewVisual();
          }}
        />

        {/* KPI strip */}
        <section aria-label="Usage KPIs" className="grid grid-cols-2 divide-x divide-y overflow-hidden rounded-lg border bg-card sm:grid-cols-3 lg:grid-cols-5 lg:divide-y-0">
          <Kpi label="Runs" value={compact(kpiRow.runs)} note="in selected range" />
          <Kpi label="Tokens" value={compact(num(kpiRow.input_tokens) + num(kpiRow.output_tokens))} note="input + output" />
          <Kpi label="Gateway cost" value={dollars(kpiRow.cost_cents)} note="measured spend" />
          <Kpi label="Measured savings" value={dollars(kpiRow.saved_cents)} note="graphify & context" accent="text-emerald-600" />
          <Kpi label="Missing cost data" value={compact(kpiRow.missing_cost_runs)} note="runs without cost" accent={num(kpiRow.missing_cost_runs) > 0 ? "text-amber-600" : "text-emerald-600"} />
        </section>

        {/* Activity grid */}
        <Panel
          title="Activity grid"
          meta="Runs by time · click cell to drill down"
          right={<PanelPager label="Activity grid" page={(panelCursors.activity?.length ?? 0) + 1} canPrevious={(panelCursors.activity?.length ?? 0) > 0} canNext={Boolean(heat?.next_cursor)} onPrevious={() => previousPanel("activity")} onNext={() => nextPanel("activity", heat)} onConfigure={onNewVisual} />}
        >
          <div className="flex flex-wrap items-center justify-between gap-2 px-4 pt-4">
            <Segments options={["hour", "day", "month"] as HeatGrain[]} value={heatGrain} onChange={(value) => { setHeatGrain(value); setPanelCursors((current) => ({ ...current, activity: [] })); }} />
            <div className="flex flex-wrap gap-1.5 text-[10px]">
              <span className="rounded border bg-[#f3f2f0] px-2 py-1">Metric: Runs</span>
              <span className="rounded border bg-[#f3f2f0] px-2 py-1">Breakdown: Person</span>
              <span className="rounded border bg-[#f3f2f0] px-2 py-1">Color: Volume</span>
            </div>
          </div>
          <ActivityHeatmap result={heat} onFilter={onTimeBucketFilter} />
          <PanelFooterPager
            label="Activity grid"
            page={(panelCursors.activity?.length ?? 0) + 1}
            canPrevious={(panelCursors.activity?.length ?? 0) > 0}
            canNext={Boolean(heat?.next_cursor)}
            onPrevious={() => previousPanel("activity")}
            onNext={() => nextPanel("activity", heat)}
            summary={heatGrain === "hour" ? "Showing hours 00–23 across 30 days" : `Showing ${heatGrain} buckets across selected range`}
          />
        </Panel>

        <div className="grid gap-3 xl:grid-cols-3">
          {/* Runs & cost over time */}
          <div className="xl:col-span-2">
            <Panel title="Runs & cost over time" meta="Click any point to filter">
              <TimeSeries result={timeSeries} />
              <div className="grid grid-cols-2 gap-4 border-t p-4">
                <BarList title="By model" result={byModel} dimension="model" metric="runs" onFilter={onFilter} />
                <BarList title="By outcome" result={byOutcome} dimension="status" metric="runs" onFilter={onFilter} colorByStatus />
              </div>
              <PanelFooterPager label="Runs and cost" page={(panelCursors["time-series"]?.length ?? 0) + 1} canPrevious={(panelCursors["time-series"]?.length ?? 0) > 0} canNext={Boolean(timeSeries?.next_cursor)} onPrevious={() => previousPanel("time-series")} onNext={() => nextPanel("time-series", timeSeries)} />
            </Panel>
          </div>
          {/* People & sources */}
          <Panel title="People & sources" meta="Click any cell to filter">
            <div className="space-y-4 p-4">
              <Eyebrow>Usage by source</Eyebrow>
              <div className="grid grid-cols-2 gap-2">
                {(bySource?.rows ?? []).map((row, index) => (
                  <button
                    key={`${str(row.source) ?? index}`}
                    type="button"
                    onClick={() => onFilter("source", str(row.source) ?? "")}
                    className="rounded-md border bg-card px-3 py-2 text-left hover:border-primary"
                  >
                    <span className="block text-[10px] text-muted-foreground">{str(row.source) ?? "Unknown"}</span>
                    <span className="block font-mono text-sm font-semibold">{compact(row.runs)}</span>
                  </button>
                ))}
              </div>
              <PeopleSourceMatrix result={peopleSource} onFilter={onFilter} />
              <Eyebrow>Provider · model · skill</Eyebrow>
              <div className="flex flex-wrap gap-1.5">
                {chipItems(chips).map((chip) => (
                  <button
                    key={`${chip.dimension}:${chip.value}`}
                    type="button"
                    onClick={() => onFilter(chip.dimension, chip.value)}
                    className="rounded border bg-muted/40 px-2 py-1 text-[10px] hover:border-primary"
                  >
                    {chip.value} · {compact(chip.count)}
                  </button>
                ))}
              </div>
            </div>
            <PanelFooterPager label="People and sources" page={(panelCursors.sources?.length ?? 0) + 1} canPrevious={(panelCursors.sources?.length ?? 0) > 0} canNext={Boolean(bySource?.next_cursor)} onPrevious={() => previousPanel("sources")} onNext={() => nextPanel("sources", bySource)} />
          </Panel>
        </div>

        <div className="grid gap-3 lg:grid-cols-2">
          <Panel title="People performance" meta="Click person or metric to filter">
            <PeopleTable result={peopleProject} onFilter={onFilter} />
            <PanelFooterPager label="People performance" page={(panelCursors["people-project"]?.length ?? 0) + 1} canPrevious={(panelCursors["people-project"]?.length ?? 0) > 0} canNext={Boolean(peopleProject?.next_cursor)} onPrevious={() => previousPanel("people-project")} onNext={() => nextPanel("people-project", peopleProject)} />
          </Panel>
          <Panel title="Projects" meta="People, usage & quality">
            <ProjectTable result={peopleProject} onFilter={onFilter} />
            <PanelFooterPager label="Projects" page={(panelCursors["people-project"]?.length ?? 0) + 1} canPrevious={(panelCursors["people-project"]?.length ?? 0) > 0} canNext={Boolean(peopleProject?.next_cursor)} onPrevious={() => previousPanel("people-project")} onNext={() => nextPanel("people-project", peopleProject)} />
          </Panel>
        </div>

        {/* Quality */}
        <Panel title="Quality gates & learning signals" meta="Measured data only · click to filter runs">
          <div className="grid lg:grid-cols-[220px_1fr]">
            <div className="border-b p-4 lg:border-b-0 lg:border-r">
              <Eyebrow>Judge gate outcome</Eyebrow>
              <Donut value={gatePass} />
              <p className="mt-2 text-center text-[10px] text-muted-foreground">
                Pass rate from stored judge verdicts, not a universal quality score.
              </p>
            </div>
            <div className="p-4">
              <Eyebrow>Categorized skill-learning observations</Eyebrow>
              <div className="mt-2 space-y-2">
                {(qualityObs?.rows ?? []).map((row, index) => {
                  const rate = num(row.quality_pass_rate);
                  return (
                    <button
                      key={`${str(row.quality_category) ?? index}`}
                      type="button"
                      onClick={() => onFilter("quality_category", str(row.quality_category) ?? "")}
                      className="grid w-full grid-cols-[150px_1fr_44px] items-center gap-3 text-left text-xs hover:opacity-80"
                    >
                      <span className="truncate text-muted-foreground">{str(row.quality_category) ?? "Uncategorized"}</span>
                      <span className="h-1.5 overflow-hidden rounded bg-muted">
                        <span className="block h-full rounded bg-emerald-500" style={{ width: `${Math.min(100, rate * 100)}%` }} />
                      </span>
                      <span className={`text-right font-mono ${passClass(rate)}`}>{pct(rate)}</span>
                    </button>
                  );
                })}
                {(qualityObs?.rows ?? []).length === 0 && (
                  <p className="text-xs text-muted-foreground">No categorized quality data yet.</p>
                )}
              </div>
              <div className="mt-4">
                <Eyebrow>Latest blocking categories</Eyebrow>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {(blocking?.rows ?? []).map((row, index) => (
                    <button
                      key={`${str(row.quality_type) ?? index}`}
                      type="button"
                      onClick={() => onFilter("quality_type", str(row.quality_type) ?? "")}
                      className="rounded border bg-muted/40 px-2 py-1 text-[10px] hover:border-primary"
                    >
                      {str(row.quality_type) ?? "Unknown"} · {compact(row.runs)}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          </div>
          <PanelFooterPager label="Quality" page={(panelCursors.quality?.length ?? 0) + 1} canPrevious={(panelCursors.quality?.length ?? 0) > 0} canNext={Boolean(qualityObs?.next_cursor)} onPrevious={() => previousPanel("quality")} onNext={() => nextPanel("quality", qualityObs)} />
        </Panel>

        {/* Matching runs */}
        <Panel
          title="Matching runs"
          meta="Every cell filters · click a row to trace"
          right={
            <input
              aria-label="Search runs"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Filter loaded runs"
              className="h-7 w-44 rounded-md border bg-muted/30 px-2 text-xs"
            />
          }
        >
          <RunsTable
            result={runsResult}
            search={search}
            selectedRunId={selectedRun ? (str(selectedRun.run) ?? "") : null}
            onSelect={(run) => {
              onRunPanelOpen?.();
              setSelectedRun(run);
            }}
            onFilter={onFilter}
            canPrevious={runsCursors.length > 0}
            onPrevious={() => setRunsCursors((c) => c.slice(0, -1))}
            onNext={() => runsResult?.next_cursor && setRunsCursors((c) => [...c, runsResult.next_cursor as string])}
          />
        </Panel>

        {loading && <p className="text-xs text-muted-foreground">Loading dashboard…</p>}
      </div>

      {selectedRun && (
        <RunDetailDrawer
          workspaceId={workspaceId}
          run={selectedRun}
          timezone={timezone}
          onClose={() => setSelectedRun(null)}
        />
      )}
    </div>
  );
}

// ---------- shared layout ----------

function Panel({
  title,
  meta,
  right,
  children,
}: {
  title: string;
  meta?: string;
  right?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section aria-label={title} className="min-w-0 overflow-hidden rounded-lg border bg-card">
      <header className="flex items-center justify-between border-b px-4 py-2.5">
        <div>
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h2>
        </div>
        <div className="flex items-center gap-3">
          {meta && <span className="hidden text-[11px] text-muted-foreground sm:inline">{meta}</span>}
          {right}
        </div>
      </header>
      {children}
    </section>
  );
}

export function PanelPager({ label = "Activity grid", page, canPrevious, canNext, onPrevious, onNext, onConfigure }: { label?: string; page: number; canPrevious: boolean; canNext: boolean; onPrevious: () => void; onNext: () => void; onConfigure?: () => void }) {
  return (
    <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
      {onConfigure && <button type="button" aria-label={`Configure ${label}`} onClick={onConfigure} className="mr-1 rounded border bg-[#f3f2f0] px-2 py-1 text-[#4b4852] hover:border-[#6557d8]">Configure</button>}
      <button type="button" aria-label={`Previous ${label} page`} disabled={!canPrevious} onClick={onPrevious} className="rounded border p-1 disabled:opacity-40"><ChevronLeft className="size-3" /></button>
      <span className="min-w-12 text-center">Page {page}</span>
      <button type="button" aria-label={`Next ${label} page`} disabled={!canNext} onClick={onNext} className="rounded border p-1 disabled:opacity-40"><ChevronRight className="size-3" /></button>
    </div>
  );
}

export function PanelFooterPager({ summary, ...props }: Omit<React.ComponentProps<typeof PanelPager>, "onConfigure"> & { summary?: string }) {
  return (
    <footer className={`flex items-center border-t px-3 py-2 ${summary ? "justify-between" : "justify-end"}`}>
      {summary && <span className="text-[10px] text-muted-foreground">{summary}</span>}
      <PanelPager {...props} />
    </footer>
  );
}

function Kpi({ label, value, note, accent }: { label: string; value: string; note: string; accent?: string }) {
  return (
    <div className="p-4">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className={`mt-1 font-mono text-xl font-semibold ${accent ?? ""}`}>{value}</div>
      <div className="mt-1 text-[10px] text-muted-foreground">{note}</div>
    </div>
  );
}

function Eyebrow({ children }: { children: React.ReactNode }) {
  return <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{children}</div>;
}

function Segments<T extends string>({ options, value, onChange }: { options: T[]; value: T; onChange: (value: T) => void }) {
  return (
    <div className="inline-flex overflow-hidden rounded-md border">
      {options.map((option) => (
        <button
          key={option}
          type="button"
          onClick={() => onChange(option)}
          className={`h-7 px-2.5 text-[11px] capitalize ${option === value ? "bg-[rgba(101,87,216,0.10)] font-semibold text-[#6557d8]" : "text-muted-foreground hover:text-foreground"}`}
        >
          {option}
        </button>
      ))}
    </div>
  );
}

// ---------- panels ----------

export function ActivityHeatmap({ result, onFilter }: { result: Result; onFilter: (value: string) => void }) {
  const rows = result?.rows ?? [];
  const max = rows.reduce((acc, row) => Math.max(acc, num(row.runs)), 0);
  const level = (value: number) => {
    if (max === 0 || value === 0) return "bg-muted";
    const ratio = value / max;
    if (ratio > 0.75) return "bg-[#6557d8]";
    if (ratio > 0.5) return "bg-[#8e82df]";
    if (ratio > 0.25) return "bg-[#bdb5ef]";
    return "bg-[#ddd9fa]";
  };
  if (rows.length === 0) {
    return <p className="p-6 text-center text-xs text-muted-foreground">No activity in this range.</p>;
  }
  return (
    <div
      role="grid"
      aria-label="Activity by time bucket"
      className="grid gap-1 overflow-hidden p-4"
      style={{ gridTemplateColumns: "repeat(42, minmax(0, 1fr))" }}
    >
      {rows.map((row, index) => {
        const value = num(row.runs);
        const label = str(row.time) ?? "";
        return (
          <button
            key={`${label}:${index}`}
            type="button"
            title={`${label} · ${value} runs`}
            onClick={() => onFilter(label)}
            className={`h-4 min-w-3 rounded-sm ${level(value)} hover:ring-2 hover:ring-foreground/40`}
          />
        );
      })}
    </div>
  );
}

export function TimeSeries({ result }: { result: Result }) {
  const series = (result?.rows ?? []).map((row) => ({
    t: shortDay(str(row.time)),
    Runs: num(row.runs),
    Savings: Math.round(num(row.saved_cents) / 100),
  }));
  if (series.length === 0) {
    return <div className="flex h-44 items-center justify-center text-xs text-muted-foreground">No runs in this range.</div>;
  }
  return (
    <div aria-label="Runs and savings trend" className="relative h-44 p-3 pt-8">
      <div className="absolute right-4 top-3 flex gap-3 text-[10px] text-muted-foreground">
        <span><i className="mr-1 inline-block size-2 rounded-full bg-[#8b7cf6]" />Runs</span>
        <span><i className="mr-1 inline-block size-2 rounded-full bg-[#65c18c]" />Savings</span>
      </div>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={series} margin={{ top: 6, right: 8, bottom: 0, left: -12 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
          <XAxis dataKey="t" tick={false} tickLine={false} axisLine={false} />
          <YAxis yAxisId="left" tick={false} tickLine={false} axisLine={false} width={0} />
          <YAxis yAxisId="right" orientation="right" tick={false} tickLine={false} axisLine={false} width={0} />
          <Tooltip contentStyle={{ fontSize: 11 }} />
          <Line yAxisId="left" type="monotone" dataKey="Runs" stroke="#8b7cf6" strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
          <Line yAxisId="right" type="monotone" dataKey="Savings" stroke="#65c18c" strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

function BarList({
  title,
  result,
  dimension,
  metric,
  onFilter,
  colorByStatus,
}: {
  title: string;
  result: Result;
  dimension: AnalyticsDimension;
  metric: AnalyticsMetric;
  onFilter: (dimension: AnalyticsDimension, value: string) => void;
  colorByStatus?: boolean;
}) {
  const rows = result?.rows ?? [];
  const max = rows.reduce((acc, row) => Math.max(acc, num(row[metric])), 0) || 1;
  const color = (value: string) => {
    if (!colorByStatus) return "bg-[#6557d8]";
    const v = value.toLowerCase();
    if (v === "completed") return "bg-emerald-500";
    if (v === "failed" || v === "error") return "bg-rose-500";
    return "bg-amber-500";
  };
  return (
    <div>
      <Eyebrow>{title}</Eyebrow>
      <div className="mt-2 space-y-2">
        {rows.map((row, index) => {
          const label = str(row[dimension]) ?? "Unknown";
          const value = num(row[metric]);
          return (
            <button
              key={`${label}:${index}`}
              type="button"
              onClick={() => onFilter(dimension, label)}
              className="grid w-full grid-cols-[80px_1fr_36px] items-center gap-2 text-left text-[11px] hover:opacity-80"
            >
              <span className="truncate text-muted-foreground">{label}</span>
              <span className="h-1.5 overflow-hidden rounded bg-muted">
                <span className={`block h-full rounded ${color(label)}`} style={{ width: `${(value / max) * 100}%` }} />
              </span>
              <span className="text-right font-mono">{compact(value)}</span>
            </button>
          );
        })}
        {rows.length === 0 && <p className="text-[11px] text-muted-foreground">No data.</p>}
      </div>
    </div>
  );
}

function PeopleSourceMatrix({ result, onFilter }: { result: Result; onFilter: (dimension: AnalyticsDimension, value: string) => void }) {
  const rows = result?.rows ?? [];
  const sources = [...new Set(rows.map((row) => str(row.source) ?? "—"))].slice(0, 4);
  const byPerson = new Map<string, Map<string, number>>();
  for (const row of rows) {
    const person = str(row.person) ?? "Unknown";
    const source = str(row.source) ?? "—";
    const map = byPerson.get(person) ?? new Map();
    map.set(source, num(row.runs));
    byPerson.set(person, map);
  }
  const people = [...byPerson.entries()]
    .map(([person, map]) => ({ person, map, total: [...map.values()].reduce((a, b) => a + b, 0) }))
    .sort((a, b) => b.total - a.total)
    .slice(0, 4);
  if (people.length === 0) return null;
  return (
    <div>
      <Eyebrow>People × source</Eyebrow>
      <div className="mt-2 space-y-1">
        {people.map(({ person, map }) => (
          <div key={person} className="grid grid-cols-[64px_repeat(4,1fr)] items-center gap-1 text-[10px]">
            <button type="button" onClick={() => onFilter("person", person)} className="truncate text-left font-medium hover:text-primary">
              {person}
            </button>
            {sources.map((source) => (
              <span key={source} className="rounded bg-muted/50 px-1 py-0.5 text-center">
                {map.get(source) ?? 0}
              </span>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function chipItems(result: Result): { dimension: AnalyticsDimension; value: string; count: number }[] {
  const rows = result?.rows ?? [];
  const seen = new Map<string, { dimension: AnalyticsDimension; value: string; count: number }>();
  const add = (dimension: AnalyticsDimension, value: string | null, count: number) => {
    if (!value) return;
    const key = `${dimension}:${value}`;
    const existing = seen.get(key);
    if (existing) existing.count += count;
    else seen.set(key, { dimension, value, count });
  };
  for (const row of rows) {
    add("runtime", str(row.runtime), num(row.runs));
    add("provider", str(row.provider), num(row.runs));
    add("model", str(row.model), num(row.runs));
    add("skill", str(row.skill), num(row.skill_invocations));
  }
  return [...seen.values()].sort((a, b) => b.count - a.count).slice(0, 8);
}

type PersonAgg = { runs: number; cost: number; projects: Set<string>; passSum: number; passRuns: number };

function aggregatePeopleProject(result: Result) {
  const rows = result?.rows ?? [];
  const people = new Map<string, PersonAgg>();
  const projects = new Map<string, { runs: number; cost: number; people: Set<string>; passSum: number; passRuns: number }>();
  for (const row of rows) {
    const person = str(row.person) ?? "Unknown";
    const project = str(row.project) ?? "Unassigned";
    const runs = num(row.runs);
    const cost = num(row.cost_cents);
    const rate = num(row.quality_pass_rate);
    const p = people.get(person) ?? { runs: 0, cost: 0, projects: new Set(), passSum: 0, passRuns: 0 };
    p.runs += runs;
    p.cost += cost;
    p.projects.add(project);
    p.passSum += rate * runs;
    p.passRuns += runs;
    people.set(person, p);
    const pr = projects.get(project) ?? { runs: 0, cost: 0, people: new Set(), passSum: 0, passRuns: 0 };
    pr.runs += runs;
    pr.cost += cost;
    pr.people.add(person);
    pr.passSum += rate * runs;
    pr.passRuns += runs;
    projects.set(project, pr);
  }
  return { people, projects };
}

export function PeopleTable({ result, onFilter }: { result: Result; onFilter: (dimension: AnalyticsDimension, value: string) => void }) {
  const { people } = aggregatePeopleProject(result);
  const rows = [...people.entries()].sort((a, b) => b[1].runs - a[1].runs).slice(0, 6);
  return (
    <table className="w-full text-left text-xs">
      <thead className="text-[10px] uppercase tracking-wide text-muted-foreground">
        <tr><Th>Person</Th><Th>Projects</Th><Th>Runs</Th><Th>Cost</Th><Th>Gate pass</Th></tr>
      </thead>
      <tbody>
        {rows.map(([person, agg]) => {
          const rate = agg.passRuns ? agg.passSum / agg.passRuns : 0;
          return (
            <tr key={person} className="cursor-pointer border-t hover:bg-primary/5" onClick={() => onFilter("person", person)}>
              <td className="px-4 py-2 font-medium">
                <span className="mr-2 inline-grid size-[22px] place-items-center rounded-full bg-[rgba(101,87,216,0.10)] font-semibold text-[#6557d8]">
                  {person.charAt(0).toUpperCase() || "?"}
                </span>
                {person}
              </td>
              <td className="px-4 py-2 tabular-nums">{agg.projects.size}</td>
              <td className="px-4 py-2 font-mono">{compact(agg.runs)}</td>
              <td className="px-4 py-2 font-mono">{dollars(agg.cost)}</td>
              <td className={`px-4 py-2 font-mono ${passClass(rate)}`}>{pct(rate)}</td>
            </tr>
          );
        })}
        {rows.length === 0 && <tr><td colSpan={5} className="px-4 py-6 text-center text-muted-foreground">No runs in this range.</td></tr>}
      </tbody>
    </table>
  );
}

function ProjectTable({ result, onFilter }: { result: Result; onFilter: (dimension: AnalyticsDimension, value: string) => void }) {
  const { projects } = aggregatePeopleProject(result);
  const rows = [...projects.entries()].sort((a, b) => b[1].runs - a[1].runs).slice(0, 6);
  return (
    <table className="w-full text-left text-xs">
      <thead className="text-[10px] uppercase tracking-wide text-muted-foreground">
        <tr><Th>Project</Th><Th>People</Th><Th>Runs</Th><Th>Cost</Th><Th>Gate pass</Th></tr>
      </thead>
      <tbody>
        {rows.map(([project, agg]) => {
          const rate = agg.passRuns ? agg.passSum / agg.passRuns : 0;
          return (
            <tr key={project} className="cursor-pointer border-t hover:bg-primary/5" onClick={() => onFilter("project", project)}>
              <td className="px-4 py-2 font-medium">{project}</td>
              <td className="px-4 py-2 tabular-nums">{agg.people.size}</td>
              <td className="px-4 py-2 font-mono">{compact(agg.runs)}</td>
              <td className="px-4 py-2 font-mono">{dollars(agg.cost)}</td>
              <td className={`px-4 py-2 font-mono ${passClass(rate)}`}>{pct(rate)}</td>
            </tr>
          );
        })}
        {rows.length === 0 && <tr><td colSpan={5} className="px-4 py-6 text-center text-muted-foreground">No runs in this range.</td></tr>}
      </tbody>
    </table>
  );
}

function RunsTable({
  result,
  search,
  selectedRunId,
  onSelect,
  onFilter,
  canPrevious,
  onPrevious,
  onNext,
}: {
  result: Result;
  search: string;
  selectedRunId: string | null;
  onSelect: (run: RunRow) => void;
  onFilter: (dimension: AnalyticsDimension, value: string) => void;
  canPrevious: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) {
  const term = search.trim().toLowerCase();
  const rows = (result?.rows ?? []).filter((row) => {
    if (!term) return true;
    return [row.person, row.reference_label, row.provider, row.model, row.status]
      .map((value) => String(value ?? "").toLowerCase())
      .some((value) => value.includes(term));
  });
  return (
    <div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-xs">
          <thead className="text-[10px] uppercase tracking-wide text-muted-foreground">
            <tr><Th>Started</Th><Th>Source</Th><Th>Person → provider → model</Th><Th>Context</Th><Th>Status</Th><Th>Cost</Th><Th>Skills</Th></tr>
          </thead>
          <tbody>
            {rows.map((row, index) => {
              const runId = str(row.run) ?? String(index);
              const status = str(row.status) ?? "unknown";
              return (
                <tr
                  key={runId}
                  onClick={() => onSelect(row)}
                  className={`cursor-pointer border-t hover:bg-primary/5 ${selectedRunId === runId ? "bg-primary/10" : ""}`}
                >
                  <td className="px-4 py-2">
                    <button type="button" aria-label={`Open run context ${str(row.reference_label) ?? runId}`} onClick={(event) => { event.stopPropagation(); onSelect(row); }} className="text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">
                      {shortStamp(str(row.time))}
                    </button>
                  </td>
                  <td className="px-4 py-2">
                    <Cell onClick={(event) => { event.stopPropagation(); onFilter("source", str(row.source) ?? ""); }} badge>{str(row.source) ?? "—"}</Cell>
                  </td>
                  <td className="px-4 py-2">
                    <span className="flex items-center gap-1 text-[11px]">
                      <Cell onClick={(event) => { event.stopPropagation(); onFilter("person", str(row.person) ?? ""); }}>{str(row.person) ?? "—"}</Cell>
                      <span className="text-muted-foreground">›</span>
                      <Cell onClick={(event) => { event.stopPropagation(); onFilter("provider", str(row.provider) ?? ""); }}>{str(row.provider) ?? "—"}</Cell>
                      <span className="text-muted-foreground">›</span>
                      <Cell onClick={(event) => { event.stopPropagation(); onFilter("model", str(row.model) ?? ""); }}>{str(row.model) ?? "—"}</Cell>
                    </span>
                  </td>
                  <td className="max-w-[160px] px-4 py-2">
                    <Cell onClick={(event) => { event.stopPropagation(); onFilter("reference_label", str(row.reference_label) ?? ""); }}>{str(row.reference_label) ?? "—"}</Cell>
                  </td>
                  <td className="px-4 py-2">
                    <Cell onClick={(event) => { event.stopPropagation(); onFilter("status", status); }}>
                      <StatusPill status={status} />
                    </Cell>
                  </td>
                  <td className="px-4 py-2 font-mono">
                    <Cell onClick={(event) => { event.stopPropagation(); onFilter("cost_kind", str(row.cost_kind) ?? ""); }}>
                      {str(row.cost_kind) === "missing" ? "Missing" : dollars(row.cost_cents)}
                    </Cell>
                  </td>
                  <td className="px-4 py-2 tabular-nums">{compact(row.skill_invocations)}</td>
                </tr>
              );
            })}
            {rows.length === 0 && <tr><td colSpan={7} className="px-4 py-8 text-center text-muted-foreground">No runs match the filters.</td></tr>}
          </tbody>
        </table>
      </div>
      <footer className="flex items-center justify-between border-t px-4 py-2 text-[11px] text-muted-foreground">
        <span>{rows.length} runs on this page</span>
        <div className="flex gap-1">
          <button type="button" aria-label="Previous runs page" disabled={!canPrevious} onClick={onPrevious} className="rounded border p-1 disabled:opacity-40">
            <ChevronLeft className="size-3.5" />
          </button>
          <button type="button" aria-label="Next runs page" disabled={!result?.next_cursor} onClick={onNext} className="rounded border p-1 disabled:opacity-40">
            <ChevronRight className="size-3.5" />
          </button>
        </div>
      </footer>
    </div>
  );
}

export function Donut({ value }: { value: number }) {
  const pctValue = Math.round(value * 100);
  const reviseEnd = pctValue + Math.round((100 - pctValue) * 0.65);
  return (
    <div
      aria-label={`Judge gate outcome ${pctValue}%`}
      className="relative mx-auto mt-2 size-28 rounded-full"
    >
      <svg aria-hidden="true" viewBox="0 0 112 112" className="size-full -rotate-90">
        <circle cx="56" cy="56" r="48" pathLength="100" fill="none" stroke="#ff3b68" strokeWidth="16" />
        <circle cx="56" cy="56" r="48" pathLength="100" fill="none" stroke="#f0b429" strokeWidth="16" strokeDasharray={`${reviseEnd} ${100 - reviseEnd}`} />
        <circle cx="56" cy="56" r="48" pathLength="100" fill="none" stroke="#00a56a" strokeWidth="16" strokeDasharray={`${pctValue} ${100 - pctValue}`} />
      </svg>
      <div className="absolute inset-4 grid place-items-center rounded-full bg-card font-mono text-lg font-bold">{pctValue}%</div>
    </div>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return <th className="px-4 py-2 font-medium">{children}</th>;
}
function Badge({ children }: { children: React.ReactNode }) {
  return <span className="rounded bg-[#f1efff] px-1.5 py-0.5 text-[10px] text-[#5748ce]">{children}</span>;
}
function Cell({ children, onClick, badge }: { children: React.ReactNode; onClick: (event: React.MouseEvent) => void; badge?: boolean }) {
  return (
    <button type="button" onClick={onClick} className="max-w-full truncate rounded px-1 py-0.5 text-left hover:bg-muted">
      {badge ? <Badge>{children}</Badge> : children}
    </button>
  );
}
function StatusPill({ status }: { status: string }) {
  const v = status.toLowerCase();
  const cls = v === "completed" ? "bg-emerald-500/10 text-emerald-600" : v === "failed" || v === "error" ? "bg-rose-500/10 text-rose-600" : "bg-amber-500/10 text-amber-600";
  return <span className={`rounded px-1.5 py-0.5 text-[10px] capitalize ${cls}`}>{status}</span>;
}

function shortDay(value: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value.slice(5, 10);
  return date.toLocaleDateString("en", { day: "2-digit", month: "short" });
}
function shortStamp(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("en", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}
