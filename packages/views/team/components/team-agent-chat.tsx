"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo } from "react";
import { useInfiniteQuery, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowLeft, Hash, History, Lock, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { upsertChatMessageToCaches } from "@multica/core/chat/message-cache";
import { hideQueuedChatMessages } from "@multica/core/chat/pending";
import {
  chatKeys,
  chatMessageIssuesOptions,
  chatMessagesPageOptions,
  chatQuickActionsPendingOptions,
  chatSessionsWithThreadsOptions,
  isThreadChatSession,
  pendingChatTaskOptions,
} from "@multica/core/chat/queries";
import { pickPrimaryChatSession } from "@multica/core/chat/primary-session";
import {
  useCreateChatThread,
  useCreateIssueFromChatMessage,
  useMarkChatSessionRead,
  useRegenerateChatQuickActions,
} from "@multica/core/chat/mutations";
import { useFeatureEnabled } from "@multica/core/config";
import { TEAM_PAGE_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWSEvent } from "@multica/core/realtime";
import { useRequiredWorkspaceSlug, useWorkspacePaths } from "@multica/core/paths";
import { getCurrentSlug } from "@multica/core/platform";
import type {
  Agent,
  ChatMessage,
  ChatMessageIssue,
  ChatMessagesPage,
  ChatSession,
} from "@multica/core/types";
import { RichContent } from "../../rich-content";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ChatMessageList, ChatMessageSkeleton } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { ChatQueue } from "../../chat/components/chat-queue";
import { EmptyState } from "../../chat/components/chat-empty-state";
import { seedAcceptedPendingTask, useChatController } from "../../chat/components/use-chat-controller";
import { OfflineBanner } from "../../chat/components/offline-banner";
import { ArchivedAgentBanner } from "../../chat/components/archived-agent-banner";
import { AgentAccessRevokedBanner } from "../../chat/components/agent-access-revoked-banner";
import { RuntimeRequiredBanner } from "../../chat/components/runtime-required-banner";
import { CHAT_COLUMN, CHAT_GUTTER } from "../../chat/components/chat-column";
import { teamHref } from "../selection";
import { channelLabelKey } from "./team-agent-panel";
import { TeamMessageExtras, ThreadSummaryLink } from "./team-message-extras";
import { ConversationComposer } from "./team-conversation";

const EMPTY_SESSIONS: ChatSession[] = [];
const EMPTY_ISSUES: ChatMessageIssue[] = [];

/**
 * Which of the viewer's conversations with an agent the Chat tab shows: the
 * one long-running conversation, or — with `sessionId`, or a thread under one —
 * an earlier conversation, read-only. `thread` is the thread open beside it.
 */
export function useAgentConversation(agent: Agent, sessionId: string | null, threadId: string | null) {
  const wsId = useWorkspaceId();
  const { data: sessions = EMPTY_SESSIONS, isSuccess: loaded } = useQuery(chatSessionsWithThreadsOptions(wsId));
  const primary = pickPrimaryChatSession(sessions, agent.id);
  const thread = threadId
    ? sessions.find((s) => s.id === threadId && s.agent_id === agent.id && isThreadChatSession(s)) ?? null
    : null;
  const threadParent = thread ? sessions.find((s) => s.id === thread.parent_chat_session_id) ?? null : null;
  const requested =
    sessionId && sessionId !== primary?.id
      ? sessions.find((s) => s.id === sessionId && s.agent_id === agent.id && !isThreadChatSession(s)) ?? null
      : null;
  const earlier = requested ?? (threadParent && threadParent.id !== primary?.id ? threadParent : null);
  return {
    loaded,
    sessions,
    primary,
    thread,
    earlier,
    mainSessionId: earlier?.id ?? primary?.id ?? null,
  };
}

/**
 * The agent's Chat tab: the viewer's conversation with this agent as a named
 * timeline.
 *
 * It drives the shared chat controller (the same one behind the Chat page and
 * the floating window) by pointing the chat store at the right session, so
 * sending, streaming, queueing, stopping and read state behave exactly as they
 * do everywhere else. A thread opens beside it (`TeamAgentThreadPane`).
 */
