"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { MessageCircle } from "lucide-react";
import {
  chatMessagesOptions,
  pendingChatTaskOptions,
} from "@multica/core/chat/queries";
import {
  useSendChatMessage,
  useMarkChatSessionRead,
} from "@multica/core/chat/mutations";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { useTimeAgo } from "../components/inbox-list-item";
import {
  registerInboxItemType,
  type InboxItemDetailProps,
  type InboxItemRowProps,
} from "./contract";

/**
 * Renderer for the `conversation` kind — an agent chat session surfaced inline
 * in the inbox feed. Selecting it opens the conversation in the inbox detail
 * pane (the same surface an issue notification opens into), NOT the floating
 * chat window. The detail pane reuses the chat thread + composer so it stays
 * in sync with the chat surface that will eventually replace the floating one.
 */
function ConversationRow({ entry, isSelected, onSelect }: InboxItemRowProps) {
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo();

  if (entry.kind !== "conversation") return null;
  const session = entry.conversation;

  return (
    <button
      type="button"
      onClick={onSelect}
      className={`group flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <ActorAvatar actorType="agent" actorId={session.agent_id} size={28} enableHoverCard />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1.5">
            {entry.unread && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" />
            )}
            <span
              className={`truncate text-sm ${entry.unread ? "font-medium" : "text-muted-foreground"}`}
            >
              {session.title}
            </span>
          </div>
          <MessageCircle className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        </div>
        <div className="mt-0.5 flex items-center justify-between gap-2">
          <p className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-muted-foreground">
            {getActorName("agent", session.agent_id)}
          </p>
          <span className="shrink-0 text-xs text-muted-foreground">
            {timeAgo(session.updated_at)}
          </span>
        </div>
      </div>
    </button>
  );
}

function ConversationDetail({ entry }: InboxItemDetailProps) {
  const session = entry.kind === "conversation" ? entry.conversation : null;
  const sessionId = session?.id ?? "";
  const agentId = session?.agent_id ?? "";

  const { getActorName } = useActorName();
  const { data: messages = [] } = useQuery(chatMessagesOptions(sessionId));
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(sessionId));
  const sendMessage = useSendChatMessage(sessionId);
  const markRead = useMarkChatSessionRead();

  // Mark read when the conversation is opened in the pane (mirrors the inbox
  // notification auto-mark-read). Optimistic, so it settles in one pass.
  const markReadMutate = markRead.mutate;
  const isUnread = entry.kind === "conversation" && entry.unread;
  useEffect(() => {
    if (!sessionId || !isUnread) return;
    markReadMutate(sessionId);
  }, [sessionId, isUnread, markReadMutate]);

  if (!session) return null;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2.5 border-b px-4">
        <ActorAvatar actorType="agent" actorId={agentId} size={24} enableHoverCard />
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{session.title}</p>
          <p className="truncate text-xs text-muted-foreground">
            {getActorName("agent", agentId)}
          </p>
        </div>
      </div>
      <div className="min-h-0 flex-1">
        <ChatMessageList
          key={sessionId}
          messages={messages}
          pendingTask={pendingTask}
          availability={undefined}
        />
      </div>
      <ChatInput
        onSend={async (content, attachmentIds, commitInput) => {
          const text = content.trim();
          if (!text) return false;
          commitInput?.({ clearEditor: true });
          try {
            await sendMessage.mutateAsync({ content: text, attachmentIds });
            return true;
          } catch {
            toast.error("Failed to send message");
            return false;
          }
        }}
        isRunning={!!pendingTask?.task_id}
        agentName={getActorName("agent", agentId)}
      />
    </div>
  );
}

registerInboxItemType({
  kind: "conversation",
  Row: ConversationRow,
  Detail: ConversationDetail,
});
