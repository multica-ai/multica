"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { MessagesSquare } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  conversationMessagesOptions,
  conversationOptions,
  flattenConversationMessages,
  newClientMessageId,
  OPTIMISTIC_SEQ,
  useMarkConversationRead,
  useSendConversationMessage,
} from "@multica/core/conversations";
import type { ConversationMessage } from "@multica/core/types";
import { ContentEditor, type ContentEditorRef, useComposerSubmit, useUploadGate } from "../../editor";
import { RichContent } from "../../rich-content";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

// Consecutive messages from one author within this window read as one block.
const GROUP_WINDOW_MS = 5 * 60 * 1000;

/**
 * One Team conversation — a channel, a direct conversation or a thread:
 * its timeline, who is replying right now, and the composer.
 *
 * `conversationId` may be null for a direct conversation nobody has written
 * in yet; the first send creates it through `ensureConversation`.
 */
export function TeamConversation({
  conversationId,
  ensureConversation,
  placeholder,
  empty,
  readOnly = false,
  readOnlyNotice,
  threadHref,
  onReplyInThread,
  header,
}: {
  conversationId: string | null;
  ensureConversation?: () => Promise<string>;
  placeholder: string;
  empty: ReactNode;
  readOnly?: boolean;
  readOnlyNotice?: ReactNode;
  /** Link to a message's thread; omitted inside a thread. */
  threadHref?: (threadId: string) => string;
  onReplyInThread?: (message: ConversationMessage) => void;
  header?: ReactNode;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const id = conversationId ?? "";
  const messagesQuery = useInfiniteQuery({
    ...conversationMessagesOptions(wsId, id),
    enabled: !!conversationId,
  });
  const { data: detail } = useQuery({ ...conversationOptions(wsId, id), enabled: !!conversationId });
  const messages = useMemo(() => flattenConversationMessages(messagesQuery.data), [messagesQuery.data]);
  const send = useSendConversationMessage();
  const markRead = useMarkConversationRead();

  const lastSeq = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i]!.seq !== OPTIMISTIC_SEQ) return messages[i]!.seq;
    }
    return 0;
  }, [messages]);
  const { mutate: markReadMutate } = markRead;
  useEffect(() => {
    if (!conversationId || !detail?.is_member || lastSeq <= detail.last_read_seq) return;
    if (typeof document !== "undefined" && document.visibilityState !== "visible") return;
    markReadMutate({ conversationId, seq: lastSeq });
  }, [conversationId, detail?.is_member, detail?.last_read_seq, lastSeq, markReadMutate]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);
  const lastMessageId = messages[messages.length - 1]?.id;
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && stickToBottom.current) el.scrollTop = el.scrollHeight;
  }, [lastMessageId, detail?.working_agents?.length]);

  const handleSend = async (content: string) => {
    try {
      const target = conversationId ?? (ensureConversation ? await ensureConversation() : null);
      if (!target) return false;
      stickToBottom.current = true;
      await send.mutateAsync({
        conversationId: target,
        content,
        clientMessageId: newClientMessageId(),
        authorId: userId,
      });
      return true;
    } catch {
      toast.error(t(($) => $.conversation.send_failed));
      return false;
    }
  };

  const working = detail?.working_agents ?? [];
  const loading = !!conversationId && messagesQuery.isLoading;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div
        ref={scrollRef}
        onScroll={(e) => {
          const el = e.currentTarget;
          stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        }}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        {header}
        {loading ? (
          <ConversationSkeleton />
        ) : (
          <div className="flex flex-col py-3">
            {messagesQuery.hasNextPage && (
              <div className="flex justify-center pb-2">
                <Button
                  size="xs"
                  variant="ghost"
                  disabled={messagesQuery.isFetchingNextPage}
                  onClick={() => {
                    stickToBottom.current = false;
                    void messagesQuery.fetchNextPage();
                  }}
                >
                  {t(($) => $.conversation.load_older)}
                </Button>
              </div>
            )}
            {messages.length === 0 && !header ? (
              <div className="px-5 py-10 text-center text-caption text-muted-foreground">{empty}</div>
            ) : (
              messages.map((message, index) => (
                <ConversationMessageRow
                  key={message.id}
                  message={message}
                  grouped={isGrouped(messages[index - 1], message)}
                  threadHref={threadHref && message.thread ? threadHref(message.thread.id) : null}
                  onReplyInThread={
                    onReplyInThread && !readOnly && message.seq !== OPTIMISTIC_SEQ
                      ? () => onReplyInThread(message)
                      : undefined
                  }
                />
              ))
            )}
            {messages.length === 0 && header && (
              <div className="px-5 py-4 text-caption text-muted-foreground">{empty}</div>
            )}
            {working.map((w) => (
              <WorkingAgentRow key={w.task_id} agentId={w.agent_id} />
            ))}
          </div>
        )}
      </div>
      {readOnly ? (
        readOnlyNotice && (
          <div className="px-4 pb-4">
            <div className="rounded-md bg-muted px-3 py-2.5 text-caption text-muted-foreground">{readOnlyNotice}</div>
          </div>
        )
      ) : (
        <ConversationComposer key={conversationId ?? "new"} placeholder={placeholder} onSend={handleSend} />
      )}
    </div>
  );
}

