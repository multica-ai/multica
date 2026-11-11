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

/** When a chat last moved; what a closed chip is compared against. */
export function chatActivity(session: ChatSession): string {
  return session.last_message?.created_at ?? session.updated_at;
}

/**
 * Which chats the bottom bar shows. A chat gets a chip when it needs watching
 * (a task is running, or a reply is unread) or was opened in the quick-chat
 * window, and keeps it until the chip is closed. A closed chip comes back
 * when its chat moves again, or is opened again.
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
  kept,
  dismissed,
  maxVisible,
}: {
  sessions: ChatSession[];
  runningSessionIds: ReadonlySet<string>;
  /** The session the open window shows; null while the window is closed. */
  openSessionId: string | null;
  /** Chats opened in the quick-chat window. */
  kept: readonly string[];
  /** Closed chips: sessionId to the chat's activity when it was closed. */
  dismissed: Readonly<Record<string, string>>;
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
    .filter(
      (item) =>
        item.open ||
        ((item.running || item.unread || kept.includes(item.session.id)) &&
          dismissed[item.session.id] !== chatActivity(item.session)),
    )
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
