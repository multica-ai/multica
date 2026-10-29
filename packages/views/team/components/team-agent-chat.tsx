"use client";

import { useCallback, useLayoutEffect, useMemo } from "react";
import { useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowLeft, Hash, History, Lock, MessagesSquare } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useChatStore } from "@multica/core/chat";
import {
  chatKeys,
  chatMessageIssuesOptions,
  chatQuickActionsPendingOptions,
  chatSessionsWithThreadsOptions,
  isThreadChatSession,
} from "@multica/core/chat/queries";
import { pickPrimaryChatSession } from "@multica/core/chat/primary-session";
import {
  useCreateChatThread,
  useCreateIssueFromChatMessage,
  useRegenerateChatQuickActions,
} from "@multica/core/chat/mutations";
import { useFeatureEnabled } from "@multica/core/config";
import { TEAM_PAGE_FLAG } from "@multica/core/feature-flags";
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
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ChatMessageList, ChatMessageSkeleton } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { ChatQueue } from "../../chat/components/chat-queue";
import { EmptyState } from "../../chat/components/chat-empty-state";
import { useChatController } from "../../chat/components/use-chat-controller";
import { OfflineBanner } from "../../chat/components/offline-banner";
import { ArchivedAgentBanner } from "../../chat/components/archived-agent-banner";
import { AgentAccessRevokedBanner } from "../../chat/components/agent-access-revoked-banner";
import { RuntimeRequiredBanner } from "../../chat/components/runtime-required-banner";
import { CHAT_COLUMN, CHAT_GUTTER } from "../../chat/components/chat-column";
import { teamHref } from "../selection";
import { channelLabelKey } from "./team-agent-panel";
import { TeamMessageExtras } from "./team-message-extras";

const EMPTY_SESSIONS: ChatSession[] = [];
const EMPTY_ISSUES: ChatMessageIssue[] = [];

