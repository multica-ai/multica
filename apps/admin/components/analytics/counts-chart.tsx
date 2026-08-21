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

/** Single-series bar chart shared by the Workspaces-created and
 * Issues-created cards — same shape, different field and color. */
export function CountsChart({
  buckets,
  metric,
  seriesLabel,
  color = "var(--chart-1)",
}: {
  buckets: AnalyticsBucket[];
  metric: "workspacesCreated" | "issuesCreated";
  seriesLabel: string;
  color?: string;
}) {
  const data = useMemo(
    () => buckets.map((b) => ({ label: bucketLabel(b.bucketStart), value: b[metric] })),
    [buckets, metric],
  );
  const config: ChartConfig = { value: { label: seriesLabel, color } };
  const total = data.reduce((s, d) => s + d.value, 0);
  if (total === 0) return <EmptyChartState message={`No ${seriesLabel.toLowerCase()} in this window.`} />;

  return (
    <ChartContainer config={config} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width="auto" />
        <ChartTooltip content={<ChartTooltipContent />} />
        <Bar dataKey="value" fill="var(--color-value)" radius={[3, 3, 0, 0]} />
      </BarChart>
    </ChartContainer>
  );
}
