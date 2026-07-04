// TECH-3708 — per-message deep links for the Dynamic inbox.
//
// The Dynamic inbox owns selection in local React state (see dynamic-inbox.tsx),
// so an open message left no trace in the URL and could not be copied/linked.
// This module defines the stable, copyable key for an entry and the reverse
// lookup, so opening a message can write `?message=<key>` to the URL and a
// pasted URL can re-open that exact message.
//
// The key mirrors `selectedKey` in dynamic-inbox.tsx: an issue notification is
// keyed by its issue (the issue detail is what opens, and the issue id is
// workspace-global / shareable), while every other entry is keyed by its own
// id. The `kind:` prefix keeps the three id spaces (notif/channel/chat) from
// ever colliding.
import type { DynInboxEntry } from "./section-filter";

/** URL search-param name that holds the open message's key. */
export const INBOX_MESSAGE_PARAM = "message";

/**
 * Stable, copyable key for an inbox entry. Mirrors the selection identity used
 * by the row list (notif → issue_id ?? item.id) so a link round-trips to the
 * same row/detail the user clicked.
 */
export function messageKeyForEntry(entry: DynInboxEntry): string {
  switch (entry.kind) {
    case "notif":
      return `notif:${entry.item.issue_id ?? entry.item.id}`;
    case "channel":
      return `channel:${entry.channel.id}`;
    case "chat":
      return `chat:${entry.session.id}`;
    case "thread":
      // FIR-1854 — a copyable link round-trips to the thread row.
      return `thread:${entry.threadRootId}`;
  }
}

/**
 * Reverse of {@link messageKeyForEntry}: find the entry a `?message=<key>` URL
 * refers to, or `null` when no current entry matches (e.g. the message was
 * archived, or the entries haven't loaded yet).
 */
export function findEntryByMessageKey(
  entries: DynInboxEntry[],
  key: string,
): DynInboxEntry | null {
  const sep = key.indexOf(":");
  if (sep < 0) return null;
  const kind = key.slice(0, sep);
  const id = key.slice(sep + 1);
  if (!id) return null;
  for (const entry of entries) {
    if (kind === "notif" && entry.kind === "notif" && (entry.item.issue_id ?? entry.item.id) === id)
      return entry;
    if (kind === "channel" && entry.kind === "channel" && entry.channel.id === id) return entry;
    if (kind === "channel" && entry.kind === "notif" && entry.item.issue_id === id) return entry;
    if (kind === "chat" && entry.kind === "chat" && entry.session.id === id) return entry;
    // FIR-1854 — round-trip a thread link back to its thread row.
    if (kind === "thread" && entry.kind === "thread" && entry.threadRootId === id) return entry;
  }
  return null;
}

/**
 * Decide the URL the "keep the address bar in sync with the open message"
 * effect should navigate to, or `null` to leave the URL untouched.
 *
 * FIR-2677 — the guard is load-bearing. That effect re-runs whenever the
 * navigation adapter hands the inbox a fresh `replace`/`replaceSilent`
 * reference (it does on every navigation), which means it can fire with a
 * STALE `selected` while a sidebar "New message" intent (`?chat`/`?agent`) or a
 * created-conversation intent (`?issue`) is still in the URL. Writing the stale
 * selection's `?message=<key>` there overwrites the incoming intent; the intent
 * reader then re-selects the previously open message and discards the new chat,
 * so the pane never moves. While an intent is present the consume effect owns
 * the URL, so this returns `null` and leaves it alone.
 */
export function nextInboxMessageUrl(params: {
  urlChat: string;
  urlIssue: string;
  /** `messageKeyForEntry(selected)` for the open row, or `null` when nothing is open. */
  selectedKey: string | null;
  /** Current value of the `?message=` param, or `null` when absent. */
  currentMessageParam: string | null;
  /** Workspace inbox path, e.g. `/acme/inbox`. */
  inboxPath: string;
}): string | null {
  const { urlChat, urlIssue, selectedKey, currentMessageParam, inboxPath } = params;
  // An unconsumed intent owns the URL — don't clobber it with a stale selection.
  if (urlChat || urlIssue) return null;
  if (selectedKey === currentMessageParam) return null;
  return selectedKey
    ? `${inboxPath}?${INBOX_MESSAGE_PARAM}=${encodeURIComponent(selectedKey)}`
    : inboxPath;
}

/**
 * Resolve the legacy `?issue=<id>` inbox URL param used by the global
 * "New message" flow. Channels/DMs also use issue ids, so prefer the channel
 * row when it exists. While channel data is still loading, callers can defer
 * issue-notification matches to avoid locking the detail pane onto IssueDetail
 * before the channel row has had a chance to arrive.
 */
export function findEntryByInboxIssueParam(
  entries: DynInboxEntry[],
  id: string,
  opts: { deferIssueNotifications?: boolean } = {},
): DynInboxEntry | null {
  if (!id) return null;
  for (const entry of entries) {
    if (entry.kind === "channel" && entry.id === id) return entry;
  }
  if (opts.deferIssueNotifications) return null;
  for (const entry of entries) {
    if (entry.kind === "notif" && (entry.item.issue_id ?? entry.item.id) === id)
      return entry;
  }
  return null;
}
