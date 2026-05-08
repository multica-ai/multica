"use client";

import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import type { DashboardOverview } from "../../core/api";

interface ActivityChartProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
}

// Per-day line chart of issues created, issues completed, and tasks
// completed for the active range. Server returns one bucket per day in
// chronological order.
export function ActivityChart({ data, isLoading }: ActivityChartProps) {
  const series = (data?.timeline ?? []).map((b) => ({
    day: shortDay(b.day),
    "Issues created": b.issues_created,
    "Issues done": b.issues_done,
    "Tasks completed": b.tasks_completed,
  }));

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Activity over time
        </h3>
        {data && (
          <span className="text-[11px] text-muted-foreground">{data.range}</span>
        )}
      </div>
      <CardContent className="p-3">
        {isLoading ? (
          <div className="h-56 animate-pulse rounded bg-muted" />
        ) : series.length === 0 ? (
          <div className="flex h-56 items-center justify-center text-xs text-muted-foreground">
            Ingen data i den valgte periode.
          </div>
        ) : (
          <div className="h-56">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={series} margin={{ top: 6, right: 12, bottom: 0, left: -8 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                <XAxis
                  dataKey="day"
                  tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
                  tickLine={false}
                  axisLine={{ stroke: "hsl(var(--border))" }}
                />
                <YAxis
                  tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
                  tickLine={false}
                  axisLine={{ stroke: "hsl(var(--border))" }}
                  width={32}
                  allowDecimals={false}
                />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 6,
                    fontSize: 11,
                  }}
                />
                <Legend wrapperStyle={{ fontSize: 11 }} />
                <Line
                  type="monotone"
                  dataKey="Issues created"
                  stroke="#3b82f6"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  type="monotone"
                  dataKey="Issues done"
                  stroke="#10b981"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  type="monotone"
                  dataKey="Tasks completed"
                  stroke="#a855f7"
                  strokeWidth={2}
                  dot={false}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function shortDay(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString("da-DK", { day: "2-digit", month: "short" });
  } catch {
    return iso;
  }
}
