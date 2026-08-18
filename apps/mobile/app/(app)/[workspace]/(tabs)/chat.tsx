/**
 * Chat tab — single-screen IA.
 *
 * Layout:
 *   View ─ Header(center: ChatTitleButton, right: ChatSessionActions)
 *        ─ (NoAgentBanner?)
 *        ─ KeyboardAvoidingView ─ ChatMessageList (includes live status
 *                                                  + timeline in its
 *                                                  ListFooterComponent)
 *                                ─ OfflineBanner
 *                                ─ ChatComposer
 *
 * Session switching, agent selection, and session deletion all happen
 * inside this screen via Modal sheets — there is no `/chat/[id]` sub-route.
 *
 * State (all local, none in Zustand):
 *   - activeSessionId   — which session is being viewed (null = new chat blank)
 *   - selectedAgentId   — overrides currentSession.agent_id when set (used
 *                         when starting a new chat with a freshly-picked agent)
 *   - sessionSheetOpen  — bottom modal visibility
 *   - agentPickerOpen   — bottom modal visibility
 *
 * Side effects:
 *   - useChatSessionRealtime(activeSessionId) for per-record WS events
 *   - auto markRead when entering a session with has_unread
 *   - ensureSession dedupe ref for concurrent first-message sends
 *
 * Optimistic send burst mirrors web's chat-window.tsx send sequence
 * (packages/views/chat/components/chat-window.tsx ~262-345):
 *   seed messages → seed pendingTask → flip activeSessionId → POST →
 *   patch pendingTask with server task_id + created_at.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActionSheetIOS,
  Alert,
  KeyboardAvoidingView,
  Platform,
  View,
} from "react-native";
import { router } from "expo-router";
import { useFocusEffect, useIsFocused } from "@react-navigation/native";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNetInfo } from "@react-native-community/netinfo";
import type {
  Agent,
  ChatMessage,
  ChatPendingTask,
} from "@multica/core/types";
import {
  hideQueuedChatMessages,
  removePendingChatTask,
} from "@multica/core/chat/pending";
import { ApiError, api } from "@/data/api";
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
import { useChatOutboxStore } from "@/data/stores/chat-outbox-store";
import {
  createChatOutboxClientId,
  MAX_CHAT_OUTBOX_ATTEMPTS,
  nextChatOutboxItem,
  nextFailedChatOutboxItem,
  permanentlyFailedChatOutboxItem,
  type ChatOutboxItem,
} from "@/data/stores/chat-outbox";
import { useChatSessionPickerStore } from "@/data/stores/chat-session-picker-store";
import { useChatSessionRealtime } from "@/data/realtime/use-chat-session-realtime";
import {
  invalidatePendingTask,
  seedAcceptedPendingTask,
} from "@/data/realtime/chat-ws-updaters";
import { canAssignAgent } from "@/lib/can-assign-agent";
import { useWorkspaceAgentAvailability } from "@/lib/workspace-agent-availability";
import { useAgentPresence } from "@/lib/use-agent-presence";
import { Header } from "@/components/ui/header";
import { ChatTitleButton } from "@/components/chat/chat-title-button";
import { ChatSessionActions } from "@/components/chat/chat-session-actions";
import { ChatMessageList } from "@/components/chat/chat-message-list";
import { ChatComposer } from "@/components/chat/chat-composer";
import { AgentPickerSheet } from "@/components/chat/agent-picker-sheet";
import { NoAgentBanner } from "@/components/chat/no-agent-banner";
import { OfflineBanner } from "@/components/chat/offline-banner";
import { RuntimeRequiredBanner } from "@/components/chat/runtime-required-banner";
import { useChatSelectStore } from "@/data/chat-select-store";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";

export default function ChatTab() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);

  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [agentPickerOpen, setAgentPickerOpen] = useState(false);
  const [editingOutbox, setEditingOutbox] = useState<{
    clientId: string;
    draftKey: string;
    previousDraft: string;
  } | null>(null);

  // Bridge to the chat-sessions formSheet route. Mirror local
  // activeSessionId into the store so the picker can render the current
  // selection's check mark; consume the picker's one-shot select request
  // via useEffect.
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
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  const availableAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
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

  const availability = useWorkspaceAgentAvailability();
  const netInfo = useNetInfo();
  const presenceDetail = useAgentPresence(wsId, currentAgent?.id);
  const presenceAvailability =
    presenceDetail === "loading" ? undefined : presenceDetail.availability;
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
  const outboxItems = useChatOutboxStore((s) => s.items);
  const enqueueOutbox = useChatOutboxStore((s) => s.enqueue);
  const updateOutbox = useChatOutboxStore((s) => s.update);
  const removeOutbox = useChatOutboxStore((s) => s.remove);

  // Session switches cannot leave another session's queued text in the
  // composer. Put the draft that preceded editing back where it came from.
  useEffect(() => {
    if (!editingOutbox || editingOutbox.draftKey === draftKey) return;
    setDraft(editingOutbox.draftKey, editingOutbox.previousDraft);
    setEditingOutbox(null);
  }, [draftKey, editingOutbox, setDraft]);

  // ── Realtime ───────────────────────────────────────────────────────────
  useChatSessionRealtime(activeSessionId, () => {
    setActiveSessionId(null);
  });

  // Exit text-selection mode whenever the chat tab loses focus. Expo
  // Router bottom tabs stay mounted across tab switches, so a plain
  // useEffect cleanup wouldn't fire — useFocusEffect is the navigation-
  // aware equivalent.
  useFocusEffect(
    useCallback(() => () => useChatSelectStore.getState().clear(), []),
  );

  // ── Auto markRead while viewing a session with unread state ──────────
  const isFocused = useIsFocused();
  const markRead = useMarkChatSessionRead();
  useEffect(() => {
    if (!isFocused) return;
    if (!activeSessionId) return;
    if (!activeSession?.has_unread) return;
    markRead.mutate(activeSessionId);
  }, [isFocused, activeSessionId, activeSession?.has_unread, markRead]);

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

  const attemptOutboxSend = useCallback(
    async (sessionId: string, requestedClientId?: string) => {
      const head = nextChatOutboxItem(
        useChatOutboxStore.getState().items,
        sessionId,
      );
      if (!head) return;
      if (requestedClientId && head.clientId !== requestedClientId) {
        Alert.alert(
          "Send in order",
          "An earlier unsent message must be sent or removed first.",
        );
        return;
      }

      updateOutbox(head.clientId, (item) => ({
        ...item,
        status: "sending",
        lastError: null,
        nextAttemptAt: null,
      }));
      try {
        // A short send-specific ceiling avoids a false-online connection
        // leaving this visible message without feedback for thirty seconds.
        const result = await api.sendChatMessage(sessionId, head.content, {
          attachmentIds:
            head.attachmentIds.length > 0 ? head.attachmentIds : undefined,
          timeoutMs: 8_000,
        });
        removeOutbox(head.clientId);
        const sent: ChatMessage = {
          id: result.message_id,
          chat_session_id: sessionId,
          role: "user",
          content: head.content,
          task_id: result.task_id,
          created_at: result.created_at,
        };
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
          old?.some((message) => message.id === sent.id)
            ? old
            : [...(old ?? []), sent],
        );
        seedAcceptedPendingTask(qc, {
          chat_session_id: sessionId,
          task_id: result.task_id,
          created_at: result.created_at,
          message_id: result.message_id,
          content: head.content,
          optimistic_task_id: `outbox-${head.clientId}`,
          supports_queue: result.supports_queue,
          queued: result.queued,
        });
        void qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      } catch (error) {
        const message =
          error instanceof Error ? error.message : "Unable to send message";
        if (
          error instanceof ApiError &&
          error.status >= 400 &&
          error.status < 500
        ) {
          // Auth expiry must retain unsent content. Mark this local item
          // failed and let the user authenticate again before a manual retry.
          updateOutbox(head.clientId, (item) =>
            permanentlyFailedChatOutboxItem(item, message),
          );
          return;
        }
        updateOutbox(head.clientId, (item) =>
          nextFailedChatOutboxItem(
            item,
            `${message}. Check this chat before sending again.`,
          ),
        );
      }
    },
    [qc, removeOutbox, updateOutbox],
  );

  const handleSend = useCallback(
    async (
      content: string,
      attachmentIds: string[] = [],
      options: { clearDraft?: boolean } = {},
    ) => {
      if (!currentAgent) return;
      if (!runtimeBound) {
        Alert.alert(
          "Runtime required",
          "Bind a runtime to this agent on web or desktop before sending a message.",
        );
        return;
      }

      if (!activeSessionId && netInfo.isConnected === false) {
        Alert.alert(
          "Connect to start a new chat",
          "New chats need an internet connection before messages can be queued.",
        );
        return;
      }
      if (!userId || !wsSlug) return;

      if (editingOutbox) {
        const item = useChatOutboxStore
          .getState()
          .items.find((candidate) => candidate.clientId === editingOutbox.clientId);
        if (!item || item.status === "sending") {
          setEditingOutbox(null);
          return;
        }

        // An edited instruction is logically a new manual-delivery attempt.
        // Keep its queue position, but give it a new local id and clear retry
        // state so it cannot be mistaken for an earlier payload.
        updateOutbox(item.clientId, (current) => ({
          ...current,
          content,
          attachmentIds: [...new Set([...current.attachmentIds, ...attachmentIds])],
          clientId: createChatOutboxClientId(),
          status: "queued",
          attemptCount: 0,
          retryable: true,
          lastError: null,
          nextAttemptAt: null,
        }));
        clearDraft(editingOutbox.draftKey);
        setEditingOutbox(null);
        return;
      }

      const isNewSession = !activeSessionId;
      const sessionId = await ensureSession(content);
      if (!sessionId) return;
      if (isNewSession) {
        promoteNewDraft(sessionId);
        setActiveSessionId(sessionId);
      }

      const item: ChatOutboxItem = {
        clientId: createChatOutboxClientId(),
        sessionId,
        workspaceSlug: wsSlug,
        userId,
        content,
        attachmentIds,
        createdAt: new Date().toISOString(),
        status: "queued",
        attemptCount: 0,
        retryable: true,
        lastError:
          netInfo.isConnected === false ? "Waiting for a connection" : null,
        nextAttemptAt: null,
      };
      enqueueOutbox(item);
      if (options.clearDraft !== false) clearDraft(sessionId);
      if (netInfo.isConnected !== false) {
        await attemptOutboxSend(sessionId, item.clientId);
      }
    },
    [
      activeSessionId,
      attemptOutboxSend,
      currentAgent,
      runtimeBound,
      ensureSession,
      promoteNewDraft,
      clearDraft,
      enqueueOutbox,
      editingOutbox,
      updateOutbox,
      netInfo.isConnected,
      userId,
      wsSlug,
    ],
  );

  const activeOutboxItems = useMemo(
    () =>
      activeSessionId
        ? outboxItems.filter((item) => item.sessionId === activeSessionId)
        : [],
    [activeSessionId, outboxItems],
  );

  const editOutboxItem = useCallback(
    (item: ChatOutboxItem) => {
      if (item.status === "sending") return;
      setEditingOutbox({
        clientId: item.clientId,
        draftKey,
        previousDraft: draft,
      });
      setDraft(draftKey, item.content);
    },
    [draft, draftKey, setDraft],
  );

  const cancelOutboxEdit = useCallback(() => {
    if (!editingOutbox) return;
    setDraft(editingOutbox.draftKey, editingOutbox.previousDraft);
    setEditingOutbox(null);
  }, [editingOutbox, setDraft]);

  const handleOutboxPress = useCallback(
    (item: ChatOutboxItem) => {
      if (item.status === "sending") {
        Alert.alert(
          "Cancel local retry?",
          "This stops future local retries. It cannot recall a message that may already have reached the server.",
          [
            { text: "Keep sending", style: "cancel" },
            {
              text: "Cancel local retry",
              style: "destructive",
              onPress: () => removeOutbox(item.clientId),
            },
          ],
        );
        return;
      }
      const canRetry =
        item.retryable && item.attemptCount < MAX_CHAT_OUTBOX_ATTEMPTS;
      const options = canRetry
        ? ["Cancel", "Edit", "Send now", "Remove"]
        : ["Cancel", "Edit", "Remove"];
      const removeIndex = options.length - 1;
      ActionSheetIOS.showActionSheetWithOptions(
        {
          options,
          cancelButtonIndex: 0,
          destructiveButtonIndex: removeIndex,
        },
        (index) => {
          if (index === 1) {
            editOutboxItem(item);
            return;
          }
          if (canRetry && index === 2) {
            if (
              item.nextAttemptAt &&
              new Date(item.nextAttemptAt).getTime() > Date.now()
            ) {
              Alert.alert(
                "Try again shortly",
                "This message is backing off after its last failure.",
              );
              return;
            }
            void attemptOutboxSend(item.sessionId, item.clientId);
            return;
          }
          if (index === removeIndex) removeOutbox(item.clientId);
        },
      );
    },
    [attemptOutboxSend, editOutboxItem, removeOutbox],
  );

  // ── Cancel in-flight ───────────────────────────────────────────────────
  const handleStop = useCallback(() => {
    if (!pendingTask?.task_id || !activeSessionId) return;
    if (pendingTask.status === "queued") return;
    const taskId = pendingTask.task_id;
    const sessionId = activeSessionId;
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), (old) =>
      removePendingChatTask(old, taskId),
    );
    void api.cancelTaskById(taskId)
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

  // Apply the user's pick from the chat-sessions route (or "no session"
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
      activeSession.title || "Untitled chat",
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
    availability === "none" ||
    isArchived === true ||
    !runtimeBound;
  const disabledReason = !currentAgent
    ? "No agent selected"
    : availability === "none"
      ? "No agents in this workspace"
      : isArchived
        ? "This chat is archived"
        : !runtimeBound
          ? "Agent needs a runtime"
        : undefined;

  return (
    <View className="flex-1 bg-background">
      <Header
        center={
          <ChatTitleButton
            currentSession={activeSession}
            currentAgent={currentAgent}
            onPress={() => {
              if (!wsSlug) return;
              router.push({
                pathname: "/[workspace]/chat-sessions",
                params: { workspace: wsSlug },
              });
            }}
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
      {availability === "none" ? <NoAgentBanner /> : null}
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        className="flex-1"
      >
        <ChatMessageList
          messages={visibleMessages}
          outboxItems={activeOutboxItems}
          editingOutboxClientId={editingOutbox?.clientId}
          loading={messagesLoading}
          hasSessions={sessions.length > 0}
          agentName={currentAgent?.name}
          onPickPrompt={(text) => setDraft(draftKey, text)}
          onQuickAction={(action) =>
            handleSend(action.prompt, [], { clearDraft: false })
          }
          quickActionsDisabled={sending || disabled}
          pendingTask={pendingTask}
          liveTaskMessages={liveTaskMessages}
          availability={presenceAvailability}
          onOutboxPress={handleOutboxPress}
        />
        {runtimeBound ? (
          <OfflineBanner
            agentName={currentAgent?.name}
            availability={presenceAvailability}
          />
        ) : currentAgent ? (
          <RuntimeRequiredBanner agentName={currentAgent.name} />
        ) : null}
        <ChatComposer
          value={draft}
          onChangeText={(next) => setDraft(draftKey, next)}
          onSend={handleSend}
          onStop={handleStop}
          sending={sending}
          allowStop={pendingTask?.status !== "queued"}
          disabled={disabled}
          disabledReason={disabledReason}
          editingOutboxClientId={editingOutbox?.clientId}
          onCancelOutboxEdit={cancelOutboxEdit}
        />
      </KeyboardAvoidingView>

      <AgentPickerSheet
        visible={agentPickerOpen}
        agents={availableAgents}
        currentAgentId={currentAgent?.id ?? null}
        onPick={handlePickAgent}
        onClose={() => setAgentPickerOpen(false)}
      />
    </View>
  );
}
