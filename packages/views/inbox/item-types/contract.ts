import type { ComponentType } from "react";
import type { ChatSession, InboxItem } from "@multica/core/types";

/**
 * Inbox is a *typed feed*: one list whose entries each declare a high-level
 * `kind`, and a registered renderer decides how that kind looks in the list
 * and behaves when opened. The envelope (id, sort time, unread) and the
 * list/triage machinery are shared across kinds; only the row + detail are
 * polymorphic.
 *
 * Two kinds ship today, both backed by real data:
 *   - `issue_notification` — an {@link InboxItem} from the inbox feed.
 *   - `conversation`        — an agent {@link ChatSession} surfaced inline.
 *
 * Further kinds (approval, digest, …) register another renderer without
 * touching the list, filtering, sorting, or read machinery. See MUL-3788.
 */
export type InboxItemKind = "issue_notification" | "conversation";

/**
 * A normalized entry in the merged feed. Sources as different as a
 * notification and a chat session are projected onto one shape — `id`,
 * `sortAt`, `unread` form the shared envelope the list sorts/filters on —
 * while the kind-specific payload rides along for its renderer.
 */
export type InboxFeedEntry =
  | {
      kind: "issue_notification";
      id: string;
      sortAt: string;
      unread: boolean;
      notification: InboxItem;
    }
  | {
      kind: "conversation";
      id: string;
      sortAt: string;
      unread: boolean;
      conversation: ChatSession;
    };

/** Project a raw notification onto a feed entry. */
export function notificationEntry(item: InboxItem): InboxFeedEntry {
  return {
    kind: "issue_notification",
    id: item.id,
    sortAt: item.created_at,
    unread: !item.read,
    notification: item,
  };
}

/** Project a chat session onto a feed entry. */
export function conversationEntry(session: ChatSession): InboxFeedEntry {
  return {
    kind: "conversation",
    id: session.id,
    sortAt: session.updated_at,
    unread: session.has_unread,
    conversation: session,
  };
}

/** Props every kind's list-row component receives. */
export interface InboxItemRowProps {
  entry: InboxFeedEntry;
  isSelected: boolean;
  onSelect: () => void;
  onArchive: () => void;
}

/** Props every kind's detail-pane component receives. */
export interface InboxItemDetailProps {
  entry: InboxFeedEntry;
  onArchive: () => void;
}

/**
 * A renderer is the per-kind half of the contract. `Row` is required (a kind
 * must be listable); `Detail` is optional — a kind may open elsewhere (e.g. a
 * conversation opens the chat window) or defer to a shared surface (an
 * issue-backed notification defers to `IssueDetail`) instead of rendering its
 * own pane.
 */
export interface InboxItemRenderer {
  kind: InboxItemKind;
  Row: ComponentType<InboxItemRowProps>;
  Detail?: ComponentType<InboxItemDetailProps>;
}

const registry = new Map<InboxItemKind, InboxItemRenderer>();

/** Register a renderer for an item kind. Last registration wins. */
export function registerInboxItemType(renderer: InboxItemRenderer): void {
  registry.set(renderer.kind, renderer);
}

export function hasInboxItemRenderer(kind: InboxItemKind): boolean {
  return registry.has(kind);
}

/**
 * Resolve the renderer for a kind. Throws if none is registered — a missing
 * renderer is a wiring bug (the registering module was not imported), not a
 * runtime condition to handle.
 */
export function getInboxItemRenderer(kind: InboxItemKind): InboxItemRenderer {
  const renderer = registry.get(kind);
  if (!renderer) {
    throw new Error(`No inbox item renderer registered for kind "${kind}"`);
  }
  return renderer;
}
