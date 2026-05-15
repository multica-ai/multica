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
import { formatDuration, type DailyDurationData } from "../../utils";

// Single-series bar — total terminal-run duration per day in the runtime's
// local tz. Y axis is hours (compact "Xh" ticks); the tooltip renders the
// raw second count via formatDuration so sub-hour spans aren't pinned to
// `0h` at the chart edge.
//
// Cache reads / writes don't apply here, so the legend in WhenChart's
// header collapses to a single "Duration" pip — see usage-section.tsx.
export const durationStackConfig = {
  hours: { label: "Duration", color: "var(--chart-1)" },
} satisfies ChartConfig;

export function DailyDurationChart({ data }: { data: DailyDurationData[] }) {
  // No internal empty-state — same convention as DailyCostChart /
  // DailyTokensChart: the parent decides what to render when there's
  // nothing to show.
  return (
    <ChartContainer config={durationStackConfig} className="aspect-[3/1] w-full">
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
          tickFormatter={(v: number) => `${v}h`}
          width={50}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              // Read the raw `seconds` payload off the row so sub-hour
              // spans don't get truncated to `0h` by the chart's plotted
              // value (which is in hours).
              formatter={(_value, _name, item) => {
                const seconds = (item?.payload as DailyDurationData | undefined)
                  ?.seconds;
                return typeof seconds === "number"
                  ? `${formatDuration(seconds)} Duration`
                  : `${_value} Duration`;
              }}
            />
          }
        />
        {/* Legend rendered by the parent in WhenChart's header. */}
        <Bar
          dataKey="hours"
          fill="var(--color-hours)"
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ChartContainer>
  );
}
