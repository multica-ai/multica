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
    if (kind === "chat" && entry.kind === "chat" && entry.session.id === id) return entry;
  }
  return null;
}
