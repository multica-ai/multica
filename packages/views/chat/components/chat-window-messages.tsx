"use client";

import { useEffect, type ReactNode } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { motion, useIsPresent } from "motion/react";
import type { AgentAvailability } from "@multica/core/agents";
import type { ChatPendingTask, ChatQuickAction } from "@multica/core/types";
import { chatMessagesPageOptions } from "@multica/core/chat/queries";
import { hideQueuedChatMessages } from "@multica/core/chat/pending";
import { useRegenerateChatQuickActions } from "@multica/core/chat/mutations";
import { ChatMessageList, ChatMessageSkeleton } from "./chat-message-list";

const CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX = 1_000_000;

/** Message-only observers leave with the fading list; the composer stays alive. */
export function ChatWindowMessages({
  sessionId,
  pendingTask,
  pendingTaskLoading,
  availability,
  onQuickAction,
  quickActionsDisabled,
  quickActionsPendingMessageId,
  onReady,
  children,
}: {
  sessionId: string | null;
  pendingTask: ChatPendingTask | null | undefined;
  pendingTaskLoading: boolean;
  availability: AgentAvailability | undefined;
  onQuickAction: (action: ChatQuickAction) => void | Promise<unknown>;
  quickActionsDisabled: boolean;
  quickActionsPendingMessageId: string | null;
  /** This area has something to show — the window can fade in around it. */
  onReady?: () => void;
  children: ReactNode;
}) {
  const isPresent = useIsPresent();
  const regenerateQuickActions = useRegenerateChatQuickActions();
  const {
    data,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery(chatMessagesPageOptions(sessionId ?? ""));
  const pages = sessionId ? data?.pages ?? [] : [];
  const messages = hideQueuedChatMessages(
    [...pages].reverse().flatMap((page) => page.messages),
    pendingTask ?? undefined,
  );
  // Older pages prepend rows without changing the newest window's indices.
  const olderMessageCount = pages.slice(1).reduce(
    (count, page) => count + page.messages.length,
    0,
  );
  const showSkeleton = !!sessionId && (isLoading || pendingTaskLoading);
  const showList = !showSkeleton && (messages.length > 0 || !!pendingTask?.task_id);

  // The skeleton and the empty state are right on their first frame; the list
  // is hidden until it lands, so it reports through `onRevealed` instead.
  useEffect(() => {
    if (isPresent && !showList) onReady?.();
  }, [isPresent, showList, onReady]);

  return (
    <motion.div
      className="flex flex-1 min-h-0 flex-col"
      initial={false}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.15 }}
    >
      {showSkeleton ? (
        <ChatMessageSkeleton />
      ) : showList ? (
        <ChatMessageList
          key={sessionId}
          active={isPresent}
          revealKey={sessionId}
          onRevealed={onReady}
          messages={messages}
          pendingTask={pendingTask}
          availability={availability}
          firstItemIndex={messages.length > 0
            ? CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX - olderMessageCount
            : 0}
          hasOlderMessages={!!hasNextPage}
          isFetchingOlderMessages={isFetchingNextPage}
          onLoadOlderMessages={() => void fetchNextPage()}
          onQuickAction={onQuickAction}
          quickActionsDisabled={quickActionsDisabled}
          onRegenerateQuickActions={(message) => sessionId
            ? regenerateQuickActions.mutateAsync({ sessionId, messageId: message.id })
            : undefined}
          quickActionsPendingMessageId={quickActionsPendingMessageId}
        />
      ) : children}
    </motion.div>
  );
}
