import {
  pointerWithin,
  closestCenter,
  type CollisionDetection,
} from "@dnd-kit/core";
import type { Issue, IssueAssigneeType, IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import type { IssueGrouping } from "@multica/core/issues/stores/view-store";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type { BoardColumnGroup } from "../components/board-column";

export type DragMoveUpdates = Pick<
  UpdateIssueRequest,
  "status" | "assignee_type" | "assignee_id" | "position"
>;

const UNASSIGNED_GROUP_ID = "assignee:unassigned";

export function makeKanbanCollision(groupIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      const items = pointer.filter((c) => !groupIds.has(c.id as string));
      if (items.length > 0) return items;
      return pointer;
    }
    return closestCenter(args);
  };
}

export function statusGroupId(status: IssueStatus): string {
  return `status:${status}`;
}

export function propertyGroupId(propertyId: string, optionId: string | null): string {
  return `property:${propertyId}:${optionId ?? "none"}`;
}

export function assigneeGroupId(
  type: IssueAssigneeType | null,
  id: string | null,
): string {
  return type && id ? `assignee:${type}:${id}` : UNASSIGNED_GROUP_ID;
}

export function getIssueGroupId(
  issue: Issue,
  grouping: IssueGrouping,
  knownOptionIds?: ReadonlySet<string>,
): string {
  if (grouping === "status") return statusGroupId(issue.status);
  const propertyId = propertyIdFromViewKey(grouping);
  if (propertyId) {
    const value = issue.properties?.[propertyId];
    let optionId = typeof value === "string" ? value : null;
    // A value referencing an option no longer in the definition (removed
    // before the in-use guard existed, or by a newer server) must bucket
    // into the No-value column — an unmatched column id would silently drop
    // the issue from the board.
    if (optionId !== null && knownOptionIds && !knownOptionIds.has(optionId)) {
      optionId = null;
    }
    return propertyGroupId(propertyId, optionId);
  }
  return assigneeGroupId(issue.assignee_type, issue.assignee_id);
}

export function buildColumns(
  issues: Issue[],
  groups: BoardColumnGroup[],
  grouping: IssueGrouping,
  knownOptionIds?: ReadonlySet<string>,
): Record<string, string[]> {
  const cols: Record<string, string[]> = {};
  for (const group of groups) cols[group.id] = [];
  for (const issue of issues) {
    const gid = getIssueGroupId(issue, grouping, knownOptionIds);
    if (cols[gid]) cols[gid].push(issue.id);
  }
  return cols;
}

export function computePosition(ids: string[], activeId: string, issueMap: Map<string, Issue>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  if (ids.length === 1) return issueMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

/**
 * Insert `id` into `ids` at the slot implied by `position ASC`, reading each
 * id's position from `issueMap`. Mirrors `insertByPosition` in
 * `@multica/core/issues/cache-helpers` so the board's optimistic placement on
 * drop matches the cache the settle reconcile rebuilds from — otherwise the
 * card would land in one slot, then jump when local columns re-derive from TQ.
 */
export function insertIdByPosition(
  ids: string[],
  id: string,
  position: number,
  issueMap: Map<string, Issue>,
): string[] {
  const idx = ids.findIndex((existing) => {
    const p = issueMap.get(existing)?.position;
    return p !== undefined && p > position;
  });
  if (idx === -1) return [...ids, id];
  return [...ids.slice(0, idx), id, ...ids.slice(idx)];
}

export function findColumn(
  columns: Record<string, string[]>,
  id: string,
  columnIds: Set<string>,
): string | null {
  if (columnIds.has(id)) return id;
  for (const [columnId, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return columnId;
  }
  return null;
}

export function issueMatchesGroup(issue: Issue, group: BoardColumnGroup): boolean {
  if (group.status) return issue.status === group.status;
  if (group.propertyId !== undefined) {
    const value = issue.properties?.[group.propertyId];
    const optionId = typeof value === "string" ? value : null;
    return optionId === (group.propertyOptionId ?? null);
  }
  return (
    (issue.assignee_type ?? null) === (group.assigneeType ?? null) &&
    (issue.assignee_id ?? null) === (group.assigneeId ?? null)
  );
}

export function getMoveUpdates(
  group: BoardColumnGroup,
  position: number,
): DragMoveUpdates {
  if (group.status) return { status: group.status, position };
  // Property columns: the value change is not part of UpdateIssueRequest —
  // the board applies it through useSetIssueProperty after the position move.
  if (group.propertyId !== undefined) return { position };
  return {
    assignee_type: group.assigneeType ?? null,
    assignee_id: group.assigneeId ?? null,
    position,
  };
}
