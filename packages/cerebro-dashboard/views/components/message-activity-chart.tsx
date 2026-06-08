"use client";

// CEREBRO-PATCH(cerebro-dashboard-message-activity-chart): TECH-3093 messages per day chart
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import type { DashboardOverview } from "../../core/api";

interface Props {
  data: DashboardOverview | undefined;
  isLoading: boolean;
}

function shortDay(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
  } catch {
    return iso;
  }
}

export function MessageActivityChart({ data, isLoading }: Props) {
  const series = (data?.timeline ?? []).map((b) => ({
    day: shortDay(b.day),
    Messages: b.messages_sent ?? 0,
  }));

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Messages over time
        </h3>
        {data && (
          <span className="text-xs text-muted-foreground">
            {series.reduce((s, b) => s + b.Messages, 0).toLocaleString()} total
          </span>
        )}
      </div>
      <CardContent className="pt-4">
        {isLoading ? (
          <div className="h-40 animate-pulse rounded bg-muted" />
        ) : series.every((b) => b.Messages === 0) ? (
          <p className="flex h-40 items-center justify-center text-xs text-muted-foreground">
            No messages in this period.
          </p>
        ) : (
          <ResponsiveContainer width="100%" height={160}>
            <BarChart data={series} margin={{ top: 4, right: 8, bottom: 0, left: -16 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
              <XAxis
                dataKey="day"
                tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }}
                axisLine={false}
                tickLine={false}
                allowDecimals={false}
              />
              <Tooltip
                contentStyle={{
                  fontSize: 12,
                  background: "hsl(var(--background))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: 6,
                }}
              />
              <Bar dataKey="Messages" fill="hsl(var(--primary))" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}
