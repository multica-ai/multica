"use client";

import {
  PieChart,
  Pie,
  Cell,
  ResponsiveContainer,
  Tooltip,
} from "recharts";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import type { DashboardOverview } from "../../core/api";

interface IssuesDonutProps {
  data: DashboardOverview | undefined;
  isLoading: boolean;
  kind: "status" | "priority";
}

const STATUS_COLORS: Record<string, string> = {
  backlog: "#94a3b8",
  todo: "#3b82f6",
  in_progress: "#f59e0b",
  in_review: "#a855f7",
  done: "#10b981",
  cancelled: "#6b7280",
};

const PRIORITY_COLORS: Record<string, string> = {
  urgent: "#ef4444",
  high: "#f97316",
  medium: "#3b82f6",
  low: "#94a3b8",
  none: "#6b7280",
};

export function IssuesDonut({ data, isLoading, kind }: IssuesDonutProps) {
  const buckets = kind === "status" ? data?.issues_by_status : data?.issues_by_priority;
  const colors = kind === "status" ? STATUS_COLORS : PRIORITY_COLORS;
  const title = kind === "status" ? "Issues by status" : "Issues by priority";

  const series = (buckets ?? [])
    .filter((b) => b.count > 0)
    .map((b) => ({ name: prettify(b.key), key: b.key, value: b.count }));
  const total = series.reduce((s, b) => s + b.value, 0);

  return (
    <Card>
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {title}
        </h3>
        {total > 0 && (
          <span className="text-[11px] text-muted-foreground">{total}</span>
        )}
      </div>
      <CardContent className="flex flex-row items-center gap-4 p-3">
        {isLoading ? (
          <div className="h-40 w-full animate-pulse rounded bg-muted" />
        ) : series.length === 0 ? (
          <div className="flex h-40 w-full items-center justify-center text-xs text-muted-foreground">
            Ingen issues.
          </div>
        ) : (
          <>
            <div className="h-40 w-40 shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={series}
                    dataKey="value"
                    innerRadius={36}
                    outerRadius={62}
                    paddingAngle={1}
                    stroke="hsl(var(--background))"
                  >
                    {series.map((s) => (
                      <Cell key={s.key} fill={colors[s.key] ?? "#9ca3af"} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{
                      background: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: 6,
                      fontSize: 11,
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <ul className="flex-1 space-y-1 text-xs">
              {series.map((s) => (
                <li key={s.key} className="flex items-center gap-2">
                  <span
                    className="size-2 shrink-0 rounded-full"
                    style={{ background: colors[s.key] ?? "#9ca3af" }}
                  />
                  <span className="flex-1 truncate text-muted-foreground">{s.name}</span>
                  <span className="tabular-nums">{s.value}</span>
                </li>
              ))}
            </ul>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function prettify(key: string): string {
  return key.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}
