import type { Issue } from "@multica/core/types";

export const ISSUE_GRAPH_NODE_WIDTH = 264;
export const ISSUE_GRAPH_NODE_HEIGHT = 112;

const COLUMN_GAP = 112;
const ROW_GAP = 42;

export interface IssueGraphPosition {
  x: number;
  y: number;
}

export function layoutIssueGraph(issues: Issue[]): Map<string, IssueGraphPosition> {
  const issueById = new Map(issues.map((issue) => [issue.id, issue]));
  const childrenByParent = new Map<string, Issue[]>();

  for (const issue of issues) {
    if (!issue.parent_issue_id || !issueById.has(issue.parent_issue_id)) continue;
    const children = childrenByParent.get(issue.parent_issue_id) ?? [];
    children.push(issue);
    childrenByParent.set(issue.parent_issue_id, children);
  }

  const compareIssues = (a: Issue, b: Issue) =>
    a.position - b.position ||
    a.number - b.number ||
    a.created_at.localeCompare(b.created_at);

  for (const children of childrenByParent.values()) children.sort(compareIssues);

  const roots = issues
    .filter(
      (issue) =>
        !issue.parent_issue_id || !issueById.has(issue.parent_issue_id),
    )
    .sort(compareIssues);

  const depthById = new Map<string, number>();
  const visited = new Set<string>();
  const queue = roots.map((issue) => ({ issue, depth: 0 }));

  while (queue.length > 0) {
    const current = queue.shift()!;
    if (visited.has(current.issue.id)) continue;
    visited.add(current.issue.id);
    depthById.set(current.issue.id, current.depth);
    for (const child of childrenByParent.get(current.issue.id) ?? []) {
      queue.push({ issue: child, depth: current.depth + 1 });
    }
  }

  // API drift or corrupt legacy data should not make the graph disappear.
  for (const issue of issues) {
    if (!visited.has(issue.id)) {
      depthById.set(issue.id, 0);
      roots.push(issue);
    }
  }

  const columns = new Map<number, Issue[]>();
  for (const issue of issues) {
    const depth = depthById.get(issue.id) ?? 0;
    const column = columns.get(depth) ?? [];
    column.push(issue);
    columns.set(depth, column);
  }

  const positions = new Map<string, IssueGraphPosition>();
  for (const [depth, column] of columns) {
    column.sort(compareIssues);
    column.forEach((issue, index) => {
      positions.set(issue.id, {
        x: depth * (ISSUE_GRAPH_NODE_WIDTH + COLUMN_GAP),
        y: index * (ISSUE_GRAPH_NODE_HEIGHT + ROW_GAP),
      });
    });
  }

  return positions;
}
