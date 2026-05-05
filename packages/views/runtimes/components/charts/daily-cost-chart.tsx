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
import { useT } from "@multica/i18n/react";
import { useMemo } from "react";
import type { DailyCostStackData } from "../../utils";

export function useCostStackConfig() {
  const t = useT("runtimes");
  return useMemo(() => ({
    input: { label: t("chart_input"), color: "var(--chart-1)" },
    output: { label: t("chart_output"), color: "var(--chart-2)" },
    cacheWrite: { label: t("chart_cache_write"), color: "var(--chart-3)" },
  }) satisfies ChartConfig, [t]);
}

export function DailyCostChart({ data }: { data: DailyCostStackData[] }) {
  const costStackConfig = useCostStackConfig();
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
