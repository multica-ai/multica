import type { RunTimeline } from "./issue-run-timeline";

// Agents take the series colours in cost order, so the agent that spent most
// is always the accent blue. Past four the hues stop being tellable apart at a
// glance; the rest share the neutral grey as "Others".
export const SERIES_COLORS = [
  "var(--chart-series-1)",
  "var(--chart-series-2)",
  "var(--chart-series-3)",
  "var(--chart-series-4)",
] as const;
export const OTHERS_KEY = "__others__";

/** Agent id → its series colour; null for an agent folded into "Others". */
export function agentColorMap(timeline: RunTimeline): Map<string, string | null> {
  const keys = timeline.costByAgent.map((s) => s.key);
  for (const lane of timeline.lanes) {
    if (!keys.includes(lane.agentId)) keys.push(lane.agentId);
  }
  return new Map(keys.map((key, i) => [key, SERIES_COLORS[i] ?? null]));
}

/** Inline style that colours a `chart-mark` element. */
export function markStyle(color: string): React.CSSProperties {
  return { "--mark": color } as React.CSSProperties;
}
