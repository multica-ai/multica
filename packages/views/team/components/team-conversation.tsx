"use client";

import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { EyeOff } from "@multica/ui/icons";
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
import { TimelineDaySeparator } from "../../chat/components/chat-message-list";
import { useT } from "../../i18n";
import { MessageHoverActions, ThreadSummaryLink } from "./team-message-extras";

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
  openThreadId,
  onReplyInThread,
  header,
  composerHint,
  privateHint,
}: {
  conversationId: string | null;
  ensureConversation?: () => Promise<string>;
  placeholder: string;
  empty: ReactNode;
  readOnly?: boolean;
  readOnlyNotice?: ReactNode;
  /** Link to a message's thread; omitted inside a thread. */
  threadHref?: (threadId: string) => string;
  openThreadId?: string | null;
  onReplyInThread?: (message: ConversationMessage) => void;
  header?: ReactNode;
  /** One line above the composer, e.g. who replies here. */
  composerHint?: ReactNode;
  /** A note only the sender sees after a send, e.g. that nobody was asked. */
  privateHint?: (content: string) => string | null;
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
  const [note, setNote] = useState<string | null>(null);

  // Where the reader left off when they opened it: the "New" line sits there.
  const readMark = useRef<number | null>(null);
  if (readMark.current === null && detail) readMark.current = detail.is_member ? detail.last_read_seq : 0;
  const firstUnreadId = useMemo(() => {
    const mark = readMark.current;
    if (!mark) return null;
    return messages.find((m) => m.seq > mark && m.seq !== OPTIMISTIC_SEQ && !(m.author_type === "member" && m.author_id === userId))?.id ?? null;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the mark is fixed once read
  }, [messages, userId, detail?.id]);

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
  }, [lastMessageId, detail?.working_agents?.length, note]);

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
      setNote(privateHint?.(content) ?? null);
      return true;
    } catch {
      toast.error(t(($) => $.conversation.send_failed));
      return false;
    }
  };

  const working = detail?.working_agents ?? [];
  const loading = !!conversationId && messagesQuery.isLoading;

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
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
          <div className="mx-auto flex w-full max-w-4xl flex-col px-5 py-3 @2xl:px-8">
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
              <div className="py-10 text-center text-caption text-muted-foreground">{empty}</div>
            ) : (
              messages.map((message, index) => {
                const prev = messages[index - 1];
                const newDay = !prev || new Date(prev.created_at).toDateString() !== new Date(message.created_at).toDateString();
                const isFirstUnread = message.id === firstUnreadId;
                return (
                  <Fragment key={message.id}>
                    {newDay && <TimelineDaySeparator at={message.created_at} />}
                    {isFirstUnread && <NewMessagesLine />}
                    <ConversationMessageRow
                      message={message}
                      grouped={!newDay && !isFirstUnread && isGrouped(prev, message)}
                      threadHref={threadHref && message.thread ? threadHref(message.thread.id) : null}
                      threadActive={!!openThreadId && message.thread?.id === openThreadId}
                      onReplyInThread={
                        onReplyInThread && !readOnly && message.seq !== OPTIMISTIC_SEQ
                          ? () => onReplyInThread(message)
                          : undefined
                      }
                    />
                  </Fragment>
                );
              })
            )}
            {messages.length === 0 && header && (
              <div className="py-4 text-caption text-muted-foreground">{empty}</div>
            )}
            {note && (
              <div className="flex items-center gap-2 py-2 pl-11 text-caption text-muted-foreground">
                <EyeOff className="size-3.5 shrink-0" />
                <span>{note}</span>
              </div>
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
            <div className="mx-auto max-w-4xl rounded-md bg-muted px-3 py-2.5 text-caption text-muted-foreground">{readOnlyNotice}</div>
          </div>
        )
      ) : (
        <ConversationComposer
          key={conversationId ?? "new"}
          placeholder={placeholder}
          onSend={handleSend}
          hint={composerHint}
        />
      )}
    </div>
  );
}

