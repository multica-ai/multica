/**
 * Unread count hooks for the bottom tab bar badges.
 *
 * Mirrors the counting logic from:
 *   - packages/core/inbox/queries.ts::useInboxUnreadCount (inbox)
 *   - packages/views/chat/components/chat-fab.tsx (chat: sessions with unread)
 *
 * Both queries (`inboxListOptions`, `chatSessionsOptions`) are already kept
 * fresh by listing-level realtime hooks mounted in
 * `app/(app)/[workspace]/_layout.tsx`, so these hooks only attach a `select`
 * to derive a scalar count — re-rendering the tab layout only when the
 * number actually changes (TQ compares select output with Object.is).
 *
 * Behavioral parity (apps/mobile/CLAUDE.md "Counts and visibility must agree"):
 * the N rendered here MUST equal the N web shows for the same user/workspace.
 */
import { useQuery } from "@tanstack/react-query";
import { inboxListOptions } from "@/data/queries/inbox";
import { chatSessionsOptions } from "@/data/queries/chat";
import { deduplicateInboxItems } from "@/lib/inbox-display";

/**
 * Unread inbox count, aligned with what the inbox list renders: archived
 * items dropped, then deduplicated by issue (one entry per issue), then
 * filtered to unread. Same definition as web's sidebar badge.
 */
export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    ...inboxListOptions(wsId ?? null),
    select: (items) =>
      deduplicateInboxItems(items).filter((i) => !i.read).length,
  });
  return data ?? 0;
}

/**
 * Number of chat sessions that have at least one unread assistant reply.
 * Matches web ChatFab's `sessions.filter(s => s.has_unread).length` — this
 * is a session count, not a message count.
 */
export function useChatUnreadSessionCount(
  wsId: string | null | undefined,
): number {
  const { data } = useQuery({
    ...chatSessionsOptions(wsId ?? null),
    select: (sessions) => sessions.filter((s) => s.has_unread).length,
  });
  return data ?? 0;
}
