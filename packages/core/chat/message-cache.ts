import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import { chatKeys } from "./queries";
import type { ChatMessage, ChatMessagesPage } from "../types";

/**
 * The single door a settled chat message goes through on its way into the
 * message caches (MUL-5711).
 *
 * Before this, every writer rolled its own: `chat:done` inline-inserted the
 * assistant reply, `chat:message` threw its payload away and only invalidated,
 * and the three send paths each appended by hand — two of them to both caches,
 * the Agent Builder to the flat one only. The gap that produced this bug was in
 * that inconsistency, not in any single writer: a human's own message was the
 * one row no WebSocket handler ever wrote, so it reached the transcript solely
 * through a refetch, and a refetch that gets cancelled takes the message with
 * it.
 *
 * Two rules, both order-independent, so the same message arriving twice by
 * different routes converges instead of duplicating or downgrading:
 *
 *   1. Identity is `message.id`. A second write for an id already present
 *      merges instead of appending.
 *   2. On merge the row already in cache wins every field it defines; the
 *      incoming row only fills fields that are missing. A send response
 *      carrying `attachments` therefore survives the WebSocket echo that has
 *      no attachments field at all — in either arrival order.
 */

/** Fields present on `incoming` fill only the gaps in `existing`. */
function mergeChatMessage(existing: ChatMessage, incoming: ChatMessage): ChatMessage {
  const fills: Partial<ChatMessage> = {};
  let filled = false;
  for (const key of Object.keys(incoming) as (keyof ChatMessage)[]) {
    if (existing[key] !== undefined || incoming[key] === undefined) continue;
    Object.assign(fills, { [key]: incoming[key] });
    filled = true;
  }
  // Reference-stable when nothing was filled, so observers do not re-render.
  return filled ? { ...existing, ...fills } : existing;
}

/**
 * Merge into the row with this id if the list has one. Never appends — the
 * caller decides where a genuinely new message goes, which for a paged cache is
 * one specific page.
 */
function mergeIntoList(messages: ChatMessage[], message: ChatMessage): ChatMessage[] {
  const index = messages.findIndex((m) => m.id === message.id);
  if (index < 0) return messages;
  const merged = mergeChatMessage(messages[index]!, message);
  if (merged === messages[index]) return messages;
  return messages.map((m, i) => (i === index ? merged : m));
}

function upsertIntoList(messages: ChatMessage[], message: ChatMessage): ChatMessage[] {
  const merged = mergeIntoList(messages, message);
  if (merged !== messages) return merged;
  return messages.some((m) => m.id === message.id) ? messages : [...messages, message];
}

function upsertIntoPages(
  data: InfiniteData<ChatMessagesPage>,
  message: ChatMessage,
): InfiniteData<ChatMessagesPage> {
  const seen = data.pages.some((page) => page.messages.some((m) => m.id === message.id));
  if (seen) {
    // Merge in place, wherever the row already lives. Appending here instead
    // would drop a copy into every OTHER page.
    let changed = false;
    const pages = data.pages.map((page) => {
      const next = mergeIntoList(page.messages, message);
      if (next === page.messages) return page;
      changed = true;
      return { ...page, messages: next };
    });
    return changed ? { ...data, pages } : data;
  }
  // Page 0 is the newest window (later pages are older cursor pages), so a new
  // message belongs at the end of it.
  return {
    ...data,
    pages: data.pages.map((page, index) =>
      index === 0 ? { ...page, messages: [...page.messages, message] } : page,
    ),
  };
}

function seedPage(message: ChatMessage): InfiniteData<ChatMessagesPage> {
  return {
    pages: [{ messages: [message], limit: 50, has_more: false, next_cursor: null }],
    pageParams: [null],
  };
}

export interface UpsertChatMessageOptions {
  /**
   * Create the cache entry when the session has never been fetched.
   *
   * Send paths pass `true`: a brand-new session must render its first message
   * before any fetch exists, and the send's own invalidate reconciles the
   * placeholder page immediately after.
   *
   * WebSocket handlers pass `false` (the default). An event for a session this
   * client has not opened must not fabricate a one-message history that a later
   * reader would mistake for the whole transcript — the first real fetch owns
   * that.
   */
  seedIfMissing?: boolean;
}

/**
 * Upsert one settled message into both message caches.
 *
 * Both are written because both are read: `chatKeys.messagesPage` backs the
 * chat surfaces, `chatKeys.messages` backs the Agent Builder. Writing one and
 * not the other is how they drift.
 */
export function upsertChatMessageToCaches(
  qc: QueryClient,
  sessionId: string,
  message: ChatMessage,
  { seedIfMissing = false }: UpsertChatMessageOptions = {},
): void {
  qc.setQueryData<ChatMessage[] | undefined>(chatKeys.messages(sessionId), (old) => {
    if (!old) return seedIfMissing ? [message] : old;
    return upsertIntoList(old, message);
  });
  qc.setQueryData<InfiniteData<ChatMessagesPage> | undefined>(
    chatKeys.messagesPage(sessionId),
    (old) => {
      if (!old?.pages.length) return seedIfMissing ? seedPage(message) : old;
      return upsertIntoPages(old, message);
    },
  );
}
