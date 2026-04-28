// Pure derivation of an agent's user-facing presence from raw server data.
// The back-end stores facts (which tasks exist, their statuses, the runtime
// last_seen_at); the front-end translates them into two orthogonal
// dimensions:
//
//   1. AgentAvailability — derived from runtime reachability only.
//   2. LastTaskState     — derived from the task snapshot only.
//
// They are computed independently and assembled into AgentPresenceDetail.
// No cross-dimension override logic — that was the source of the previous
// model's "sticky red dot" confusion.

import { deriveRuntimeHealth } from "../runtimes/derive-health";
import type { Agent, AgentRuntime, AgentTask, TaskFailureReason } from "../types";
import type {
  AgentAvailability,
  AgentPresenceDetail,
  LastTaskState,
} from "./types";

// AgentAvailability mirrors RuntimeHealth's reachability buckets but folds
// `about_to_gc` into `offline` — both mean "long unreachable" from the
// user's standpoint; the GC-warning copy belongs to the runtime card, not
// the agent dot.
export function deriveAgentAvailability(
  runtime: AgentRuntime | null,
  now: number,
): AgentAvailability {
  if (!runtime) return "offline";
  const health = deriveRuntimeHealth(runtime, now);
  if (health === "online") return "online";
  if (health === "recently_lost") return "unstable";
  return "offline"; // offline | about_to_gc collapse here
}

interface LastTaskResult {
  state: LastTaskState;
  runningCount: number;
  queuedCount: number;
  failureReason?: TaskFailureReason;
  lastTaskCompletedAt?: string;
}

// Single pass: count actives + track latest terminal by completed_at. A
// running OR queued task means the agent is currently busy ("running"
// state); only when nothing is in flight do we fall through to the latest
// terminal (which can be completed / failed / cancelled). With no terminal
// history at all, we report `idle`.
//
// Cancelled is no longer filtered out — under the new model the dot is
// availability-driven so honestly surfacing "cancelled" doesn't risk
// lying about whether the agent works. The previous "exclude cancelled
// to keep red sticky" hack is gone.
export function deriveLastTaskState(tasks: readonly AgentTask[]): LastTaskResult {
  let runningCount = 0;
  let queuedCount = 0;
  let latestTerminal: AgentTask | null = null;
  let latestTerminalAt = -Infinity;

  for (const t of tasks) {
    if (t.status === "running") {
      runningCount += 1;
    } else if (t.status === "queued" || t.status === "dispatched") {
      queuedCount += 1;
    } else if (t.completed_at) {
      const ts = new Date(t.completed_at).getTime();
      if (!Number.isNaN(ts) && ts > latestTerminalAt) {
        latestTerminalAt = ts;
        latestTerminal = t;
      }
    }
  }

  if (runningCount + queuedCount > 0) {
    return { state: "running", runningCount, queuedCount };
  }

  if (!latestTerminal) {
    return { state: "idle", runningCount: 0, queuedCount: 0 };
  }

  const completedAt = latestTerminal.completed_at ?? undefined;
  if (latestTerminal.status === "failed") {
    return {
      state: "failed",
      runningCount: 0,
      queuedCount: 0,
      failureReason: latestTerminal.failure_reason || undefined,
      lastTaskCompletedAt: completedAt,
    };
  }
  if (latestTerminal.status === "cancelled") {
    return {
      state: "cancelled",
      runningCount: 0,
      queuedCount: 0,
      lastTaskCompletedAt: completedAt,
    };
  }
  // completed
  return {
    state: "completed",
    runningCount: 0,
    queuedCount: 0,
    lastTaskCompletedAt: completedAt,
  };
}

interface DerivePresenceInput {
  agent: Agent;
  runtime: AgentRuntime | null;
  // Tasks for THIS agent only. Callers (buildPresenceMap, hooks) pre-filter
  // by agent_id — we don't re-check here.
  tasks: readonly AgentTask[];
  // Wall-clock millis used by deriveAgentAvailability to bucket runtime
  // health. Threading it as a parameter keeps the function pure.
  now: number;
}

export function deriveAgentPresenceDetail(input: DerivePresenceInput): AgentPresenceDetail {
  const availability = deriveAgentAvailability(input.runtime, input.now);
  const last = deriveLastTaskState(input.tasks);

  return {
    availability,
    lastTask: last.state,
    runningCount: last.runningCount,
    queuedCount: last.queuedCount,
    capacity: input.agent.max_concurrent_tasks,
    failureReason: last.failureReason,
    lastTaskCompletedAt: last.lastTaskCompletedAt,
  };
}

// Workspace-level batch builder. One pass over the workspace's agents
// produces a Map<agentId, AgentPresenceDetail> that every list / card /
// runtime sub-page can read without re-deriving.
export function buildPresenceMap(args: {
  agents: readonly Agent[];
  runtimes: readonly AgentRuntime[];
  // The workspace agent task snapshot: every active task + each agent's
  // most recent terminal task. Comes straight from getAgentTaskSnapshot()
  // — no pre-filtering needed.
  snapshot: readonly AgentTask[];
  now: number;
}): Map<string, AgentPresenceDetail> {
  const out = new Map<string, AgentPresenceDetail>();
  const runtimesById = new Map<string, AgentRuntime>();
  for (const r of args.runtimes) runtimesById.set(r.id, r);

  // Group tasks by agent_id once — O(N) — so per-agent derivation is O(1)
  // task scans rather than O(N×M).
  const tasksByAgent = new Map<string, AgentTask[]>();
  for (const t of args.snapshot) {
    const list = tasksByAgent.get(t.agent_id);
    if (list) list.push(t);
    else tasksByAgent.set(t.agent_id, [t]);
  }

  for (const agent of args.agents) {
    const runtime = runtimesById.get(agent.runtime_id) ?? null;
    const tasks = tasksByAgent.get(agent.id) ?? [];
    out.set(agent.id, deriveAgentPresenceDetail({ agent, runtime, tasks, now: args.now }));
  }
  return out;
}
