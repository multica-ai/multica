import { PieChart, Pie, Cell, Label } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import type { ModelDistribution } from "../../utils";
import { formatTokens } from "../../utils";

const MODEL_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
];

export function ModelDistributionChart({ data }: { data: ModelDistribution[] }) {
  if (data.length === 0) return null;

  const totalTokens = data.reduce((sum, d) => sum + d.tokens, 0);
  const chartConfig = Object.fromEntries(
    data.map((d, i) => [
      d.model,
      { label: d.model, color: MODEL_COLORS[i % MODEL_COLORS.length] },
    ]),
  ) satisfies ChartConfig;

  return (
    <div className="rounded-lg border p-4">
      <h4 className="text-xs font-medium text-muted-foreground mb-3">Token Usage by Model</h4>
      <ChartContainer config={chartConfig} className="mx-auto aspect-square max-h-[200px]">
        <PieChart>
          <ChartTooltip
            content={
              <ChartTooltipContent
                formatter={(value) =>
                  typeof value === "number" ? formatTokens(value) : String(value)
                }
                nameKey="model"
              />
            }
          />
          <Pie
            data={data}
            dataKey="tokens"
            nameKey="model"
            innerRadius={50}
            outerRadius={80}
            paddingAngle={2}
          >
            {data.map((entry, i) => (
              <Cell
                key={entry.model}
                fill={MODEL_COLORS[i % MODEL_COLORS.length]}
              />
            ))}
            <Label
              content={({ viewBox }) => {
                if (viewBox && "cx" in viewBox && "cy" in viewBox) {
                  return (
                    <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                      <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-lg font-bold">
                        {formatTokens(totalTokens)}
                      </tspan>
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-xs">
                        tokens
                      </tspan>
                    </text>
                  );
                }
                return null;
              }}
            />
          </Pie>
        </PieChart>
      </ChartContainer>
      {/* Model legend with cost */}
      <div className="mt-3 space-y-1.5">
        {data.map((d, i) => (
          <div key={d.model} className="flex items-center justify-between text-xs">
            <div className="flex items-center gap-2 min-w-0">
              <div
                className="h-2 w-2 shrink-0 rounded-full"
                style={{ backgroundColor: MODEL_COLORS[i % MODEL_COLORS.length] }}
              />
              <span className="truncate font-mono">{d.model}</span>
            </div>
            <div className="flex items-center gap-3 shrink-0 text-muted-foreground tabular-nums">
              <span>{formatTokens(d.tokens)}</span>
              {d.cost > 0 && <span>${d.cost.toFixed(2)}</span>}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
