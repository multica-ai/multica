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
import type { DailyCostData } from "../../utils";

const costChartConfig = {
  cost: { label: "Cost", color: "hsl(var(--chart-1))" },
} satisfies ChartConfig;

export function DailyCostChart({ data }: { data: DailyCostData[] }) {
  if (data.every((d) => d.cost === 0)) return null;

  return (
    <div className="rounded-lg border p-4">
      <h4 className="text-xs font-medium text-muted-foreground mb-3">Daily Estimated Cost</h4>
      <ChartContainer config={costChartConfig} className="aspect-[2.5/1] w-full">
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
                formatter={(value) =>
                  typeof value === "number" ? `$${value.toFixed(2)}` : String(value)
                }
              />
            }
          />
          <Bar dataKey="cost" fill="var(--color-cost)" radius={[3, 3, 0, 0]} />
        </BarChart>
      </ChartContainer>
    </div>
  );
}
