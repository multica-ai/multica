/**
 * Which of the inbox's two lists is showing.
 *
 * "inbox" is the default list of active notifications; "archived" is the
 * sub-view reached from the entry at the bottom of that list. The two are
 * mutually exclusive per issue — the server decides which list an issue
 * belongs to (see ListArchivedInboxItems) — so a view is a complete swap of
 * the list's data source and row action, not a filter layered on one list.
 *
 * Persisted in the URL as `?view=archived` so a refresh, a back/forward step,
 * or a mobile detail-back returns to the list the user was actually in.
 */
export type InboxView = "inbox" | "archived";

export const ARCHIVED_VIEW_PARAM = "archived";