function isGrouped(prev: ConversationMessage | undefined, message: ConversationMessage): boolean {
  if (!prev || prev.kind !== "message" || message.kind !== "message") return false;
  if (prev.author_type !== message.author_type || prev.author_id !== message.author_id) return false;
  return new Date(message.created_at).getTime() - new Date(prev.created_at).getTime() < GROUP_WINDOW_MS;
}

function formatMessageTime(iso: string): string {
  const d = new Date(iso);
  const sameDay = d.toDateString() === new Date().toDateString();
  return sameDay
    ? d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
    : d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function ConversationMessageRow({
  message,
  grouped = false,
  threadHref,
  onReplyInThread,
}: {
  message: ConversationMessage;
  grouped?: boolean;
  threadHref?: string | null;
  onReplyInThread?: () => void;
}) {
  const { t } = useT("team");
  const { getActorName } = useActorName();

  if (message.kind === "event" || message.author_type === "system") {
    return (
      <div className="px-5 py-1.5 text-center text-caption text-muted-foreground">{message.content}</div>
    );
  }

  const authorType = message.author_type === "agent" ? "agent" : "member";
  const authorId = message.author_id ?? "";
  const pending = message.seq === OPTIMISTIC_SEQ;
  const replies = message.thread?.reply_count ?? 0;

  return (
    <div
      className={cn(
        "group/message relative flex gap-3 px-5 hover:bg-surface-hover/60",
        grouped ? "py-0.5" : "pt-2 pb-0.5",
        pending && "opacity-60",
      )}
    >
      <div className="w-8 shrink-0">
        {!grouped && <ActorAvatar actorType={authorType} actorId={authorId} size="md" enableHoverCard />}
      </div>
      <div className="min-w-0 flex-1">
        {!grouped && (
          <div className="flex items-baseline gap-2">
            <span className="truncate text-body font-medium">{getActorName(authorType, authorId)}</span>
            {authorType === "agent" && (
              <span className="rounded bg-muted px-1 text-micro text-muted-foreground">
                {t(($) => $.conversation.agent_badge)}
              </span>
            )}
            <time className="shrink-0 text-micro text-muted-foreground" dateTime={message.created_at}>
              {formatMessageTime(message.created_at)}
            </time>
          </div>
        )}
        <div className="text-body">
          <RichContent content={message.content} density="compact" />
        </div>
        {threadHref && replies > 0 && (
          <AppLink
            href={threadHref}
            className="mt-1 inline-flex h-6 items-center gap-1.5 rounded-md border bg-background px-2 text-caption text-brand transition-colors hover:bg-accent"
          >
            <MessagesSquare className="size-3.5" />
            {t(($) => $.chat.thread.replies, { count: replies })}
          </AppLink>
        )}
      </div>
      {onReplyInThread && !(threadHref && replies > 0) && (
        <div className="absolute right-4 -top-3 hidden rounded-md border bg-background shadow-sm group-hover/message:flex focus-within:flex">
          <Button
            size="xs"
            variant="ghost"
            onClick={onReplyInThread}
            aria-label={t(($) => $.chat.thread.reply)}
            title={t(($) => $.chat.thread.reply)}
          >
            <MessagesSquare className="size-3.5" />
            {t(($) => $.chat.thread.reply)}
          </Button>
        </div>
      )}
    </div>
  );
}

function WorkingAgentRow({ agentId }: { agentId: string }) {
  const { t } = useT("team");
  const { getActorName } = useActorName();
  return (
    <div className="flex items-center gap-3 px-5 py-2 text-caption text-muted-foreground">
      <div className="flex w-8 shrink-0 justify-center">
        <ActorAvatar actorType="agent" actorId={agentId} size="sm" />
      </div>
      <span className="animate-pulse">{t(($) => $.conversation.replying, { name: getActorName("agent", agentId) })}</span>
    </div>
  );
}

function ConversationSkeleton() {
  return (
    <div className="flex flex-col gap-4 px-5 py-4">
      {[0, 1, 2].map((i) => (
        <div key={i} className="flex gap-3">
          <Skeleton className="size-8 rounded-full" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-3 w-32" />
            <Skeleton className="h-3 w-3/4" />
          </div>
        </div>
      ))}
    </div>
  );
}

function ConversationComposer({
  placeholder,
  onSend,
}: {
  placeholder: string;
  onSend: (content: string) => Promise<boolean>;
}) {
  const editorRef = useRef<ContentEditorRef>(null);
  const uploadGate = useUploadGate(editorRef);
  const [isEmpty, setIsEmpty] = useState(true);
  const { submitting, submit } = useComposerSubmit({
    editorRef,
    uploadGate,
    onSubmit: onSend,
    onAccepted: () => {
      editorRef.current?.clearContent();
      setIsEmpty(true);
    },
    afterAccepted: "refocus",
  });

  return (
    <div className="px-4 pb-4">
      <div className="relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border focus-within:ring-ring">
        <div
          className={cn("max-h-[40vh] min-h-0 overflow-y-auto px-3 py-2", submitting && "pointer-events-none opacity-60")}
          aria-busy={submitting || undefined}
        >
          <ContentEditor
            ref={editorRef}
            defaultValue=""
            placeholder={placeholder}
            onUpdate={(md) => setIsEmpty(!md.trim())}
            onSubmit={submit}
            debounceMs={100}
            showBubbleMenu={false}
          />
        </div>
        <div className="absolute bottom-1 right-1.5">
          <SubmitButton onClick={submit} disabled={isEmpty} loading={submitting} ariaLabel={placeholder} />
        </div>
      </div>
    </div>
  );
}
