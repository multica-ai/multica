"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { dashboardOptions } from "@multica/core/time-entries/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  Clock,
  ChevronLeft,
  ChevronRight,
  TrendingUp,
  CalendarDays,
  Hash,
  CheckCircle2,
  AlertCircle,
  Loader2,
  MinusCircle,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../layout/page-header";
import { cn } from "@multica/ui/lib/utils";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import { BarChart, Bar, XAxis, YAxis, PieChart, Pie, Cell } from "recharts";
import type { TimeEntrySyncStatus } from "@multica/core/types";
import { MonthlyCalendarView } from "./monthly-calendar";
import { useT } from "../i18n";

// ─── Helpers ────────────────────────────────────────────────────────────────

function getWeekRange(offset: number): {
  start: string;
  end: string;
  label: string;
} {
  const now = new Date();
  const day = now.getDay();
  const diff = day === 0 ? 6 : day - 1; // Monday-based
  const monday = new Date(now);
  monday.setDate(now.getDate() - diff + offset * 7);
  monday.setHours(0, 0, 0, 0);

  const sunday = new Date(monday);
  sunday.setDate(monday.getDate() + 6);

  const fmt = (d: Date) => d.toISOString().split("T")[0]!;
  const shortFmt = (d: Date) =>
    d.toLocaleDateString("en-US", { month: "short", day: "numeric" });

  return {
    start: fmt(monday),
    end: fmt(sunday),
    label:
      offset === 0
        ? "This week"
        : offset === -1
          ? "Last week"
          : `${shortFmt(monday)} – ${shortFmt(sunday)}`,
  };
}

function formatMinutes(minutes: number): string {
  if (minutes === 0) return "0h";
  if (minutes < 60) return `${minutes}m`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr + "T00:00:00");
  return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

const CHART_COLOR_KEYS = [
  "chart-1",
  "chart-2",
  "chart-3",
  "chart-4",
  "chart-5",
  "chart-6",
  "chart-7",
  "chart-8",
] as const;

// ─── Sync status badge ───────────────────────────────────────────────────────

function SyncBadge({ status }: { status: TimeEntrySyncStatus }) {
  const { t } = useT("time-tracking");
  if (status === "synced") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="size-2.5" />
        {t(($) => $.sync_synced)}
      </span>
    );
  }
  if (status === "failed") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-destructive/10 px-2 py-0.5 text-[10px] font-medium text-destructive">
        <AlertCircle className="size-2.5" />
        {t(($) => $.sync_failed)}
      </span>
    );
  }
  if (status === "pending") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
        <Loader2 className="size-2.5 animate-spin" />
        {t(($) => $.sync_pending)}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
      <MinusCircle className="size-2.5" />
      {t(($) => $.sync_not_linked)}
    </span>
  );
}

// ─── KPI card ────────────────────────────────────────────────────────────────

function KpiCard({
  label,
  value,
  sub,
  icon: Icon,
}: {
  label: string;
  value: string;
  sub?: string;
  icon: React.ElementType;
}) {
  return (
    <div className="rounded-lg border bg-card px-4 py-3">
      <div className="mb-1.5 flex items-center justify-between">
        <p className="text-xs text-muted-foreground">{label}</p>
        <Icon className="size-3.5 text-muted-foreground/50" />
      </div>
      <p className="text-xl font-semibold tabular-nums tracking-tight">
        {value}
      </p>
      {sub && <p className="mt-0.5 text-[11px] text-muted-foreground">{sub}</p>}
    </div>
  );
}

// ─── Loading skeleton ────────────────────────────────────────────────────────

function DashboardSkeleton() {
  return (
    <div className="space-y-6 p-6">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-19 rounded-lg" />
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-75 rounded-lg" />
        <Skeleton className="h-75 rounded-lg" />
      </div>
      <Skeleton className="h-55 rounded-lg" />
      <Skeleton className="h-75 rounded-lg" />
    </div>
  );
}

// ─── Empty state ─────────────────────────────────────────────────────────────

function EmptyState() {
  const { t } = useT("time-tracking");
  return (
    <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
      <Clock className="h-10 w-10 text-muted-foreground/40" />
      <p className="text-sm">{t(($) => $.empty_title)}</p>
      <p className="text-xs">{t(($) => $.empty_subtitle)}</p>
    </div>
  );
}

