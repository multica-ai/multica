import type { IssueViewDefinition } from "../issues/stores/view-store";

export type IssueViewTemplateId =
  | "current"
  | "blocked"
  | "needs_review"
  | "unassigned"
  | "urgent";

export const ISSUE_VIEW_TEMPLATE_IDS: IssueViewTemplateId[] = [
  "current",
  "blocked",
  "needs_review",
  "unassigned",
  "urgent",
];

export function applyIssueViewTemplate(
  definition: IssueViewDefinition,
  template: IssueViewTemplateId,
): IssueViewDefinition {
  if (template === "current") return definition;
  const next: IssueViewDefinition = {
    ...definition,
    statusFilters: [],
    priorityFilters: [],
    assigneeFilters: [],
    includeNoAssignee: false,
    creatorFilters: [],
    projectFilters: [],
    includeNoProject: false,
    labelFilters: [],
    propertyFilters: {},
    dateFilter: null,
    agentRunningFilter: false,
  };
  if (template === "blocked") next.statusFilters = ["blocked"];
  if (template === "needs_review") next.statusFilters = ["in_review"];
  if (template === "unassigned") next.includeNoAssignee = true;
  if (template === "urgent") next.priorityFilters = ["urgent"];
  return next;
}
