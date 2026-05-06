"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { dashboardOptions } from "@multica/core/time-entries/queries";
import { workCalendarListOptions } from "@multica/core/work-calendars/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { Clock, TrendingUp, CalendarDays, Hash } from "lucide-react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import type { CalendarDay, WorkCalendar } from "@multica/core/types";
import { useT } from "../i18n";

// ─── Helpers ────────────────────────────────────────────────────────────────

function getMonthRange(year: number, month: number) {
  const start = new Date(year, month, 1);
  const end = new Date(year, month + 1, 0);
  const fmt = (d: Date) => d.toISOString().split("T")[0]!;
  return { start: fmt(start), end: fmt(end) };
}

function getMonthLabel(year: number, month: number): string {
  const d = new Date(year, month, 1);
  return d.toLocaleDateString("en-US", { month: "long", year: "numeric" });
}

function formatMinutes(minutes: number): string {
  if (minutes === 0) return "0h";
  if (minutes < 60) return `${minutes}m`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

function formatHours(hours: number): string {
  if (hours === 0) return "0h";
  const h = Math.floor(hours);
  const m = Math.round((hours - h) * 60);
  if (h === 0) return `${m}m`;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

type DayStatus =
  | "complete"
  | "over"
  | "partial"
  | "missing"
  | "holiday"
  | "weekend"
  | "future";

interface DayInfo {
  date: string;
  dayOfMonth: number;
  status: DayStatus;
  loggedMinutes: number;
  requiredHours: number;
  label?: string;
  isToday: boolean;
  entriesCount: number;
}

function computeDayStatus(
  loggedMinutes: number,
  requiredHours: number,
  calendarDayType: string | undefined,
  isFuture: boolean,
): DayStatus {
  if (isFuture) return "future";
  if (calendarDayType === "holiday") return "holiday";
  if (calendarDayType === "weekend") return "weekend";
  if (calendarDayType === "reduced") {
    if (loggedMinutes >= requiredHours * 60) return "complete";
    if (loggedMinutes > 0) return "partial";
    return "missing";
  }
  if (requiredHours === 0) return "weekend";
  if (loggedMinutes > requiredHours * 60) return "over";
  if (loggedMinutes >= requiredHours * 60) return "complete";
  if (loggedMinutes > 0) return "partial";
  return "missing";
}

// ─── Day Cell ────────────────────────────────────────────────────────────────

function DayCell({ day }: { day: DayInfo }) {
  const pct =
    day.requiredHours > 0
      ? Math.min((day.loggedMinutes / (day.requiredHours * 60)) * 100, 100)
      : 0;

  const isWorkDay =
    day.status !== "weekend" &&
    day.status !== "holiday" &&
    day.status !== "future";

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div
            className={cn(
              "relative flex flex-col items-stretch rounded-md border px-2 py-1.5 transition-colors",
              "cursor-default select-none",
              day.status === "weekend" && "border-transparent bg-muted/30",
              day.status === "holiday" && "border-transparent bg-muted/30",
              day.status === "future" && "border-dashed border-border/40",
              (day.status === "complete" ||
                day.status === "over" ||
                day.status === "partial" ||
                day.status === "missing") &&
                "border-border",
              day.isToday && "ring-1 ring-foreground/20",
            )}
          >
            {/* Row 1: day number + logged time */}
            <div className="flex items-baseline justify-between gap-1">
              <span
                className={cn(
                  "text-[11px] tabular-nums",
                  day.isToday
                    ? "font-semibold text-foreground"
                    : day.status === "weekend" || day.status === "holiday"
                      ? "text-muted-foreground/60"
                      : day.status === "future"
                        ? "text-muted-foreground/40"
                        : "text-muted-foreground",
                )}
              >
                {day.dayOfMonth}
              </span>
              {day.loggedMinutes > 0 && (
                <span className="text-[10px] font-medium tabular-nums text-foreground">
                  {formatMinutes(day.loggedMinutes)}
                </span>
              )}
              {day.status === "holiday" && day.label && (
                <span className="truncate text-[9px] text-muted-foreground/50">
                  {day.label}
                </span>
              )}
            </div>

            {/* Row 2: progress bar (only for past work days) */}
            {isWorkDay && (
              <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={cn(
                    "h-full rounded-full transition-all duration-300",
                    day.status === "complete" || day.status === "over"
                      ? "bg-emerald-500"
                      : day.status === "partial"
                        ? "bg-amber-500"
                        : "bg-red-400",
                  )}
                  style={{ width: `${pct}%` }}
                />
              </div>
            )}
          </div>
        }
      />
      <TooltipContent side="top" className="max-w-48">
        <DayTooltip day={day} />
      </TooltipContent>
    </Tooltip>
  );
}

