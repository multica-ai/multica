import type {
  AgentStatus,
  AgentTask,
  AgentVisibility,
  IssuePriority,
  IssueStatus,
} from "@multica/core/types";

type Translate = (key: string, options?: Record<string, unknown>) => string;

export function formatIssueStatus(t: Translate, status: IssueStatus) {
  return t(`issues.statuses.${status}`);
}

export function formatIssuePriority(t: Translate, priority: IssuePriority) {
  return t(`issues.priorities.${priority}`);
}

export function formatAgentStatus(t: Translate, status: AgentStatus) {
  return t(`agents.statuses.${status}`);
}

export function formatAgentTaskStatus(t: Translate, status: AgentTask["status"]) {
  return t(`agents.task_statuses.${status}`);
}

export function formatAgentVisibility(t: Translate, visibility: AgentVisibility) {
  return t(`agents.visibility.${visibility}`);
}

export function formatRuntimeMode(t: Translate, mode: "cloud" | "local") {
  return t(`common.runtime_modes.${mode}`);
}
