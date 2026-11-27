import { describe, expect, it } from "vitest";
import type { AgentTask, WorkingAgentSummary } from "@multica/core/types";
import {
  collapseWorkingAgentsWithSnapshot,
  groupExecutionTasksByActor,
} from "./execution-actors";

function makeTask(overrides: Partial<AgentTask> & { id: string; agent_id: string }): AgentTask {
  return {
    runtime_id: "rt-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-11-24T00:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-11-24T00:00:00Z",
    ...overrides,
  };
}

describe("groupExecutionTasksByActor", () => {
  it("collapses leader and delegated member tasks sharing a squad_id into one squad unit", () => {
    const tasks: AgentTask[] = [
      makeTask({ id: "t1", agent_id: "emacs", squad_id: "jsquad", status: "running" }),
      makeTask({ id: "t2", agent_id: "lambda", squad_id: "jsquad", status: "queued" }),
      makeTask({ id: "t3", agent_id: "codex", status: "running" }),
    ];

    const groups = groupExecutionTasksByActor(tasks, new Set(["jsquad"]));
    expect(groups).toHaveLength(2);
    expect(groups[0]).toMatchObject({
      type: "squad",
      id: "jsquad",
      runningCount: 1,
      queuedCount: 1,
      agentIds: ["emacs", "lambda"],
    });
    expect(groups[0]?.tasks.map((t) => t.id)).toEqual(["t1", "t2"]);
    expect(groups[1]).toMatchObject({
      type: "agent",
      id: "codex",
      runningCount: 1,
      queuedCount: 0,
      agentIds: ["codex"],
    });
  });

  it("falls back to agent when squad_id is not in knownSquadIds", () => {
    const tasks: AgentTask[] = [
      makeTask({ id: "t1", agent_id: "emacs", squad_id: "deleted-squad" }),
    ];

    const groups = groupExecutionTasksByActor(tasks, new Set(["jsquad"]));
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({
      type: "agent",
      id: "emacs",
      agentIds: ["emacs"],
    });
  });
});

describe("collapseWorkingAgentsWithSnapshot", () => {
  it("collapses squad members into one squad execution unit and preserves standalone agents", () => {
    const workingAgents: WorkingAgentSummary[] = [
      { id: "emacs", running_task_count: 1 },
      { id: "lambda", running_task_count: 1 },
      { id: "codex", running_task_count: 2 },
    ];
    const snapshot: AgentTask[] = [
      makeTask({ id: "t1", agent_id: "emacs", squad_id: "jsquad", status: "running" }),
      makeTask({ id: "t2", agent_id: "lambda", squad_id: "jsquad", status: "running" }),
      makeTask({ id: "t3", agent_id: "codex", status: "running" }),
      makeTask({ id: "t4", agent_id: "codex", status: "running" }),
    ];

    const units = collapseWorkingAgentsWithSnapshot(
      workingAgents,
      snapshot,
      new Set(["jsquad"]),
    );
    expect(units).toEqual([
      {
        type: "squad",
        id: "jsquad",
        runningTaskCount: 2,
        memberAgentIds: ["emacs", "lambda"],
      },
      {
        type: "agent",
        id: "codex",
        runningTaskCount: 2,
        memberAgentIds: [],
      },
    ]);
  });

  it("caps attributed counts to the surface-projected running_task_count", () => {
    const workingAgents: WorkingAgentSummary[] = [
      { id: "emacs", running_task_count: 1 },
    ];
    const snapshot: AgentTask[] = [
      makeTask({ id: "t1", agent_id: "emacs", squad_id: "jsquad", status: "running" }),
      makeTask({ id: "t2", agent_id: "emacs", squad_id: "jsquad", status: "running" }),
      makeTask({ id: "t3", agent_id: "emacs", status: "running" }),
    ];

    const units = collapseWorkingAgentsWithSnapshot(
      workingAgents,
      snapshot,
      new Set(["jsquad"]),
    );
    expect(units).toEqual([
      {
        type: "squad",
        id: "jsquad",
        runningTaskCount: 1,
        memberAgentIds: ["emacs"],
      },
    ]);
  });

  it("falls back to raw working agents when snapshot has not loaded yet", () => {
    const workingAgents: WorkingAgentSummary[] = [
      { id: "emacs", running_task_count: 2 },
    ];
    expect(collapseWorkingAgentsWithSnapshot(workingAgents, [], null)).toEqual([
      {
        type: "agent",
        id: "emacs",
        runningTaskCount: 2,
        memberAgentIds: [],
      },
    ]);
  });
});