// ─── View mode segment ───────────────────────────────────────────────────────

type ViewMode = "weekly" | "monthly";

function ViewSegment({
  mode,
  setMode,
}: {
  mode: ViewMode;
  setMode: (v: ViewMode) => void;
}) {
  const { t } = useT("time-tracking");
  return (
    <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      <button
        type="button"
        onClick={() => setMode("weekly")}
        className={cn(
          "inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors",
          mode === "weekly"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        {t(($) => $.view_weekly)}
      </button>
      <button
        type="button"
        onClick={() => setMode("monthly")}
        className={cn(
          "inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors",
          mode === "monthly"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        {t(($) => $.view_monthly)}
      </button>
    </div>
  );
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

export function TimeTrackingDashboard() {
  const wsId = useWorkspaceId();
  const { t } = useT("time-tracking");
  const [viewMode, setViewMode] = useState<ViewMode>("weekly");
  const [weekOffset, setWeekOffset] = useState(0);

  // Month state
  const now = new Date();
  const [monthYear, setMonthYear] = useState(now.getFullYear());
  const [monthIdx, setMonthIdx] = useState(now.getMonth());

  const range = useMemo(() => getWeekRange(weekOffset), [weekOffset]);

  const weekLabel =
    weekOffset === 0
      ? t(($) => $.week_this)
      : weekOffset === -1
        ? t(($) => $.week_last)
        : range.label;

  const dailyChartConfig = useMemo(
    () =>
      ({
        hours: {
          label: t(($) => $.chart_hours_label),
          color: "var(--color-chart-1)",
        },
      }) satisfies ChartConfig,
    [t],
  );

  const issueChartConfig = useMemo(
    () =>
      ({
        hours: {
          label: t(($) => $.chart_hours_label),
          color: "var(--color-chart-2)",
        },
      }) satisfies ChartConfig,
    [t],
  );

  const DAY_NAMES = useMemo(
    () => [
      t(($) => $.day_mon),
      t(($) => $.day_tue),
      t(($) => $.day_wed),
      t(($) => $.day_thu),
      t(($) => $.day_fri),
      t(($) => $.day_sat),
      t(($) => $.day_sun),
    ],
    [t],
  );

  const { data, isLoading } = useQuery({
    ...dashboardOptions(wsId, range.start, range.end),
    enabled: viewMode === "weekly",
  });

  // Today's day index (Mon=0..Sun=6), only relevant when viewing current week
  const todayDayIdx = useMemo(() => {
    if (weekOffset !== 0) return -1;
    const d = new Date().getDay();
    return d === 0 ? 6 : d - 1;
  }, [weekOffset]);

  // Full 7-day array — fills gaps with zeros so all bars are always rendered
  const fullWeekData = useMemo(() => {
    const byDay = new Map<number, { hours: number; minutes: number }>();
    for (const d of data?.daily ?? []) {
      const date = new Date(d.date + "T00:00:00");
      const dayIdx = (date.getDay() + 6) % 7;
      byDay.set(dayIdx, {
        hours: +(d.total_minutes / 60).toFixed(2),
        minutes: d.total_minutes,
      });
    }
    return DAY_NAMES.map((name, idx) => ({
      name,
      hours: byDay.get(idx)?.hours ?? 0,
      minutes: byDay.get(idx)?.minutes ?? 0,
      isToday: idx === todayDayIdx,
    }));
  }, [data?.daily, todayDayIdx, DAY_NAMES]);

  // Activity breakdown sorted descending
  const activityData = useMemo(() => {
    if (!data?.by_activity) return [];
    const total = data.total_minutes;
    return data.by_activity
      .slice()
      .sort((a, b) => b.total_minutes - a.total_minutes)
      .map((a, i) => ({
        name: a.activity || t(($) => $.no_activity),
        key: `act-${i}`,
        value: a.total_minutes,
        pct: total > 0 ? Math.round((a.total_minutes / total) * 100) : 0,
        colorKey: CHART_COLOR_KEYS[i % CHART_COLOR_KEYS.length]!,
      }));
  }, [data?.by_activity, data?.total_minutes, t]);

  const activityChartConfig = useMemo(
    () =>
      Object.fromEntries(
        activityData.map((a) => [
          a.key,
          { label: a.name, color: `var(--color-${a.colorKey})` },
        ]),
      ) satisfies ChartConfig,
    [activityData],
  );

  // Top issues (horizontal bars)
  const issueData = useMemo(() => {
    if (!data?.by_issue) return [];
    return data.by_issue
      .slice()
      .sort((a, b) => b.total_minutes - a.total_minutes)
      .slice(0, 8)
      .map((i) => ({
        name: `#${i.issue_number}`,
        title: i.issue_title,
        hours: +(i.total_minutes / 60).toFixed(2),
        minutes: i.total_minutes,
      }));
  }, [data?.by_issue]);

  // KPI values
  const kpis = useMemo(() => {
    if (!data) return null;
    const activeDays = data.daily.filter((d) => d.total_minutes > 0).length;
    const avgMinutes =
      activeDays > 0 ? Math.round(data.total_minutes / activeDays) : 0;
    const peak = data.daily.reduce(
      (best: { date: string; total_minutes: number } | null, d) =>
        !best || d.total_minutes > best.total_minutes ? d : best,
      null,
    );
    return {
      total: formatMinutes(data.total_minutes),
      totalSub: t(($) => $.kpi_entry, { count: data.entries.length }),
      avg: avgMinutes > 0 ? formatMinutes(avgMinutes) : "—",
      avgSub:
        activeDays > 0
          ? t(($) => $.kpi_active_day, { count: activeDays })
          : t(($) => $.kpi_no_logged_days),
      peak:
        peak && peak.total_minutes > 0
          ? formatMinutes(peak.total_minutes)
          : "—",
      peakSub:
        peak && peak.total_minutes > 0 ? formatDate(peak.date) : undefined,
      entries: String(data.entries.length),
      entriesSub: `${formatDate(data.start_date)} – ${formatDate(data.end_date)}`,
    };
  }, [data, t]);

  // Month navigation helpers
  const isCurrentMonth =
    monthYear === now.getFullYear() && monthIdx === now.getMonth();
  const monthLabel = new Date(monthYear, monthIdx, 1).toLocaleDateString(
    "en-US",
    {
      month: "long",
      year: "numeric",
    },
  );

  function goToPreviousMonth() {
    if (monthIdx === 0) {
      setMonthYear((y) => y - 1);
      setMonthIdx(11);
    } else {
      setMonthIdx((m) => m - 1);
    }
  }

  function goToNextMonth() {
    if (monthIdx === 11) {
      setMonthYear((y) => y + 1);
      setMonthIdx(0);
    } else {
      setMonthIdx((m) => m + 1);
    }
  }

  function goToCurrentMonth() {
    setMonthYear(now.getFullYear());
    setMonthIdx(now.getMonth());
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-3">
          <Clock className="size-4 text-muted-foreground" />
          <span className="text-sm font-medium">{t(($) => $.header)}</span>
          <ViewSegment mode={viewMode} setMode={setViewMode} />
        </div>
        <div className="flex items-center gap-1">
          {viewMode === "weekly" ? (
            <>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={() => setWeekOffset((w) => w - 1)}
              >
                <ChevronLeft className="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className={cn(
                  "h-7 min-w-27 justify-center text-xs",
                  weekOffset !== 0 && "text-muted-foreground",
                )}
                onClick={() => setWeekOffset(0)}
              >
                {weekLabel}
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={() => setWeekOffset((w) => w + 1)}
                disabled={weekOffset >= 0}
              >
                <ChevronRight className="size-4" />
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={goToPreviousMonth}
              >
                <ChevronLeft className="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className={cn(
                  "h-7 min-w-36 justify-center text-xs",
                  !isCurrentMonth && "text-muted-foreground",
                )}
                onClick={goToCurrentMonth}
              >
                {monthLabel}
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                onClick={goToNextMonth}
                disabled={isCurrentMonth}
              >
                <ChevronRight className="size-4" />
              </Button>
            </>
          )}
        </div>
      </PageHeader>

      {/* Content */}
      {viewMode === "monthly" ? (
        <div className="flex-1 min-h-0 overflow-y-auto">
          <MonthlyCalendarView year={monthYear} month={monthIdx} />
        </div>
      ) : !isLoading && (!data || data.total_minutes === 0) ? (
        <EmptyState />
      ) : (
        <div className="flex-1 min-h-0 overflow-y-auto">
          {isLoading ? (
            <DashboardSkeleton />
          ) : (
            <div className="space-y-6 p-6">
              {/* KPI row */}
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
                <KpiCard
                  label={t(($) => $.kpi_total_logged)}
                  value={kpis!.total}
                  sub={kpis!.totalSub}
                  icon={Clock}
                />
                <KpiCard
                  label={t(($) => $.kpi_avg_per_day)}
                  value={kpis!.avg}
                  sub={kpis!.avgSub}
                  icon={TrendingUp}
                />
                <KpiCard
                  label={t(($) => $.kpi_peak_day)}
                  value={kpis!.peak}
                  sub={kpis!.peakSub}
                  icon={CalendarDays}
                />
                <KpiCard
                  label={t(($) => $.kpi_entries_label)}
                  value={kpis!.entries}
                  sub={kpis!.entriesSub}
                  icon={Hash}
                />
              </div>

              {/* Charts row */}
              <div
                className={cn(
                  "grid gap-4",
                  activityData.length > 0 ? "lg:grid-cols-2" : "grid-cols-1",
                )}
              >
                {/* Daily breakdown */}
                <div className="rounded-lg border bg-card p-5">
                  <h2 className="mb-4 text-sm font-medium">
                    {t(($) => $.section_daily_breakdown)}
                  </h2>
                  <ChartContainer
                    config={dailyChartConfig}
                    className="h-60 w-full"
                  >
                    <BarChart data={fullWeekData} barCategoryGap="30%">
                      <XAxis
                        dataKey="name"
                        tick={{ fontSize: 11 }}
                        axisLine={false}
                        tickLine={false}
                      />
                      <YAxis
                        tick={{ fontSize: 11 }}
                        axisLine={false}
                        tickLine={false}
                        tickFormatter={(v) => `${v}h`}
                        width={32}
                      />
                      <ChartTooltip
                        content={
                          <ChartTooltipContent
                            formatter={(_value, _name, item) => {
                              const min = (
                                item as { payload: { minutes: number } }
                              ).payload.minutes;
                              return min > 0 ? formatMinutes(min) : "—";
                            }}
                          />
                        }
                      />
                      <Bar
                        dataKey="hours"
                        radius={[4, 4, 0, 0]}
                        maxBarSize={48}
                      >
                        {fullWeekData.map((d, idx) => (
                          <Cell
                            key={idx}
                            fill={
                              d.hours > 0
                                ? "var(--color-chart-1)"
                                : "var(--color-border)"
                            }
                            opacity={d.isToday ? 1 : d.hours > 0 ? 0.7 : 1}
                          />
                        ))}
                      </Bar>
                    </BarChart>
                  </ChartContainer>
                </div>

                {/* Activity breakdown */}
                {activityData.length > 0 && (
                  <div className="rounded-lg border bg-card p-5">
                    <h2 className="mb-4 text-sm font-medium">
                      {t(($) => $.section_by_activity)}
                    </h2>
                    <div className="flex items-center gap-5">
                      <ChartContainer
                        config={activityChartConfig}
                        className="aspect-square h-45 shrink-0"
                      >
                        <PieChart>
                          <ChartTooltip
                            content={
                              <ChartTooltipContent
                                formatter={(value) =>
                                  typeof value === "number"
                                    ? formatMinutes(value)
                                    : String(value)
                                }
                                nameKey="name"
                              />
                            }
                          />
                          <Pie
                            data={activityData}
                            dataKey="value"
                            nameKey="name"
                            innerRadius={50}
                            outerRadius={78}
                            paddingAngle={2}
                          >
                            {activityData.map((a) => (
                              <Cell
                                key={a.key}
                                fill={`var(--color-${a.colorKey})`}
                              />
                            ))}
                          </Pie>
                        </PieChart>
                      </ChartContainer>
                      {/* Legend */}
                      <div className="flex min-w-0 flex-1 flex-col gap-2.5">
                        {activityData.map((a) => (
                          <div
                            key={a.key}
                            className="flex min-w-0 items-center gap-2"
                          >
                            <span
                              className="size-2 shrink-0 rounded-full"
                              style={{
                                backgroundColor: `var(--color-${a.colorKey})`,
                              }}
                            />
                            <span className="flex-1 truncate text-xs text-muted-foreground">
                              {a.name}
                            </span>
                            <span className="shrink-0 text-xs font-medium tabular-nums">
                              {formatMinutes(a.value)}
                            </span>
                            <span className="w-8 shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">
                              {a.pct}%
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  </div>
                )}
              </div>

              {/* Top issues */}
              {issueData.length > 0 && (
                <div className="rounded-lg border bg-card p-5">
                  <h2 className="mb-4 text-sm font-medium">
                    {t(($) => $.section_top_issues)}
                  </h2>
                  <ChartContainer
                    config={issueChartConfig}
                    className="w-full"
                    style={{
                      height: `${Math.max(issueData.length * 44 + 40, 80)}px`,
                    }}
                    initialDimension={{
                      width: 800,
                      height: Math.max(issueData.length * 44 + 40, 80),
                    }}
                  >
                    <BarChart
                      data={issueData}
                      layout="vertical"
                      margin={{ left: 8, right: 8 }}
                    >
                      <XAxis
                        type="number"
                        tick={{ fontSize: 11 }}
                        axisLine={false}
                        tickLine={false}
                        tickFormatter={(v) => `${v}h`}
                      />
                      <YAxis
                        type="category"
                        dataKey="name"
                        tick={{ fontSize: 11 }}
                        axisLine={false}
                        tickLine={false}
                        width={48}
                      />
                      <ChartTooltip
                        content={
                          <ChartTooltipContent
                            formatter={(_value, _name, item) => {
                              const { title, minutes } = (
                                item as {
                                  payload: { title: string; minutes: number };
                                }
                              ).payload;
                              return `${formatMinutes(minutes)} — ${title}`;
                            }}
                          />
                        }
                      />
                      <Bar
                        dataKey="hours"
                        fill="var(--color-chart-2)"
                        radius={[0, 4, 4, 0]}
                        maxBarSize={28}
                      />
                    </BarChart>
                  </ChartContainer>
                </div>
              )}

              {/* Entries table */}
              <div className="rounded-lg border bg-card">
                <div className="flex items-center justify-between border-b px-5 py-3">
                  <h2 className="text-sm font-medium">
                    {t(($) => $.section_all_entries)}
                  </h2>
                  <span className="text-xs text-muted-foreground">
                    {t(($) => $.kpi_entry, {
                      count: data!.entries.length,
                    })}
                  </span>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-[12px]">
                    <thead>
                      <tr className="border-b bg-muted/30">
                        <th className="whitespace-nowrap px-5 py-2 text-left font-medium text-muted-foreground">
                          {t(($) => $.table_date)}
                        </th>
                        <th className="px-4 py-2 text-left font-medium text-muted-foreground">
                          {t(($) => $.table_issue)}
                        </th>
                        <th className="whitespace-nowrap px-4 py-2 text-right font-medium text-muted-foreground">
                          {t(($) => $.table_duration)}
                        </th>
                        <th className="px-4 py-2 text-left font-medium text-muted-foreground">
                          {t(($) => $.table_activity)}
                        </th>
                        <th className="px-5 py-2 text-left font-medium text-muted-foreground">
                          {t(($) => $.table_sync)}
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y">
                      {data!.entries.map((entry) => (
                        <tr
                          key={entry.id}
                          className="transition-colors hover:bg-muted/20"
                        >
                          <td className="whitespace-nowrap px-5 py-2.5 text-muted-foreground">
                            {formatDate(entry.spent_on)}
                          </td>
                          <td className="max-w-100 px-4 py-2.5">
                            <span className="text-muted-foreground">
                              #{entry.issue_number}
                            </span>{" "}
                            <span className="truncate">
                              {entry.issue_title}
                            </span>
                          </td>
                          <td className="whitespace-nowrap px-4 py-2.5 text-right font-medium tabular-nums">
                            {formatMinutes(entry.duration_minutes)}
                          </td>
                          <td className="px-4 py-2.5">
                            {entry.activity_name ? (
                              <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                                {entry.activity_name}
                              </span>
                            ) : (
                              <span className="text-muted-foreground/40">
                                —
                              </span>
                            )}
                          </td>
                          <td className="px-5 py-2.5">
                            <SyncBadge status={entry.sync_status} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
