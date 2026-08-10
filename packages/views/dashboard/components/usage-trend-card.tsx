"use client";

import { useState } from "react";
import { BarChart3 } from "lucide-react";
import {
  DailyCostChart,
  DailyTokensChart,
  DailyTimeChart,
  DailyTasksChart,
  WeeklyCostChart,
  WeeklyTokensChart,
  WeeklyTimeChart,
  WeeklyTasksChart,
} from "../../runtimes/components/charts";
import { aggregateByWeek } from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  aggregateDailyCost,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  formatDuration,
} from "../utils";
import { Segmented, type Dim } from "./dashboard-shared";
import { DimSegmented } from "./dim-segmented";

type UsageMetric = "tokens" | "cost" | "time" | "tasks";

/**
 * Spend over time: one x-axis, four metrics behind a toggle so the reader can
 * mentally overlay them by flipping between them.
 *
 * Errors used to be the fifth metric here. It is a different question — "what
 * broke" rather than "what did it cost" — and sharing a toggle with the spend
 * metrics meant the only way to see failures was to hide spend. It now has its
 * own chart on the Errors tab.
 */
export function UsageTrendCard({
  allowedDims,
  dailyCost,
  dailyTokens,
  dailyTime,
  dailyTasks,
  weeklyCost,
  weeklyTokens,
  weeklyTime,
  weeklyTasks,
  lessThanMinuteLabel,
}: {
  allowedDims: readonly Dim[];
  dailyCost: ReturnType<typeof aggregateDailyCost>;
  dailyTokens: ReturnType<typeof aggregateDailyTokens>;
  dailyTime: ReturnType<typeof aggregateDailyTime>;
  dailyTasks: ReturnType<typeof aggregateDailyTasks>;
  weeklyCost: ReturnType<typeof aggregateByWeek>["weeklyCostStack"];
  weeklyTokens: ReturnType<typeof aggregateByWeek>["weeklyTokens"];
  weeklyTime: ReturnType<typeof aggregateWeeklyTime>;
  weeklyTasks: ReturnType<typeof aggregateWeeklyTasks>;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [metric, setMetric] = useState<UsageMetric>("tokens");
  const [dim, setDim] = useState<Dim>("daily");
  // Derived, never reset: when the range narrows to 1d the card simply draws
  // the one dimension that range allows, and a later widening restores the
  // reader's choice. Writing the correction back into state would make the
  // card forget it.
  const effectiveDim: Dim = allowedDims.includes(dim) ? dim : allowedDims[0]!;
  const weekly = effectiveDim === "weekly";

  // Empty-state is per-metric so each toggle option independently decides
  // whether it has data — e.g. tokens recorded but no terminal runs yet
  // should show Tokens normally while Time / Tasks fall through to empty.
  const costData = weekly ? weeklyCost : dailyCost;
  const tokensData = weekly ? weeklyTokens : dailyTokens;
  const timeData = weekly ? weeklyTime : dailyTime;
  const tasksData = weekly ? weeklyTasks : dailyTasks;

  const totalCost = costData.reduce((sum, d) => sum + d.total, 0);
  const totalTokens = tokensData.reduce(
    (sum, d) => sum + d.input + d.output + d.cacheRead + d.cacheWrite,
    0,
  );
  const totalSeconds = timeData.reduce((sum, d) => sum + d.totalSeconds, 0);
  const totalTasks = tasksData.reduce((sum, d) => sum + d.completed + d.failed, 0);
  const isEmpty =
    metric === "cost"
      ? totalCost === 0
      : metric === "tokens"
        ? totalTokens === 0
        : metric === "time"
          ? totalSeconds === 0
          : totalTasks === 0;

  const title = weekly
    ? metric === "cost"
      ? t(($) => $.weekly.title_cost)
      : metric === "tokens"
        ? t(($) => $.weekly.title_tokens)
        : metric === "time"
          ? t(($) => $.weekly.title_time)
          : t(($) => $.weekly.title_tasks)
    : metric === "cost"
      ? t(($) => $.daily.title_cost)
      : metric === "tokens"
        ? t(($) => $.daily.title_tokens)
        : metric === "time"
          ? t(($) => $.daily.title_time)
          : t(($) => $.daily.title_tasks);

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-body font-semibold">{title}</h4>
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            label={t(($) => $.daily.metric_label)}
            value={metric}
            onChange={setMetric}
            options={[
              { label: t(($) => $.daily.metric_tokens), value: "tokens" as const },
              { label: t(($) => $.daily.metric_cost), value: "cost" as const },
              { label: t(($) => $.daily.metric_time), value: "time" as const },
              { label: t(($) => $.daily.metric_tasks), value: "tasks" as const },
            ]}
          />
          <DimSegmented allowedDims={allowedDims} value={effectiveDim} onChange={setDim} />
        </div>
      </div>
      <div className="min-h-[240px]">
        {isEmpty ? (
          <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
            <BarChart3 className="h-5 w-5 text-faint-foreground" />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.daily.no_data)}
            </p>
          </div>
        ) : weekly ? (
          metric === "cost" ? (
            <WeeklyCostChart data={weeklyCost} />
          ) : metric === "tokens" ? (
            <WeeklyTokensChart data={weeklyTokens} />
          ) : metric === "time" ? (
            <WeeklyTimeChart
              data={weeklyTime}
              formatY={(s) => formatDuration(s, lessThanMinuteLabel)}
              formatTooltip={(s) => formatDuration(s, lessThanMinuteLabel)}
            />
          ) : (
            <WeeklyTasksChart data={weeklyTasks} />
          )
        ) : metric === "cost" ? (
          <DailyCostChart data={dailyCost} />
        ) : metric === "tokens" ? (
          <DailyTokensChart data={dailyTokens} />
        ) : metric === "time" ? (
          <DailyTimeChart
            data={dailyTime}
            formatY={(s) => formatDuration(s, lessThanMinuteLabel)}
            formatTooltip={(s) => formatDuration(s, lessThanMinuteLabel)}
          />
        ) : (
          <DailyTasksChart data={dailyTasks} />
        )}
      </div>
    </div>
  );
}
