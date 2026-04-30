import { useMemo } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";

// Hour-of-day cost. The "WHEN" tab in the runtime detail uses this to show
// "during what hours of the day did this runtime spend money", which is
// fundamentally different from "how much per calendar day". Data is fed in
// by the parent (single orchestrator pattern) — this component is dumb.
const hourlyChartConfig = {
  cost: { label: "Cost", color: "var(--color-chart-1)" },
} satisfies ChartConfig;

export interface HourlyCostPoint {
  hour: number;
  cost: number;
}

export function HourlyActivityChart({ data }: { data: HourlyCostPoint[] }) {
  // Always render 24 buckets so the X axis is continuous. The parent passes
  // pre-aggregated server data which may omit hours with zero activity;
  // we fill those in with $0 here so visual gaps are intentional ("nothing
  // ran at 03:00") rather than missing data.
  const chartData = useMemo(() => {
    const map = new Map(data.map((d) => [d.hour, d.cost]));
    return Array.from({ length: 24 }, (_, i) => ({
      hour: i,
      label: `${i.toString().padStart(2, "0")}:00`,
      cost: map.get(i) ?? 0,
    }));
  }, [data]);

  return (
    <ChartContainer config={hourlyChartConfig} className="aspect-[3/1] w-full">
      <BarChart data={chartData} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="label"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval={2}
          fontSize={10}
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(v: number) => `$${v}`}
          width={40}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value) =>
                typeof value === "number"
                  ? `$${value.toFixed(2)}`
                  : String(value)
              }
            />
          }
        />
        <Bar dataKey="cost" fill="var(--color-cost)" radius={[2, 2, 0, 0]} />
      </BarChart>
    </ChartContainer>
  );
}