function NewMessagesLine() {
  const { t } = useT("team");
  return (
    <div className="flex items-center gap-2 py-2 text-micro font-medium text-destructive" role="separator">
      <span className="h-px flex-1 bg-destructive/40" />
      {t(($) => $.conversation.new_messages)}
    </div>
  );
}

function isGrouped(prev: ConversationMessage | undefined, message: ConversationMessage): boolean {
  if (!prev || prev.kind !== "message" || message.kind !== "message") return false;
  if (prev.author_type !== message.author_type || prev.author_id !== message.author_id) return false;
  if (prev.thread) return false;
  return new Date(message.created_at).getTime() - new Date(prev.created_at).getTime() < GROUP_WINDOW_MS;
}

function formatMessageTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

export function ConversationMessageRow({
  message,
  grouped = false,
  threadHref,
  threadActive = false,
  onReplyInThread,
}: {
  message: ConversationMessage;
  grouped?: boolean;
  threadHref?: string | null;
  threadActive?: boolean;
  onReplyInThread?: () => void;
}) {
  const { getActorName } = useActorName();

  if (message.kind === "event" || message.author_type === "system") {
    return (
      <div className="py-1.5 pl-11 text-caption text-muted-foreground">{message.content}</div>
    );
  }

  const authorType = message.author_type === "agent" ? "agent" : "member";
  const authorId = message.author_id ?? "";
  const pending = message.seq === OPTIMISTIC_SEQ;
  const replies = message.thread?.reply_count ?? 0;

  return (
    <div
      className={cn(
        "group/message relative -mx-2 flex gap-3 rounded-md px-2 hover:bg-surface-hover/60",
        grouped ? "py-0.5" : "pt-2 pb-0.5",
        pending && "opacity-60",
      )}
    >
      <div className="w-8 shrink-0">
        {!grouped && (
          <ActorAvatar
            actorType={authorType}
            actorId={authorId}
            size="md"
            showStatusDot={authorType === "agent"}
            enableHoverCard
          />
        )}
      </div>
      <div className="min-w-0 flex-1">
        {!grouped && (
          <div className="flex items-baseline gap-2">
            <span className="truncate text-body font-medium">{getActorName(authorType, authorId)}</span>
            <time className="shrink-0 text-micro text-muted-foreground" dateTime={message.created_at}>
              {formatMessageTime(message.created_at)}
            </time>
          </div>
        )}
        <div className="text-body">
          <RichContent content={message.content} density="compact" />
        </div>
        {threadHref && replies > 0 && (
          <ThreadSummaryLink
            href={threadHref}
            count={replies}
            lastReplyAt={message.thread?.last_reply_at}
            active={threadActive}
          />
        )}
      </div>
      <MessageHoverActions onReply={onReplyInThread} />
    </div>
  );
}

function WorkingAgentRow({ agentId }: { agentId: string }) {
  const { t } = useT("team");
  const { getActorName } = useActorName();
  return (
    <div className="flex items-center gap-3 py-2 text-caption text-muted-foreground">
      <div className="flex w-8 shrink-0 justify-center">
        <ActorAvatar actorType="agent" actorId={agentId} size="sm" />
      </div>
      <span className="animate-pulse">{t(($) => $.conversation.replying, { name: getActorName("agent", agentId) })}</span>
    </div>
  );
}

function ConversationSkeleton() {
  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-4 px-5 py-4">
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

export function ConversationComposer({
  placeholder,
  onSend,
  hint,
  disabled = false,
}: {
  placeholder: string;
  onSend: (content: string) => Promise<boolean>;
  hint?: ReactNode;
  disabled?: boolean;
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
      <div className="mx-auto max-w-4xl">
        {hint && <div className="flex items-center gap-1.5 px-1 pb-1.5 text-caption text-muted-foreground">{hint}</div>}
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
            <SubmitButton onClick={submit} disabled={isEmpty || disabled} loading={submitting} ariaLabel={placeholder} />
          </div>
        </div>
      </div>
    </div>
  );
}
