import type {
  Issue,
  IssueStatus,
  IssuePriority,
  IssueAssigneeType,
} from "../types";

/**
 * Shared assignee across a selection. `{ type: null, id: null }` means every
 * selected issue is unassigned — a real shared value, distinct from a mixed
 * selection (which {@link commonIssueFields} reports as `assignee: null`).
 */
export interface CommonAssignee {
  type: IssueAssigneeType | null;
  id: string | null;
}

/**
 * The status / priority / assignee shared by every issue in a batch selection.
 * A field is `null` when the selection is empty or the issues disagree
 * ("mixed"). Batch property pickers use this to reflect the real common value
 * and fall back to an empty (no-checkmark) state when the values differ,
 * instead of asserting a hardcoded default.
 */
export interface CommonIssueFields {
  status: IssueStatus | null;
  priority: IssuePriority | null;
  assignee: CommonAssignee | null;
}

/**
 * Returns the value shared by every item, or `null` when the list is empty or
 * the items disagree. Comparison is by primitive equality, so callers pass a
 * scalar key (collapse composite values to a string before calling).
 */
function sharedValue<T>(values: readonly T[]): T | null {
  if (values.length === 0) return null;
  const first = values[0]!;
  return values.every((v) => v === first) ? first : null;
}

const ASSIGNEE_KEY_SEP = "\u0000";

/**
 * Collapse a polymorphic assignee (type + id, either nullable) into a single
 * comparable key so all-unassigned issues compare equal to each other and
 * distinct from any assigned actor.
 */
function assigneeKey(type: IssueAssigneeType | null, id: string | null): string {
  return `${type ?? ""}${ASSIGNEE_KEY_SEP}${id ?? ""}`;
}

/**
 * Derive the common status / priority / assignee of the selected issues.
 * Pass the already-filtered selection (the issues that are actually selected),
 * mirroring how the skill list filters its rows by `selectedIds` before
 * handing them to its batch toolbar.
 */
export function commonIssueFields(issues: readonly Issue[]): CommonIssueFields {
  const status = sharedValue(issues.map((i) => i.status));
  const priority = sharedValue(issues.map((i) => i.priority));

  const sharedAssigneeKey = sharedValue(
    issues.map((i) => assigneeKey(i.assignee_type, i.assignee_id)),
  );
  const assignee =
    sharedAssigneeKey !== null && issues.length > 0
      ? { type: issues[0]!.assignee_type, id: issues[0]!.assignee_id }
      : null;

  return { status, priority, assignee };
}
