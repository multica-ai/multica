import type { Issue } from "@multica/core/types";

/**
 * Board sub-issue nesting, per column. Sub-issues render inside their
 * parent's card, recursively:
 * - a parent in the same column hosts its sub-issues in its own card;
 * - a parent outside the column is shown as a muted "ghost" container around
 *   its sub-issues, so a sub-issue moved to another status keeps its context.
 *   The ghost itself sits inside the grandparent's card when that one is in
 *   the column, otherwise at the top level.
 * Every issue in the column appears exactly once.
 */
export type BoardTreeNode =
  | {
      kind: "issue";
      key: string;
      issue: Issue;
      children: BoardTreeNode[];
      /** Issue cards anywhere beneath this node in the column. */
      descendantCount: number;
    }
  | {
      kind: "ghost";
      key: string;
      parentId: string;
      /** Undefined while the parent is not loaded on the board or fetched yet. */
      parent: Issue | undefined;
      children: BoardTreeNode[];
      descendantCount: number;
    };

export type ParentLookup = (issueId: string) => Issue | undefined;

/** Collapse state is keyed by the hosting issue id (real card or ghost). */
export function nodeIssueId(node: BoardTreeNode): string {
  return node.kind === "issue" ? node.issue.id : node.parentId;
}

export function buildBoardColumnTree(
  ids: readonly string[],
  issueMap: ReadonlyMap<string, Issue>,
  lookupParent: ParentLookup,
): BoardTreeNode[] {
  const inColumn = new Set(ids);
  const issueNodes = new Map<string, BoardTreeNode & { kind: "issue" }>();
  for (const id of ids) {
    const issue = issueMap.get(id);
    if (issue && !issueNodes.has(id)) {
      issueNodes.set(id, {
        kind: "issue",
        key: id,
        issue,
        children: [],
        descendantCount: 0,
      });
    }
  }

  const roots: BoardTreeNode[] = [];
  const ghosts = new Map<string, BoardTreeNode & { kind: "ghost" }>();
  const hostIn = (hostId: string | null | undefined, selfId: string) =>
    hostId && hostId !== selfId && inColumn.has(hostId)
      ? issueNodes.get(hostId)
      : undefined;

  for (const node of issueNodes.values()) {
    const parentId = node.issue.parent_issue_id;
    const host = hostIn(parentId, node.issue.id);
    if (host) {
      host.children.push(node);
      continue;
    }
    if (!parentId || parentId === node.issue.id) {
      roots.push(node);
      continue;
    }
    let ghost = ghosts.get(parentId);
    if (!ghost) {
      const parent = issueMap.get(parentId) ?? lookupParent(parentId);
      ghost = {
        kind: "ghost",
        key: `ghost:${parentId}`,
        parentId,
        parent,
        children: [],
        descendantCount: 0,
      };
      ghosts.set(parentId, ghost);
      const ghostHost = hostIn(parent?.parent_issue_id, parentId);
      (ghostHost ? ghostHost.children : roots).push(ghost);
    }
    ghost.children.push(node);
  }

  // Corrupt parent data could form a cycle that no root reaches. Surface
  // those cards flat at the end instead of dropping them from the board.
  const reached = new Set<string>();
  const count = (node: BoardTreeNode): number => {
    if (node.kind === "issue") {
      if (reached.has(node.issue.id)) return 0;
      reached.add(node.issue.id);
    }
    let total = 0;
    for (const child of node.children) {
      total += count(child) + (child.kind === "issue" ? 1 : 0);
    }
    node.descendantCount = total;
    return total;
  };
  for (const root of roots) count(root);
  for (const node of issueNodes.values()) {
    if (reached.has(node.issue.id)) continue;
    roots.push({ ...node, children: [], descendantCount: 0 });
    reached.add(node.issue.id);
  }
  return roots;
}

/** Issue ids in display order, skipping the contents of collapsed nodes. */
export function visibleTreeIssueIds(
  nodes: readonly BoardTreeNode[],
  collapsed: ReadonlySet<string>,
): string[] {
  const result: string[] = [];
  const walk = (list: readonly BoardTreeNode[]) => {
    for (const node of list) {
      if (node.kind === "issue") result.push(node.issue.id);
      if (!collapsed.has(nodeIssueId(node))) walk(node.children);
    }
  };
  walk(nodes);
  return result;
}

/** Ids that share `activeId`'s parent — the cards it is ordered against. */
export function siblingIds(
  ids: readonly string[],
  activeId: string,
  issueMap: ReadonlyMap<string, Issue>,
): string[] {
  const parentOf = (id: string) => issueMap.get(id)?.parent_issue_id ?? null;
  const level = parentOf(activeId);
  return ids.filter((id) => parentOf(id) === level);
}

/**
 * Issue ids rendered inside `rootId`'s card in this column, at any depth and
 * including collapsed ones — the sub-issues that travel with it when the card
 * is dragged to another column. Sub-issues in other columns are not included.
 */
export function nestedIssueIds(
  ids: readonly string[],
  issueMap: ReadonlyMap<string, Issue>,
  lookupParent: ParentLookup,
  rootId: string,
): string[] {
  const result: string[] = [];
  const collect = (nodes: readonly BoardTreeNode[]) => {
    for (const node of nodes) {
      if (node.kind === "issue") result.push(node.issue.id);
      collect(node.children);
    }
  };
  const find = (nodes: readonly BoardTreeNode[]): boolean => {
    for (const node of nodes) {
      if (node.kind === "issue" && node.issue.id === rootId) {
        collect(node.children);
        return true;
      }
      if (find(node.children)) return true;
    }
    return false;
  };
  find(buildBoardColumnTree(ids, issueMap, lookupParent));
  return result;
}