/**
 * The agent's Chat tab: the viewer's one long-running conversation with this
 * agent, or — with `sessionId` — an earlier conversation, shown read-only, or
 * — with `threadId` — the thread under one of their messages.
 *
 * It drives the shared chat controller (the same one behind the Chat page and
 * the floating window) by pointing the chat store at the right session, so
 * sending, streaming, queueing, stopping and read state behave exactly as they
 * do everywhere else.
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
  const qc = useQueryClient();
  const teamPageEnabled = useFeatureEnabled(TEAM_PAGE_FLAG, false);
  const wsPaths = useWorkspacePaths();
  const workspaceSlug = useRequiredWorkspaceSlug();
  const routeActive = pathname === wsPaths.team() && getCurrentSlug() === workspaceSlug;
  const c = useChatController({ isActive: routeActive });
  const { data: sessions = EMPTY_SESSIONS, isSuccess: sessionsLoaded } = useQuery(
    chatSessionsWithThreadsOptions(c.wsId),
  );
  const { data: quickActionsPending = null } = useQuery(
    chatQuickActionsPendingOptions(c.activeSessionId ?? ""),
  );
  const regenerateQuickActions = useRegenerateChatQuickActions();

  const primary = pickPrimaryChatSession(sessions, agent.id);
  const thread = threadId
    ? sessions.find(
        (s) => s.id === threadId && s.agent_id === agent.id && isThreadChatSession(s),
      ) ?? null
    : null;
  const threadParent = thread
    ? sessions.find((s) => s.id === thread.parent_chat_session_id) ?? null
    : null;
  const earlier =
    !thread && sessionId && sessionId !== primary?.id
      ? sessions.find(
          (s) => s.id === sessionId && s.agent_id === agent.id && !isThreadChatSession(s),
        ) ?? null
      : null;
  const targetSessionId = thread?.id ?? earlier?.id ?? primary?.id ?? null;
  // A thread is as writable as the conversation it hangs off.
  const fromEarlier = thread ? !!threadParent && threadParent.id !== primary?.id : !!earlier;

  // Point the shared chat store at this agent's conversation. Deliberately not
  // keyed on the live activeSessionId: the first send in a brand-new chat
  // creates a session before the list refetch reports it as primary, and that
  // window must not be "corrected" back to an empty compose. A layout effect,
  // so switching agents never paints a frame bound to the previous one.
  useLayoutEffect(() => {
    if (!sessionsLoaded || !routeActive) return;
    const live = useChatStore.getState();
    if (targetSessionId) {
      if (live.activeSessionId !== targetSessionId) live.setActiveSession(targetSessionId);
      if (live.selectedAgentId !== agent.id) live.setSelectedAgentId(agent.id);
      return;
    }
    if (live.selectedAgentId !== agent.id) live.setSelectedAgentId(agent.id);
    if (live.activeSessionId !== null) {
      live.setSelectedProjectId(null);
      live.setActiveSession(null);
    }
  }, [agent.id, targetSessionId, sessionsLoaded, routeActive]);

  // The controller resolves its agent from the open session, or from the
  // picker list for a new chat. A new chat with an agent the viewer may not
  // invoke would otherwise fall back to some other agent; never compose then.
  const boundToAgent = c.activeAgent?.id === agent.id;
  const synced =
    boundToAgent && (c.activeSessionId === targetSessionId || targetSessionId === null);

  const extras = useTeamMessageExtras({
    wsId: c.wsId,
    agent,
    sessions,
    shownSessionId: synced ? targetSessionId : null,
    inThread: !!thread,
    canAct: teamPageEnabled && !fromEarlier && !c.isAgentArchived && !c.isAgentAccessRevoked,
  });

  if (!sessionsLoaded) return <ChatMessageSkeleton />;

  if (!targetSessionId && !boundToAgent) {
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

  const readOnly = fromEarlier;
  const backHref = teamHref(wsPaths.team(), { kind: "agent", id: agent.id });
  const queuedTasks = c.pendingTask?.queued_tasks ?? [];
  const channel = (earlier ?? threadParent)?.channel_source?.channel_type;

  return (
    <div className="flex min-h-0 flex-1 flex-col @container">
      {thread && (
        <TeamThreadHeader
          thread={thread}
          backHref={
            threadParent && threadParent.id !== primary?.id
              ? teamHref(wsPaths.team(), { kind: "agent", id: agent.id, sessionId: threadParent.id })
              : backHref
          }
          rootMessage={findCachedMessage(qc, thread.parent_chat_session_id, thread.root_chat_message_id)}
        />
      )}
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
        />
      ) : thread ? (
        <div className={cn(CHAT_GUTTER, "flex flex-1 items-center justify-center")}>
          <p className="max-w-sm text-center text-caption text-muted-foreground">
            {t(($) => $.chat.thread.empty, { name: agent.name })}
          </p>
        </div>
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

// The message a thread replies to, so the reader knows what it's about. Read
// from the conversation's cached messages; a deep link without that cache
// falls back to the thread's title, which is the message's first line.
function TeamThreadHeader({
  thread,
  backHref,
  rootMessage,
}: {
  thread: ChatSession;
  backHref: string;
  rootMessage: ChatMessage | null;
}) {
  const { t } = useT("team");
  return (
    <div className={cn(CHAT_GUTTER, "pt-3")}>
      <div className={cn(CHAT_COLUMN, "flex flex-col gap-2 rounded-md bg-muted px-3 py-2.5")}>
        <div className="flex items-center gap-2 text-caption">
          <MessagesSquare className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="flex-1 font-medium">{t(($) => $.chat.thread.title)}</span>
          <Button size="xs" variant="outline" render={<AppLink href={backHref} />} nativeButton={false}>
            <ArrowLeft className="size-3" />
            {t(($) => $.chat.thread.back)}
          </Button>
        </div>
        <div className="border-l-2 pl-3 text-caption text-muted-foreground">
          <p className="mb-0.5 text-faint-foreground">{t(($) => $.chat.thread.root_label)}</p>
          {rootMessage ? (
            <div className="line-clamp-4">
              <RichContent content={rootMessage.content} density="compact" phase="settled" />
            </div>
          ) : (
            <p className="truncate">{thread.title?.trim() || t(($) => $.chat.untitled)}</p>
          )}
        </div>
      </div>
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
  inThread,
  canAct,
}: {
  wsId: string;
  agent: Agent;
  sessions: readonly ChatSession[];
  shownSessionId: string | null;
  inThread: boolean;
  canAct: boolean;
}) {
  const { t } = useT("team");
  const { push } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const qc = useQueryClient();
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
    (id: string) => teamHref(wsPaths.team(), { kind: "agent", id: agent.id, threadId: id }),
    [wsPaths, agent.id],
  );

  const busy = createThread.isPending || createIssue.isPending;

  return useCallback(
    (message: ChatMessage) => {
      if (!shownSessionId || message.failure_reason) return null;
      const thread = threadsByRoot.get(message.id) ?? null;
      return (
        <TeamMessageExtras
          message={message}
          thread={thread}
          threadHref={thread ? threadHref(thread.id) : null}
          issues={issuesByMessage.get(message.id) ?? EMPTY_ISSUES}
          canReply={canAct && !inThread}
          canCreateIssue={canAct}
          busy={busy}
          onReply={() =>
            openThread(
              { sessionId: shownSessionId, rootMessageId: message.id },
              {
                onSuccess: (created) => push(threadHref(created.id)),
                onError: () => toast.error(t(($) => $.chat.thread.open_failed)),
              },
            )
          }
          onCreateIssue={() =>
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
          }
        />
      );
    },
    [
      shownSessionId,
      threadsByRoot,
      issuesByMessage,
      threadHref,
      canAct,
      inThread,
      busy,
      openThread,
      issueFromMessage,
      push,
      t,
      agent.name,
    ],
  );
}
