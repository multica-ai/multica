import type { Agent, AgentRuntime } from "../types";

export type AssignmentOption = {
  id: string;
  label: string;
  assigned: boolean;
  editable: boolean;
  supported: boolean;
};

/** Only explicit choices are changed; inaccessible bindings are never replaced. */
export function codeGraphAssignmentChanges(
  options: AssignmentOption[],
  selected: ReadonlySet<string>,
  touched?: ReadonlySet<string>,
) {
  return options
    .filter(
      (option) =>
        (!touched || touched.has(option.id)) &&
        option.editable &&
        option.assigned !== selected.has(option.id) &&
        (!selected.has(option.id) || option.supported),
    )
    .map((option) => ({ id: option.id, assigned: selected.has(option.id) }));
}

export function canManageCodeGraphAgent(
  agent: Pick<Agent, "owner_id" | "archived_at">,
  workspaceWriter: boolean,
  userId?: string,
) {
  return (
    !agent.archived_at &&
    (workspaceWriter || Boolean(userId && agent.owner_id === userId))
  );
}

export function agentSupportsCodeGraph(agent: Agent, runtimes: AgentRuntime[]) {
  return runtimes.some(
    (runtime) =>
      runtime.id === agent.runtime_id && runtime.supports_code_graph === true,
  );
}
