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
import type { DailyCostStackData } from "../../utils";

// Three-segment stack (input / output / cache write) — keeps the user's
// attention on what's actually driving spend. Cache reads are excluded
// because their per-token rate is two orders of magnitude smaller and
// would be visually invisible in a stack; we surface their *savings*
// separately as a KPI.
//
// Series → CSS chart token: stack reads bottom-up as chart-1 (deepest brand
// blue, "input") → chart-2 (mid) → chart-3 (lightest, "cache write"), so the
// visual depth maps directly to "primary cost driver → secondary".
export const costStackConfig = {
  input: { label: "Input", color: "var(--chart-1)" },
  output: { label: "Output", color: "var(--chart-2)" },
  cacheWrite: { label: "Cache write", color: "var(--chart-3)" },
} satisfies ChartConfig;

export function DailyCostChart({ data }: { data: DailyCostStackData[] }) {
  // No internal empty-state — the parent decides what to show in place of
  // the chart (often a diagnostic explaining *why* there's no cost). Letting
  // recharts render an empty axis would be both ugly and uninformative.
  return (
    <ChartContainer config={costStackConfig} className="aspect-[3/1] w-full">
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
          tickFormatter={(v: number) => `$${v}`}
          width={50}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) =>
                typeof value === "number"
                  ? `$${value.toFixed(2)} ${name}`
                  : `${value} ${name}`
              }
            />
          }
        />
        {/* Legend is intentionally rendered by the parent (in the chart card
            header, top-right) so the chart body stays clean and gets the full
            vertical real estate. */}
        <Bar
          dataKey="input"
          stackId="cost"
          fill="var(--color-input)"
          radius={[0, 0, 0, 0]}
        />
        <Bar
          dataKey="output"
          stackId="cost"
          fill="var(--color-output)"
          radius={[0, 0, 0, 0]}
        />
        <Bar
          dataKey="cacheWrite"
          stackId="cost"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ChartContainer>
  );
}
