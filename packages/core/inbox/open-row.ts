import type { InboxItem, SnoozedInboxIssue } from "../types";
import { isInboxItemSnoozed } from "./snooze";

/** A row's group key: its issue, or the notification itself when it has none. */
export function inboxRowKey(item: Pick<InboxItem, "issue_id" | "id">): string {
  return item.issue_id ?? item.id;
}

/** The open notification's row as a list last showed it, and its place there. */
export interface HeldInboxRow {
  key: string;
  item: InboxItem;
  index: number;
  /** The filters the list showed it under. */
  filters: unknown;
}

/**
 * Why the open row is kept although the list no longer has it: its
 * notification was archived or snoozed, or it no longer matches the filters.
 */
export type KeptInboxRowReason = "archived" | "snoozed" | "filtered";

export interface KeptInboxRow {
  key: string;
  item: InboxItem;
  reason: KeptInboxRowReason;
}

/**
 * The list with the open row kept where it was. Something other than the
 * user's own Inbox action can take the open row out: its issue reaching
 * Done or Cancelled archives it, an edit can stop it matching the filters,
 * another window can archive or snooze it. Pulling the row out from under
 * the issue the user is reading would leave the detail with no row and the
 * highlight with nowhere to go, so the row stays, marked as settled, until
 * the user moves on. The Inbox's own actions move on first and never reach
 * here (see `leaveInboxRow`).
 *
 * `notifications` is every active notification, unfiltered, which tells an
 * archived row from a filtered-out one. The active list leaves snoozed
 * notifications out, so `snoozed`, the viewer's snoozed issues, tells a
 * snoozed row from an archived one. Changing the filters is itself a request
 * to filter rows out, so a row they drop is kept only under the `filters` it
 * was shown with.
 */
export function keepOpenInboxRow(
  rows: InboxItem[],
  notifications: readonly InboxItem[],
  held: HeldInboxRow | undefined,
  openKey: string | null,
  filters: unknown,
  { snoozed = [], now = Date.now() }: { snoozed?: readonly SnoozedInboxIssue[]; now?: number } = {},
): { rows: InboxItem[]; kept: KeptInboxRow | null } {
  if (!openKey || !held || held.key !== openKey) return { rows, kept: null };
  if (rows.some((row) => inboxRowKey(row) === openKey)) return { rows, kept: null };
  const group = notifications.filter((item) => inboxRowKey(item) === openKey);
  const snooze =
    group.length === 0
      ? snoozed.find((entry) => entry.issue_id === openKey && new Date(entry.snoozed_until).getTime() > now)
      : undefined;
  const reason: KeptInboxRowReason = snooze
    ? "snoozed"
    : group.length === 0
      ? "archived"
      : group.some((item) => isInboxItemSnoozed(item, now))
        ? "snoozed"
        : "filtered";
  if (reason === "filtered" && held.filters !== filters) return { rows, kept: null };
  // Not archived: show it as it is now. A snoozed issue's notification is the
  // one its snooze entry names, which the snooze endpoints take.
  const item = snooze
    ? { ...held.item, id: snooze.item_id, snoozed_until: snooze.snoozed_until }
    : reason === "archived"
      ? held.item
      : (group.find((other) => other.id === held.item.id) ?? group[0]!);
  const at = Math.min(held.index, rows.length);
  return {
    rows: [...rows.slice(0, at), item, ...rows.slice(at)],
    kept: { key: openKey, item, reason },
  };
}

/**
 * The row to open once `key` leaves: rows run newest first, so the older one
 * below it, else the newer one above. `leaving` names other rows going at the
 * same time, as a batch archive does.
 */
export function nextInboxRow(
  rows: readonly InboxItem[],
  key: string,
  leaving: (item: InboxItem) => boolean = () => false,
): InboxItem | null {
  const index = rows.findIndex((row) => inboxRowKey(row) === key);
  if (index === -1) return null;
  const stays = (row: InboxItem) => inboxRowKey(row) !== key && !leaving(row);
  return rows.slice(index + 1).find(stays) ?? rows.slice(0, index).findLast(stays) ?? null;
}
