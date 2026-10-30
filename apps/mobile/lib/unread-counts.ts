/**
 * Unread count hooks for the bottom tab bar badges.
 *
 * Mirrors the counting logic from:
 *   - packages/core/inbox/queries.ts::useInboxUnreadCount (inbox — which,
 *     like this one, reads the server-computed cross-workspace summary)
 *   - packages/core/chat/unread.ts::countUnreadChatMessages (chat — the
 *     shared pure function IS the definition; web's sidebar calls the same
 *     one, so the platforms cannot drift apart)
 *
 * Both queries (`inboxUnreadSummaryOptions`, `chatSessionsOptions`) are
 * already kept fresh by listing-level realtime hooks mounted in
 * `app/(app)/[workspace]/_layout.tsx`, so these hooks only attach a `select`
 * to derive a scalar count — re-rendering the tab layout only when the
 * number actually changes (TQ compares select output with Object.is).
 *
 * Behavioral parity (apps/mobile/CLAUDE.md "Counts and visibility must agree"):
 * the N rendered here MUST equal the N web shows for the same user/workspace.
 */
import { useQuery } from "@tanstack/react-query";
import { countUnreadChatMessages } from "@multica/core/chat/unread";
import { inboxUnreadSummaryOptions } from "@/data/queries/inbox";
import { chatSessionsOptions } from "@/data/queries/chat";

/**
 * Unread inbox count for the tab badge.
 *
 * Read from the cross-workspace unread summary, not from the inbox list.
 * The summary is one small server-computed row per workspace, and the server
 * applies the same newest-per-issue rule `deduplicateInboxItems`
 * (lib/inbox-display.ts) applies before render — so the N here still equals
 * the N web shows and the N the inbox tab lists. Counting the list locally
 * meant fetching the entire unbounded inbox on app start just to render this
 * number (MUL-6967).
 *
 * The lookup mirrors `unreadCountForWorkspace` in
 * packages/core/inbox/queries.ts rather than importing it — that module pulls
 * in core's API client, which mobile does not use (same reason
 * `deduplicateInboxItems` is mirrored into lib/inbox-display.ts). A workspace
 * with nothing unread is absent from the response, so a missing entry is zero.
 */
export function useInboxUnreadCount(wsId: string | null | undefined): number {
  const { data } = useQuery({
    ...inboxUnreadSummaryOptions(wsId ?? null),
    select: (summary) =>
      wsId ? (summary.find((s) => s.workspace_id === wsId)?.count ?? 0) : 0,
  });
  return data ?? 0;
}

/**
 * Total unread assistant *messages* across chat sessions (IM-style), the
 * same number web/desktop's sidebar Chat badge shows. Was a session count
 * before MUL-4286; that matched the (since removed) web ChatFab badge and
 * disagreed with the sidebar.
 *
 * No excludeSessionId here: the chat tab renders the active conversation
 * itself, and the focused-screen auto mark-read (chat.tsx) plus the
 * mutation's optimistic unread_count reset clear that session's share of
 * the badge immediately — a badge decrementing on the tab you are already
 * inside is normal IM behavior, not a phantom.
 */
export function useChatUnreadMessageCount(
  wsId: string | null | undefined,
): number {
  const { data } = useQuery({
    ...chatSessionsOptions(wsId ?? null),
    select: (sessions) => countUnreadChatMessages(sessions),
  });
  return data ?? 0;
}
