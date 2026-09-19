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
  icon?: string;
  phase: IssueWorkflowPhase;
  policy: IssueWorkflowEntryPolicy;
}

export function manualEntryPolicy(): IssueWorkflowEntryPolicy {
  return {
    executor: { type: "none" },
    instructions: "",
  };
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
          ? "done"
          : index === 0
            ? "unstarted"
            : "started",
      policy: manualEntryPolicy(),
    }),
  );
  return {
    initialKey: statuses[0]?.key ?? "",
    statuses,
  };
}

/** Preserve initial status and machine keys when opening an existing definition. */
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
      icon: s.icon,
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
  return { ...draft, statuses };
}

export function removeWorkflowStatus(
  draft: WorkflowDraft,
  key: string,
): WorkflowDraft {
  if (key === draft.initialKey || draft.statuses.length <= 1) return draft;
  const statuses = draft.statuses.filter((s) => s.key !== key);
  return { ...draft, statuses };
}

export type WorkflowProblem =
  | "empty"
  | "name"
  | "duplicate"
  | "executor"
  | "instructions"
  | "initial"
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
      !["unstarted", "started", "done", "closed"].includes(
        s.phase,
      )
    )
      problems.push({ key: s.key, problem: "phase" });
    if (s.policy.executor.type !== "none" && !s.policy.executor.id)
      problems.push({ key: s.key, problem: "executor" });
    if (s.policy.executor.type !== "none" && !s.policy.instructions.trim())
      problems.push({ key: s.key, problem: "instructions" });
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
      icon: s.icon,
      phase: s.phase,
      entry_policy: { executor: { ...s.policy.executor }, instructions: s.policy.instructions },
    })),
  };
}
