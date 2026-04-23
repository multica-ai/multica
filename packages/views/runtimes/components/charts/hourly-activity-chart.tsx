import { useState, useEffect, useMemo } from "react";
import { BarChart3 } from "lucide-react";
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
import { api } from "@multica/core/api";
import type { RuntimeHourlyActivity } from "@multica/core/types";

const hourlyChartConfig = {
  count: { label: "Tasks", color: "var(--color-chart-2)" },
} satisfies ChartConfig;

export function HourlyActivityChart({ runtimeId }: { runtimeId: string }) {
  const [data, setData] = useState<RuntimeHourlyActivity[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api
      .getRuntimeTaskActivity(runtimeId)
      .then(setData)
      .catch(() => setData([]))
      .finally(() => setLoading(false));
  }, [runtimeId]);

  const chartData = useMemo(() => {
    const map = new Map(data.map((d) => [d.hour, d.count]));
    return Array.from({ length: 24 }, (_, i) => ({
      hour: i,
      label: `${i.toString().padStart(2, "0")}:00`,
      count: map.get(i) ?? 0,
    }));
  }, [data]);

  const hasData = chartData.some((d) => d.count > 0);

  return (
    <div className="rounded-lg border p-4">
      <h4 className="text-xs font-medium text-muted-foreground mb-3">Hourly Distribution</h4>
      {loading ? (
        <div className="flex h-[140px] items-center justify-center text-xs text-muted-foreground">
          Loading...
        </div>
      ) : !hasData ? (
        <div className="flex h-[140px] flex-col items-center justify-center">
          <BarChart3 className="h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">No task data yet</p>
        </div>
      ) : (
        <ChartContainer config={hourlyChartConfig} className="aspect-[2.5/1] w-full">
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
              width={30}
              allowDecimals={false}
            />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Bar dataKey="count" fill="var(--color-count)" radius={[2, 2, 0, 0]} />
          </BarChart>
        </ChartContainer>
      )}
    </div>
  );
}
