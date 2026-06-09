// Pure derivation of a runtime's user-facing "health" state from the raw
// server fields (status + last_seen_at). Splitting the offline state into
// time-bucketed flavors lets the UI distinguish "just lost — likely
// transient" from "long gone — needs attention" with no schema change.

import type { AgentRuntime } from "../types";
import type { RuntimeHealth } from "./types";

const FIVE_MINUTES_MS = 5 * 60 * 1000;
// The runtime sweeper GCs runtimes that have been offline for 7 days. We
// flag the last 24 hours of that window so users can rescue a runtime
// before it disappears silently.
const ABOUT_TO_GC_THRESHOLD_MS = 6 * 24 * 3600 * 1000; // 6 days

export function deriveRuntimeHealth(runtime: AgentRuntime, now: number): RuntimeHealth {
  if (runtime.status === "online") return "online";

  // No last_seen timestamp ever recorded — treat as long-offline. This is
  // an unusual case (the back-end always sets last_seen_at on register),
  // but defending against it keeps the UI from crashing on legacy rows.
  const lastSeen = runtime.last_seen_at ? new Date(runtime.last_seen_at).getTime() : 0;
  const offlineFor = now - lastSeen;

  if (offlineFor < FIVE_MINUTES_MS) return "recently_lost";
  if (offlineFor > ABOUT_TO_GC_THRESHOLD_MS) return "about_to_gc";
  return "offline";
}