function DayTooltip({ day }: { day: DayInfo }) {
  const { t } = useT("time-tracking");
  const statusLabel = {
    complete: t(($) => $.day_status_complete),
    over: t(($) => $.day_status_over),
    partial: t(($) => $.day_status_partial),
    missing: t(($) => $.day_status_missing),
    holiday: t(($) => $.day_status_holiday),
    weekend: t(($) => $.day_status_weekend),
    future: t(($) => $.day_status_future),
  }[day.status];

  return (
    <div className="space-y-1 py-0.5">
      <p className="text-xs font-medium">{statusLabel}</p>
      {day.label && (
        <p className="text-[10px] text-muted-foreground">{day.label}</p>
      )}
      {day.status !== "future" &&
        day.status !== "weekend" &&
        day.status !== "holiday" && (
          <div className="space-y-0.5 text-[10px] text-muted-foreground">
            <p>
              {t(($) => $.tooltip_logged)}{" "}
              <span className="font-medium text-foreground">
                {formatMinutes(day.loggedMinutes)}
              </span>
              {day.requiredHours > 0 && (
                <> / {formatHours(day.requiredHours)}</>
              )}
            </p>
            {day.entriesCount > 0 && (
              <p>{t(($) => $.tooltip_entry, { count: day.entriesCount })}</p>
            )}
          </div>
        )}
    </div>
  );
}

function EmptyDayCell() {
  return <div />;
}

// ─── KPI card (matches dashboard.tsx exactly) ────────────────────────────────

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

// ─── Legend ──────────────────────────────────────────────────────────────────

function CalendarLegend() {
  const { t } = useT("time-tracking");
  const items: { color: string; label: string }[] = [
    { color: "bg-emerald-500", label: t(($) => $.legend_on_target) },
    { color: "bg-amber-500", label: t(($) => $.legend_partial) },
    { color: "bg-red-400", label: t(($) => $.legend_missing) },
  ];

  return (
    <div className="flex items-center gap-3">
      {items.map((item) => (
        <div key={item.label} className="flex items-center gap-1.5">
          <span className={cn("h-1.5 w-3 rounded-full", item.color)} />
          <span className="text-[11px] text-muted-foreground">
            {item.label}
          </span>
        </div>
      ))}
    </div>
  );
}

// ─── Loading Skeleton ────────────────────────────────────────────────────────

