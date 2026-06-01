import type { Issue, IssueStatus } from "@multica/core/types";

export const UNPARENTED_SWIMLANE_ID = "unparented";

export interface IssueTreeRow {
  issue: Issue;
  depth: number;
  parentIssue?: Issue;
}

export interface IssueSwimlane {
  id: string;
  parentIssue?: Issue;
  missingParentId?: string;
  isUnparented?: boolean;
  issueIdsByStatus: Record<IssueStatus, string[]>;
  total: number;
}

function issueMap(issues: Issue[]) {
  return new Map(issues.map((issue) => [issue.id, issue]));
}

function emptyStatusBuckets(visibleStatuses: readonly IssueStatus[]) {
  const buckets = {} as Record<IssueStatus, string[]>;
  for (const status of visibleStatuses) buckets[status] = [];
  return buckets;
}

export function buildIssueTreeRows(
  issues: Issue[],
  allIssues: Issue[] = issues,
): IssueTreeRow[] {
  const parentById = issueMap(allIssues);
  const issueSet = new Set(issues.map((issue) => issue.id));
  const childrenByParent = new Map<string, Issue[]>();

  for (const issue of issues) {
    if (!issue.parent_issue_id || !issueSet.has(issue.parent_issue_id)) continue;
    const children = childrenByParent.get(issue.parent_issue_id) ?? [];
    children.push(issue);
    childrenByParent.set(issue.parent_issue_id, children);
  }

  const rows: IssueTreeRow[] = [];
  const visited = new Set<string>();
  const append = (issue: Issue, depth: number) => {
    if (visited.has(issue.id)) return;
    visited.add(issue.id);
    rows.push({
      issue,
      depth,
      parentIssue: issue.parent_issue_id ? parentById.get(issue.parent_issue_id) : undefined,
    });
    for (const child of childrenByParent.get(issue.id) ?? []) {
      append(child, depth + 1);
    }
  };

  for (const issue of issues) {
    if (!issue.parent_issue_id || !issueSet.has(issue.parent_issue_id)) {
      append(issue, issue.parent_issue_id ? 1 : 0);
    }
  }
  for (const issue of issues) {
    append(issue, issue.parent_issue_id ? 1 : 0);
  }
  return rows;
}

export function buildIssueSwimlanes(
  issues: Issue[],
  allIssues: Issue[],
  visibleStatuses: readonly IssueStatus[],
): IssueSwimlane[] {
  const parentById = issueMap(allIssues);
  const visibleStatusSet = new Set<IssueStatus>(visibleStatuses);
  const childrenByParent = new Map<string, Issue[]>();
  const parentIdsInOrder: string[] = [];
  const seenParentIds = new Set<string>();

  for (const issue of issues) {
    if (!issue.parent_issue_id) continue;
    const children = childrenByParent.get(issue.parent_issue_id) ?? [];
    children.push(issue);
    childrenByParent.set(issue.parent_issue_id, children);
    if (!seenParentIds.has(issue.parent_issue_id)) {
      seenParentIds.add(issue.parent_issue_id);
      parentIdsInOrder.push(issue.parent_issue_id);
    }
  }

  for (const issue of issues) {
    if (issue.parent_issue_id || !childrenByParent.has(issue.id)) continue;
    if (!seenParentIds.has(issue.id)) {
      seenParentIds.add(issue.id);
      parentIdsInOrder.push(issue.id);
    }
  }

  const lanes: IssueSwimlane[] = [];
  const visibleParentIds = new Set<string>();

  for (const parentId of parentIdsInOrder) {
    const lane: IssueSwimlane = {
      id: `parent:${parentId}`,
      parentIssue: parentById.get(parentId),
      missingParentId: parentById.has(parentId) ? undefined : parentId,
      issueIdsByStatus: emptyStatusBuckets(visibleStatuses),
      total: 0,
    };
    for (const child of childrenByParent.get(parentId) ?? []) {
      if (!visibleStatusSet.has(child.status)) continue;
      lane.issueIdsByStatus[child.status].push(child.id);
      lane.total += 1;
    }
    if (lane.total > 0) {
      lanes.push(lane);
      visibleParentIds.add(parentId);
    }
  }

  const unparented: IssueSwimlane = {
    id: UNPARENTED_SWIMLANE_ID,
    isUnparented: true,
    issueIdsByStatus: emptyStatusBuckets(visibleStatuses),
    total: 0,
  };

  for (const issue of issues) {
    if (issue.parent_issue_id) continue;
    if (visibleParentIds.has(issue.id)) continue;
    if (!visibleStatusSet.has(issue.status)) continue;
    unparented.issueIdsByStatus[issue.status].push(issue.id);
    unparented.total += 1;
  }

  if (unparented.total > 0) lanes.push(unparented);
  return lanes;
}
