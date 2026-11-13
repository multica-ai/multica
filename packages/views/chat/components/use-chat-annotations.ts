"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import { useInfiniteQuery, type InfiniteData, type UseInfiniteQueryResult } from "@tanstack/react-query";
import { toast } from "sonner";
import { chatMessagesPageOptions } from "@multica/core/chat/queries";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { ChatMessage, ChatMessagesPage } from "@multica/core/types";
import { useCommentAnnotations } from "../../issues/components/use-comment-annotations";
import { useT } from "../../i18n";

/** Persisted assistant answers are the only annotatable chat text. */
export function isAnnotatableChatMessage(message: ChatMessage): boolean {
  return message.role === "assistant" && !message.failure_reason &&
    message.message_kind !== "no_response" && !!message.content.trim();
}

/**
 * Registered by the message list: releases the bottom follow and scrolls the
 * message's row into Virtuoso's window. False when the message is not listed.
 */
type RevealChatMessage = (messageId: string) => boolean;

const REVEAL_RETRY_FRAMES = 30;

type MessagesQuery = UseInfiniteQueryResult<InfiniteData<ChatMessagesPage>>;

function hasMessage(pages: readonly ChatMessagesPage[] | undefined, messageId: string): boolean {
  return !!pages?.some((page) => page.messages.some((message) => message.id === messageId));
}

/** Page older history in until the message is loaded or history runs out. */
async function loadOlderUntil(query: MessagesQuery, messageId: string): Promise<boolean> {
  let result = query;
  while (!hasMessage(result.data?.pages, messageId) && result.hasNextPage) {
    // Join a page load the list already started rather than cancelling it.
    result = await result.fetchNextPage({ cancelRefetch: false });
    if (result.isFetchNextPageError) return false;
  }
  return hasMessage(result.data?.pages, messageId);
}

export type ChatAnnotations = ReturnType<typeof useChatAnnotations>;

/** Issue reply annotations, sourced from one chat session's assistant replies. */
export function useChatAnnotations({ sessionId, agentName, enabled }: {
  sessionId: string | null;
  agentName: string | undefined;
  enabled: boolean;
}) {
  const { t } = useT("issues");
  const query = useInfiniteQuery(chatMessagesPageOptions(sessionId ?? ""));
  const queryRef = useRef(query);
  queryRef.current = query;
  const pages = sessionId ? query.data?.pages : undefined;
  const sources = useMemo(
    () => (pages ?? []).flatMap((page) => page.messages)
      .filter(isAnnotatableChatMessage)
      .map((message) => ({ id: message.id, name: agentName ?? "" })),
    [pages, agentName],
  );
  const draftKey: CommentDraftKey = `chat:${sessionId ?? ""}`;
  // The floating chat window is z-50; markers default below it.
  const annotations = useCommentAnnotations({ draftKey, sources, enabled: enabled && !!sessionId, markerZIndex: 50 });
  const revealSourceRef = useRef<RevealChatMessage | null>(null);
  const editRef = useRef(annotations.editAnnotation);
  editRef.current = annotations.editAnnotation;
  const retryFrame = useRef<number | null>(null);
  // Bumped by every navigation so a slow page load cannot open a stale note.
  const attemptRef = useRef(0);
  const cancelRetry = useCallback(() => {
    attemptRef.current += 1;
    if (retryFrame.current !== null) cancelAnimationFrame(retryFrame.current);
    retryFrame.current = null;
  }, []);
  useEffect(() => cancelRetry, [draftKey, cancelRetry]);

  // Rows mount a frame or more after their data lands, and virtualized rows
  // leave the DOM once scrolled away: retry until the source can anchor.
  const openWhenMounted = useCallback((id: string, messageId: string, revealed: boolean) => {
    const attempt = attemptRef.current;
    let frames = 0;
    const retry = () => {
      retryFrame.current = null;
      if (attempt !== attemptRef.current) return;
      if (!revealed) revealed = !!revealSourceRef.current?.(messageId);
      if (revealed && editRef.current(id, true)) return;
      if (++frames >= REVEAL_RETRY_FRAMES) {
        toast.error(t(($) => $.reply.annotations.source_changed));
        return;
      }
      retryFrame.current = requestAnimationFrame(retry);
    };
    retryFrame.current = requestAnimationFrame(retry);
  }, [t]);

  const editAnnotation = useCallback((id: string, scrollToSource = false): boolean => {
    cancelRetry();
    const annotation = useCommentDraftStore.getState().getAnnotations(draftKey).find((a) => a.id === id);
    if (!annotation) return false;
    if (!scrollToSource) return editRef.current(id);
    const messageId = annotation.sourceCommentId;
    // A restored draft can quote a reply older than the pages loaded so far.
    if (!hasMessage(queryRef.current.data?.pages, messageId)) {
      if (!queryRef.current.hasNextPage) return editRef.current(id, true);
      const attempt = attemptRef.current;
      void loadOlderUntil(queryRef.current, messageId).then((found) => {
        if (attempt !== attemptRef.current) return;
        if (found) openWhenMounted(id, messageId, false);
        else toast.error(t(($) => $.reply.annotations.source_changed));
      });
      return true;
    }
    if (!revealSourceRef.current?.(messageId)) return editRef.current(id, true);
    if (editRef.current(id, true)) return true;
    openWhenMounted(id, messageId, true);
    return true;
  }, [draftKey, t, cancelRetry, openWhenMounted]);

  return { ...annotations, draftKey, editAnnotation, revealSourceRef };
}
