"use client";

import { useMemo } from "react";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { AnalyticsBucket } from "@/lib/types";
import { bucketLabel } from "./bucket-label";
import { EmptyChartState } from "./empty-chart-state";

// completed (bottom, primary) -> skipped (a deliberate no-op, not an error)
// -> other (still in flight: issue_created/running) -> failed (top, most
// alarming) — same "worst outcome on top" convention as daily-tasks-chart.
const AUTOPILOT_CHART_CONFIG = {
  completed: { label: "Completed", color: "var(--chart-1)" },
  skipped: { label: "Skipped", color: "var(--chart-3)" },
  other: { label: "In flight", color: "var(--chart-4)" },
  failed: { label: "Failed", color: "var(--chart-5)" },
} satisfies ChartConfig;

export function AutopilotRunsChart({ buckets }: { buckets: AnalyticsBucket[] }) {
  const data = useMemo(
    () => buckets.map((b) => ({ label: bucketLabel(b.bucketStart), ...b.autopilotRuns })),
    [buckets],
  );
  const total = data.reduce((s, d) => s + d.completed + d.failed + d.skipped + d.other, 0);
  if (total === 0) return <EmptyChartState message="No autopilot runs in this window." />;

  return (
    <ChartContainer config={AUTOPILOT_CHART_CONFIG} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width="auto" />
        <ChartTooltip content={<ChartTooltipContent />} />
        <Bar dataKey="completed" stackId="runs" fill="var(--color-completed)" radius={[0, 0, 0, 0]} />
        <Bar dataKey="skipped" stackId="runs" fill="var(--color-skipped)" radius={[0, 0, 0, 0]} />
        <Bar dataKey="other" stackId="runs" fill="var(--color-other)" radius={[0, 0, 0, 0]} />
        <Bar dataKey="failed" stackId="runs" fill="var(--color-failed)" radius={[3, 3, 0, 0]} />
      </BarChart>
    </ChartContainer>
  );
}
