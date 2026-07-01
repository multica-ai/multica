import type { CreateIssueRequest, UpdateIssueRequest, IssueStatus, IssuePriority } from "../types";
import { adfToText } from "./adf";
import type { JiraIssue } from "./types";

/** Built-in Jira-status (lowercased) -> Multica status. Unmatched -> backlog.
 *  User overrides (also lowercased keys) take precedence in mapStatus. */
const DEFAULT_STATUS_MAP: Record<string, IssueStatus> = {
  backlog: "backlog",
  "to do": "todo",
  open: "todo",
  "in progress": "in_progress",
  "in review": "in_review",
  "挂起": "backlog",
  done: "done",
  closed: "done",
  resolved: "done",
};

/** Language-independent fallback: Jira's statusCategory.key is always one of
 *  these three regardless of instance language, so it maps non-English status
 *  names (e.g. "待修复") that DEFAULT_STATUS_MAP can't match. */
const CATEGORY_MAP: Record<string, IssueStatus> = {
  new: "todo",
  indeterminate: "in_progress",
  done: "done",
};

const DEFAULT_PRIORITY_MAP: Record<string, IssuePriority> = {
  highest: "urgent",
  high: "high",
  medium: "medium",
  low: "low",
  lowest: "low",
};

/** Resolve a Multica status from a Jira status. Precedence: user override by
 *  status name → built-in name map → statusCategory.key fallback → backlog. */
export function mapStatus(
  jiraStatus: string,
  categoryKey: string,
  overrides: Record<string, IssueStatus>,
): IssueStatus {
  const key = jiraStatus.trim().toLowerCase();
  return (
    overrides[key] ??
    DEFAULT_STATUS_MAP[key] ??
    CATEGORY_MAP[categoryKey.trim().toLowerCase()] ??
    "backlog"
  );
}

export function mapPriority(jiraPriority: string | null | undefined): IssuePriority {
  if (!jiraPriority) return "none";
  return DEFAULT_PRIORITY_MAP[jiraPriority.trim().toLowerCase()] ?? "none";
}

export function jiraIssueToCreateRequest(
  issue: JiraIssue,
  statusOverrides: Record<string, IssueStatus>,
  currentMemberId: string,
): CreateIssueRequest {
  const f = issue.fields;
  return {
    title: f.summary,
    description: adfToText(f.description),
    status: mapStatus(f.status.name, f.status.statusCategory.key, statusOverrides),
    priority: mapPriority(f.priority?.name),
    ...(f.duedate ? { due_date: f.duedate } : {}),
    assignee_type: "member",
    assignee_id: currentMemberId,
  };
}

export function jiraIssueToUpdateRequest(
  issue: JiraIssue,
  statusOverrides: Record<string, IssueStatus>,
): UpdateIssueRequest {
  const f = issue.fields;
  return {
    title: f.summary,
    description: adfToText(f.description),
    status: mapStatus(f.status.name, f.status.statusCategory.key, statusOverrides),
    priority: mapPriority(f.priority?.name),
    due_date: f.duedate ?? null,
  };
}
