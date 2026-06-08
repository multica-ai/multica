"use client";

import {
  BarChart,
  Bar,
  Cell,
  XAxis,
  YAxis,
  ResponsiveContainer,
  Tooltip,
} from "recharts";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import type { DashboardOverview } from "../../core/api";

interface IssuesOnBehalfOfProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
}

// Stable palette cycled per member so each bar/legend row reads distinctly.
const PALETTE = [
  "#3b82f6",
  "#10b981",
  "#f59e0b",
  "#a855f7",
  "#ef4444",
  "#06b6d4",
  "#f97316",
  "#84cc16",
  "#ec4899",
  "#6366f1",
];

// Shows the distribution of agent-created issues by the human they were
// started on behalf of (MUL-2553) — so it's visible who is generating agent
// work across the workspace.
export function IssuesOnBehalfOf({ data, isLoading }: IssuesOnBehalfOfProps) {
  const series = (data?.issues_by_on_behalf_of ?? [])
    .filter((a) => a.count > 0)
    .map((a, i) => ({
      id: a.id,
      name: a.name || "Ukendt",
      value: a.count,
      fill: PALETTE[i % PALETTE.length],
    }));
  const total = series.reduce((s, a) => s + a.value, 0);

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Issues started on behalf of
        </h3>
        {total > 0 && (
          <span className="text-[11px] text-muted-foreground">{total}</span>
        )}
      </div>
      <CardContent className="p-3">
        {isLoading ? (
          <div className="h-48 w-full animate-pulse rounded bg-muted" />
        ) : series.length === 0 ? (
          <div className="flex h-48 w-full items-center justify-center text-xs text-muted-foreground">
            No issues started by agents on behalf of a member yet.
          </div>
        ) : (
          <div style={{ height: Math.max(120, series.length * 34 + 16) }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart
                data={series}
                layout="vertical"
                margin={{ top: 0, right: 16, bottom: 0, left: 8 }}
              >
                <XAxis type="number" hide allowDecimals={false} />
                <YAxis
                  type="category"
                  dataKey="name"
                  width={120}
                  tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }}
                  tickLine={false}
                  axisLine={false}
                />
                <Tooltip
                  cursor={{ fill: "hsl(var(--muted))" }}
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 6,
                    fontSize: 11,
                  }}
                />
                <Bar dataKey="value" radius={[0, 4, 4, 0]} barSize={18}>
                  {series.map((s) => (
                    <Cell key={s.id} fill={s.fill} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
