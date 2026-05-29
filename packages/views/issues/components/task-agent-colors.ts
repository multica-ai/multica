import { useMemo } from "react";
import type { AgentTask } from "@multica/core/types";

// 8-color palette; each unique agent_id gets a stable color based on
// first-appearance order (sorted by created_at).
const AGENT_COLORS = [
  "border-l-blue-500",
  "border-l-emerald-500",
  "border-l-amber-500",
  "border-l-violet-500",
  "border-l-rose-500",
  "border-l-cyan-500",
  "border-l-orange-500",
  "border-l-fuchsia-500",
];

export function useAgentColorMap(tasks: AgentTask[]): Map<string, string> | null {
  return useMemo(() => {
    const map = new Map<string, string>();
    const sorted = [...tasks].sort(
      (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    );
    for (const t of sorted) {
      if (t.agent_id && !map.has(t.agent_id)) {
        map.set(t.agent_id, AGENT_COLORS[map.size % AGENT_COLORS.length]!);
      }
    }
    // Only show colors when there are multiple agents.
    return map.size > 1 ? map : null;
  }, [tasks]);
}
