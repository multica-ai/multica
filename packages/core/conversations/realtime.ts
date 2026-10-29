import type { QueryClient } from "@tanstack/react-query";
import type { ConversationEventPayload } from "../types";
import { conversationKeys, upsertConversationMessage } from "./queries";

/**
 * Apply a conversation realtime event to the caches. A new message lands in
 * the loaded timeline directly; everything derived from it (unread counts,
 * who is replying, a parent's thread summary) is refetched.
 */
export function applyConversationEvent(
  qc: QueryClient,
  wsId: string,
  type: "conversation:message" | "conversation:updated",
  payload: ConversationEventPayload,
) {
  if (!payload?.conversation_id) return;
  if (type === "conversation:message" && payload.message) {
    upsertConversationMessage(qc, wsId, payload.message);
  }
  qc.invalidateQueries({ queryKey: conversationKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: conversationKeys.detail(wsId, payload.conversation_id) });
  if (payload.parent_conversation_id) {
    qc.invalidateQueries({ queryKey: conversationKeys.messages(wsId, payload.parent_conversation_id) });
  }
}