export function TeamAgentChat({
  agent,
  sessionId,
  threadId,
}: {
  agent: Agent;
  sessionId: string | null;
  threadId: string | null;
}) {
  const { t } = useT("team");
  const { pathname } = useNavigation();
  const teamPageEnabled = useFeatureEnabled(TEAM_PAGE_FLAG, false);
  const wsPaths = useWorkspacePaths();
  const workspaceSlug = useRequiredWorkspaceSlug();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const routeActive = pathname === wsPaths.team() && getCurrentSlug() === workspaceSlug;
  const c = useChatController({ isActive: routeActive });
  const { loaded, sessions, earlier, mainSessionId } = useAgentConversation(agent, sessionId, threadId);
  const { data: quickActionsPending = null } = useQuery(
    chatQuickActionsPendingOptions(c.activeSessionId ?? ""),
  );
  const regenerateQuickActions = useRegenerateChatQuickActions();

  // Point the shared chat store at this agent's conversation. Deliberately not
  // keyed on the live activeSessionId: the first send in a brand-new chat
  // creates a session before the list refetch reports it as primary, and that
  // window must not be "corrected" back to an empty compose. A layout effect,
  // so switching agents never paints a frame bound to the previous one.
  useLayoutEffect(() => {
    if (!loaded || !routeActive) return;
    const live = useChatStore.getState();
    if (mainSessionId) {
      if (live.activeSessionId !== mainSessionId) live.setActiveSession(mainSessionId);
      if (live.selectedAgentId !== agent.id) live.setSelectedAgentId(agent.id);
      return;
    }
    if (live.selectedAgentId !== agent.id) live.setSelectedAgentId(agent.id);
    if (live.activeSessionId !== null) {
      live.setSelectedProjectId(null);
      live.setActiveSession(null);
    }
  }, [agent.id, mainSessionId, loaded, routeActive]);

  // The controller resolves its agent from the open session, or from the
  // picker list for a new chat. A new chat with an agent the viewer may not
  // invoke would otherwise fall back to some other agent; never compose then.
  const boundToAgent = c.activeAgent?.id === agent.id;
  const synced = boundToAgent && (c.activeSessionId === mainSessionId || mainSessionId === null);
  const readOnly = !!earlier;

  const extras = useTeamMessageExtras({
    wsId: c.wsId,
    agent,
    sessions,
    shownSessionId: synced ? mainSessionId : null,
    earlierSessionId: earlier?.id ?? null,
    openThreadId: threadId,
    canAct: teamPageEnabled && !readOnly && !c.isAgentArchived && !c.isAgentAccessRevoked,
  });

  if (!loaded) return <ChatMessageSkeleton />;

  if (!mainSessionId && !boundToAgent) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-muted-foreground">
        <Lock className="h-8 w-8" />
        <p className="text-body">
          {agent.archived_at
            ? t(($) => $.chat.archived_no_chat, { name: agent.name })
            : t(($) => $.chat.no_access, { name: agent.name })}
        </p>
      </div>
    );
  }

  const backHref = teamHref(wsPaths.team(), { kind: "agent", id: agent.id });
  const queuedTasks = c.pendingTask?.queued_tasks ?? [];
  const channel = earlier?.channel_source?.channel_type;

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col @container">
      {earlier && (
        <div className={cn(CHAT_GUTTER, "pt-3")}>
          <div className={cn(CHAT_COLUMN, "flex items-center gap-2 rounded-md bg-muted px-3 py-2 text-caption")}>
            {channel ? (
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
            ) : (
              <History className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">{earlier.title?.trim() || t(($) => $.chat.untitled)}</p>
              <p className="truncate text-muted-foreground">
                {channel
                  ? t(($) => $.chat.earlier_from_channel, {
                      channel: t(($) => $.chat.channel_label[channelLabelKey(channel)]),
                    })
                  : t(($) => $.chat.earlier_label, {
                      date: new Date(earlier.created_at).toLocaleDateString(),
                    })}
              </p>
            </div>
            <Button size="xs" variant="outline" render={<AppLink href={backHref} />} nativeButton={false}>
              <ArrowLeft className="size-3" />
              {t(($) => $.chat.back_to_current)}
            </Button>
          </div>
        </div>
      )}

      {!synced || c.showSkeleton ? (
        <ChatMessageSkeleton />
      ) : c.hasMessages ? (
        <ChatMessageList
          key={c.activeSessionId}
          messages={c.messages}
          pendingTask={c.pendingTask}
          availability={c.availability}
          firstItemIndex={c.firstItemIndex}
          hasOlderMessages={c.hasOlderMessages}
          isFetchingOlderMessages={c.isFetchingOlderMessages}
          onLoadOlderMessages={() => void c.fetchOlderMessages()}
          onQuickAction={(action) => c.handleSend(action.prompt)}
          quickActionsDisabled={
            readOnly ||
            !!c.pendingTaskId ||
            c.isSessionArchived ||
            c.isAgentArchived ||
            c.isAgentAccessRevoked ||
            !c.isAgentRuntimeBound
          }
          onRegenerateQuickActions={
            readOnly
              ? undefined
              : (message) =>
                  c.activeSessionId
                    ? regenerateQuickActions.mutateAsync({
                        sessionId: c.activeSessionId,
                        messageId: message.id,
                      })
                    : undefined
          }
          quickActionsPendingMessageId={quickActionsPending?.message_id ?? null}
          renderMessageExtras={extras}
          timeline={{ agentId: agent.id, userId }}
        />
      ) : (
        <EmptyState
          agent={c.activeAgent}
          hasSessions={false}
          onPickPrompt={c.sendConversationStarter}
          customizeHref={c.customizeConversationStartersHref}
        />
      )}

      {readOnly ? (
        <div className={cn(CHAT_GUTTER, "pb-4")}>
          <div className={cn(CHAT_COLUMN, "flex items-center gap-3 rounded-md bg-muted px-3 py-2.5 text-caption text-muted-foreground")}>
            <span className="flex-1">
              {channel
                ? t(($) => $.chat.read_only_channel, {
                    channel: t(($) => $.chat.channel_label[channelLabelKey(channel)]),
                  })
                : t(($) => $.chat.read_only)}
            </span>
            <Button size="xs" variant="outline" render={<AppLink href={backHref} />} nativeButton={false}>
              {t(($) => $.chat.back_to_current)}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {c.isAgentAccessRevoked ? (
            <AgentAccessRevokedBanner agentName={c.activeAgent?.name} />
          ) : c.isAgentArchived ? (
            <ArchivedAgentBanner agentName={c.activeAgent?.name} />
          ) : !c.isAgentRuntimeBound && c.activeAgent ? (
            <RuntimeRequiredBanner agentId={c.activeAgent.id} agentName={c.activeAgent.name} />
          ) : (
            <OfflineBanner agentName={c.activeAgent?.name} availability={c.availability} />
          )}
          <ChatQueue
            tasks={queuedTasks}
            headStatus={c.pendingTask?.status}
            onSendNow={c.handleSendQueuedTaskNow}
            sendNowDisabled={c.isAgentAccessRevoked}
            onEdit={c.handleEditQueuedTask}
            onRemove={c.handleRemoveQueuedTask}
            onClear={c.handleClearQueuedTasks}
          />
          <ChatInput
            onSend={c.handleSend}
            restoreDraftRequest={c.restoreDraftRequest}
            conversationStarterRequest={c.conversationStarterRequest}
            onConversationStarterApplied={c.handleConversationStarterApplied}
            onRestoreDraftApplied={c.handleRestoreDraftApplied}
            uploadEnabled={c.uploadEnabled && !c.isAgentAccessRevoked}
            onStop={c.handleStop}
            isRunning={!!c.pendingTaskId}
            allowSubmitWhileRunning={c.pendingTask?.supports_queue === true}
            disabled={
              !synced ||
              c.isSessionArchived ||
              c.isAgentArchived ||
              c.isAgentAccessRevoked ||
              !c.isAgentRuntimeBound
            }
            agentArchived={c.isAgentArchived}
            agentAccessRevoked={c.isAgentAccessRevoked}
            agentRuntimeRequired={!c.isAgentRuntimeBound}
            agentName={c.activeAgent?.name}
            projects={c.projects}
            projectId={c.activeProjectId}
            projectContextUnsupported={c.projectContextUnsupported}
            onProjectChange={c.handleProjectChange}
            isProjectUpdating={c.isProjectUpdating}
            focusRequest={c.focusInputRequest}
          />
        </>
      )}
    </div>
  );
}

