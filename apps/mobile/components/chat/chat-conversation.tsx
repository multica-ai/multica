/**
 * Shared conversation body for both chat/[id] (existing session) and
 * chat/new (agent picked, no session yet). Renders the Stack header
 * (title + delete action) inline via <Stack.Screen>, same pattern as
 * issue/[id].tsx. See docs/superpowers/specs/2026-07-10-mobile-chat-tab-
 * navigation-design.md.
 *
 * "new" mode has no delete action (nothing exists yet to delete) and no
 * per-record realtime subscription (no session id to subscribe to).
 * Starting a fresh chat from within an open conversation isn't
 * supported here — that's the session list's "+" button now; this
 * component only manages the one conversation it's given.
 */
import { useCallback, useEffect, useMemo, useRef } from "react";
import { Alert, KeyboardAvoidingView, Platform, View } from "react-native";
import { router, Stack } from "expo-router";
import { useFocusEffect, useIsFocused } from "@react-navigation/native";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { Agent, ChatMessage, ChatPendingTask } from "@multica/core/types";
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
import { useChatSessionRealtime } from "@/data/realtime/use-chat-session-realtime";
import { canAssignAgent } from "@/lib/can-assign-agent";
import { useWorkspaceAgentAvailability } from "@/lib/workspace-agent-availability";
import { useAgentPresence } from "@/lib/use-agent-presence";
import { IconButton } from "@/components/ui/icon-button";
import { ChatTitleButton } from "@/components/chat/chat-title-button";
import { ChatMessageList } from "@/components/chat/chat-message-list";
import { ChatComposer } from "@/components/chat/chat-composer";
import { NoAgentBanner } from "@/components/chat/no-agent-banner";
import { OfflineBanner } from "@/components/chat/offline-banner";
import { useChatSelectStore } from "@/data/chat-select-store";

type Props =
  | { mode: "session"; sessionId: string }
  | { mode: "new"; agentId: string };

export function ChatConversationView(props: Props) {
  const { t } = useTranslation("chat");
  const { t: tCommon } = useTranslation("common");
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);

  const activeSessionId = props.mode === "session" ? props.sessionId : null;

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

  const currentAgent: Agent | null = useMemo(() => {
    if (props.mode === "new") {
      return availableAgents.find((a) => a.id === props.agentId) ?? null;
    }
    if (activeSession) {
      return agents.find((a) => a.id === activeSession.agent_id) ?? null;
    }
    return null;
  }, [props, availableAgents, activeSession, agents]);

  const availability = useWorkspaceAgentAvailability();
  const presenceDetail = useAgentPresence(wsId, currentAgent?.id);
  const presenceAvailability =
    presenceDetail === "loading" ? undefined : presenceDetail.availability;
  const isArchived = activeSession?.status === "archived";
  const sending = !!pendingTask?.task_id;

  // ── Drafts ─────────────────────────────────────────────────────────────
  const draftKey = activeSessionId ?? DRAFT_NEW_SESSION;
  const draft = useChatDraftsStore((s) => s.drafts[draftKey] ?? "");
  const setDraft = useChatDraftsStore((s) => s.setDraft);
  const clearDraft = useChatDraftsStore((s) => s.clearDraft);
  const promoteNewDraft = useChatDraftsStore((s) => s.promoteNewDraft);

  // ── Realtime ───────────────────────────────────────────────────────────
  // A no-op when activeSessionId is null (mode === "new") — same as the
  // original single-screen chat.tsx calling this unconditionally before
  // hydration picked a session.
  useChatSessionRealtime(activeSessionId, () => {
    if (wsSlug) router.replace(`/${wsSlug}/chat`);
  });

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

  const handleSend = useCallback(
    async (content: string, attachmentIds: string[] = []) => {
      if (!currentAgent) return;

      const isNewSession = !activeSessionId;
      const sessionId = await ensureSession(content);
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
      qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
        old ? [...old, optimistic] : [optimistic],
      );
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: `optimistic-${optimistic.id}`,
        status: "queued",
        created_at: sentAt,
      });
      if (isNewSession) {
        promoteNewDraft(sessionId);
        if (wsSlug) router.replace(`/${wsSlug}/chat/${sessionId}`);
      }

      try {
        const result = await api.sendChatMessage(sessionId, content, {
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
        });
        qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
          task_id: result.task_id,
          status: "queued",
          created_at: result.created_at,
        });
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        clearDraft(sessionId);
      } catch (err) {
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
          old ? old.filter((m) => m.id !== optimistic.id) : old,
        );
        qc.setQueryData(chatKeys.pendingTask(sessionId), {});
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
      wsSlug,
    ],
  );

  // ── Cancel in-flight ───────────────────────────────────────────────────
  const handleStop = useCallback(() => {
    if (!pendingTask?.task_id || !activeSessionId) return;
    qc.setQueryData(chatKeys.pendingTask(activeSessionId), {});
    void api.cancelTaskById(pendingTask.task_id).catch(() => {
      // Silent — task may have already terminated server-side.
    });
  }, [pendingTask?.task_id, activeSessionId, qc]);

  const handleDeleteActive = useCallback(() => {
    if (!activeSession) return;
    Alert.alert(
      t("delete_chat.title"),
      activeSession.title || t("untitled_chat"),
      [
        { text: tCommon("cancel"), style: "cancel" },
        {
          text: t("delete_chat.confirm"),
          style: "destructive",
          onPress: () => {
            deleteSession.mutate(activeSession.id);
            if (wsSlug) router.replace(`/${wsSlug}/chat`);
          },
        },
      ],
      { cancelable: true },
    );
  }, [activeSession, deleteSession, t, tCommon, wsSlug]);

  // ── Composer disabled-state ────────────────────────────────────────────
  const disabled =
    !currentAgent || availability === "none" || isArchived === true;
  const disabledReason = !currentAgent
    ? t("disabled_reason.no_agent")
    : availability === "none"
      ? t("disabled_reason.no_agents_workspace")
      : isArchived
        ? t("disabled_reason.archived")
        : undefined;

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          headerTitle: () => (
            <ChatTitleButton
              currentSession={activeSession}
              currentAgent={currentAgent}
            />
          ),
          headerRight:
            props.mode === "session"
              ? () => (
                  <IconButton
                    name="ellipsis-horizontal"
                    onPress={handleDeleteActive}
                    accessibilityLabel={t(
                      "session_actions.session_actions_label",
                    )}
                  />
                )
              : undefined,
        }}
      />
      {availability === "none" ? <NoAgentBanner /> : null}
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        className="flex-1"
      >
        <ChatMessageList
          messages={messages}
          loading={messagesLoading}
          hasSessions={sessions.length > 0}
          agentName={currentAgent?.name}
          onPickPrompt={(text) => setDraft(draftKey, text)}
          pendingTask={pendingTask}
          liveTaskMessages={liveTaskMessages}
          availability={presenceAvailability}
        />
        <OfflineBanner
          agentName={currentAgent?.name}
          availability={presenceAvailability}
        />
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
    </View>
  );
}