function CalendarSkeleton() {
  return (
    <div className="space-y-6 p-6">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-19 rounded-lg" />
        ))}
      </div>
      <Skeleton className="h-80 rounded-lg" />
    </div>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function MonthlyCalendarView({
  year,
  month,
}: {
  year: number;
  month: number;
}) {
  const wsId = useWorkspaceId();
  const { t } = useT("time-tracking");

  const range = useMemo(() => getMonthRange(year, month), [year, month]);

  const { data: dashboardData, isLoading: isLoadingDashboard } = useQuery(
    dashboardOptions(wsId, range.start, range.end),
  );

  const { data: calendarsData, isLoading: isLoadingCalendars } = useQuery(
    workCalendarListOptions(wsId),
  );

  const isLoading = isLoadingDashboard || isLoadingCalendars;

  const activeCalendar = useMemo<WorkCalendar | undefined>(() => {
    if (!calendarsData?.calendars) return undefined;
    return calendarsData.calendars.find((c) => c.year === year);
  }, [calendarsData, year]);

  const calendarDayMap = useMemo(() => {
    const map = new Map<string, CalendarDay>();
    if (!activeCalendar) return map;
    for (const day of activeCalendar.days) {
      map.set(day.date, day);
    }
    return map;
  }, [activeCalendar]);

  const dailyLoggedMap = useMemo(() => {
    const map = new Map<string, number>();
    if (!dashboardData?.daily) return map;
    for (const d of dashboardData.daily) {
      map.set(d.date, d.total_minutes);
    }
    return map;
  }, [dashboardData?.daily]);

  const entriesCountMap = useMemo(() => {
    const map = new Map<string, number>();
    if (!dashboardData?.entries) return map;
    for (const entry of dashboardData.entries) {
      const current = map.get(entry.spent_on) ?? 0;
      map.set(entry.spent_on, current + 1);
    }
    return map;
  }, [dashboardData?.entries]);

  const daysGrid = useMemo(() => {
    const firstDay = new Date(year, month, 1);
    const lastDay = new Date(year, month + 1, 0);
    const daysInMonth = lastDay.getDate();

    let startDow = firstDay.getDay() - 1;
    if (startDow < 0) startDow = 6;

    const today = new Date();
    today.setHours(0, 0, 0, 0);

    const days: (DayInfo | null)[] = [];

    for (let i = 0; i < startDow; i++) {
      days.push(null);
    }

    for (let d = 1; d <= daysInMonth; d++) {
      const date = `${year}-${String(month + 1).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
      const dateObj = new Date(year, month, d);
      dateObj.setHours(0, 0, 0, 0);
      const isToday = dateObj.getTime() === today.getTime();
      const isFuture = dateObj > today;

      const calDay = calendarDayMap.get(date);
      const loggedMinutes = dailyLoggedMap.get(date) ?? 0;
      const entriesCount = entriesCountMap.get(date) ?? 0;

      const dayOfWeek = dateObj.getDay();
      const isWeekend = dayOfWeek === 0 || dayOfWeek === 6;
      const requiredHours = calDay?.hours ?? (isWeekend ? 0 : 8);
      const dayType = calDay?.type ?? (isWeekend ? "weekend" : "normal");

      const status = computeDayStatus(
        loggedMinutes,
        requiredHours,
        dayType,
        isFuture,
      );

      days.push({
        date,
        dayOfMonth: d,
        status,
        loggedMinutes,
        requiredHours,
        label: calDay?.label,
        isToday,
        entriesCount,
      });
    }

    return days;
  }, [year, month, calendarDayMap, dailyLoggedMap, entriesCountMap]);

  // KPIs
  const kpis = useMemo(() => {
    const validDays = daysGrid.filter((d): d is DayInfo => d !== null);
    const pastWorkDays = validDays.filter(
      (d) =>
        d.status !== "future" &&
        d.status !== "weekend" &&
        d.status !== "holiday" &&
        d.requiredHours > 0,
    );

    const totalRequired = pastWorkDays.reduce(
      (sum, d) => sum + d.requiredHours * 60,
      0,
    );
    const totalLogged = validDays.reduce((sum, d) => sum + d.loggedMinutes, 0);

    const completeDays = pastWorkDays.filter(
      (d) => d.status === "complete" || d.status === "over",
    ).length;

    const allWorkDays = validDays.filter(
      (d) =>
        d.status !== "weekend" && d.status !== "holiday" && d.requiredHours > 0,
    );
    const monthlyExpected = allWorkDays.reduce(
      (sum, d) => sum + d.requiredHours * 60,
      0,
    );

    const completionPct =
      totalRequired > 0 ? Math.round((totalLogged / totalRequired) * 100) : 0;

    return {
      totalLogged,
      monthlyExpected,
      completionPct,
      completeDays,
      pastWorkDays: pastWorkDays.length,
      totalEntries: dashboardData?.entries.length ?? 0,
    };
  }, [daysGrid, dashboardData?.entries.length]);

  if (isLoading) {
    return <CalendarSkeleton />;
  }

  return (
    <div className="space-y-6 p-6">
      {/* KPI row */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <KpiCard
          label={t(($) => $.monthly_logged_this_month)}
          value={formatMinutes(kpis.totalLogged)}
          sub={t(($) => $.monthly_of_expected, {
            value: formatMinutes(kpis.monthlyExpected),
          })}
          icon={Clock}
        />
        <KpiCard
          label={t(($) => $.monthly_completion)}
          value={`${kpis.completionPct}%`}
          sub={t(($) => $.monthly_days_on_target, {
            done: kpis.completeDays,
            total: kpis.pastWorkDays,
          })}
          icon={TrendingUp}
        />
        <KpiCard
          label={t(($) => $.monthly_work_days)}
          value={`${kpis.completeDays}/${kpis.pastWorkDays}`}
          sub={t(($) => $.monthly_days_full_hours)}
          icon={CalendarDays}
        />
        <KpiCard
          label={t(($) => $.monthly_entries)}
          value={String(kpis.totalEntries)}
          sub={getMonthLabel(year, month)}
          icon={Hash}
        />
      </div>

      {/* Calendar grid */}
      <div className="rounded-lg border bg-card p-5">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-medium">
            {t(($) => $.monthly_daily_status)}
          </h2>
          <div className="flex items-center gap-4">
            <CalendarLegend />
            {!activeCalendar && (
              <span className="text-[11px] text-muted-foreground">
                {t(($) => $.monthly_default_hours)}
              </span>
            )}
          </div>
        </div>

        {/* Weekday headers */}
        <div className="mb-1 grid grid-cols-7 gap-1">
          {[
            t(($) => $.day_mon),
            t(($) => $.day_tue),
            t(($) => $.day_wed),
            t(($) => $.day_thu),
            t(($) => $.day_fri),
            t(($) => $.day_sat),
            t(($) => $.day_sun),
          ].map((day) => (
            <div
              key={day}
              className="px-2 py-1 text-[11px] font-medium text-muted-foreground"
            >
              {day}
            </div>
          ))}
        </div>

        {/* Day cells */}
        <div className="grid grid-cols-7 gap-1">
          {daysGrid.map((day, idx) =>
            day === null ? (
              <EmptyDayCell key={`empty-${idx}`} />
            ) : (
              <DayCell key={day.date} day={day} />
            ),
          )}
        </div>
      </div>

      {/* Top issues for the month */}
      {dashboardData && dashboardData.by_issue.length > 0 && (
        <div className="rounded-lg border bg-card">
          <div className="flex items-center justify-between border-b px-5 py-3">
            <h2 className="text-sm font-medium">
              {t(($) => $.monthly_top_issues)}
            </h2>
            <span className="text-xs text-muted-foreground">
              {t(($) => $.monthly_entry, { count: kpis.totalEntries })}
            </span>
          </div>
          <div className="divide-y">
            {dashboardData.by_issue
              .slice()
              .sort((a, b) => b.total_minutes - a.total_minutes)
              .slice(0, 10)
              .map((issue) => {
                const pct =
                  dashboardData.total_minutes > 0
                    ? (issue.total_minutes / dashboardData.total_minutes) * 100
                    : 0;
                return (
                  <div
                    key={issue.issue_id}
                    className="flex items-center gap-3 px-5 py-2.5 transition-colors hover:bg-muted/20"
                  >
                    <span className="w-12 shrink-0 text-xs text-muted-foreground tabular-nums">
                      #{issue.issue_number}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-xs">
                      {issue.issue_title}
                    </span>
                    <div className="h-1.5 w-20 shrink-0 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-chart-2 transition-all duration-300"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <span className="w-14 shrink-0 text-right text-xs font-medium tabular-nums">
                      {formatMinutes(issue.total_minutes)}
                    </span>
                  </div>
                );
              })}
          </div>
        </div>
      )}
    </div>
  );
}
