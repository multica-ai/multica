"use client";

import { useMemo } from "react";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from "recharts";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@multica/ui/components/ui/chart";
import type { AnalyticsBucket } from "@/lib/types";
import { bucketLabel } from "./bucket-label";
import { EmptyChartState } from "./empty-chart-state";
import { activeFailureClasses, failureClassChartConfig, labelOf } from "./failure-class-visuals";

/**
 * Failed tasks per bucket, stacked by the same 7-class taxonomy the
 * per-workspace Usage → Errors tab uses (failureClassOf from
 * @multica/core/dashboard), just folded across every workspace.
 */
export function ErrorsChart({
  buckets,
  onSegmentClick,
}: {
  buckets: AnalyticsBucket[];
  onSegmentClick?: (bucketStart: string, segment: string) => void;
}) {
  const data = useMemo(
    () => buckets.map((b) => ({ bucketStart: b.bucketStart, label: bucketLabel(b.bucketStart), ...b.errors })),
    [buckets],
  );
  const classes = activeFailureClasses(data);
  if (classes.length === 0) return <EmptyChartState message="No errors in this window." />;

  return (
    <ChartContainer
      config={failureClassChartConfig}
      className="aspect-[3/1] w-full"
      aria-label="Errors chart. Click a bar segment to see its workspace breakdown."
    >
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval="preserveStartEnd" />
        <YAxis tickLine={false} axisLine={false} tickMargin={8} allowDecimals={false} width="auto" />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) => `${value} ${labelOf(failureClassChartConfig, name)}`}
              footer="Click a segment for its workspace breakdown."
            />
          }
        />
        {classes.map((c, i) => (
          <Bar
            key={c}
            dataKey={c}
            stackId="errors"
            fill={`var(--color-${c})`}
            radius={i === classes.length - 1 ? [3, 3, 0, 0] : [0, 0, 0, 0]}
            className="cursor-pointer"
            onClick={(data) => data.payload?.bucketStart && onSegmentClick?.(data.payload.bucketStart, c)}
          />
        ))}
      </BarChart>
    </ChartContainer>
  );
}
