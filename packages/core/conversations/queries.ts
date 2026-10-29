import { infiniteQueryOptions, queryOptions, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { Conversation, ConversationMessage, ConversationMessagesPage } from "../types";

export const CONVERSATION_PAGE_SIZE = 50;

export const conversationKeys = {
  all: (wsId: string) => ["conversations", wsId] as const,
  list: (wsId: string) => [...conversationKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...conversationKeys.all(wsId), "detail", id] as const,
  messages: (wsId: string, id: string) => [...conversationKeys.all(wsId), "messages", id] as const,
};

/** Channels and direct conversations the viewer can see, with unread counts. */
export function conversationListOptions(wsId: string) {
  return queryOptions({
    queryKey: conversationKeys.list(wsId),
    queryFn: () => api.listConversations(),
    staleTime: 30_000,
  });
}

export function conversationOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: conversationKeys.detail(wsId, id),
    queryFn: () => api.getConversation(id),
    staleTime: 30_000,
  });
}

/**
 * Messages, newest page first; each page is oldest-first. Older pages are
 * fetched by `seq` cursor, so a message arriving meanwhile never shifts them.
 */
export function conversationMessagesOptions(wsId: string, id: string) {
  return infiniteQueryOptions({
    queryKey: conversationKeys.messages(wsId, id),
    queryFn: ({ pageParam }) =>
      api.listConversationMessages(id, {
        before_seq: pageParam ?? undefined,
        limit: CONVERSATION_PAGE_SIZE,
      }),
    initialPageParam: null as number | null,
    getNextPageParam: (last: ConversationMessagesPage) =>
      last.has_more && last.messages.length > 0 ? last.messages[0]!.seq : undefined,
    staleTime: Infinity,
  });
}

/** All loaded messages, oldest first. */
export function flattenConversationMessages(
  data: InfiniteData<ConversationMessagesPage> | undefined,
): ConversationMessage[] {
  if (!data) return [];
  const out: ConversationMessage[] = [];
  for (let i = data.pages.length - 1; i >= 0; i--) out.push(...data.pages[i]!.messages);
  return out;
}

/** An optimistic message sorts after every real one until the server answers. */
export const OPTIMISTIC_SEQ = Number.MAX_SAFE_INTEGER;

/**
 * Put a message into the newest loaded page, replacing the copy it supersedes
 * (same id, or the optimistic copy with the same client id). A cache that was
 * never loaded is left alone; the first fetch will include the message.
 */
export function upsertConversationMessage(
  qc: QueryClient,
  wsId: string,
  message: ConversationMessage,
) {
  qc.setQueryData<InfiniteData<ConversationMessagesPage>>(
    conversationKeys.messages(wsId, message.conversation_id),
    (data) => {
      if (!data || data.pages.length === 0) return data;
      const supersedes = (m: ConversationMessage) =>
        m.id === message.id ||
        (!!message.client_message_id &&
          m.client_message_id === message.client_message_id &&
          m.author_id === message.author_id);
      const previous = data.pages.flatMap((p) => p.messages).find(supersedes);
      const pages = data.pages.map((page, index) => {
        const kept = page.messages.filter((m) => !supersedes(m));
        if (index !== 0) return kept.length === page.messages.length ? page : { ...page, messages: kept };
        const next = [...kept, { ...message, thread: message.thread ?? previous?.thread }];
        next.sort((a, b) => a.seq - b.seq);
        return { ...page, messages: next };
      });
      return { ...data, pages };
    },
  );
}

export function removeConversationMessage(
  qc: QueryClient,
  wsId: string,
  conversationId: string,
  messageId: string,
) {
  qc.setQueryData<InfiniteData<ConversationMessagesPage>>(
    conversationKeys.messages(wsId, conversationId),
    (data) =>
      data && {
        ...data,
        pages: data.pages.map((page) => ({
          ...page,
          messages: page.messages.filter((m) => m.id !== messageId),
        })),
      },
  );
}

/** Reflect that the viewer has read up to `seq`, without a refetch. */
export function markConversationReadInCache(
  qc: QueryClient,
  wsId: string,
  conversationId: string,
  seq: number,
) {
  const apply = (c: Conversation): Conversation =>
    c.id !== conversationId || c.last_read_seq >= seq
      ? c
      : { ...c, last_read_seq: seq, unread_count: Math.max(c.last_message_seq - seq, 0) };
  qc.setQueryData<Conversation[]>(conversationKeys.list(wsId), (list) => list?.map(apply));
  qc.setQueryData<Conversation>(conversationKeys.detail(wsId, conversationId), (c) => c && apply(c));
}

/** The direct conversation with another member, if one exists yet. */
export function findDirectConversation(
  conversations: readonly Conversation[],
  viewerId: string,
  otherId: string,
): Conversation | null {
  return (
    conversations.find((c) => {
      if (c.kind !== "direct") return false;
      const ids = c.members.filter((m) => m.member_type === "member").map((m) => m.member_id);
      if (viewerId === otherId) return ids.length === 1 && ids[0] === viewerId;
      return ids.length === 2 && ids.includes(viewerId) && ids.includes(otherId);
    }) ?? null
  );
}

/** The other person in a direct conversation (the viewer, for notes to self). */
export function directCounterpart(conversation: Conversation, viewerId: string): string | null {
  const ids = conversation.members.filter((m) => m.member_type === "member").map((m) => m.member_id);
  return ids.find((id) => id !== viewerId) ?? ids[0] ?? null;
}
