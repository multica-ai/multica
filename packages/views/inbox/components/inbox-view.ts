import type { InboxTab } from "@multica/core/inbox/selection-store";
import type { WorkspacePaths } from "@multica/core/paths";

export type { InboxTab };

/**
 * Which list a notification row belongs to: the live Inbox or the archive.
 *
 * The two are mutually exclusive per issue — the server decides which list
 * an issue belongs to (see ListArchivedInboxItems) — so a view is a complete
 * swap of the list's data source and row action, not a filter layered on one
 * list.
 */
export type InboxView = "inbox" | "archived";

export const ARCHIVED_VIEW_PARAM = "archived";
export const SNOOZED_VIEW_PARAM = "snoozed";

/**
 * The Inbox page's tab, from its `?view=`. The URL carries it so a refresh, a
 * back/forward step, or a phone's detail-back returns to the tab the user was
 * actually in.
 */
export function inboxTabFromParam(view: string | null): InboxTab {
  return view === ARCHIVED_VIEW_PARAM ? "archived" : view === SNOOZED_VIEW_PARAM ? "snoozed" : "inbox";
}

/**
 * The Inbox page on `tab`, with `key` open: a notification's row key
 * (`issue_id ?? id`), or nothing for the tab's list.
 */
export function inboxTabHref(p: WorkspacePaths, tab: InboxTab, key?: string | null): string {
  const params = new URLSearchParams();
  if (tab !== "inbox") params.set("view", tab);
  if (key) params.set("issue", key);
  const query = params.toString();
  return query ? `${p.inbox()}?${query}` : p.inbox();
}
