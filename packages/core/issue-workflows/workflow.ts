import type {
  IssueWorkflowEntryPolicy,
  IssueWorkflowPhase,
  IssueWorkflowResponse,
  IssueWorkflowSpec,
} from "../types";

export interface WorkflowDraft {
  initialKey: string;
  statuses: WorkflowStatus[];
}
export interface WorkflowStatus {
  key: string;
  name: string;
  description: string;
  color: string;
  phase: IssueWorkflowPhase;
  policy: IssueWorkflowEntryPolicy;
}

export function manualEntryPolicy(): IssueWorkflowEntryPolicy {
  return {
    assignee: { type: "keep" },
    executor: { type: "none" },
    instructions: "",
    advance: "human_confirms",
  };
}

/** Connections are materialized by editor operations, never inferred at execution time. */
function connectWorkflowStatuses(
  statuses: WorkflowStatus[],
): WorkflowStatus[] {
  return statuses.map((status, index) => ({
    ...status,
    policy: {
      ...status.policy,
      next_status_key:
        status.phase === "completed" || status.phase === "cancelled"
          ? ""
          : (statuses[index + 1]?.key ?? ""),
    },
  }));
}

export function createWorkflowDraft(
  names: string[],
): WorkflowDraft {
  const statuses = names.map(
    (name, index): WorkflowStatus => ({
      key: `status_${index + 1}`,
      name,
      description: "",
      color: "#6b7280",
      phase:
        index === names.length - 1
          ? "completed"
          : index === 0
            ? "backlog"
            : "started",
      policy: manualEntryPolicy(),
    }),
  );
  return {
    initialKey: statuses[0]?.key ?? "",
    statuses: connectWorkflowStatuses(statuses),
  };
}

/** Preserve explicit links, initial status, and machine keys when opening an existing definition. */
export function workflowFromDefinition(
  data: IssueWorkflowResponse,
): WorkflowDraft {
  const active = data.statuses
    .filter((s) => !s.archived_at)
    .sort((a, b) => a.position - b.position);
  return {
    initialKey:
      active.find((s) => s.id === data.workflow.initial_status_id)?.spec_key ??
      active[0]?.spec_key ??
      "",
    statuses: active.map((s) => ({
      key: s.spec_key,
      name: s.name,
      description: s.description,
      color: s.color,
      phase: s.phase as IssueWorkflowPhase,
      policy: structuredClone(s.entry_policy),
    })),
  };
}

export function moveWorkflowStatus(
  draft: WorkflowDraft,
  from: number,
  to: number,
): WorkflowDraft {
  if (
    from < 0 ||
    to < 0 ||
    from >= draft.statuses.length ||
    to >= draft.statuses.length ||
    from === to
  )
    return draft;
  const statuses = [...draft.statuses];
  const [status] = statuses.splice(from, 1);
  if (!status) return draft;
  statuses.splice(to, 0, status);
  return { ...draft, statuses: connectWorkflowStatuses(statuses) };
}

export function removeWorkflowStatus(
  draft: WorkflowDraft,
  key: string,
): WorkflowDraft {
  if (key === draft.initialKey || draft.statuses.length <= 1) return draft;
  const removed = draft.statuses.find((s) => s.key === key);
  // Repair only links to the removed status; keep unrelated custom handoffs.
  const statuses = draft.statuses
    .filter((s) => s.key !== key)
    .map((s) =>
      s.policy.next_status_key === key
        ? {
            ...s,
            policy: {
              ...s.policy,
              next_status_key:
                removed?.policy.next_status_key === s.key
                  ? ""
                  : (removed?.policy.next_status_key ?? ""),
            },
          }
        : s,
    );
  return { ...draft, statuses };
}

export type WorkflowProblem =
  | "empty"
  | "name"
  | "duplicate"
  | "executor"
  | "instructions"
  | "initial"
  | "next"
  | "phase";
export function workflowProblems(
  draft: WorkflowDraft,
): Array<{ key: string; problem: WorkflowProblem }> {
  const problems: Array<{ key: string; problem: WorkflowProblem }> = [];
  const names = new Set<string>();
  const keys = new Set(draft.statuses.map((s) => s.key));
  if (!draft.statuses.length || draft.statuses.length > 50)
    problems.push({ key: "", problem: "empty" });
  if (!keys.has(draft.initialKey))
    problems.push({ key: "", problem: "initial" });
  for (const s of draft.statuses) {
    const name = s.name.trim().toLowerCase();
    if (!name || [...s.name.trim()].length > 64)
      problems.push({ key: s.key, problem: "name" });
    if (names.has(name)) problems.push({ key: s.key, problem: "duplicate" });
    names.add(name);
    if (
      !["backlog", "unstarted", "started", "completed", "cancelled"].includes(
        s.phase,
      )
    )
      problems.push({ key: s.key, problem: "phase" });
    if (
      (s.policy.executor.type !== "none" && !s.policy.executor.id) ||
      (s.policy.assignee.type !== "keep" && !s.policy.assignee.id)
    )
      problems.push({ key: s.key, problem: "executor" });
    if (s.policy.executor.type !== "none" && !s.policy.instructions.trim())
      problems.push({ key: s.key, problem: "instructions" });
    if (
      s.policy.next_status_key &&
      (!keys.has(s.policy.next_status_key) ||
        s.policy.next_status_key === s.key)
    )
      problems.push({ key: s.key, problem: "next" });
  }
  return problems;
}

export function workflowToSpec(
  draft: WorkflowDraft,
  name: string,
): IssueWorkflowSpec {
  return {
    api_version: 1,
    name: [...name.trim()].slice(0, 64).join(""),
    initial_status: draft.initialKey,
    statuses: draft.statuses.map((s) => ({
      key: s.key,
      name: s.name.trim(),
      description: s.description,
      color: s.color,
      phase: s.phase,
      entry_policy: structuredClone(s.policy),
    })),
  };
}