function findCachedMessage(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string | null | undefined,
  messageId: string | null | undefined,
): ChatMessage | null {
  if (!sessionId || !messageId) return null;
  const pages = qc.getQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sessionId));
  for (const page of pages?.pages ?? []) {
    const hit = page.messages.find((m) => m.id === messageId);
    if (hit) return hit;
  }
  return null;
}

/**
 * A thread under one message of an agent conversation, beside it: the message
 * it replies to, the replies, and a composer. It reads and writes the thread's
 * own session directly, so the main conversation keeps the shared chat
 * controller.
 */
export function TeamAgentThreadPane({
  agent,
  thread,
  closeHref,
  readOnly,
}: {
  agent: Agent;
  thread: ChatSession;
  closeHref: string;
  readOnly: boolean;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const { byAgent } = useWorkspacePresenceMap(wsId);
  const teamPageEnabled = useFeatureEnabled(TEAM_PAGE_FLAG, false);
  const pagesQuery = useInfiniteQuery(chatMessagesPageOptions(thread.id));
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(thread.id));
  const pages = pagesQuery.data?.pages ?? [];
  const messages = hideQueuedChatMessages(
    [...pages].reverse().flatMap((page) => page.messages),
    pendingTask,
  );
  const rootMessage = findCachedMessage(qc, thread.parent_chat_session_id, thread.root_chat_message_id);
  const rootIsUser = rootMessage?.role === "user";
  const userName = useAuthStore((s) => s.user?.name ?? "");
  const { mutate: markRead } = useMarkChatSessionRead();
  const unread = thread.unread_count ?? 0;
  useEffect(() => {
    if (unread > 0) markRead(thread.id);
  }, [unread, thread.id, markRead, messages.length]);

  const extras = useTeamMessageExtras({
    wsId,
    agent,
    sessions: EMPTY_SESSIONS,
    shownSessionId: thread.id,
    earlierSessionId: null,
    openThreadId: null,
    inThread: true,
    canAct: teamPageEnabled && !readOnly && !agent.archived_at,
  });

  const send = async (content: string) => {
    try {
      const result = await api.sendChatMessage(thread.id, content);
      upsertChatMessageToCaches(
        qc,
        thread.id,
        {
          id: result.message_id,
          chat_session_id: thread.id,
          role: "user",
          content,
          task_id: result.task_id,
          created_at: result.created_at,
          attachments: [],
        },
        { seedIfMissing: true },
      );
      seedAcceptedPendingTask(qc, thread.id, {
        task_id: result.task_id,
        created_at: result.created_at,
        message_id: result.message_id,
        content,
        supports_queue: result.supports_queue,
        queued: result.queued,
      });
      return true;
    } catch {
      toast.error(t(($) => $.conversation.send_failed));
      return false;
    }
  };

  const replies = thread.thread_reply_count ?? messages.length;
  return (
    <aside className="flex w-[400px] shrink-0 flex-col border-l @container">
      <ThreadPaneHeader where={agent.name} closeHref={closeHref} />
      <div className="shrink-0 border-b px-5 pb-3 pt-3">
        <div className="flex gap-3">
          <ActorAvatar actorType={rootIsUser ? "member" : "agent"} actorId={rootIsUser ? userId : agent.id} size="md" />
          <div className="min-w-0 flex-1">
            <div className="flex items-baseline gap-2">
              <span className="truncate text-body font-medium">{rootIsUser ? userName : agent.name}</span>
              {rootMessage && (
                <time className="shrink-0 text-micro text-muted-foreground" dateTime={rootMessage.created_at}>
                  {new Date(rootMessage.created_at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}
                </time>
              )}
            </div>
            <div className="line-clamp-6 text-body">
              {rootMessage ? (
                <RichContent content={rootMessage.content} density="compact" phase="settled" />
              ) : (
                <p className="truncate">{thread.title?.trim() || t(($) => $.chat.untitled)}</p>
              )}
            </div>
          </div>
        </div>
      </div>
      {replies > 0 && (
        <div className="flex shrink-0 items-center gap-3 px-5 pt-2 text-micro text-muted-foreground">
          {t(($) => $.chat.thread.replies, { count: replies })}
          <span className="h-px flex-1 bg-border" />
        </div>
      )}
      {pagesQuery.isLoading ? (
        <ChatMessageSkeleton />
      ) : messages.length > 0 || pendingTask?.task_id ? (
        <ChatMessageList
          key={thread.id}
          messages={messages}
          pendingTask={pendingTask}
          availability={byAgent.get(agent.id)?.availability}
          renderMessageExtras={extras}
          timeline={{ agentId: agent.id, userId, anchor: "top" }}
        />
      ) : (
        <div className="flex flex-1 items-center justify-center px-6">
          <p className="max-w-xs text-center text-caption text-muted-foreground">
            {t(($) => $.chat.thread.empty, { name: agent.name })}
          </p>
        </div>
      )}
      {!readOnly && (
        <ConversationComposer
          key={`composer:${thread.id}`}
          placeholder={t(($) => $.conversation.thread_placeholder)}
          onSend={send}
          disabled={!!pendingTask?.task_id && pendingTask.supports_queue !== true}
        />
      )}
    </aside>
  );
}

export function ThreadPaneHeader({ where, closeHref }: { where: string; closeHref: string }) {
  const { t } = useT("team");
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <span className="text-body font-semibold">{t(($) => $.chat.thread.title)}</span>
      <span className="min-w-0 truncate text-caption text-muted-foreground">
        {t(($) => $.chat.thread.in, { name: where })}
      </span>
      <span className="flex-1" />
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label={t(($) => $.chat.thread.close)}
        render={<AppLink href={closeHref} />}
        nativeButton={false}
      >
        <X className="size-4" />
      </Button>
    </div>
  );
}

