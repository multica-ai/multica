import type { WorkflowAssignee, WorkflowNode } from "../types/workflow";

export type WorkflowTaskMode = "task" | "review" | "condition";

export function workflowTaskMode(node: WorkflowNode): WorkflowTaskMode {
  if (node.type === "human_review") return "review";
  if (node.type === "condition") return "condition";
  return "task";
}

export function workflowTaskActor(node: WorkflowNode): WorkflowAssignee | undefined {
  if (node.assignee) return node.assignee;
  if (node.agentId) return { type: "agent", id: node.agentId };
  return undefined;
}

/**
 * Keep the editor's single task configuration mapped to the engine's
 * compatible node types. The engine still distinguishes runnable agents,
 * human work items, reviews, and conditions at execution time.
 */
export function workflowTaskModePatch(node: WorkflowNode, mode: WorkflowTaskMode): Partial<WorkflowNode> {
  const actor = workflowTaskActor(node);
  if (mode === "condition") {
    return {
      type: "condition",
      agentId: undefined,
      assignee: undefined,
      config: { ...(node.config ?? {}), has_default: true },
    };
  }
  if (mode === "review") {
    return {
      type: "human_review",
      agentId: undefined,
      assignee: actor?.type === "member" ? actor : undefined,
      config: { ...(node.config ?? {}), has_default: true },
    };
  }
  return actor?.type === "member"
    ? { type: "human_task", agentId: undefined, assignee: actor }
    : { type: "agent", agentId: actor?.type === "agent" ? actor.id : undefined, assignee: undefined };
}

export function workflowTaskActorPatch(node: WorkflowNode, actor: WorkflowAssignee): Partial<WorkflowNode> {
  if (node.type === "human_review") {
    return actor.type === "member"
      ? { assignee: actor, agentId: undefined }
      : {};
  }
  return actor.type === "member"
    ? { type: "human_task", assignee: actor, agentId: undefined }
    : { type: "agent", agentId: actor.id, assignee: undefined };
}
