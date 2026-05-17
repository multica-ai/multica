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

// Single-series bar — total daily run time in seconds. The y-axis tick
// formatter and tooltip both use the same `formatDuration` so the user
// reads the same unit ladder (h / m / s) everywhere.
const timeChartConfig = {
  totalSeconds: { label: "Run time", color: "var(--chart-1)" },
} satisfies ChartConfig;

export interface DailyTimeData {
  date: string;
  label: string;
  totalSeconds: number;
}

export function DailyTimeChart({
  data,
  formatY,
  formatTooltip,
}: {
  data: DailyTimeData[];
  // Caller passes a `formatDuration`-style fn so the chart stays UI-string
  // agnostic (the "< 1m" fallback label is localized by the parent).
  formatY: (seconds: number) => string;
  formatTooltip: (seconds: number) => string;
}) {
  return (
    <ChartContainer config={timeChartConfig} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="label"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval="preserveStartEnd"
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(v: number) => formatY(v)}
          width={56}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) =>
                typeof value === "number"
                  ? `${formatTooltip(value)} ${name}`
                  : `${value} ${name}`
              }
            />
          }
        />
        <Bar
          dataKey="totalSeconds"
          fill="var(--color-totalSeconds)"
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ChartContainer>
  );
}
