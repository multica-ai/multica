import type { Rock, RockIssue } from "./types";

// Builds the "connected execution" tree shown under a rock: the connected
// projects are the branches, their issues hang under them, and a sub-issue
// hangs under its parent issue whenever the parent is part of the same branch.
// Kept free of React so the shape is unit-tested directly (see rock-tree.test.ts).

export type RockTreeNodeKind = "project" | "issue";

export interface RockTreeNode {
  key: string;
  kind: RockTreeNodeKind;
  id: string;
  title: string;
  identifier?: string;
  status?: string;
  issueCount: number;
  doneIssueCount: number;
  children: RockTreeNode[];
}

const UNGROUPED = "";

const issueNode = (issue: RockIssue): RockTreeNode => ({
  key: `issue:${issue.id}`,
  kind: "issue",
  id: issue.id,
  title: issue.title,
  identifier: issue.identifier,
  status: issue.status,
  issueCount: 0,
  doneIssueCount: 0,
  children: [],
});

const countDescendants = (node: RockTreeNode): { total: number; done: number } => {
  let total = 0;
  let done = 0;
  for (const child of node.children) {
    total += 1;
    if (child.status === "done") done += 1;
    const nested = countDescendants(child);
    total += nested.total;
    done += nested.done;
  }
  return { total, done };
};

/**
 * Turns a rock's connected projects and issues into a tree.
 *
 * - Each connected project becomes a branch, with its issues underneath.
 * - Issues that belong to no connected project sit at the root of the tree.
 * - A sub-issue nests under its parent when the parent lives in the same
 *   branch; otherwise it is lifted to that branch's own root, so nothing is
 *   ever hidden by a parent the rock is not connected to.
 * - A parent cycle degrades to a flat branch instead of recursing forever.
 */
export function buildRockTree(rock: Pick<Rock, "projects" | "issues">): RockTreeNode[] {
  const projectIds = new Set(rock.projects.map((project) => project.id));
  const branchOf = (issue: RockIssue) => (issue.project_id && projectIds.has(issue.project_id) ? issue.project_id : UNGROUPED);

  const byId = new Map(rock.issues.map((issue) => [issue.id, issue]));
  const nodes = new Map(rock.issues.map((issue) => [issue.id, issueNode(issue)]));

  // A parent only counts when it is loaded, sits in the same branch, and does
  // not lead back to the issue itself.
  const parentOf = (issue: RockIssue): RockIssue | undefined => {
    const parent = issue.parent_id ? byId.get(issue.parent_id) : undefined;
    if (!parent || branchOf(parent) !== branchOf(issue)) return undefined;
    const seen = new Set([issue.id]);
    for (let step: RockIssue | undefined = parent; step; step = step.parent_id ? byId.get(step.parent_id) : undefined) {
      if (seen.has(step.id)) return undefined;
      seen.add(step.id);
    }
    return parent;
  };

  const roots = new Map<string, RockTreeNode[]>([[UNGROUPED, []]]);
  for (const id of projectIds) roots.set(id, []);

  for (const issue of rock.issues) {
    const node = nodes.get(issue.id);
    if (!node) continue;
    const parent = parentOf(issue);
    if (parent) nodes.get(parent.id)?.children.push(node);
    else roots.get(branchOf(issue))?.push(node);
  }

  const tree: RockTreeNode[] = rock.projects.map((project) => {
    const node: RockTreeNode = {
      key: `project:${project.id}`,
      kind: "project",
      id: project.id,
      title: project.title,
      issueCount: project.issue_count,
      doneIssueCount: project.done_issue_count,
      children: roots.get(project.id) ?? [],
    };
    return node;
  });

  for (const node of nodes.values()) {
    const { total, done } = countDescendants(node);
    node.issueCount = total;
    node.doneIssueCount = done;
  }

  return [...tree, ...(roots.get(UNGROUPED) ?? [])];
}
