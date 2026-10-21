// Agent presence has two independent current-state dimensions: runtime
// availability and active task workload. Past failures and completed tasks belong
// in Recent Work and Inbox rather than persisting as a list-level error state.

// Runtime-reachability dimension. `unstable` is the transient amber state
// during the runtime sweeper's grace window (offline < 5 min); it decays
// into `offline` with no new server data, hence the 30s presence tick on
// the consuming hooks.
//
// `archived` is the one non-runtime value: it reflects the agent's lifecycle
// (agent.archived_at is set), not its runtime. It is the top-priority state —
// derived BEFORE runtime health in deriveAgentPresenceDetail — so a retired
// agent with a leftover online runtime row never reads as live. Every dot /
// label that maps from this union (availabilityConfig) renders it for free;
// it is intentionally NOT in availabilityOrder (archived agents have their
// own list view, not an availability filter chip).
export type AgentAvailability =
  | "online" // 🟢 runtime online and reachable
  | "unstable" // 🟡 runtime recently_lost (< 5 min) — transient
  | "offline" // ⚫ runtime long offline / missing / never registered
  | "archived"; // ⚫ agent.archived_at set — retired, wins over runtime health

// Current task load on this agent. Three states — never historical,
// never an error predictor (Inbox + Recent Work handle that):
//
//   working → runningCount > 0. The runningCount/queuedCount on the detail
//             object preserve the breakdown for display.
//   queued  → no running task but ≥1 queued/dispatched. Most often means
//             the runtime is offline and tasks are stuck waiting; a brief
//             flash on online runtimes between dispatch and run is a
//             harmless race.
//   idle    → nothing on the plate.
//
// Pair with availability for the full picture: `online + working` is
// normal; `offline + queued` is the "stuck" state we explicitly surface;
// `offline + idle` is "agent unavailable, nothing waiting" — both honest.
export type Workload =
  | "working" // ≥1 task currently running
  | "queued" // nothing running, but ≥1 queued/dispatched
  | "idle"; // nothing on the plate

export interface AgentPresenceDetail {
  availability: AgentAvailability;
  workload: Workload;
  runningCount: number;
  queuedCount: number;
  // Mirrors agent.max_concurrent_tasks — pulled into the detail so the UI
  // can render `running / capacity` ratios without re-fetching the agent.
  capacity: number;
}
