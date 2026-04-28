// Derived presence types for agents — the user-facing state we display
// across the UI (list dots, hover cards, status lines). Computed in the
// front-end from raw server data (agent + runtime + recent tasks); the
// back-end never knows about these enums.
//
// Two orthogonal dimensions, derived independently:
//
//   1. AgentAvailability — "Can this agent take work right now?"
//      Depends only on runtime reachability. The dot colour everywhere in
//      the app reflects this single dimension; never sticky-red because of
//      a past task outcome.
//
//   2. LastTaskState — "What was the last thing this agent did?"
//      Depends only on the workspace task snapshot. Surfaced as text + icon
//      on focused surfaces (hover card, agent detail, agent list, runtime
//      detail). Never colours the dot.
//
// The previous single 5-state union conflated the two: a runtime-healthy
// agent whose last task failed would show a red dot indistinguishable from
// a daemon-dead agent. Splitting them lets each signal be unambiguous.

import type { TaskFailureReason } from "../types";

// Runtime-reachability dimension. `unstable` is the transient amber state
// during the runtime sweeper's grace window (offline < 5 min); it decays
// into `offline` with no new server data, hence the 30s presence tick on
// the consuming hooks.
export type AgentAvailability =
  | "online" // 🟢 runtime online and reachable
  | "unstable" // 🟡 runtime recently_lost (< 5 min) — transient
  | "offline"; // ⚫ runtime long offline / missing / never registered

// Last-task dimension. Active and terminal merged into one enum because
// only one applies at a time: while there's any in-flight task the state
// is `running`; once everything terminates we read the latest outcome;
// with no history at all, `idle`.
//
// `running` covers both `running` and `queued/dispatched` tasks because
// from the user's perspective "agent is busy" is the same answer; the
// running/queued counts on the detail object preserve the breakdown.
//
// `cancelled` is included as a discrete state (vs. folding into the
// previous filter that excluded cancelled from terminal selection). With
// the dot no longer colour-coded by task state, surfacing "cancelled"
// honestly is fine — it doesn't risk lying about availability.
export type LastTaskState =
  | "running" // ≥1 task running or queued right now
  | "completed" // latest terminal: completed
  | "failed" // latest terminal: failed
  | "cancelled" // latest terminal: cancelled
  | "idle"; // no active task and no terminal history

export interface AgentPresenceDetail {
  availability: AgentAvailability;
  lastTask: LastTaskState;
  runningCount: number;
  queuedCount: number;
  // Mirrors agent.max_concurrent_tasks — pulled into the detail so the UI
  // can render `running / capacity` ratios without re-fetching the agent.
  capacity: number;
  // Set only when lastTask === "failed". The label lookup happens at the
  // UI layer; deriving exposes the raw classifier so the UI can choose copy.
  failureReason?: TaskFailureReason;
  // Wall-clock timestamp of the latest terminal task. Set whenever
  // lastTask is one of completed / failed / cancelled. Used to render
  // "Last run: failed · 12 min ago" copy. Undefined for `running` (no
  // terminal yet) and `idle` (no history).
  lastTaskCompletedAt?: string;
}
