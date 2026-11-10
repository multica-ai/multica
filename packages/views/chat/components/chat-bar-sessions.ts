import type { ChatSession } from "@multica/core/types";

export interface ChatBarSession {
  session: ChatSession;
  running: boolean;
  unread: boolean;
  /** The chat window is open on this session. */
  open: boolean;
}

export interface ChatBarSessions {
  visible: ChatBarSession[];
  overflow: ChatBarSession[];
}

/**
 * Which chats the bottom bar shows. A chat earns a chip while it needs
 * watching (a task is running, or a reply is unread) or while the window is
 * open on it; a finished, read chat goes back to the history.
 *
 * Chips are ordered by creation, newest first, so a chip keeps its place for
 * the chat's whole life: ordering by activity would swap two running chats on
 * every message. The open chat is always among the visible chips, because the
 * window opens above its chip.
 */
export function selectChatBarSessions({
  sessions,
  runningSessionIds,
  openSessionId,
  maxVisible,
}: {
  sessions: ChatSession[];
  runningSessionIds: ReadonlySet<string>;
  /** The session the open window shows; null while the window is closed. */
  openSessionId: string | null;
  maxVisible: number;
}): ChatBarSessions {
  const items = sessions
    .filter((session) => session.status !== "archived")
    .map((session) => ({
      session,
      running: runningSessionIds.has(session.id),
      unread: session.has_unread,
      open: session.id === openSessionId,
    }))
    .filter((item) => item.running || item.unread || item.open)
    .sort(
      (a, b) =>
        new Date(b.session.created_at).getTime() - new Date(a.session.created_at).getTime(),
    );

  const limit = Math.max(1, maxVisible);
  const openIndex = items.findIndex((item) => item.open);
  if (openIndex >= limit) {
    const [open] = items.splice(openIndex, 1);
    if (open) items.unshift(open);
  }
  return { visible: items.slice(0, limit), overflow: items.slice(limit) };
}
