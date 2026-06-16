import type { Issue } from "@multica/core/types";

export interface IssueRelationshipTrace {
  ancestors: Set<string>;
  descendants: Set<string>;
}

export function traceIssueRelationships(
  issues: Issue[],
  selectedId: string | null,
): IssueRelationshipTrace {
  const ancestors = new Set<string>();
  const descendants = new Set<string>();
  if (!selectedId) return { ancestors, descendants };

  const issueById = new Map(issues.map((issue) => [issue.id, issue]));
  const childrenByParent = new Map<string, string[]>();
  for (const issue of issues) {
    if (!issue.parent_issue_id || !issueById.has(issue.parent_issue_id)) continue;
    const children = childrenByParent.get(issue.parent_issue_id) ?? [];
    children.push(issue.id);
    childrenByParent.set(issue.parent_issue_id, children);
  }

  const visitedAncestors = new Set([selectedId]);
  let parentId = issueById.get(selectedId)?.parent_issue_id ?? null;
  while (parentId && issueById.has(parentId) && !visitedAncestors.has(parentId)) {
    ancestors.add(parentId);
    visitedAncestors.add(parentId);
    parentId = issueById.get(parentId)?.parent_issue_id ?? null;
  }

  const visitedDescendants = new Set([selectedId]);
  const queue = [...(childrenByParent.get(selectedId) ?? [])];
  while (queue.length > 0) {
    const issueId = queue.shift()!;
    if (visitedDescendants.has(issueId)) continue;
    visitedDescendants.add(issueId);
    descendants.add(issueId);
    queue.push(...(childrenByParent.get(issueId) ?? []));
  }

  return { ancestors, descendants };
}
