/**
 * Chat tab — single-screen IA. HarmonyOS port of
 * apps/mobile/app/(app)/[workspace]/(tabs)/chat.tsx.
 *
 * Layout:
 *   View ─ Header(center: ChatTitleButton, right: ChatSessionActions)
 *        ─ (NoAgentBanner?)
 *        ─ KeyboardAvoidingView ─ ChatMessageList (includes live status
 *                                                  + timeline in its
 *                                                  list footer)
 *                                ─ (OfflineBanner | RuntimeRequiredBanner)
 *                                ─ ChatComposer
 *
 * Session switching (ChatSessionsSheet), agent selection (AgentPickerSheet)
 * and message long-press actions (ChatMessageLongPressMenu) are mounted
 * inside this screen — there is no /chat/[id] sub-route, matching iOS where
 * sessions lived on a formSheet route.
 *
 * State (all local, none in Zustand):
 *   - activeSessionId   — which session is being viewed (null = new chat blank)
 *   - selectedAgentId   — overrides currentSession.agent_id when set (used
 *                         when starting a new chat with a freshly-picked agent)
 *   - sessionsSheetOpen — session-switch sheet visibility
 *   - agentPickerOpen   — agent picker sheet visibility
 *   - menuMessage       — message whose long-press action sheet is open
 *
 * Side effects:
 *   - useChatSessionRealtime(activeSessionId) for per-record WS events
 *   - auto markRead while viewing a session with has_unread
 *   - ensureSession dedupe ref for concurrent first-message sends
 *
 * Optimistic send burst mirrors web's chat-window.tsx send sequence:
 * seed messages → seed pendingTask → flip activeSessionId → POST →
 * patch pendingTask with server task_id + created_at.
 *
 * Platform substitutions (RNOH 0.82):
 *   - Navigation is callback props, not expo-router. `onOpenAgents` wires
 *     the NoAgentBanner (iOS pushed `/${wsSlug}/more/agents`). The task's
 *     suggested `onOpenIssue` / `onOpenProject` callbacks are intentionally
 *     omitted: chat has no direct issue/project navigation (mention://
 *     links already flow through the shell's markdown navigator), so they
 *     would be dead props.
 *   - `active` stands in for iOS's useIsFocused: the hand-rolled TabView
 *     keeps visited tabs mounted (display:none) with no focus events, so
 *     the shell passes the tab's active flag; it defaults to true. It
 *     gates the auto mark-read effect and clears text-selection on blur —
 *     same semantics as iOS's useFocusEffect/useIsFocused pair.
 *   - Per-agent presence (iOS useAgentPresence) is not ported; availability
 *     stays `undefined` — the "loading silence" contract — so the offline
 *     banner and the StatusPill's Offline/Reconnecting stages stay
 *     suppressed until presence lands.
 *   - Alerts use RN Alert.alert (same pattern as the ported switch-workspace
 *     screen); delete-active keeps the iOS Cancel / destructive-Delete pair.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, StyleSheet, View } from "react-native";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  Agent,
  ChatMessage,
  ChatPendingTask,
} from "@multica/core/types";
import type { AgentAvailability } from "@multica/core/agents";
import {
  enqueuePendingChatTask,
  hideQueuedChatMessages,
  removePendingChatTask,
} from "@multica/core/chat/pending";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { api } from "@/data/api";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { agentListOptions } from "@/data/queries/agents";
import { memberListOptions } from "@/data/queries/members";
import {
  chatKeys,
  chatMessagesOptions,
  chatSessionsOptions,
  pendingChatTaskOptions,
  taskMessagesOptions,
} from "@/data/queries/chat";
import {
  useCreateChatSession,
  useDeleteChatSession,
  useMarkChatSessionRead,
} from "@/data/mutations/chat";
import {
  DRAFT_NEW_SESSION,
  useChatDraftsStore,
} from "@/data/stores/chat-drafts-store";
import { useChatSessionPickerStore } from "@/data/stores/chat-session-picker-store";
import { useChatSessionRealtime } from "@/data/realtime/use-chat-session-realtime";
import {
  invalidatePendingTask,
  seedAcceptedPendingTask,
} from "@/data/realtime/chat-ws-updaters";
import { useWorkspaceAgentAvailability } from "@/lib/workspace-agent-availability";
import { sendFailureMessage } from "@/lib/dispatch-reason";
import { useChatSelectStore } from "@/data/chat-select-store";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";
import { chatSessionDisplayTitle } from "@/lib/chat-session-title";
import { useThemeColors } from "@/lib/use-theme-colors";
import { Header } from "@/components/ui/header";
import { ChatTitleButton } from "@/components/chat/chat-title-button";
import { ChatSessionActions } from "@/components/chat/chat-session-actions";
import { ChatMessageList } from "@/components/chat/chat-message-list";
import { ChatComposer } from "@/components/chat/chat-composer";
import { AgentPickerSheet } from "@/components/chat/agent-picker-sheet";
import { ChatSessionsSheet } from "@/components/chat/chat-sessions-sheet";
import { ChatMessageLongPressMenu } from "@/components/chat/message-long-press";
import { NoAgentBanner } from "@/components/chat/no-agent-banner";
import { OfflineBanner } from "@/components/chat/offline-banner";
import { RuntimeRequiredBanner } from "@/components/chat/runtime-required-banner";

interface Props {
  /** Opens the agents list (iOS pushed `/${wsSlug}/more/agents`). */
  onOpenAgents?: () => void;
  /** Chat tab visibility (see module comment). Defaults to focused. */
  active?: boolean;
}

