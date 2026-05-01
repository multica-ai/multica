import type { Issue, IssueAssigneeType, IssuePriority, IssueStatus } from "@/shared/types";
import type { IssueDraft } from "@/features/issues/stores/draft-store";

export interface CreateIssueTemplateData extends Record<string, unknown> {
  title?: string;
  description?: string | null;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  parent_issue_id?: string | null;
  project_id?: string | null;
  due_date?: string | null;
  start_date?: string | null;
  end_date?: string | null;
  label_ids?: string[];
}

export interface CreateIssueInitialValues {
  title: string;
  description: string;
  status: IssueStatus;
  priority: IssuePriority;
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  parentIssueId?: string;
  projectId?: string;
  dueDate: string | null;
  startDate: string | null;
  endDate: string | null;
  labelIds: string[];
}

function hasOwn(data: Record<string, unknown> | null | undefined, key: string): boolean {
  return !!data && Object.prototype.hasOwnProperty.call(data, key);
}

function readString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function readNullableString(value: unknown): string | null | undefined {
  if (value === null) return null;
  return typeof value === "string" ? value : undefined;
}

function readLabelIds(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return Array.from(new Set(value.filter((item): item is string => typeof item === "string" && item.length > 0)));
}

export function getCreateIssueInitialValues(
  draft: IssueDraft,
  data?: Record<string, unknown> | null,
): CreateIssueInitialValues {
  return {
    title: readString(data?.title) ?? draft.title,
    description: hasOwn(data, "description") ? readNullableString(data?.description) ?? "" : draft.description,
    status: (readString(data?.status) as IssueStatus | undefined) ?? draft.status,
    priority: (readString(data?.priority) as IssuePriority | undefined) ?? draft.priority,
    assigneeType: hasOwn(data, "assignee_type")
      ? (readNullableString(data?.assignee_type) as IssueAssigneeType | null | undefined) ?? undefined
      : draft.assigneeType,
    assigneeId: hasOwn(data, "assignee_id") ? readNullableString(data?.assignee_id) ?? undefined : draft.assigneeId,
    parentIssueId: hasOwn(data, "parent_issue_id") ? readNullableString(data?.parent_issue_id) ?? undefined : draft.parentIssueId,
    projectId: hasOwn(data, "project_id") ? readNullableString(data?.project_id) ?? undefined : undefined,
    dueDate: hasOwn(data, "due_date") ? readNullableString(data?.due_date) ?? null : draft.dueDate,
    startDate: hasOwn(data, "start_date") ? readNullableString(data?.start_date) ?? null : draft.startDate,
    endDate: hasOwn(data, "end_date") ? readNullableString(data?.end_date) ?? null : draft.endDate,
    labelIds: readLabelIds(data?.label_ids),
  };
}

export function buildIssueTemplateData(issue: Issue): CreateIssueTemplateData {
  return {
    title: `Copy of ${issue.title}`,
    description: issue.description ?? "",
    status: issue.status,
    priority: issue.priority,
    assignee_type: issue.assignee_type,
    assignee_id: issue.assignee_id,
    parent_issue_id: issue.parent_issue_id,
    project_id: issue.project_id,
    due_date: issue.due_date,
    start_date: issue.start_date,
    end_date: issue.end_date,
    label_ids: issue.labels?.map((label) => label.id) ?? [],
  };
}