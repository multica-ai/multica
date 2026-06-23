import type { Issue } from "@multica/core/types";

/**
 * Info about a cross-status parent, used to render a clickable chip
 * on child rows whose parent lives in a different status column.
 */
export interface ParentInfo {
  identifier: string;
  status: string;
  parentId: string;
}

/**
 * Lightweight reference to a cross-status child, used in the
 * expandable dropdown on the parent row.
 */
export interface CrossStatusChild {
  identifier: string;
  status: string;
  id: string;
}

/**
 * A single item in the render tree for a status group.
 * Produced by {@link buildHierarchy} and consumed by {@link ListView}.
 */
export interface RenderItem {
  issue: Issue;
  /** 0 = top-level, 1 = first-level child, etc. */
  indent: number;
  /** True when this issue has visible children nested under it (in the same status). */
  isParent: boolean;
  /** Number of children nested under this parent in the same status group. */
  childCount: number;
  /** Number of children in other status groups (for cross-status badge on parent row). */
  crossStatusChildCount: number;
  /** Cross-status children for the expandable dropdown on the parent row. */
  crossStatusChildren: CrossStatusChild[];
  /** When a child's parent is NOT in the same status group, info about that parent. */
  parentInfo?: ParentInfo;
  /** When a child's parent is in the same status group but hidden (e.g., filtered out). */
  orphaned?: boolean;
}

/**
 * Recursively render a single issue and, if it is an expanded parent,
 * all of its same-status descendants.
 */
function renderSubtree(
  issue: Issue,
  indent: number,
  childrenInStatus: Map<string, Issue[]>,
  childrenMap: Map<string, readonly Issue[]>,
  expandedParents: ReadonlySet<string>,
  statusIssueIds: Set<string>,
  parentInfoMap?: Map<string, ParentInfo>,
): RenderItem[] {
  const nestedChildren = childrenInStatus.get(issue.id);
  const isParent = !!nestedChildren && nestedChildren.length > 0;
  const expanded = expandedParents.has(issue.id);

  const allChildren = childrenMap.get(issue.id) ?? [];
  const sameStatusCount = nestedChildren?.length ?? 0;
  const crossStatusCount = allChildren.length - sameStatusCount;

  // Build the list of cross-status children for the dropdown.
  const crossStatusChildren: CrossStatusChild[] = [];
  if (isParent && crossStatusCount > 0) {
    for (const child of allChildren) {
      if (!statusIssueIds.has(child.id)) {
        crossStatusChildren.push({
          identifier: child.identifier,
          status: child.status,
          id: child.id,
        });
      }
    }
  }

  // Resolve parent info for top-level items whose parent is cross-status.
  // Children rendered via recursion always have their parent in the same status,
  // so parentInfo is only needed for top-level cross-status orphans.
  let parentInfo: ParentInfo | undefined;
  if (indent === 0 && issue.parent_issue_id && !statusIssueIds.has(issue.parent_issue_id)) {
    parentInfo = parentInfoMap?.get(issue.parent_issue_id);
  }

  const result: RenderItem[] = [
    {
      issue,
      indent,
      isParent,
      childCount: isParent ? sameStatusCount : 0,
      crossStatusChildCount: isParent ? crossStatusCount : 0,
      crossStatusChildren,
      parentInfo,
    },
  ];

  if (isParent && expanded) {
    for (const child of nestedChildren!) {
      result.push(
        ...renderSubtree(
          child,
          indent + 1,
          childrenInStatus,
          childrenMap,
          expandedParents,
          statusIssueIds,
          parentInfoMap,
        ),
      );
    }
  }

  return result;
}

/**
 * Organise a status-group's flat issue list into a hierarchical order:
 * - Top-level issues first (no parent, or parent in a different status).
 * - Children whose parent IS in the same status group are nested right after
 *   the parent with increasing indent, recursively for any depth.
 * - Children whose parent is in a different status group render at top level
 *   with a clickable `parentInfo` chip.
 *
 * @param issues - a single status-group's issues (already filtered by status).
 * @param childrenMap - parent_issue_id → its direct children (all statuses).
 * @param statusIssueIds - the set of all issue ids that belong to this status group.
 * @param expandedParents - the set of parent issue ids whose children are expanded.
 * @param parentInfoMap - parent issue id → { identifier, status } for all known parents.
 */
export function buildHierarchy(
  issues: Issue[],
  childrenMap: Map<string, readonly Issue[]>,
  statusIssueIds: Set<string>,
  expandedParents: ReadonlySet<string>,
  parentInfoMap?: Map<string, ParentInfo>,
): RenderItem[] {
  const result: RenderItem[] = [];

  // Build a lookup for which parents have children in this status.
  const childrenInStatus = new Map<string, Issue[]>();
  for (const issue of issues) {
    if (!issue.parent_issue_id) continue;
    const children = childrenMap.get(issue.parent_issue_id);
    if (!children) continue;

    // Only count children that are in the same status group.
    const sameStatusChildren = children.filter((c) => statusIssueIds.has(c.id));
    if (sameStatusChildren.length === 0) continue;

    const bucket = childrenInStatus.get(issue.parent_issue_id);
    if (bucket) {
      bucket.push(issue);
    } else {
      childrenInStatus.set(issue.parent_issue_id, [issue]);
    }
  }

  // Separate top-level vs. child issues.
  const topLevel: Issue[] = [];

  for (const issue of issues) {
    if (!issue.parent_issue_id || !statusIssueIds.has(issue.parent_issue_id)) {
      // No parent, or parent not in this status group.
      topLevel.push(issue);
    }
  }

  // Recursively render each top-level issue and its descendants.
  for (const issue of topLevel) {
    result.push(
      ...renderSubtree(
        issue,
        0,
        childrenInStatus,
        childrenMap,
        expandedParents,
        statusIssueIds,
        parentInfoMap,
      ),
    );
  }

  // Append any issues that weren't rendered (shouldn't happen in normal use,
  // but guards against edge cases where parent was filtered or not loaded).
  for (const issue of issues) {
    if (!result.some((r) => r.issue.id === issue.id)) {
      result.push({
        issue,
        indent: 0,
        isParent: false,
        childCount: 0,
        crossStatusChildCount: 0,
        crossStatusChildren: [],
        orphaned: !!issue.parent_issue_id,
      });
    }
  }

  return result;
}