export function ChatScreen({ onOpenAgents, active = true }: Props) {
  const qc = useQueryClient();
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id);

  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [sessionsSheetOpen, setSessionsSheetOpen] = useState(false);
  const [agentPickerOpen, setAgentPickerOpen] = useState(false);
  const [menuMessage, setMenuMessage] = useState<ChatMessage | null>(null);

  // Bridge to the ChatSessionsSheet. Mirror local activeSessionId into the
  // store so the sheet can render the current selection's check mark;
  // consume the sheet's one-shot select request via useEffect.
  const setStoreActiveSessionId = useChatSessionPickerStore(
    (s) => s.setActiveSessionId,
  );
  const selectRequest = useChatSessionPickerStore((s) => s.selectRequest);
  const consumeSelect = useChatSessionPickerStore((s) => s.consumeSelect);
  useEffect(() => {
    setStoreActiveSessionId(activeSessionId);
  }, [activeSessionId, setStoreActiveSessionId]);

  // ── Server state ───────────────────────────────────────────────────────
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  // ── Auto-hydrate active session on first Chat tab entry ────────────────
  // Mobile-only deviation from web: web's chat-window opens to an empty
  // state when no `activeSessionId` is persisted; on a phone, picking
  // a session is 4 taps, so jump straight to the most recent session.
  // Hydration is one-shot per workspace.
  const hydratedWsRef = useRef<string | null>(null);
  useEffect(() => {
    if (!wsId) return;
    if (hydratedWsRef.current === wsId) return;
    if (sessions.length === 0) {
      hydratedWsRef.current = wsId;
      return;
    }
    hydratedWsRef.current = wsId;
    setActiveSessionId(sessions[0].id);
  }, [wsId, sessions]);
  const { data: messages = [], isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(activeSessionId),
  );
  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId),
  );
  const visibleMessages = hideQueuedChatMessages(messages, pendingTask);
  // Live execution trace for the in-flight task. `task:message` WS events
  // append rows to this same cache key via `appendTaskMessage`, so the
  // list/pill stay in sync without a polling fetch. `enabled` is gated by
  // `isTaskMessageTaskId` inside taskMessagesOptions — optimistic ids
  // never hit the network.
  const { data: liveTaskMessages = [] } = useQuery(
    taskMessagesOptions(pendingTask?.task_id),
  );

  // ── Derived ────────────────────────────────────────────────────────────
  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role ?? null,
    [members, userId],
  );

  // The picker must list only agents this user can actually TRIGGER — sending
  // a message enqueues a run, so it clears the server's invoke gate
  // (`canInvokeAgent`), which has no admin bypass. Shared rule, not a mobile
  // copy: a local mirror drifted from it and let admins pick a teammate's
  // personal agent only to be 403'd on send (MUL-6380 / GH #7180).
  const availableAgents = useMemo(
    () =>
      agents.filter(
        (a) =>
          !a.archived_at &&
          canAssignAgentToIssue(a, { userId: userId ?? null, role: memberRole })
            .allowed,
      ),
    [agents, userId, memberRole],
  );

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? null,
    [sessions, activeSessionId],
  );

  // Active agent: explicit selection wins; otherwise inherit from the
  // active session; otherwise pick the first available agent.
  const currentAgent: Agent | null = useMemo(() => {
    if (selectedAgentId) {
      return availableAgents.find((a) => a.id === selectedAgentId) ?? null;
    }
    if (activeSession) {
      return agents.find((a) => a.id === activeSession.agent_id) ?? null;
    }
    return availableAgents[0] ?? null;
  }, [selectedAgentId, availableAgents, activeSession, agents]);

  // A session outlives the permission that created it: the agent can be
  // flipped to personal, change owner, or drop this member from its
  // allow-list, and the server then refuses every send with
  // `invocation_not_allowed` while still serving the transcript (MUL-4525 —
  // read uses the view gate, send re-runs the invoke gate). `currentAgent`
  // deliberately resolves an open session's agent from the FULL list so the
  // header stays honest, which means the picker filter above cannot cover
  // this case — judge the bound agent too (MUL-6380).
  const accessRevoked =
    currentAgent !== null &&
    !canAssignAgentToIssue(currentAgent, {
      userId: userId ?? null,
      role: memberRole,
    }).allowed;

  const availability = useWorkspaceAgentAvailability();
  // Per-agent presence (iOS useAgentPresence) is not ported on this slice.
  // `undefined` keeps the "loading silence" contract: no speculative
  // Offline copy, no Offline/Reconnecting pill stages.
  const presenceAvailability: AgentAvailability | undefined = undefined;
  const isArchived = activeSession?.status === "archived";
  const runtimeBound =
    currentAgent !== null && isAgentRuntimeBound(currentAgent);
  const sending = !!pendingTask?.task_id;

  // ── Drafts ─────────────────────────────────────────────────────────────
  const draftKey = activeSessionId ?? DRAFT_NEW_SESSION;
  const draft = useChatDraftsStore((s) => s.drafts[draftKey] ?? "");
  const setDraft = useChatDraftsStore((s) => s.setDraft);
  const clearDraft = useChatDraftsStore((s) => s.clearDraft);
  const promoteNewDraft = useChatDraftsStore((s) => s.promoteNewDraft);

  // ── Realtime ───────────────────────────────────────────────────────────
  useChatSessionRealtime(activeSessionId, () => {
    setActiveSessionId(null);
  });

  // Exit text-selection mode whenever the chat tab loses focus or unmounts.
  // The hand-rolled TabView keeps visited tabs mounted across switches, so
  // a plain unmount cleanup wouldn't fire on tab switch — the `active` flag
  // is the navigation-aware equivalent of iOS's useFocusEffect.
  useEffect(() => {
    if (!active) useChatSelectStore.getState().clear();
  }, [active]);
  useEffect(() => {
    return () => useChatSelectStore.getState().clear();
  }, []);

  // ── Auto markRead while viewing a session with unread state ──────────
  const markRead = useMarkChatSessionRead();
  useEffect(() => {
    if (!active) return;
    if (!activeSessionId) return;
    if (!activeSession?.has_unread) return;
    markRead.mutate(activeSessionId);
  }, [active, activeSessionId, activeSession?.has_unread, markRead]);

  // ── Mutations ──────────────────────────────────────────────────────────
  const createSession = useCreateChatSession();
  const deleteSession = useDeleteChatSession();

  // ── Send burst ─────────────────────────────────────────────────────────
  const sessionPromiseRef = useRef<Promise<string | null> | null>(null);

  const ensureSession = useCallback(
    async (titleSeed: string): Promise<string | null> => {
      if (activeSessionId) return activeSessionId;
      if (!currentAgent) return null;
      if (sessionPromiseRef.current) return sessionPromiseRef.current;

      const promise = (async () => {
        try {
          const session = await createSession.mutateAsync({
            agent_id: currentAgent.id,
            title: titleSeed.slice(0, 50),
          });
          return session.id;
        } finally {
          sessionPromiseRef.current = null;
        }
      })();
      sessionPromiseRef.current = promise;
      return promise;
    },
    [activeSessionId, currentAgent, createSession],
  );

  const handleSend = useCallback(
    async (
      content: string,
      attachmentIds: string[] = [],
      options: { clearDraft?: boolean } = {},
    ) => {
      if (!currentAgent) return;
      // Invoke permission was revoked while this session was open — the
      // server would refuse before persisting anything. The composer is
      // disabled in this state; this is the belt-and-braces guard.
      if (accessRevoked) {
        Alert.alert(
          "No permission to run this agent",
          "You no longer have permission to run this agent, so the message was not sent. Ask its owner for access.",
        );
        return;
      }
      if (!runtimeBound) {
        Alert.alert(
          "Runtime required",
          "Bind a runtime to this agent on web or desktop before sending a message.",
        );
        return;
      }

      const isNewSession = !activeSessionId;
      let sessionId: string | null;
      try {
        sessionId = await ensureSession(content);
      } catch (err) {
        // Session create runs the same invoke gate as a send, so a
        // permission change refuses here too — and this is the only layer
        // that sees the reason code (MUL-6380).
        Alert.alert("Message not sent", sendFailureMessage(err));
        throw err;
      }
      if (!sessionId) return;

      const sentAt = new Date().toISOString();
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: "user",
        content,
        task_id: null,
        created_at: sentAt,
      };
      const optimisticTaskId = `optimistic-${optimistic.id}`;
      qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
        old ? [...old, optimistic] : [optimistic],
      );
      qc.setQueryData<ChatPendingTask>(
        chatKeys.pendingTask(sessionId),
        (old) =>
          enqueuePendingChatTask(
            old,
            {
              task_id: optimisticTaskId,
              status: "queued",
              created_at: sentAt,
              message_id: optimistic.id,
              content,
            },
            Boolean(old?.task_id),
          ),
      );
      if (isNewSession) {
        promoteNewDraft(sessionId);
        setActiveSessionId(sessionId);
      }

      try {
        const result = await api.sendChatMessage(sessionId, content, {
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
        });
        // Replace the local bubble before reconciling pending state. When
        // the server says this is a follow-up, its real message id lets the
        // shared queue filter hide it immediately instead of waiting for
        // the refetch.
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
          old?.map((message) =>
            message.id === optimistic.id
              ? {
                  ...message,
                  id: result.message_id,
                  task_id: result.task_id,
                  created_at: result.created_at,
                }
              : message,
          ),
        );
        seedAcceptedPendingTask(qc, {
          chat_session_id: sessionId,
          task_id: result.task_id,
          created_at: result.created_at,
          message_id: result.message_id,
          content,
          optimistic_task_id: optimisticTaskId,
          supports_queue: result.supports_queue,
          queued: result.queued,
        });
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        if (options.clearDraft !== false) {
          clearDraft(sessionId);
        }
      } catch (err) {
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
          old ? old.filter((m) => m.id !== optimistic.id) : old,
        );
        qc.setQueryData<ChatPendingTask>(
          chatKeys.pendingTask(sessionId),
          (old) => removePendingChatTask(old, optimisticTaskId),
        );
        // The composer restores the draft on a thrown rejection but says
        // nothing about it, so a revoked-permission 403 used to read as a
        // silent no-op (MUL-6380). Name the cause here: only this layer
        // sees the error body.
        Alert.alert("Message not sent", sendFailureMessage(err));
        throw err;
      }
    },
    [
      activeSessionId,
      currentAgent,
      accessRevoked,
      runtimeBound,
      ensureSession,
      qc,
      promoteNewDraft,
      clearDraft,
    ],
  );

  // ── Cancel in-flight ───────────────────────────────────────────────────
  const handleStop = useCallback(() => {
    if (!pendingTask?.task_id || !activeSessionId) return;
    if (pendingTask.status === "queued") return;
    const taskId = pendingTask.task_id;
    const sessionId = activeSessionId;
    qc.setQueryData<ChatPendingTask>(
      chatKeys.pendingTask(sessionId),
      (old) => removePendingChatTask(old, taskId),
    );
    void api
      .cancelTaskById(taskId)
      .catch(() => {
        // Silent — task may have already terminated server-side.
      })
      .finally(() => invalidatePendingTask(qc, sessionId));
  }, [pendingTask?.task_id, pendingTask?.status, activeSessionId, qc]);

  // ── Header / sheet actions ─────────────────────────────────────────────
  const handleNewChat = useCallback(() => {
    if (availableAgents.length > 1) {
      setAgentPickerOpen(true);
      return;
    }
    setSelectedAgentId(null);
    setActiveSessionId(null);
  }, [availableAgents.length]);

  const handlePickAgent = useCallback((agent: Agent) => {
    setSelectedAgentId(agent.id);
    setActiveSessionId(null);
  }, []);

  // Apply the user's pick from the sessions sheet (or "no session"
  // when they delete the active one in the sheet).
  useEffect(() => {
    if (!selectRequest) return;
    setSelectedAgentId(null);
    setActiveSessionId(selectRequest.id);
    consumeSelect();
  }, [selectRequest, consumeSelect]);

  const handleDeleteActive = useCallback(() => {
    if (!activeSession) return;
    Alert.alert(
      "Delete this chat?",
      chatSessionDisplayTitle(activeSession.title),
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => {
            const id = activeSession.id;
            setActiveSessionId(null);
            deleteSession.mutate(id);
          },
        },
      ],
      { cancelable: true },
    );
  }, [activeSession, deleteSession]);

  // ── Composer disabled-state ────────────────────────────────────────────
  const disabled =
    !currentAgent ||
    accessRevoked ||
    availability === "none" ||
    isArchived === true ||
    !runtimeBound;
  const disabledReason = !currentAgent
    ? "No agent selected"
    : accessRevoked
      ? "You can no longer run this agent"
      : availability === "none"
        ? "No agents in this workspace"
        : isArchived
          ? "This chat is archived"
          : !runtimeBound
            ? "Agent needs a runtime"
          : undefined;

  const keyboardHeight = useKeyboardHeight();
  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <Header
        center={
          <ChatTitleButton
            currentSession={activeSession}
            currentAgent={currentAgent}
            onPress={() => setSessionsSheetOpen(true)}
          />
        }
        right={
          <ChatSessionActions
            showMore={!!activeSession}
            onMorePress={handleDeleteActive}
            onNewPress={handleNewChat}
          />
        }
      />
      {availability === "none" ? <NoAgentBanner onOpenAgents={onOpenAgents} /> : null}
      <View style={[styles.flex, { paddingBottom: keyboardHeight }]}>
        <ChatMessageList
          messages={visibleMessages}
          loading={messagesLoading}
          hasSessions={sessions.length > 0}
          agent={currentAgent}
          onPickPrompt={(text) => setDraft(draftKey, text)}
          onQuickAction={(action) =>
            handleSend(action.prompt, [], { clearDraft: false })
          }
          quickActionsDisabled={sending || disabled}
          pendingTask={pendingTask}
          liveTaskMessages={liveTaskMessages}
          availability={presenceAvailability}
          menuMessageId={menuMessage?.id ?? null}
          onMessageLongPress={setMenuMessage}
        />
        {runtimeBound ? (
          <OfflineBanner
            agentName={currentAgent?.name}
            availability={presenceAvailability}
          />
        ) : currentAgent ? (
          <RuntimeRequiredBanner agentName={currentAgent.name} />
        ) : null}
        {/* iOS shows the composer flush above its translucent blurred tab
            bar; the opaque harmony bar needs explicit breathing room or the
            pill reads as overlapping the bar's hairline. */}
        <View style={{ paddingBottom: 8 }}>
          <ChatComposer
            value={draft}
            onChangeText={(next) => setDraft(draftKey, next)}
            onSend={handleSend}
            onStop={handleStop}
            sending={sending}
            allowStop={pendingTask?.status !== "queued"}
            disabled={disabled}
            disabledReason={disabledReason}
          />
        </View>
      </View>

      <AgentPickerSheet
        visible={agentPickerOpen}
        agents={availableAgents}
        currentAgentId={currentAgent?.id ?? null}
        onPick={handlePickAgent}
        onClose={() => setAgentPickerOpen(false)}
      />
      <ChatSessionsSheet
        visible={sessionsSheetOpen}
        onClose={() => setSessionsSheetOpen(false)}
      />
      <ChatMessageLongPressMenu
        message={menuMessage}
        visible={menuMessage !== null}
        onClose={() => setMenuMessage(null)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  flex: { flex: 1 },
});
