// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { WorkflowNode } from "../types/workflow";
import { workflowTaskActor, workflowTaskActorPatch, workflowTaskMode, workflowTaskModePatch } from "./task";

const node = (patch: Partial<WorkflowNode> = {}): WorkflowNode => ({
  id: "task",
  type: "agent",
  label: "Task",
  position: { x: 0, y: 0 },
  ...patch,
});

describe("workflow task configuration", () => {
  it.each([
    ["agent", "task"],
    ["human_task", "task"],
    ["human_review", "review"],
    ["condition", "condition"],
  ] as const)("maps %s to %s", (type, expected) => {
    expect(workflowTaskMode(node({ type }))).toBe(expected);
  });

  it("uses the same actor shape for agent and member tasks", () => {
    const member = { type: "member", id: "member-1" } as const;
    expect(workflowTaskActor(node({ agentId: "agent-1" }))).toEqual({ type: "agent", id: "agent-1" });
    expect(workflowTaskActor(node({ assignee: member }))).toEqual(member);
    expect(workflowTaskActorPatch(node(), member)).toMatchObject({ type: "human_task", assignee: member });
    expect(workflowTaskActorPatch(node(), { type: "agent", id: "agent-1" })).toMatchObject({ type: "agent", agentId: "agent-1" });
  });

  it("keeps a member when switching a task into review", () => {
    const member = { type: "member", id: "member-1" } as const;
    expect(workflowTaskModePatch(node({ assignee: member }), "review")).toMatchObject({ type: "human_review", assignee: member });
    expect(workflowTaskModePatch(node(), "condition")).toMatchObject({ type: "condition", config: { has_default: true } });
  });
});
