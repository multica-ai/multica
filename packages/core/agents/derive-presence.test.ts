import { describe, expect, it } from "vitest";
import type { Agent, AgentRuntime, AgentTask } from "../types";
import {
  buildPresenceMap,
  deriveAgentAvailability,
  deriveAgentPresenceDetail,
  deriveWorkload,
  deriveWorkloadDetail,
} from "./derive-presence";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: "Test Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 6,
    model: "",
    owner_id: null,
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Test Runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    timezone: "UTC",
    last_seen_at: "2026-04-27T11:59:50Z",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

// Anchor for all wall-clock comparisons in the suite. Pairs with the
// runtime fixture's last_seen_at (10s before NOW) so an "online" runtime
// looks fresh by default.
const NOW = new Date("2026-04-27T12:00:00Z").getTime();

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "rt-1",
    issue_id: "",
    status: "queued",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-04-27T11:00:00Z",
    ...overrides,
  };
}

describe("deriveAgentAvailability", () => {
  // Reachability dimension only — runtime + clock decide it; tasks are
  // irrelevant to this axis.

  it("returns online when runtime is fresh-online", () => {
    expect(deriveAgentAvailability(makeRuntime(), NOW)).toBe("online");
  });

  it("returns unstable when runtime just dropped (< 5 min)", () => {
    expect(
      deriveAgentAvailability(
        makeRuntime({ status: "offline", last_seen_at: "2026-04-27T11:59:30Z" }),
        NOW,
      ),
    ).toBe("unstable");
  });

  it("returns offline when runtime has been gone > 5 min", () => {
    expect(
      deriveAgentAvailability(
        makeRuntime({ status: "offline", last_seen_at: "2026-04-27T11:50:00Z" }),
        NOW,
      ),
    ).toBe("offline");
  });

  it("collapses about_to_gc into offline (it's a runtime-card concern, not the dot)", () => {
    expect(
      deriveAgentAvailability(
        // 6.5 days ago — past the 6-day about_to_gc threshold.
        makeRuntime({ status: "offline", last_seen_at: "2026-04-21T00:00:00Z" }),
        NOW,
      ),
    ).toBe("offline");
  });

  it("returns offline when the runtime is null (deleted / never registered)", () => {
    expect(deriveAgentAvailability(null, NOW)).toBe("offline");
  });
});

describe("deriveWorkload", () => {
  // Atomic 3-way classifier — used by both Agent (per-agent task counts)
  // and Runtime (per-runtime aggregated counts). Pure functional mapping
  // from a count pair to a workload label.

  it("returns working when runningCount > 0", () => {
    expect(deriveWorkload({ runningCount: 1, queuedCount: 0 })).toBe("working");
    expect(deriveWorkload({ runningCount: 3, queuedCount: 5 })).toBe("working");
  });

  it("returns queued when nothing running but queuedCount > 0", () => {
    expect(deriveWorkload({ runningCount: 0, queuedCount: 1 })).toBe("queued");
    expect(deriveWorkload({ runningCount: 0, queuedCount: 5 })).toBe("queued");
  });

  it("returns idle when both counts are zero", () => {
    expect(deriveWorkload({ runningCount: 0, queuedCount: 0 })).toBe("idle");
  });
});

