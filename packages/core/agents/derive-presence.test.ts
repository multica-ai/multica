import { describe, expect, it } from "vitest";
import type { Agent, AgentRuntime, AgentTask, TaskFailureReason } from "../types";
import {
  buildPresenceMap,
  deriveAgentAvailability,
  deriveAgentPresenceDetail,
  deriveLastTaskState,
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
  // irrelevant. The whole point of splitting from LastTaskState is that
  // these tests can ignore task fixtures entirely.

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

describe("deriveLastTaskState", () => {
  // Task dimension only — runtime status is not consulted.

  it("returns idle when no tasks at all", () => {
    const r = deriveLastTaskState([]);
    expect(r.state).toBe("idle");
    expect(r.runningCount).toBe(0);
    expect(r.queuedCount).toBe(0);
  });

  it("returns running when at least one task is running", () => {
    const r = deriveLastTaskState([makeTask({ status: "running" })]);
    expect(r.state).toBe("running");
    expect(r.runningCount).toBe(1);
  });

  it("returns running when only queued / dispatched tasks exist (no running yet)", () => {
    const r = deriveLastTaskState([
      makeTask({ status: "queued" }),
      makeTask({ id: "t2", status: "dispatched" }),
    ]);
    expect(r.state).toBe("running");
    expect(r.runningCount).toBe(0);
    expect(r.queuedCount).toBe(2);
  });

  it("returns running even when an older terminal exists (active wins over historical)", () => {
    const r = deriveLastTaskState([
      makeTask({
        id: "old-failed",
        status: "failed",
        completed_at: "2026-04-27T10:00:00Z",
      }),
      makeTask({ id: "new-running", status: "running" }),
    ]);
    expect(r.state).toBe("running");
  });

  it("returns the latest terminal state when no tasks are active (latest = failed)", () => {
    const r = deriveLastTaskState([
      makeTask({
        id: "old",
        status: "completed",
        completed_at: "2026-04-27T10:00:00Z",
      }),
      makeTask({
        id: "new",
        status: "failed",
        completed_at: "2026-04-27T11:30:00Z",
      }),
    ]);
    expect(r.state).toBe("failed");
    expect(r.lastTaskCompletedAt).toBe("2026-04-27T11:30:00Z");
  });

  it("returns the latest terminal state when no tasks are active (latest = completed)", () => {
    const r = deriveLastTaskState([
      makeTask({
        id: "old",
        status: "failed",
        completed_at: "2026-04-27T10:00:00Z",
      }),
      makeTask({
        id: "new",
        status: "completed",
        completed_at: "2026-04-27T11:30:00Z",
      }),
    ]);
    expect(r.state).toBe("completed");
  });

  it("surfaces failure_reason on a failed latest terminal", () => {
    const reason: TaskFailureReason = "runtime_offline";
    const r = deriveLastTaskState([
      makeTask({
        status: "failed",
        completed_at: "2026-04-27T11:30:00Z",
        failure_reason: reason,
      }),
    ]);
    expect(r.state).toBe("failed");
    expect(r.failureReason).toBe(reason);
  });

  it("leaves failureReason undefined when the failed terminal has empty failure_reason", () => {
    const r = deriveLastTaskState([
      makeTask({
        status: "failed",
        completed_at: "2026-04-27T11:30:00Z",
        failure_reason: "",
      }),
    ]);
    expect(r.state).toBe("failed");
    expect(r.failureReason).toBeUndefined();
  });

  it("returns cancelled when the latest terminal is cancelled", () => {
    // Under the new model cancelled is a real state — the dot is
    // availability-driven so honestly surfacing it doesn't lie.
    const r = deriveLastTaskState([
      makeTask({
        status: "cancelled",
        completed_at: "2026-04-27T11:30:00Z",
      }),
    ]);
    expect(r.state).toBe("cancelled");
  });

  it("ignores terminals without completed_at (treated as not-terminal)", () => {
    // Defensive: a malformed row (no completed_at) shouldn't derail the
    // latest-terminal scan. With nothing else in flight, idle.
    const r = deriveLastTaskState([makeTask({ status: "failed", completed_at: null })]);
    expect(r.state).toBe("idle");
  });
});

describe("deriveAgentPresenceDetail", () => {
  // Composition: the two dimensions are derived independently and the
  // detail object exposes both. No cross-axis override (the old "unstable
  // overrides failed" rule is gone — they coexist now).

  it("composes online + running for the common busy case", () => {
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
    expect(detail.lastTask).toBe("running");
    expect(detail.runningCount).toBe(1);
    expect(detail.queuedCount).toBe(1);
    expect(detail.capacity).toBe(6);
  });

  it("composes online + failed — agent is reachable but last task failed (no longer sticky red dot)", () => {
    // The whole motivation for the split: this combination was previously
    // collapsed to a single red "failed" state, hiding the fact that the
    // runtime is fine. Now the two dimensions are visible separately.
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime(),
      tasks: [
        makeTask({
          status: "failed",
          completed_at: "2026-04-27T11:30:00Z",
          failure_reason: "agent_error",
        }),
      ],
      now: NOW,
    });
    expect(detail.availability).toBe("online");
    expect(detail.lastTask).toBe("failed");
    expect(detail.failureReason).toBe("agent_error");
    expect(detail.lastTaskCompletedAt).toBe("2026-04-27T11:30:00Z");
  });

  it("composes unstable + running — runtime hiccup with queued tasks still in flight", () => {
    // Previously "unstable" overrode "working"; now both signals are
    // surfaced. The UI shows amber dot AND running chip — user sees both
    // "connection issue" and "queue is paused".
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime({
        status: "offline",
        last_seen_at: "2026-04-27T11:59:00Z",
      }),
      tasks: [makeTask({ status: "queued" })],
      now: NOW,
    });
    expect(detail.availability).toBe("unstable");
    expect(detail.lastTask).toBe("running");
    expect(detail.queuedCount).toBe(1);
  });

  it("composes offline + idle for a brand-new agent on a dead runtime", () => {
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
    expect(detail.lastTask).toBe("idle");
  });

  it("handles a missing runtime by reporting offline + the task-driven last state", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: null,
      tasks: [
        makeTask({
          status: "completed",
          completed_at: "2026-04-27T11:30:00Z",
        }),
      ],
      now: NOW,
    });
    expect(detail.availability).toBe("offline");
    expect(detail.lastTask).toBe("completed");
  });

  it("leaves failureReason / lastTaskCompletedAt undefined when not relevant", () => {
    const detail = deriveAgentPresenceDetail({
      agent: makeAgent(),
      runtime: makeRuntime(),
      tasks: [makeTask({ status: "running" })],
      now: NOW,
    });
    expect(detail.failureReason).toBeUndefined();
    expect(detail.lastTaskCompletedAt).toBeUndefined();
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
        makeTask({
          id: "t2",
          agent_id: "b",
          status: "failed",
          completed_at: "2026-04-27T11:30:00Z",
        }),
      ],
      now: NOW,
    });
    const a = map.get("a");
    const b = map.get("b");
    expect(a?.availability).toBe("online");
    expect(a?.lastTask).toBe("running");
    expect(b?.availability).toBe("online");
    expect(b?.lastTask).toBe("failed");
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
    // Task dimension still resolves independently — running task counts.
    expect(o?.lastTask).toBe("running");
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
        makeTask({
          id: "t2",
          agent_id: "b",
          status: "failed",
          completed_at: "2026-04-27T11:00:00Z",
        }),
      ],
      now: NOW,
    });
    expect(map.get("a")?.availability).toBe("unstable");
    expect(map.get("b")?.availability).toBe("unstable");
    // Last-task remains independent: a is running (queued), b is failed.
    expect(map.get("a")?.lastTask).toBe("running");
    expect(map.get("b")?.lastTask).toBe("failed");
  });
});
