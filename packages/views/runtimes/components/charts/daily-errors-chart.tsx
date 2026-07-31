import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@multica/ui/components/ui/chart";
import { useT } from "../../../i18n";
import {
  activeFailureClasses,
  formatRate,
  labelOf,
  useFailureClassConfig,
  type FailureBucketTotals,
  type FailureClassCounts,
} from "./failure-class-visuals";

export interface DailyErrorsData extends FailureClassCounts, FailureBucketTotals {
  date: string;
  label: string;
}

/**
 * Failed tasks per day, stacked by failure class.
 *
 * Bar height is the absolute failure count — a spike is what an operator
 * scans for. The rate lives in the tooltip instead of on the y-axis: as a
 * chart series it would be dominated by low-volume days, where a single
 * failure out of one task reads as a 100% outage.
 */
export function DailyErrorsChart({ data }: { data: DailyErrorsData[] }) {
  const { t } = useT("usage");
  const config = useFailureClassConfig();
  const classes = activeFailureClasses(data);

  return (
    <ChartContainer config={config} className="aspect-[3/1] w-full">
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
          allowDecimals={false}
          width={40}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              // `name` is the Recharts dataKey — the raw class id
              // ("rate_limit"). Resolve it through the chart config so the
              // tooltip shows the translated label the legend already uses.
              formatter={(value, name) => `${value} ${labelOf(config, name)}`}
              footer={(payload) => {
                const row = payload[0]?.payload as DailyErrorsData | undefined;
                if (!row) return null;
                return (
                  <div className="flex items-center justify-between gap-2 font-medium">
                    <span>{t(($) => $.errors.tooltip_rate)}</span>
                    <span className="font-mono tabular-nums">
                      {formatRate(row.failed, row.total)}
                    </span>
                  </div>
                );
              }}
            />
          }
        />
        {classes.map((c, i) => (
          <Bar
            key={c}
            dataKey={c}
            stackId="errors"
            fill={`var(--color-${c})`}
            // Only the topmost segment gets rounded corners, so the stack
            // reads as one bar rather than a pile of pills.
            radius={i === classes.length - 1 ? [3, 3, 0, 0] : [0, 0, 0, 0]}
          />
        ))}
      </BarChart>
    </ChartContainer>
  );
}