describe("deriveWorkloadDetail", () => {
  // Aggregates a task list into running/queued counts before classifying.
  // Terminal statuses (completed / failed / cancelled) are silently
  // ignored — workload is "what's on the plate right now", not history.

  it("returns idle when no tasks at all", () => {
    const r = deriveWorkloadDetail([]);
    expect(r.workload).toBe("idle");
    expect(r.runningCount).toBe(0);
    expect(r.queuedCount).toBe(0);
  });

  it("returns working when at least one task is running", () => {
    const r = deriveWorkloadDetail([makeTask({ status: "running" })]);
    expect(r.workload).toBe("working");
    expect(r.runningCount).toBe(1);
    expect(r.queuedCount).toBe(0);
  });

  it("returns queued when only queued / dispatched tasks exist (no running)", () => {
    // The "stuck on offline runtime" scenario in isolation: runningCount=0,
    // queuedCount>0 surfaces as `queued` so the UI can honestly say
    // "Queued · N" instead of misleading "Running 0/3 +Nq".
    const r = deriveWorkloadDetail([
      makeTask({ status: "queued" }),
      makeTask({ id: "t2", status: "dispatched" }),
    ]);
    expect(r.workload).toBe("queued");
    expect(r.runningCount).toBe(0);
    expect(r.queuedCount).toBe(2);
  });

  it("returns working when running coexists with queued (overflow)", () => {
    // Capacity-saturated agent: still running, but with a queue building.
    // The chip says "Working" with the queue expressed as a `+Nq` badge.
    const r = deriveWorkloadDetail([
      makeTask({ id: "t1", status: "running" }),
      makeTask({ id: "t2", status: "queued" }),
      makeTask({ id: "t3", status: "queued" }),
    ]);
    expect(r.workload).toBe("working");
    expect(r.runningCount).toBe(1);
    expect(r.queuedCount).toBe(2);
  });

  it("ignores terminal statuses entirely (no historical state in workload)", () => {
    // Failed / completed / cancelled tasks contribute no count and don't
    // change the verdict — Recent Work + Inbox handle history, not workload.
    const r = deriveWorkloadDetail([
      makeTask({
        id: "t-failed",
        status: "failed",
        completed_at: "2026-04-27T11:30:00Z",
      }),
      makeTask({
        id: "t-completed",
        status: "completed",
        completed_at: "2026-04-27T11:00:00Z",
      }),
      makeTask({
        id: "t-cancelled",
        status: "cancelled",
        completed_at: "2026-04-27T10:30:00Z",
      }),
    ]);
    expect(r.workload).toBe("idle");
    expect(r.runningCount).toBe(0);
    expect(r.queuedCount).toBe(0);
  });

  it("classifies running over queued when both present, regardless of order", () => {
    const r = deriveWorkloadDetail([
      makeTask({ id: "t1", status: "queued" }),
      makeTask({ id: "t2", status: "running" }),
    ]);
    expect(r.workload).toBe("working");
  });
});

describe("deriveAgentPresenceDetail", () => {
  // Composition: the two dimensions are derived independently and the
  // detail object exposes both. No cross-axis override — workload never
  // colours the dot, availability never overrides workload.

  it("composes online + working for the common busy case", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime(),
      tasks: [
        makeTask({ status: "running" }),
        makeTask({ id: "t2", status: "queued" }),
      ],
      now: NOW,
    });
    expect(detail.availability).toBe("online");
    expect(detail.workload).toBe("working");
    expect(detail.runningCount).toBe(1);
    expect(detail.queuedCount).toBe(1);
    expect(detail.capacity).toBe(6);
  });

  it("composes offline + queued — the canonical 'stuck' case (was previously misleading 'running 0/N')", () => {
    // The motivation for the redesign: runtime offline + queued tasks
    // used to surface as `running` with `0/3 +2q` counts (literally false).
    // Workload now returns `queued` honestly, paired with offline
    // availability — UI reads "Offline · Queued · 2".
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime({
        status: "offline",
        last_seen_at: "2026-04-27T11:50:00Z",
      }),
      tasks: [
        makeTask({ status: "queued" }),
        makeTask({ id: "t2", status: "queued" }),
      ],
      now: NOW,
    });
    expect(detail.availability).toBe("offline");
    expect(detail.workload).toBe("queued");
    expect(detail.runningCount).toBe(0);
    expect(detail.queuedCount).toBe(2);
  });

  it("composes unstable + working — runtime hiccup with tasks still in flight", () => {
    // Recently-lost runtime, but a task is still recorded as running.
    // Both signals surface independently — amber dot AND working chip —
    // so the user sees "connection wobbling" alongside "agent is busy".
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime({
        status: "offline",
        last_seen_at: "2026-04-27T11:59:00Z",
      }),
      tasks: [makeTask({ status: "running" })],
      now: NOW,
    });
    expect(detail.availability).toBe("unstable");
    expect(detail.workload).toBe("working");
  });

  it("composes offline + idle for an unreachable agent with no tasks pending", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime({
        status: "offline",
        last_seen_at: "2026-04-27T11:50:00Z",
      }),
      tasks: [],
      now: NOW,
    });
    expect(detail.availability).toBe("offline");
    expect(detail.workload).toBe("idle");
  });

  it("handles a missing runtime by reporting offline + the task-driven workload", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: null,
      tasks: [makeTask({ status: "running" })],
      now: NOW,
    });
    expect(detail.availability).toBe("offline");
    expect(detail.workload).toBe("working");
  });

  it("returns idle workload when only terminal tasks are present (history doesn't bleed in)", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime(),
      tasks: [
        makeTask({
          status: "failed",
          completed_at: "2026-04-27T11:30:00Z",
        }),
      ],
      now: NOW,
    });
    expect(detail.availability).toBe("online");
    expect(detail.workload).toBe("idle");
  });

  it("mirrors agent.max_concurrent_tasks into capacity", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent({ max_concurrent_tasks: 3 }),
      runtime: makeRuntime(),
      tasks: [],
      now: NOW,
    });
    expect(detail.capacity).toBe(3);
  });
});

