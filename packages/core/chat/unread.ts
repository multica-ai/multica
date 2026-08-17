import type { ChatSession } from "../types/chat";

/**
 * The chat unread badge number: total unread assistant *messages* across
 * sessions (IM-style), NOT the number of sessions with unread.
 *
 * Single source of truth for every surface that renders an aggregate chat
 * unread count (web/desktop sidebar, mobile tab bar) — the platforms must
 * show the same N for the same account state (see "Behavioral parity" in
 * apps/mobile/CLAUDE.md). Pure function so mobile can import it directly.
 *
 * `excludeSessionId` drops one session from the sum: the session the user is
 * currently reading. The chat thread list renders that session's own badge
 * as 0 (its unread is about to be cleared by the viewer's auto mark-read),
 * so an aggregate that still counted it would show a number the list can't
 * account for. Callers pass it only while a chat surface is actually showing
 * that session; a background pointer (e.g. a remembered selection while the
 * chat page is closed) must NOT be excluded, or new replies would never
 * badge.
 */
export function countUnreadChatMessages(
  sessions: readonly ChatSession[] | undefined,
  excludeSessionId?: string | null,
): number {
  if (!sessions) return 0;
  return sessions.reduce(
    (sum, s) =>
      s.id === excludeSessionId ? sum : sum + (s.unread_count ?? 0),
    0,
  );
}
