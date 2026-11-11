"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion } from "motion/react";
import { Minus, Maximize2, Minimize2, ChevronDown, Plus, Check, Archive, Pencil, Loader2, Square, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useWorkspaceId } from "@multica/core/hooks";
import { useReducedMotion } from "@multica/core/appearance";
import { api } from "@multica/core/api";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { ActorAvatar } from "../../common/actor-avatar";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import {
  RowActionsMenu,
  handleRowActivationKey,
  type RowActionItem,
} from "../../common/row-actions-menu";
import {
  PickerEmpty,
  PickerItem,
  PickerSection,
  PropertyPicker,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { OfflineBanner } from "./offline-banner";
import { NoAgentBanner } from "./no-agent-banner";
import { ArchivedAgentBanner } from "./archived-agent-banner";
import { AgentAccessRevokedBanner } from "./agent-access-revoked-banner";
import { RuntimeRequiredBanner } from "./runtime-required-banner";
import {
  pendingChatTasksOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import { useUpdateChatSession } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { chatQuickActionsPendingOptions } from "@multica/core/chat/queries";
import { useQuickActionsPendingTimeout } from "@multica/core/chat/use-quick-actions-pending-timeout";
import { useQuickActionsFailureToast } from "./use-quick-actions-failure-toast";
import { removeChatMessageFromCaches } from "@multica/core/realtime";
import { useChatInputFocus } from "./use-chat-input-focus";
import { useChatOpenReveal } from "./use-chat-open-reveal";
import { ChatWindowMessages } from "./chat-window-messages";
import { ChatInput } from "./chat-input";
import { useChatAnnotations } from "./use-chat-annotations";
import { ChatQueue } from "./chat-queue";
import { ChatAppRequests } from "../../common/app-requests";
import { EmptyState } from "./chat-empty-state";
import { SessionRenameInput } from "./session-rename-input";
import { SessionHistoryList } from "./session-history-list";
import { chatActivity } from "./chat-bar-sessions";
import { ChatResizeHandles } from "./chat-resize-handles";
import { useChatContextItems } from "./use-chat-context-items";
import { useChatResize } from "./use-chat-resize";
import { useVisualViewportKeyboard } from "./use-visual-viewport-keyboard";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { CHAT_WINDOW_INSET, useChatWindowAnchor } from "./use-chat-window-anchor";
import { useFormatTimeAgo } from "./use-format-time-ago";
import { useChatController } from "./use-chat-controller";
import { createLogger } from "@multica/core/logger";
import type { Agent, ChatSession, PendingChatTasksResponse } from "@multica/core/types";
import { useT } from "../../i18n";

const uiLogger = createLogger("chat.ui");
const apiLogger = createLogger("chat.api");


export function ChatWindow() {
  const { t } = useT("chat");
  const isOpen = useChatStore((s) => s.isOpen);
  const isExpanded = useChatStore((s) => s.isExpanded);
  const setOpen = useChatStore((s) => s.setOpen);
  const dismissChatFromBar = useChatStore((s) => s.dismissChatFromBar);
  // Conversation state and behavior are shared with the Chat tab. `isOpen`
  // gates everything that assumes the user is looking: this window stays
  // MOUNTED when closed (it is only hidden, see isVisible below).
  const c = useChatController({ isActive: isOpen });
  const { wsId, activeSessionId, currentSession, activeAgent, pendingTask, pendingTaskId } = c;
  const { data: quickActionsPending = null } = useQuery(
    chatQuickActionsPendingOptions(activeSessionId ?? ""),
  );
  // Drop a stuck pending marker (dead daemon / failed supplement) so the pill
  // spinner stops and a later refresh starts clean (MUL-5149).
  useQuickActionsPendingTimeout(activeSessionId ?? null, quickActionsPending);
  // Toast when an accepted refresh later fails in the daemon (async half).
  useQuickActionsFailureToast(activeSessionId ?? null);

  // The window adds its own focus moments — opening it, and Ask Multica starting
  // a new chat — on top of the controller's new-chat nonce. Both counters only
  // grow, so their sum changes whenever either one does.
  const windowRef = useRef<HTMLDivElement>(null);
  const newChatRequest = useChatStore((s) => s.newChatRequest);
  const { focusRequest: openFocusRequest } = useChatInputFocus(isOpen, windowRef, newChatRequest);
  const focusRequest = c.focusInputRequest + openFocusRequest;

  const annotations = useChatAnnotations({
    sessionId: activeSessionId,
    agentName: activeAgent?.name,
    enabled: !c.isSessionArchived && !c.isAgentArchived && !c.isAgentAccessRevoked && c.isAgentRuntimeBound && !c.noAgent,
  });

  // Mount / unmount logging. ChatWindow lives in DashboardLayout, so this
  // fires on layout mount (login / workspace switch / fresh page load).
  useEffect(() => {
    uiLogger.info("ChatWindow mount", {
      isOpen,
      activeSessionId,
      pendingTaskId,
      selectedAgentId: c.selectedAgentId,
      wsId,
    });
    return () => {
      uiLogger.info("ChatWindow unmount", {
        activeSessionId,
        pendingTaskId,
      });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per mount
  }, []);

  const handleMinimize = useCallback(() => {
    uiLogger.info("minimize (close)", {
      activeSessionId,
      pendingTaskId,
    });
    setOpen(false);
  }, [activeSessionId, pendingTaskId, setOpen]);

  // Minimize leaves the chat's chip in the bottom bar; close also takes the
  // chip away, the same as closing it from the bar.
  const handleClose = useCallback(() => {
    uiLogger.info("close", {
      activeSessionId,
      pendingTaskId,
    });
    setOpen(false);
    if (currentSession) dismissChatFromBar(currentSession.id, chatActivity(currentSession));
  }, [activeSessionId, pendingTaskId, currentSession, setOpen, dismissChatFromBar]);

  const handleArchiveSession = (session: ChatSession) => {
    c.advanceSelectionAfterArchive(session);
    c.archiveSession(session.id);
  };

  const { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag } = useChatResize(windowRef);
  const reduceMotion = useReducedMotion();
  const { contentReady, markContentReady } = useChatOpenReveal(isOpen);

  const isVisible = isOpen && (isExpanded || boundsReady) && contentReady;

  // Small screens drop the floating-card form entirely — a 90%-of-375px
  // "window" is all chrome and no content, so the panel goes full-screen
  // (Lark/IM-style) and the resize/expand affordances disappear with it.
  const isMobile = useIsMobile();

  // `@container`: the window is user-resizable from 360px to 90% of the
  // viewport, so the chat body's gutter (CHAT_GUTTER) has to key off the
  // window's own width, not the page behind it.
  const containerClass = cn(
    "absolute z-50 flex flex-col overflow-hidden bg-surface-raised @container",
    isMobile
      ? "inset-x-0"
      : "rounded-xl shadow-[var(--floating-shadow)] ring-1 ring-floating-border",
  );
  // Soft keyboards shrink only the *visual* viewport — the layout viewport
  // (and this panel's bottom-anchored parent) keeps its full height, so
  // without correction the panel's lower half, composer included, sits
  // behind the keyboard while iOS pans the page and chops the panel's top
  // instead. While the keyboard is up, pin the panel to the visual
  // viewport: bottom glued to its bottom edge (occludedBottom tracks any
  // pan), height/maxHeight bounded by the visible strip, so the composer
  // rides the keyboard's top edge.
  //
  // Every branch writes the SAME style keys with explicit values: motion.div
  // applies styles imperatively and never unsets a key that merely
  // disappears from the style prop, so a conditional spread here would
  // leave the keyboard geometry stuck on the DOM after the keyboard closes.
  const keyboard = useVisualViewportKeyboard();
  const anchorRight = useChatWindowAnchor(windowRef, isOpen, renderWidth, isDragging);
  const containerStyle: React.CSSProperties = {
    transformOrigin: "bottom right",
    pointerEvents: isOpen ? "auto" : "none",
    ...(isMobile
      ? {
          // Full-screen panel anchored to the visible bottom edge;
          // width/height live in the motion animate below.
          top: "auto",
          right: 0,
          bottom: keyboard ? keyboard.occludedBottom : 0,
          maxHeight: "none",
        }
      : {
          // Floating card; only the anchor and the height cap live here.
          top: "auto",
          right: anchorRight,
          bottom: keyboard ? keyboard.occludedBottom + CHAT_WINDOW_INSET : CHAT_WINDOW_INSET,
          maxHeight: keyboard ? keyboard.viewportHeight - 16 : "none",
        }),
  };

  // Width/height are ALWAYS owned by motion, in both modes: useIsMobile
  // resolves after the first client render, and a key that merely vanishes
  // from `animate` keeps its last DOM value — a phone's first frame would
  // otherwise leave the desktop card's inline width stuck on the
  // full-screen panel. Mixed units (px <-> "100%") skip interpolation and
  // jump, which is the behavior we want for keyboard snaps anyway.
  const motionSize = isMobile
    ? {
        width: "100%",
        height: keyboard ? keyboard.viewportHeight : "100%",
      }
    : { width: renderWidth, height: renderHeight };

  const contextItems = useChatContextItems(wsId);
  const pageContext = contextItems.find((item) => item.group === "current") ?? null;
  const queuedTasks = pendingTask?.queued_tasks ?? [];

  const showRestore = isExpanded || isAtMax;
  const newChatLabel = t(($) => $.window.new_chat_tooltip);
  const expandLabel = showRestore
    ? t(($) => $.window.restore_tooltip)
    : t(($) => $.window.expand_tooltip);
  const minimizeLabel = t(($) => $.window.minimize_tooltip);
  const closeLabel = t(($) => $.window.close_tooltip);
  const sizeTransition =
    isDragging || reduceMotion ? { duration: 0 } : { type: "spring" as const, duration: 0.3, bounce: 0 };

  return (
    <motion.div
      data-mc="chat-window"
      ref={windowRef}
      inert={!isOpen}
      aria-hidden={!isOpen}
      className={containerClass}
      style={containerStyle}
      initial={{ opacity: 0, ...motionSize }}
      animate={{
        opacity: isVisible ? 1 : 0,
        ...motionSize,
      }}
      transition={{
        width: sizeTransition,
        height: sizeTransition,
        opacity: { duration: 0.15 },
      }}
    >
      {!isMobile && <ChatResizeHandles onDragStart={startDrag} />}
      {/* Header — ⊕ new + session dropdown | window tools */}
      <div className="flex items-center justify-between border-b px-4 py-2.5 gap-2">
        <div className="flex items-center gap-1 min-w-0">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  aria-label={newChatLabel}
                  onClick={c.handleNewChat}
                />
              }
            >
              <Plus />
            </TooltipTrigger>
            <TooltipContent side="top">{newChatLabel}</TooltipContent>
          </Tooltip>
          <SessionDropdown
            sessions={c.sessions}
            // Use the full agent list (incl. archived) so historical
            // sessions can still resolve their avatar.
            agents={c.agents}
            activeSessionId={activeSessionId}
            onSelectSession={c.handleSelectSession}
            onArchiveSession={handleArchiveSession}
          />
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
          {!isMobile && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground"
                    aria-label={expandLabel}
                    onClick={toggleExpand}
                  />
                }
              >
                {showRestore ? <Minimize2 /> : <Maximize2 />}
              </TooltipTrigger>
              <TooltipContent side="top">{expandLabel}</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  aria-label={minimizeLabel}
                  onClick={handleMinimize}
                />
              }
            >
              <Minus />
            </TooltipTrigger>
            <TooltipContent side="top">
              {minimizeLabel}
              <ActionShortcutKeycaps action="toggleChat" />
            </TooltipContent>
          </Tooltip>
          {!isMobile && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground"
                    aria-label={closeLabel}
                    onClick={handleClose}
                  />
                }
              >
                <X />
              </TooltipTrigger>
              <TooltipContent side="top">{closeLabel}</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      {/* Messages / skeleton / empty state */}
      <AnimatePresence initial={false}>
        {isOpen && (
          <ChatWindowMessages
            sessionId={activeSessionId}
            pendingTask={pendingTask}
            pendingTaskLoading={c.pendingTaskLoading}
            availability={c.availability}
            onQuickAction={(action) => c.handleSend(action.prompt)}
            quickActionsDisabled={
              !!pendingTaskId ||
              c.isSessionArchived ||
              c.isAgentArchived ||
              c.isAgentAccessRevoked ||
              !c.isAgentRuntimeBound ||
              c.noAgent
            }
            quickActionsPendingMessageId={quickActionsPending?.message_id ?? null}
            onReady={markContentReady}
            annotations={annotations}
          >
            <EmptyState
              agent={activeAgent}
              hasSessions={c.sessions.length > 0}
              onPickPrompt={c.sendConversationStarter}
            />
          </ChatWindowMessages>
        )}
      </AnimatePresence>

      {/* Status banner above the input — single mutually-exclusive slot.
       *  Priority: no-agent > offline / unstable. Agent presence is the
       *  hard prerequisite (you can't send anything without one), so it
       *  always wins over a presence hint. Recent issue/project navigation
       *  lives in the input action row; it is not message/session state.
       *
       *  We key off `noAgent` (the resolved-empty state) rather than
       *  `!activeAgent`, so the loading window between mount and the
       *  first agent-list response stays banner-free. */}
      {c.noAgent ? (
        <NoAgentBanner />
      ) : c.isAgentAccessRevoked ? (
        <AgentAccessRevokedBanner agentName={activeAgent?.name} />
      ) : c.isAgentArchived ? (
        <ArchivedAgentBanner agentName={activeAgent?.name} />
      ) : !c.isAgentRuntimeBound && activeAgent ? (
        <RuntimeRequiredBanner
          agentId={activeAgent.id}
          agentName={activeAgent.name}
        />
      ) : (
        <OfflineBanner agentName={activeAgent?.name} availability={c.availability} />
      )}

      <div className="px-4">
        <ChatAppRequests sessionId={activeSessionId} />
      </div>

      <ChatQueue
        tasks={queuedTasks}
        headStatus={pendingTask?.status}
        onSendNow={c.handleSendQueuedTaskNow}
        sendNowDisabled={c.isAgentAccessRevoked}
        onEdit={c.handleEditQueuedTask}
        onRemove={c.handleRemoveQueuedTask}
        onClear={c.handleClearQueuedTasks}
      />

      {/* Input — disabled for legacy archived sessions and for sessions whose
       *  agent has been archived (read-only); locked out entirely when there's
       *  no agent (the EmptyState above carries the CTA). */}
      <ChatInput
        onSend={c.handleSend}
        restoreDraftRequest={c.restoreDraftRequest}
        conversationStarterRequest={c.conversationStarterRequest}
        onConversationStarterApplied={c.handleConversationStarterApplied}
        onRestoreDraftApplied={c.handleRestoreDraftApplied}
        uploadEnabled={c.uploadEnabled && !c.isAgentAccessRevoked}
        onStop={c.handleStop}
        isRunning={!!pendingTaskId}
        allowSubmitWhileRunning={pendingTask?.supports_queue === true}
        disabled={
          c.isSessionArchived ||
          c.isAgentArchived ||
          c.isAgentAccessRevoked ||
          !c.isAgentRuntimeBound
        }
        noAgent={c.noAgent}
        agentArchived={c.isAgentArchived}
        agentAccessRevoked={c.isAgentAccessRevoked}
        agentRuntimeRequired={!c.isAgentRuntimeBound}
        agentName={activeAgent?.name}
        projects={c.projects}
        projectId={c.activeProjectId}
        onProjectChange={c.handleProjectChange}
        projectContextUnsupported={c.projectContextUnsupported}
        isProjectUpdating={c.isProjectUpdating}
        leftAdornment={
          <AgentDropdown
            agents={c.availableAgents}
            activeAgent={activeAgent}
            userId={c.user?.id}
            onSelect={c.handleSelectAgent}
          />
        }
        contextItems={contextItems}
        pageContext={pageContext}
        focusRequest={focusRequest}
        annotations={annotations}
      />
    </motion.div>
  );
}

/**
 * Agent dropdown: avatar trigger, lists all available agents. Selecting a
 * different agent = switch agent + start a fresh chat (session=null).
 * The current agent is marked with a check and not clickable.
 */
export function AgentDropdown({
  agents,
  activeAgent,
  userId,
  onSelect,
}: {
  agents: Agent[];
  activeAgent: Agent | null;
  userId: string | undefined;
  onSelect: (agent: Agent) => void;
}) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  // Split into the user's own agents and everyone else so the menu groups
  // them — matches the old AgentSelector layout.
  const { mine, others } = useMemo(() => {
    const mine: Agent[] = [];
    const others: Agent[] = [];
    for (const a of agents) {
      if (a.owner_id === userId) mine.push(a);
      else others.push(a);
    }
    return { mine, others };
  }, [agents, userId]);

  const query = filter.trim().toLowerCase();
  const matches = (name: string) =>
    !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const filteredMine = mine.filter((agent) => matches(agent.name));
  const filteredOthers = others.filter((agent) => matches(agent.name));

  const handlePick = (agent: Agent) => {
    onSelect(agent);
    setOpen(false);
  };

  if (!activeAgent) {
    return <span className="text-caption text-muted-foreground">{t(($) => $.window.no_agents)}</span>;
  }

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-64"
      align="start"
      side="top"
      searchable
      searchPlaceholder={t(($) => $.window.agent_filter_placeholder)}
      onSearchChange={setFilter}
      triggerRender={
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-md px-1.5 py-1 -ml-1 cursor-pointer outline-none transition-colors hover:bg-accent aria-expanded:bg-accent"
        />
      }
      trigger={
        <>
          <ActorAvatar
            actorType="agent"
            actorId={activeAgent.id}
            size="md"
            enableHoverCard
            showStatusDot
          />
          <span className="text-caption font-medium max-w-28 truncate">{activeAgent.name}</span>
          <ChevronDown className="size-3 text-muted-foreground shrink-0" />
        </>
      }
    >
      {filteredMine.length === 0 && filteredOthers.length === 0 ? (
        <PickerEmpty />
      ) : (
        <>
          {filteredMine.length > 0 && (
            <PickerSection label={t(($) => $.window.my_agents)}>
              {filteredMine.map((agent) => (
                <AgentPickerItem
                  key={agent.id}
                  agent={agent}
                  isCurrent={agent.id === activeAgent.id}
                  onSelect={handlePick}
                />
              ))}
            </PickerSection>
          )}
          {filteredOthers.length > 0 && (
            <PickerSection label={t(($) => $.window.others)}>
              {filteredOthers.map((agent) => (
                <AgentPickerItem
                  key={agent.id}
                  agent={agent}
                  isCurrent={agent.id === activeAgent.id}
                  onSelect={handlePick}
                />
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}

function AgentPickerItem({
  agent,
  isCurrent,
  onSelect,
}: {
  agent: Agent;
  isCurrent: boolean;
  onSelect: (agent: Agent) => void;
}) {
  const { t } = useT("chat");
  const runtimeBound = isAgentRuntimeBound(agent);
  return (
    <PickerItem
      selected={isCurrent}
      disabled={!runtimeBound}
      tooltip={
        runtimeBound ? undefined : t(($) => $.window.agent_needs_runtime_hint)
      }
      onClick={() => onSelect(agent)}
    >
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size="md"
        enableHoverCard
        showStatusDot
      />
      <span className="truncate flex-1">{agent.name}</span>
      {!runtimeBound && (
        <span className="shrink-0 text-micro text-amber-600 dark:text-amber-400">
          {t(($) => $.window.agent_needs_runtime)}
        </span>
      )}
    </PickerItem>
  );
}

interface SessionRowAction extends RowActionItem {
  /** Extra visible text in the hover strip (the stop button reads "Stop"). */
  stripText?: string;
}

/**
 * Session dropdown: a flat "Chat history" list of all non-archived
 * sessions. Selecting a session from a different agent implicitly
 * switches the agent too
 * (sessions are bound 1:1 to an agent). "New chat" lives in the header's
 * ⊕ button, not inside this dropdown.
 */
function SessionDropdown({
  sessions,
  agents,
  activeSessionId,
  onSelectSession,
  onArchiveSession,
}: {
  sessions: ChatSession[];
  agents: Agent[];
  activeSessionId: string | null;
  onSelectSession: (session: ChatSession) => void;
  onArchiveSession: (session: ChatSession) => void;
}) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const title = activeSession?.title?.trim() || t(($) => $.window.untitled);
  const triggerAgent = activeSession ? agentById.get(activeSession.agent_id) ?? null : null;

  // The old soft-archive feature was removed. Pre-existing rows with
  // status='archived' are legacy dead data and are excluded from history.
  const historySessions = useMemo(
    () => sessions.filter((s) => s.status !== "archived"),
    [sessions],
  );

  const [isHistoryOpen, setIsHistoryOpen] = useState(false);
  const [confirmingStopId, setConfirmingStopId] = useState<string | null>(null);
  const [stoppingTaskId, setStoppingTaskId] = useState<string | null>(null);
  const [completedFlashIds, setCompletedFlashIds] = useState<Set<string>>(() => new Set());
  const previousInFlightRef = useRef<Set<string>>(new Set());
  const completedFlashTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  // Inline rename: only one row can be in edit mode at a time. We track the
  // session id (not the full session) so a stale closure can't overwrite a
  // newer rename pulled in via WS.
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const updateSession = useUpdateChatSession();
  const queryClient = useQueryClient();
  const formatTimeAgo = useFormatTimeAgo();

  // Aggregate "which sessions have an in-flight task right now". Reuses
  // the same workspace-scoped query the FAB consumes, so toggling the chat
  // window doesn't fire a second request — TanStack dedupes by key.
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));
  const pendingTaskBySessionId = useMemo(
    () => new Map((pending?.tasks ?? []).map((task) => [task.chat_session_id, task])),
    [pending],
  );
  const inFlightSessionIds = useMemo(
    () => new Set(pendingTaskBySessionId.keys()),
    [pendingTaskBySessionId],
  );

  useEffect(() => {
    const previous = previousInFlightRef.current;
    const unreadSessionIds = new Set(sessions.filter((s) => s.has_unread).map((s) => s.id));

    for (const sessionId of previous) {
      if (inFlightSessionIds.has(sessionId) || !unreadSessionIds.has(sessionId)) continue;

      setCompletedFlashIds((current) => {
        if (current.has(sessionId)) return current;
        return new Set(current).add(sessionId);
      });

      const existingTimer = completedFlashTimersRef.current.get(sessionId);
      if (existingTimer) clearTimeout(existingTimer);

      const timer = setTimeout(() => {
        setCompletedFlashIds((current) => {
          if (!current.has(sessionId)) return current;
          const next = new Set(current);
          next.delete(sessionId);
          return next;
        });
        completedFlashTimersRef.current.delete(sessionId);
      }, 1600);
      completedFlashTimersRef.current.set(sessionId, timer);
    }

    previousInFlightRef.current = inFlightSessionIds;
  }, [inFlightSessionIds, sessions]);

  useEffect(() => {
    const timers = completedFlashTimersRef.current;
    return () => {
      for (const timer of timers.values()) clearTimeout(timer);
      timers.clear();
    };
  }, []);

  useEffect(() => {
    if (!confirmingStopId || pendingTaskBySessionId.has(confirmingStopId)) return;
    setConfirmingStopId(null);
  }, [confirmingStopId, pendingTaskBySessionId]);

  // Header state split:
  // - inside the trigger: the current chat's own live state
  // - beside the trigger: aggregate activity from other chats
  const currentSessionRunning = activeSessionId ? inFlightSessionIds.has(activeSessionId) : false;
  const otherRunningCount = sessions.filter(
    (s) => s.id !== activeSessionId && inFlightSessionIds.has(s.id),
  ).length;
  const otherUnreadCount = sessions.filter(
    (s) => s.id !== activeSessionId && s.has_unread,
  ).length;

  // Archive (not hard-delete) is the reversible, one-click default here — the
  // same safety model as ChatThreadList. The floating window offers NO
  // hard-delete: unarchive / delete live only in the full Chat page's Archived
  // view (reachable via the expand button), so a stale floating dropdown can't
  // bypass the "archive first, delete only from Archived" semantics.
  const handleSubmitRename = (sessionId: string, raw: string) => {
    const trimmed = raw.trim();
    const current = sessions.find((s) => s.id === sessionId);
    setRenamingId(null);
    // No-op submits (unchanged or blank) skip the network round-trip — the
    // server would reject a blank title anyway, and an unchanged title would
    // just bump updated_at for no user-visible reason.
    if (!trimmed || trimmed === current?.title) return;
    updateSession.mutate({ sessionId, title: trimmed });
  };

  const handleSelectSession = (session: ChatSession) => {
    onSelectSession(session);
    setIsHistoryOpen(false);
  };

  const handleConfirmStop = (session: ChatSession, task: PendingChatTasksResponse["tasks"][number]) => {
    setStoppingTaskId(task.task_id);
    previousInFlightRef.current = new Set(
      [...previousInFlightRef.current].filter((sessionId) => sessionId !== session.id),
    );

    // Same optimistic behavior as the active chat Stop button: remove the
    // running affordance immediately, then let task:cancelled / refetches
    // converge every open surface on the server truth.
    queryClient.setQueryData<PendingChatTasksResponse>(chatKeys.pendingTasks(wsId), (current) => {
      if (!current) return current;
      return {
        ...current,
        tasks: current.tasks.filter((item) => item.task_id !== task.task_id),
      };
    });
    queryClient.setQueryData(chatKeys.pendingTask(session.id), {});
    queryClient.invalidateQueries({ queryKey: chatKeys.messages(session.id) });
    queryClient.invalidateQueries({ queryKey: chatKeys.messagesPage(session.id) });

    api.cancelTaskById(task.task_id).then(
      (result) => {
        const restored = result.cancelled_chat_message;
        if (restored?.restore_to_input) {
          removeChatMessageFromCaches(queryClient, restored.chat_session_id, restored.message_id);
        }
        apiLogger.info("cancelTask.success (history row)", { taskId: task.task_id, sessionId: session.id });
      },
      (err) =>
        apiLogger.warn("cancelTask.error (history row; task may have already finished)", {
          taskId: task.task_id,
          sessionId: session.id,
          err,
        }),
    ).finally(() => {
      queryClient.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
      queryClient.invalidateQueries({ queryKey: chatKeys.pendingTask(session.id) });
      setStoppingTaskId(null);
      setConfirmingStopId(null);
    });
  };

  const renderRow = (session: ChatSession) => {
    const isCurrent = session.id === activeSessionId;
    const agent = agentById.get(session.agent_id) ?? null;
    const pendingTask = pendingTaskBySessionId.get(session.id);
    const isRunning = !!pendingTask;
    const showCompleted = completedFlashIds.has(session.id) && !isCurrent;
    const showUnread = session.has_unread && !isCurrent;
    const isRenaming = renamingId === session.id;
    const isConfirmingStop = confirmingStopId === session.id && !!pendingTask;
    const isConfirmingAction = isConfirmingStop;
    const titleText = session.title?.trim() || t(($) => $.window.untitled);
    const trailingStatus = isRunning
      ? t(($) => $.session_history.row_subtitle.working)
      : showCompleted
        ? t(($) => $.session_history.row_subtitle.completed)
        : showUnread
          ? t(($) => $.session_history.row_subtitle.new_reply)
          : formatTimeAgo(session.updated_at);

    // One list drives both action surfaces — the compact menu without hover
    // and the hover strip with it — so they cannot drift.
    const rowActions: SessionRowAction[] = isRunning
      ? [
          {
            key: "stop",
            icon: <Square className="size-2.5 fill-current" />,
            label: t(($) => $.session_history.row_stop_aria),
            stripText: t(($) => $.session_history.stop_action),
            danger: true,
            onSelect: () => setConfirmingStopId(session.id),
          },
        ]
      : [
          {
            key: "rename",
            icon: <Pencil className="size-3.5" />,
            label: t(($) => $.session_history.row_rename_aria),
            onSelect: () => setRenamingId(session.id),
          },
          {
            key: "archive",
            icon: <Archive className="size-3.5" />,
            label: t(($) => $.list.archive),
            onSelect: () => onArchiveSession(session),
          },
        ];

    return (
      <div
        key={session.id}
        aria-current={isCurrent ? "true" : undefined}
        tabIndex={0}
        onClick={() => {
          if (isRenaming || isConfirmingAction) return;
          handleSelectSession(session);
        }}
        onKeyDown={(e) => {
          if (isRenaming || isConfirmingAction) return;
          handleRowActivationKey(e, () => handleSelectSession(session));
        }}
        className={cn(
          "group/history-row relative flex min-h-11 min-w-0 cursor-default items-center gap-2 overflow-hidden rounded-md py-1.5 pl-2 pr-2 outline-none transition-colors hover:bg-accent/60 focus-visible:bg-accent/60 focus-visible:ring-1 focus-visible:ring-ring",
          isCurrent && "bg-accent/70",
          isConfirmingAction && "bg-destructive/5 hover:bg-destructive/5",
        )}
      >
        {isCurrent && <span className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-full bg-brand" />}
        {agent ? (
          <ActorAvatar
            actorType="agent"
            actorId={agent.id}
            size="md"
            enableHoverCard
            showStatusDot
          />
        ) : (
          <span className="size-6 shrink-0" />
        )}
        <div className="min-w-0 flex-1">
          {isRenaming ? (
            <SessionRenameInput
              initialValue={session.title ?? ""}
              onSubmit={(value) => handleSubmitRename(session.id, value)}
              onCancel={() => setRenamingId(null)}
            />
          ) : isConfirmingStop ? (
            <div className="truncate text-body font-medium text-destructive">
              {t(($) => $.session_history.stop_dialog.title)}
            </div>
          ) : (
            <div
              className={cn("truncate text-body", (showUnread || showCompleted) && !isRunning && "font-medium")}
              style={{
                maskImage: "linear-gradient(to right, black calc(100% - 18px), transparent)",
                WebkitMaskImage: "linear-gradient(to right, black calc(100% - 18px), transparent)",
              }}
            >
              {titleText}
            </div>
          )}
        </div>
        {!isRenaming && (
          isConfirmingStop && pendingTask ? (
            <div className="flex shrink-0 items-center gap-1">
              <button
                type="button"
                onPointerDown={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                }}
                onClick={(e) => {
                  e.stopPropagation();
                  e.preventDefault();
                  setConfirmingStopId(null);
                }}
                disabled={stoppingTaskId === pendingTask.task_id}
                className="inline-flex h-7 items-center rounded-xs px-2 text-micro font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50"
              >
                {t(($) => $.session_history.stop_dialog.cancel)}
              </button>
              <button
                type="button"
                onPointerDown={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                }}
                onClick={(e) => {
                  e.stopPropagation();
                  e.preventDefault();
                  handleConfirmStop(session, pendingTask);
                }}
                disabled={stoppingTaskId === pendingTask.task_id}
                className="inline-flex h-7 items-center rounded-xs px-2 text-micro font-medium text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50"
              >
                {stoppingTaskId === pendingTask.task_id
                  ? t(($) => $.session_history.stop_dialog.confirming)
                  : t(($) => $.session_history.stop_dialog.confirm)}
              </button>
            </div>
          ) : (
            <div className="flex shrink-0 items-center">
              <div className="flex h-7 items-center justify-end gap-1.5 text-caption text-muted-foreground [@media(hover:hover)]:group-hover/history-row:hidden [@media(hover:hover)]:group-focus-within/history-row:hidden">
                {isRunning && <Loader2 className="size-3 animate-spin" />}
                {showCompleted && !isRunning && <Check className="size-3 text-emerald-500" />}
                {showUnread && !isRunning && !showCompleted && (
                  <span
                    aria-label={t(($) => $.window.unread)}
                    title={t(($) => $.window.unread)}
                    className="size-1.5 rounded-full bg-brand"
                  />
                )}
                <span className={cn("truncate", (showUnread || showCompleted || isRunning) && "font-medium text-foreground")}>{trailingStatus}</span>
              </div>
              {/* Touch has no hover: without it the status above stays put and
                  these same actions move into the row's compact menu. */}
              <RowActionsMenu
                label={t(($) => $.session_history.row_actions_aria)}
                groups={[rowActions]}
              />
              <div className="hidden h-7 items-center gap-0.5 [@media(hover:hover)]:group-hover/history-row:flex [@media(hover:hover)]:group-focus-within/history-row:flex">
                {rowActions.map((action) => {
                  const button = (
                    <button
                      key={action.key}
                      type="button"
                      onPointerDown={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                      }}
                      onClick={(e) => {
                        e.stopPropagation();
                        e.preventDefault();
                        action.onSelect();
                      }}
                      className={
                        action.danger
                          ? "inline-flex h-7 items-center gap-1 rounded-xs px-1.5 text-micro font-medium text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive focus-visible:bg-destructive/10 focus-visible:text-destructive focus-visible:outline-none"
                          : "inline-flex size-7 items-center justify-center rounded-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:bg-accent focus-visible:text-foreground focus-visible:outline-none"
                      }
                      aria-label={action.label}
                    >
                      {action.icon}
                      {action.stripText}
                    </button>
                  );
                  return <DeferredTooltip key={action.key} content={action.label} trigger={button} />;
                })}
              </div>
            </div>
          )
        )}
      </div>
    );
  };

  return (
    <>
      <Popover open={isHistoryOpen} onOpenChange={setIsHistoryOpen}>
        <div className="flex min-w-0 items-center gap-1">
          <PopoverTrigger className="flex max-w-96 min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 transition-colors hover:bg-accent data-[popup-open]:bg-accent data-open:bg-accent">
            {triggerAgent && (
              <ActorAvatar
                actorType="agent"
                actorId={triggerAgent.id}
                size="md"
                enableHoverCard
                showStatusDot
              />
            )}
            <span className="min-w-0 truncate text-body font-medium">{title}</span>
            {currentSessionRunning && (
              <Loader2
                aria-label={t(($) => $.session_history.row_subtitle.working)}
                className="size-3 shrink-0 animate-spin text-muted-foreground"
              />
            )}
            <ChevronDown className="size-3 text-muted-foreground shrink-0" />
          </PopoverTrigger>
          {otherRunningCount > 0 ? (
            <span
              aria-label={t(($) => $.window.another_running)}
              title={t(($) => $.window.another_running)}
              className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-1.5 text-caption font-medium text-muted-foreground"
            >
              <Loader2 className="size-3 animate-spin" />
              {otherRunningCount > 1 && <span>{otherRunningCount}</span>}
            </span>
          ) : otherUnreadCount > 0 ? (
            <span
              aria-label={t(($) => $.window.another_unread)}
              title={t(($) => $.window.another_unread)}
              className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md px-1.5 text-caption font-medium text-muted-foreground"
            >
              <span className="size-1.5 rounded-full bg-brand" />
              {otherUnreadCount > 1 && <span>{otherUnreadCount}</span>}
            </span>
          ) : null}
        </div>
        <PopoverContent
          align="start"
          className="max-h-96 w-auto min-w-[max(16rem,var(--anchor-width,16rem))] max-w-96 gap-0 overflow-hidden p-1"
          onClick={(e) => e.stopPropagation()}
        >
          {historySessions.length === 0 ? (
            <div className="px-2 py-1.5 text-caption text-muted-foreground">
              {t(($) => $.window.no_previous)}
            </div>
          ) : (
            <div role="group" aria-label={t(($) => $.window.history_group)}>
              <div className="px-1.5 py-1 text-caption font-medium text-muted-foreground">
                {t(($) => $.window.history_group)}
              </div>
              <SessionHistoryList
                sessions={historySessions}
                renderRow={renderRow}
                estimatedRowHeight={44}
                maxHeight={320}
              />
            </div>
          )}
        </PopoverContent>
      </Popover>
    </>
  );
}
