"use client";

// FIR-1931: the development curve over a session — how the context window filled
// up across the session's runs (one point per agent run), with compaction drops
// marked. Forward-only: sessions that ran before this feature shipped have no
// history, so the chart shows an explicit empty state, never a blank graph.
//
// FIR-1931 follow-up: the value must read WITHOUT hovering. A line chart hides
// every number in a hover tooltip, and a session with a single run draws no line
// at all (recharts needs >=2 points), so it rendered as a lone dot that only
// revealed "5% used" on hover. Two fixes: a single run renders its measurement
// directly (no near-empty graph), and the multi-run curve carries always-visible
// value labels plus a filled area so the shape and the numbers are legible at a
// glance.

import {
  Area,
  AreaChart,
  LabelList,
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

  // A single run can't form a curve: recharts draws no line through one point, so
  // the graph looked empty and the value only appeared on hover. Render the
  // measurement directly instead, and say the curve fills in as the session runs.
  if (points.length === 1) {
    const p = points[0]!;
    return (
      <div className="rounded border bg-muted/20 px-3 py-2">
        <div className="flex items-baseline justify-between gap-2">
          <span className="text-[11px] text-muted-foreground">
            Run 1{p.is_compaction ? " · compaction" : ""}
          </span>
          <span className="text-sm font-semibold tabular-nums text-foreground">
            {p.used_percent}% used
          </span>
        </div>
        <p className="mt-1 text-[10px] text-muted-foreground">
          The curve builds out as the session runs again.
        </p>
      </div>
    );
  }

  const series = points.map((p, i) => ({
    i,
    used: p.used_percent,
    compaction: p.is_compaction,
  }));

  return (
    <div className="h-28">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={series} margin={{ top: 14, right: 10, bottom: 0, left: -24 }}>
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
          {/* Filled area so the curve reads as a shape, not a hairline; the line
              is the area's stroke. Value labels sit above each point so the
              numbers are visible without hovering. */}
          <Area
            type="monotone"
            dataKey="used"
            stroke="hsl(var(--primary))"
            strokeWidth={1.5}
            fill="hsl(var(--primary))"
            fillOpacity={0.12}
            dot={{ r: 2.5, fill: "hsl(var(--primary))", strokeWidth: 0 }}
            activeDot={{ r: 4 }}
            isAnimationActive={false}
          >
            <LabelList
              dataKey="used"
              position="top"
              offset={6}
              formatter={(value) => `${value}%`}
              style={{ fontSize: 9, fill: "hsl(var(--muted-foreground))" }}
            />
          </Area>
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
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