describe("buildPresenceMap", () => {
  it("returns one entry per agent, sourcing tasks by agent_id from a flat list", () => {
    const agentA = makeAgent({ id: "a", runtime_id: "rt-1" });
    const agentB = makeAgent({ id: "b", runtime_id: "rt-1" });
    const map = buildPresenceMap({
      agents: [agentA, agentB],
      runtimes: [makeRuntime()],
      snapshot: [
        makeTask({ id: "t1", agent_id: "a", status: "running" }),
        makeTask({ id: "t2", agent_id: "b", status: "queued" }),
      ],
      now: NOW,
    });
    const a = map.get("a");
    const b = map.get("b");
    expect(a?.availability).toBe("online");
    expect(a?.workload).toBe("working");
    expect(b?.availability).toBe("online");
    expect(b?.workload).toBe("queued");
  });

  it("returns offline availability for agents whose runtime_id has no matching runtime", () => {
    const orphan = makeAgent({ id: "orphan", runtime_id: "missing" });
    const map = buildPresenceMap({
      agents: [orphan],
      runtimes: [],
      snapshot: [makeTask({ agent_id: "orphan", status: "running" })],
      now: NOW,
    });
    const o = map.get("orphan");
    expect(o?.availability).toBe("offline");
    // Workload still resolves independently — running task counts.
    expect(o?.workload).toBe("working");
  });

  it("threads the same `now` so every agent on a shared runtime gets the same availability", () => {
    // Multi-agent scenario: one local daemon backs N agents, daemon dies.
    // All dependent agents should report unstable together — the shared
    // `now` parameter is what guarantees consistent bucket boundaries.
    const agentA = makeAgent({ id: "a", runtime_id: "rt-1" });
    const agentB = makeAgent({ id: "b", runtime_id: "rt-1" });
    const map = buildPresenceMap({
      agents: [agentA, agentB],
      runtimes: [
        makeRuntime({
          status: "offline",
          last_seen_at: "2026-04-27T11:59:00Z",
        }),
      ],
      snapshot: [
        makeTask({ id: "t1", agent_id: "a", status: "queued" }),
        makeTask({ id: "t2", agent_id: "b", status: "running" }),
      ],
      now: NOW,
    });
    expect(map.get("a")?.availability).toBe("unstable");
    expect(map.get("b")?.availability).toBe("unstable");
    // Workload remains independent: a is queued (waiting), b is working.
    expect(map.get("a")?.workload).toBe("queued");
    expect(map.get("b")?.workload).toBe("working");
  });

  it("ignores terminal tasks in the snapshot when building per-agent workload", () => {
    // Snapshot intentionally still includes each agent's most recent
    // terminal task (back-end SQL didn't change); the front-end now
    // filters them out at the workload-derivation step.
    const agentA = makeAgent({ id: "a", runtime_id: "rt-1" });
    const map = buildPresenceMap({
      agents: [agentA],
      runtimes: [makeRuntime()],
      snapshot: [
        makeTask({
          id: "t-terminal",
          agent_id: "a",
          status: "failed",
          completed_at: "2026-04-27T11:30:00Z",
        }),
      ],
      now: NOW,
    });
    expect(map.get("a")?.workload).toBe("idle");
  });
});
