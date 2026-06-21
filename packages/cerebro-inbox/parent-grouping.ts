// "Group by → Parent issue" — cerebro-only inbox grouping (FIR-1717).
//
// Buckets each inbox entry by the parent of the issue it refers to, so every
// notification about sub-issues of the same parent clusters into one group and
// the user can see they belong together. Items whose issue has no parent (or
// non-issue rows like chats/channels) land in a single "No parent issue"
// fallback bucket.
//
// The grouping logic lives here (cerebro zone) so the upstream inbox page only
// needs a couple of marked CEREBRO-PATCH touchpoints that delegate into it.

// Side-effect import: pulls the cerebro module augmentation that adds
// parent_issue_id / parent_issue_title to the upstream InboxItem interface.
import "@multica/cerebro-types";
import type { InboxItem } from "@multica/core/types";

/**
 * The "Parent issue" entry for the inbox Group-by menu. English label by
 * product decision (FIR-1717) — kept stable across locales for now.
 */
export const INBOX_PARENT_GROUP_BY_OPTION = {
  value: "parent" as const,
  label: "Parent issue",
};

/** Structural subset of the inbox page's MergedEntry we bucket against. */
export type InboxParentEntry =
  | { kind: "notif"; item: InboxItem }
  | { kind: "chat"; session: unknown }
  | { kind: "channel"; channel: unknown };

/**
 * Adapter for the inbox page's `bucketize`. Returns the same shape the other
 * group-by modes return. Pure — unit-tested in parent-grouping.test.ts.
 */
export function bucketizeInboxParent(
  entry: InboxParentEntry,
): { key: string; label: string; isFallback: boolean } {
  if (entry.kind === "notif" && entry.item.parent_issue_id) {
    return {
      key: entry.item.parent_issue_id,
      label: entry.item.parent_issue_title ?? "Unknown parent",
      isFallback: false,
    };
  }
  return { key: "__no_parent__", label: "No parent issue", isFallback: true };
}
