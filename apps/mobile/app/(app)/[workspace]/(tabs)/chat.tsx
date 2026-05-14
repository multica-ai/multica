/**
 * Chat tab — single-screen IA.
 *
 * Layout:
 *   SafeAreaView ─ ChatHeader ─ (NoAgentBanner?) ─ ChatMessageList
 *                                                   └─ StatusPill
 *                                                   └─ ChatComposer
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
  Alert,
  KeyboardAvoidingView,
  Platform,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  Agent,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
} from "@multica/core/types";
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
import { useChatSessionRealtime } from "@/data/realtime/use-chat-session-realtime";
import { canAssignAgent } from "@/lib/can-assign-agent";
import { useWorkspaceAgentAvailability } from "@/lib/workspace-agent-availability";
import { ChatHeader } from "@/components/chat/chat-header";
import { ChatMessageList } from "@/components/chat/chat-message-list";
import { ChatComposer } from "@/components/chat/chat-composer";
import { StatusPill } from "@/components/chat/status-pill";
import { SessionSheet } from "@/components/chat/session-sheet";
import { AgentPickerSheet } from "@/components/chat/agent-picker-sheet";
import { NoAgentBanner } from "@/components/chat/no-agent-banner";

export default function ChatTab() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id);

  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [sessionSheetOpen, setSessionSheetOpen] = useState(false);
  const [agentPickerOpen, setAgentPickerOpen] = useState(false);

  // ── Server state ───────────────────────────────────────────────────────
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: messages = [], isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(activeSessionId),
  );
  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId),
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
  // active session; otherwise pick the first available agent so a fresh
  // workspace lands on the right header rather than "Chat" placeholder.
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
  const isArchived = activeSession?.status === "archived";
  const sending = !!pendingTask?.task_id;

  // ── Drafts ─────────────────────────────────────────────────────────────
  const draftKey = activeSessionId ?? DRAFT_NEW_SESSION;
  const draft = useChatDraftsStore((s) => s.drafts[draftKey] ?? "");
  const setDraft = useChatDraftsStore((s) => s.setDraft);
  const clearDraft = useChatDraftsStore((s) => s.clearDraft);
  const promoteNewDraft = useChatDraftsStore((s) => s.promoteNewDraft);

  // ── Realtime ───────────────────────────────────────────────────────────
  // Per-record subscription for the active session. If the session is
  // deleted by another client, drop the pointer so we land back on the
  // new-chat blank state instead of a phantom view.
  useChatSessionRealtime(activeSessionId, () => {
    setActiveSessionId(null);
  });

  // ── Auto markRead on entering a session with unread state ─────────────
  const markRead = useMarkChatSessionRead();
  const lastMarkedRef = useRef<string | null>(null);
  // mutate() triggers an optimistic setQueryData inside onMutate — that's a
  // cache write, and writing to a cache this component reads from during
  // render breaks React's purity contract (StrictMode in dev fires render
  // twice). Always run mutations from an effect.
  useEffect(() => {
    if (!activeSessionId) return;
    if (!activeSession?.has_unread) return;
    if (lastMarkedRef.current === activeSessionId) return;
    lastMarkedRef.current = activeSessionId;
    markRead.mutate(activeSessionId);
  }, [activeSessionId, activeSession?.has_unread, markRead]);

  // ── Mutations ──────────────────────────────────────────────────────────
  const createSession = useCreateChatSession();
  const deleteSession = useDeleteChatSession();

  // ── Send burst ─────────────────────────────────────────────────────────
  // Ensures a single in-flight createChatSession when the user fires
  // multiple sends back-to-back on a new chat.
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
    async (content: string) => {
      if (!currentAgent) return;

      const isNewSession = !activeSessionId;
      const sessionId = await ensureSession(content);
      if (!sessionId) return;

      // Optimistic burst — every visual cue lands before the HTTP
      // roundtrip so the user sees their message + StatusPill instantly.
      const sentAt = new Date().toISOString();
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: "user",
        content,
        task_id: null,
        created_at: sentAt,
      };
      // Seed messages cache BEFORE flipping activeSessionId so the
      // useQuery subscription doesn't render an empty/loading state for
      // one frame.
      qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
        old ? [...old, optimistic] : [optimistic],
      );
      // Seed pendingTask with a temporary id so StatusPill mounts and
      // starts ticking immediately. The real task_id arrives below.
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: `optimistic-${optimistic.id}`,
        status: "queued",
        created_at: sentAt,
      });
      if (isNewSession) {
        promoteNewDraft(sessionId);
        setActiveSessionId(sessionId);
      }

      try {
        const result = await api.sendChatMessage(sessionId, content);
        // Replace the temporary task_id with the server's authoritative
        // one + snap created_at so the StatusPill timer doesn't jump.
        qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
          task_id: result.task_id,
          status: "queued",
          created_at: result.created_at,
        });
        // Refetch messages to pick up the persisted user message with its
        // real id (replacing the `optimistic-*` placeholder).
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        clearDraft(sessionId);
      } catch (err) {
        // Roll back the optimistic message + pendingTask seed.
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
          old ? old.filter((m) => m.id !== optimistic.id) : old,
        );
        qc.setQueryData(chatKeys.pendingTask(sessionId), {});
        // Re-throw so ChatComposer restores the user's text into the
        // input (it catches and calls onChangeText to repopulate).
        throw err;
      }
    },
    [
      activeSessionId,
      currentAgent,
      ensureSession,
      qc,
      promoteNewDraft,
      clearDraft,
    ],
  );

  // ── Cancel in-flight ───────────────────────────────────────────────────
  const handleStop = useCallback(() => {
    if (!pendingTask?.task_id || !activeSessionId) return;
    // Optimistic clear — pill disappears immediately. WS task:cancelled
    // (eventual) will confirm. If the cancel POST fails because the task
    // already finished, the success path's WS chat:done already wrote
    // the assistant message and there's nothing to recover.
    qc.setQueryData(chatKeys.pendingTask(activeSessionId), {});
    void api.cancelTaskById(pendingTask.task_id).catch(() => {
      // Silent — task may have already terminated server-side.
    });
  }, [pendingTask?.task_id, activeSessionId, qc]);

  // ── Header / sheet actions ─────────────────────────────────────────────
  const handleNewChat = useCallback(() => {
    // Multi-agent → ask the user. Single-agent or none → just clear the
    // active session and let the empty state guide them.
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

  const handleSelectSession = useCallback((session: ChatSession) => {
    // Clearing selectedAgentId lets currentAgent inherit from the
    // session's agent_id (which may differ from what the picker last
    // showed).
    setSelectedAgentId(null);
    setActiveSessionId(session.id);
  }, []);

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

  const handleDeleteFromSheet = useCallback(
    (sessionId: string) => {
      if (sessionId === activeSessionId) {
        setActiveSessionId(null);
      }
      deleteSession.mutate(sessionId);
    },
    [activeSessionId, deleteSession],
  );

  // ── Composer disabled-state ────────────────────────────────────────────
  const disabled =
    !currentAgent || availability === "none" || isArchived === true;
  const disabledReason = !currentAgent
    ? "No agent selected"
    : availability === "none"
      ? "No agents in this workspace"
      : isArchived
        ? "This chat is archived"
        : undefined;

  return (
    <SafeAreaView
      className="flex-1 bg-background"
      edges={["top", "bottom"]}
    >
      <ChatHeader
        currentSession={activeSession}
        currentAgent={currentAgent}
        onTitlePress={() => setSessionSheetOpen(true)}
        onMorePress={handleDeleteActive}
        onNewPress={handleNewChat}
      />
      {availability === "none" ? <NoAgentBanner /> : null}
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        className="flex-1"
      >
        <View className="flex-1">
          <ChatMessageList messages={messages} loading={messagesLoading} />
        </View>
        <StatusPill pendingTask={pendingTask} onStop={handleStop} />
        <ChatComposer
          value={draft}
          onChangeText={(next) => setDraft(draftKey, next)}
          onSend={handleSend}
          onStop={handleStop}
          sending={sending}
          disabled={disabled}
          disabledReason={disabledReason}
        />
      </KeyboardAvoidingView>

      <SessionSheet
        visible={sessionSheetOpen}
        sessions={sessions}
        activeSessionId={activeSessionId}
        onSelectSession={handleSelectSession}
        onDeleteSession={handleDeleteFromSheet}
        onOpenAgentPicker={() => setAgentPickerOpen(true)}
        onClose={() => setSessionSheetOpen(false)}
      />
      <AgentPickerSheet
        visible={agentPickerOpen}
        agents={availableAgents}
        currentAgentId={currentAgent?.id ?? null}
        onPick={handlePickAgent}
        onClose={() => setAgentPickerOpen(false)}
      />
    </SafeAreaView>
  );
}