/**
 * Builds the per-message extras for the shown session: the thread under each
 * message and the issue it became, with the actions that create them.
 */
function useTeamMessageExtras({
  wsId,
  agent,
  sessions,
  shownSessionId,
  earlierSessionId,
  openThreadId,
  inThread = false,
  canAct,
}: {
  wsId: string;
  agent: Agent;
  sessions: readonly ChatSession[];
  shownSessionId: string | null;
  earlierSessionId: string | null;
  openThreadId: string | null;
  inThread?: boolean;
  canAct: boolean;
}) {
  const { t } = useT("team");
  const { push } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const teamPageEnabled = useFeatureEnabled(TEAM_PAGE_FLAG, false);
  const createThread = useCreateChatThread();
  const createIssue = useCreateIssueFromChatMessage();
  const { mutate: openThread } = createThread;
  const { mutate: issueFromMessage } = createIssue;
  const { data: issues = EMPTY_ISSUES } = useQuery({
    ...chatMessageIssuesOptions(wsId, shownSessionId ?? ""),
    enabled: teamPageEnabled && !!shownSessionId,
  });

  useWSEvent(
    "issue:updated",
    useCallback(() => {
      if (shownSessionId) {
        qc.invalidateQueries({ queryKey: chatKeys.messageIssues(wsId, shownSessionId) });
      }
    }, [qc, wsId, shownSessionId]),
  );

  const threadsByRoot = useMemo(() => {
    const out = new Map<string, ChatSession>();
    if (inThread || !shownSessionId) return out;
    for (const s of sessions) {
      if (s.parent_chat_session_id === shownSessionId && s.root_chat_message_id) {
        out.set(s.root_chat_message_id, s);
      }
    }
    return out;
  }, [sessions, shownSessionId, inThread]);

  const issuesByMessage = useMemo(() => {
    const out = new Map<string, ChatMessageIssue[]>();
    for (const issue of issues) {
      const list = out.get(issue.chat_message_id) ?? [];
      list.push(issue);
      out.set(issue.chat_message_id, list);
    }
    return out;
  }, [issues]);

  const threadHref = useCallback(
    (id: string) =>
      teamHref(wsPaths.team(), { kind: "agent", id: agent.id, sessionId: earlierSessionId, threadId: id }),
    [wsPaths, agent.id, earlierSessionId],
  );

  const busy = createThread.isPending || createIssue.isPending;

  return useCallback(
    (message: ChatMessage) => {
      if (!shownSessionId || message.failure_reason || message.message_kind === "issue_event") return null;
      const thread = threadsByRoot.get(message.id) ?? null;
      const messageIssues = issuesByMessage.get(message.id) ?? EMPTY_ISSUES;
      const replies = thread?.thread_reply_count ?? 0;
      return (
        <TeamMessageExtras
          issues={messageIssues}
          busy={busy}
          thread={
            thread && replies > 0 ? (
              <ThreadSummaryLink
                href={threadHref(thread.id)}
                count={replies}
                lastReplyAt={thread.last_message?.created_at ?? thread.updated_at}
                active={openThreadId === thread.id}
                unread={(thread.unread_count ?? 0) > 0}
                avatars={
                  <>
                    <ActorAvatar actorType="agent" actorId={agent.id} size="xs" />
                    <ActorAvatar actorType="member" actorId={userId} size="xs" />
                  </>
                }
              />
            ) : null
          }
          onReply={
            canAct && !inThread
              ? () =>
                  thread
                    ? push(threadHref(thread.id))
                    : openThread(
                        { sessionId: shownSessionId, rootMessageId: message.id },
                        {
                          onSuccess: (created) => push(threadHref(created.id)),
                          onError: () => toast.error(t(($) => $.chat.thread.open_failed)),
                        },
                      )
              : undefined
          }
          onCreateIssue={
            canAct && messageIssues.length === 0
              ? () =>
                  issueFromMessage(
                    { sessionId: shownSessionId, messageId: message.id },
                    {
                      onSuccess: (res) =>
                        toast.success(
                          t(($) => $.chat.issue.created, {
                            identifier: res.issue.identifier,
                            name: agent.name,
                          }),
                        ),
                      onError: () => toast.error(t(($) => $.chat.issue.create_failed)),
                    },
                  )
              : undefined
          }
        />
      );
    },
    [
      shownSessionId,
      threadsByRoot,
      issuesByMessage,
      threadHref,
      openThreadId,
      canAct,
      inThread,
      busy,
      openThread,
      issueFromMessage,
      push,
      t,
      agent.id,
      agent.name,
      userId,
    ],
  );
}
