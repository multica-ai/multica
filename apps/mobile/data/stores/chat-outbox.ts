/**
 * Local-only chat delivery state.
 *
 * The server currently has no idempotency-key contract for chat sends. These
 * helpers therefore deliberately do not schedule background or connectivity
 * driven retries: a lost response can mean the server already accepted the
 * message. A user may explicitly retry after checking the conversation.
 */
export type ChatOutboxStatus = "queued" | "sending" | "failed";

export interface ChatOutboxItem {
  clientId: string;
  sessionId: string;
  workspaceSlug: string;
  userId: string;
  content: string;
  attachmentIds: string[];
  createdAt: string;
  status: ChatOutboxStatus;
  attemptCount: number;
  retryable: boolean;
  lastError: string | null;
  nextAttemptAt: string | null;
}

export const MAX_CHAT_OUTBOX_ATTEMPTS = 3;
export const CHAT_OUTBOX_RETRY_BASE_MS = 1_000;

export function createChatOutboxClientId(): string {
  // RFC 4122-shaped v4 identifier. It is retained even though the current
  // server cannot consume it, so a later API idempotency contract has a
  // stable client identity for every already-persisted local item.
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (char) => {
    const random = Math.floor(Math.random() * 16);
    const value = char === "x" ? random : (random & 0x3) | 0x8;
    return value.toString(16);
  });
}

export function sortChatOutbox(items: ChatOutboxItem[]): ChatOutboxItem[] {
  return [...items].sort((a, b) => {
    const byCreatedAt = a.createdAt.localeCompare(b.createdAt);
    return byCreatedAt !== 0 ? byCreatedAt : a.clientId.localeCompare(b.clientId);
  });
}

/** The first unconfirmed item blocks every later item in that session. */
export function nextChatOutboxItem(
  items: ChatOutboxItem[],
  sessionId: string,
  now = new Date(),
): ChatOutboxItem | null {
  const head = sortChatOutbox(items.filter((item) => item.sessionId === sessionId))[0];
  if (!head || head.status !== "queued") return null;
  if (head.nextAttemptAt && new Date(head.nextAttemptAt) > now) return null;
  return head;
}

export function retryDelayMs(attemptCount: number): number {
  return CHAT_OUTBOX_RETRY_BASE_MS * 2 ** Math.max(0, attemptCount - 1);
}

export function nextFailedChatOutboxItem(
  item: ChatOutboxItem,
  error: string,
  now = new Date(),
): ChatOutboxItem {
  const attemptCount = item.attemptCount + 1;
  if (attemptCount >= MAX_CHAT_OUTBOX_ATTEMPTS) {
    return {
      ...item,
      attemptCount,
      status: "failed",
      retryable: false,
      lastError: error,
      nextAttemptAt: null,
    };
  }
  return {
    ...item,
    attemptCount,
    status: "queued",
    retryable: true,
    lastError: error,
    nextAttemptAt: new Date(now.getTime() + retryDelayMs(attemptCount)).toISOString(),
  };
}

export function permanentlyFailedChatOutboxItem(
  item: ChatOutboxItem,
  error: string,
): ChatOutboxItem {
  return {
    ...item,
    attemptCount: item.attemptCount + 1,
    status: "failed",
    retryable: false,
    lastError: error,
    nextAttemptAt: null,
  };
}
