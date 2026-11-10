"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { chatMessagesPageOptions } from "@multica/core/chat/queries";
import { useCommentDraftStore, type CommentDraftKey } from "@multica/core/issues/stores";
import type { ChatMessage } from "@multica/core/types";
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

export type ChatAnnotations = ReturnType<typeof useChatAnnotations>;

/** Issue reply annotations, sourced from one chat session's assistant replies. */
export function useChatAnnotations({ sessionId, agentName, enabled }: {
  sessionId: string | null;
  agentName: string | undefined;
  enabled: boolean;
}) {
  const { t } = useT("issues");
  const { data } = useInfiniteQuery(chatMessagesPageOptions(sessionId ?? ""));
  const pages = sessionId ? data?.pages : undefined;
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
  const cancelRetry = useCallback(() => {
    if (retryFrame.current !== null) cancelAnimationFrame(retryFrame.current);
    retryFrame.current = null;
  }, []);
  useEffect(() => cancelRetry, [draftKey, cancelRetry]);

  // Virtualized rows leave the DOM once scrolled away, so a saved quote's
  // source may need to be scrolled back into the list before it can anchor.
  const editAnnotation = useCallback((id: string, scrollToSource = false): boolean => {
    cancelRetry();
    const annotation = useCommentDraftStore.getState().getAnnotations(draftKey).find((a) => a.id === id);
    if (!annotation) return false;
    if (!scrollToSource) return editRef.current(id);
    if (!revealSourceRef.current?.(annotation.sourceCommentId)) return editRef.current(id, true);
    if (editRef.current(id, true)) return true;
    let frames = 0;
    const retry = () => {
      retryFrame.current = null;
      if (editRef.current(id, true)) return;
      if (++frames >= REVEAL_RETRY_FRAMES) {
        toast.error(t(($) => $.reply.annotations.source_changed));
        return;
      }
      retryFrame.current = requestAnimationFrame(retry);
    };
    retryFrame.current = requestAnimationFrame(retry);
    return true;
  }, [draftKey, t, cancelRetry]);

  return { ...annotations, draftKey, editAnnotation, revealSourceRef };
}
