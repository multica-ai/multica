"use client";

import { useLayoutEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Hash, History, Lock } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useChatStore } from "@multica/core/chat";
import { chatQuickActionsPendingOptions, chatSessionsOptions } from "@multica/core/chat/queries";
import { pickPrimaryChatSession } from "@multica/core/chat/primary-session";
import { useRegenerateChatQuickActions } from "@multica/core/chat/mutations";
import { useRequiredWorkspaceSlug, useWorkspacePaths } from "@multica/core/paths";
import { getCurrentSlug } from "@multica/core/platform";
import type { Agent } from "@multica/core/types";
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

/**
 * The agent's Chat tab: the viewer's one long-running conversation with this
 * agent, or — with `sessionId` — an earlier conversation, shown read-only.
 *
 * It drives the shared chat controller (the same one behind the Chat page and
 * the floating window) by pointing the chat store at the right session, so
 * sending, streaming, queueing, stopping and read state behave exactly as they
 * do everywhere else.
 */
export function TeamAgentChat({ agent, sessionId }: { agent: Agent; sessionId: string | null }) {
  const { t } = useT("team");
  const { pathname } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const workspaceSlug = useRequiredWorkspaceSlug();
  const routeActive = pathname === wsPaths.team() && getCurrentSlug() === workspaceSlug;
  const c = useChatController({ isActive: routeActive });
  const { isSuccess: sessionsLoaded } = useQuery(chatSessionsOptions(c.wsId));
  const { data: quickActionsPending = null } = useQuery(
    chatQuickActionsPendingOptions(c.activeSessionId ?? ""),
  );
  const regenerateQuickActions = useRegenerateChatQuickActions();

  const primary = pickPrimaryChatSession(c.sessions, agent.id);
  const earlier =
    sessionId && sessionId !== primary?.id
      ? c.sessions.find((s) => s.id === sessionId && s.agent_id === agent.id) ?? null
      : null;
  const targetSessionId = earlier?.id ?? primary?.id ?? null;

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

  const readOnly = !!earlier;
  const backHref = teamHref(wsPaths.team(), { kind: "agent", id: agent.id });
  const queuedTasks = c.pendingTask?.queued_tasks ?? [];
  const channel = earlier?.channel_source?.channel_type;

  return (
    <div className="flex min-h-0 flex-1 flex-col @container">
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
