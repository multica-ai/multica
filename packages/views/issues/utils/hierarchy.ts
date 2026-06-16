import type { Issue } from "@multica/core/types";

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
  /** When a child's parent is NOT in the same status group, show the parent's identifier. */
  parentIdentifier?: string;
  /** When a child's parent is in the same status group but hidden (e.g., filtered out). */
  orphaned?: boolean;
}

/**
 * Organise a status-group's flat issue list into a hierarchical order:
 * - Top-level issues first (no parent, or parent in a different status).
 * - Children whose parent IS in the same status group are nested right after
 *   the parent, with indent=1.
 * - Children whose parent is in a different status group render at top level
 *   with a subtle `parentIdentifier` label.
 *
 * @param issues - a single status-group's issues (already filtered by status).
 * @param childrenMap - parent_issue_id → its direct children (all statuses).
 * @param statusIssueIds - the set of all issue ids that belong to this status group.
 * @param expandedParents - the set of parent issue ids whose children are expanded.
 * @param parentIdentifierMap - parent issue id → its identifier string (e.g., "MUL-123").
 */
export function buildHierarchy(
  issues: Issue[],
  childrenMap: Map<string, readonly Issue[]>,
  statusIssueIds: Set<string>,
  expandedParents: ReadonlySet<string>,
  parentIdentifierMap?: Map<string, string>,
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
  const childIds = new Set<string>();

  for (const issue of issues) {
    if (!issue.parent_issue_id || !statusIssueIds.has(issue.parent_issue_id)) {
      // No parent, or parent not in this status group.
      topLevel.push(issue);
    } else {
      // Parent is in this status group — this issue will be nested.
      childIds.add(issue.id);
    }
  }

  for (const issue of topLevel) {
    const nestedChildren = childrenInStatus.get(issue.id);
    const isParent = !!nestedChildren && nestedChildren.length > 0;
    const expanded = expandedParents.has(issue.id);

    // Resolve parent identifier for children whose parent is in another status group.
    let parentIdentifier: string | undefined;
    if (issue.parent_issue_id && !statusIssueIds.has(issue.parent_issue_id)) {
      parentIdentifier = parentIdentifierMap?.get(issue.parent_issue_id);
    }

    result.push({
      issue,
      indent: 0,
      isParent,
      childCount: isParent ? nestedChildren!.length : 0,
      parentIdentifier,
    });

    if (isParent && expanded) {
      for (const child of nestedChildren!) {
        result.push({
          issue: child,
          indent: 1,
          isParent: false,
          childCount: 0,
        });
      }
    }
  }

  // Append any children whose parent wasn't in the top-level (shouldn't happen,
  // but guards against edge cases where parent was filtered or not loaded).
  for (const issue of issues) {
    if (!result.some((r) => r.issue.id === issue.id)) {
      result.push({
        issue,
        indent: 0,
        isParent: false,
        childCount: 0,
        orphaned: !!issue.parent_issue_id,
      });
    }
  }

  return result;
}
