// Derived presence types for agents — the user-facing state we display
// across the UI (list dots, hover cards, status lines). Computed in the
// front-end from raw server data (agent + runtime + recent tasks); the
// back-end never knows about these enums.
//
// Two orthogonal dimensions, derived independently and answering only
// "what's true right now?" — historical / error context lives on the
// agent detail page (Recent Work, failure_reason) and Inbox, not in the
// list-level summary state:
//
//   1. AgentAvailability — "Can this agent take work right now?"
//      Depends only on runtime reachability. The dot colour everywhere in
//      the app reflects this single dimension; never sticky-red because of
//      a past task outcome.
//
//   2. Workload — "What is on this agent's plate right now?"
//      Depends only on the workspace task snapshot. Three states, each
//      pointing at a clear user action:
//        working → tasks running, normal
//        queued  → tasks queued but nothing running (= stuck if availability
//                  is offline/unstable; momentary if online)
//        idle    → nothing to do
//      No `failed` / `completed` / `cancelled` states — those are historical,
//      surfaced via Recent Work + Inbox.

// Runtime-reachability dimension. `unstable` is the transient amber state
// during the runtime sweeper's grace window (offline < 5 min); it decays
// into `offline` with no new server data, hence the 30s presence tick on
// the consuming hooks.
export type AgentAvailability =
  | "online" // 🟢 runtime online and reachable
  | "unstable" // 🟡 runtime recently_lost (< 5 min) — transient
  | "offline"; // ⚫ runtime long offline / missing / never registered

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
