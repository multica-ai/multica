"use client";

// FIR-1931: the development curve over a session — how the context window filled
// up across the session's runs (one point per agent run), with compaction drops
// marked. Forward-only: sessions that ran before this feature shipped have no
// history, so the chart shows an explicit empty state, never a blank graph.

import {
  Line,
  LineChart,
  ReferenceDot,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { useContextTimeline } from "./use-sessions";

export function SessionTimelineChart({
  issueId,
  sessionId,
  enabled,
}: {
  issueId: string;
  sessionId: string;
  enabled: boolean;
}) {
  const { data, isLoading } = useContextTimeline(issueId, sessionId, enabled);
  const points = data?.points ?? [];

  if (isLoading) {
    return <div className="h-24 animate-pulse rounded bg-muted" />;
  }
  if (!data?.has_data || points.length === 0) {
    return (
      <div className="rounded border bg-muted/20 px-3 py-2 text-[11px] text-muted-foreground">
        The development curve is collected from now on — no history for this
        session yet.
      </div>
    );
  }

  const series = points.map((p, i) => ({
    i,
    used: p.used_percent,
    compaction: p.is_compaction,
  }));

  return (
    <div className="h-24">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={series} margin={{ top: 6, right: 8, bottom: 0, left: -24 }}>
          <XAxis dataKey="i" hide />
          <YAxis
            domain={[0, 100]}
            ticks={[0, 50, 100]}
            tick={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }}
            tickLine={false}
            axisLine={false}
            width={28}
          />
          <Tooltip
            cursor={{ stroke: "hsl(var(--border))" }}
            contentStyle={{
              fontSize: 11,
              borderRadius: 6,
              border: "1px solid hsl(var(--border))",
              background: "hsl(var(--popover))",
            }}
            formatter={(value) => [`${value}% used`, "Context"]}
            labelFormatter={(label) => `Run ${Number(label) + 1}`}
          />
          <Line
            type="monotone"
            dataKey="used"
            stroke="hsl(var(--primary))"
            strokeWidth={1.5}
            dot={{ r: 2 }}
            isAnimationActive={false}
          />
          {/* Mark each compaction drop with a destructive dot. */}
          {series
            .filter((p) => p.compaction)
            .map((p) => (
              <ReferenceDot
                key={p.i}
                x={p.i}
                y={p.used}
                r={3.5}
                fill="hsl(var(--destructive))"
                stroke="none"
              />
            ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
