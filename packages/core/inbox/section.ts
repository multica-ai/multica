import type { InboxItem, InboxSection } from "../types";

/** The item's Inbox section; a backend that predates sections means "action". */
export function inboxItemSection(item: Pick<InboxItem, "section">): InboxSection {
  return item.section === "activity" ? "activity" : "action";
}

/**
 * Whether the item shows as unread. Activity is never unread, whatever its
 * stored `read` flag: the server leaves it out of every unread count.
 */
export function isInboxItemUnread(item: Pick<InboxItem, "read" | "section">): boolean {
  return !item.read && inboxItemSection(item) === "action";
}
