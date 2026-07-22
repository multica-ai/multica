# Mobile Chat Tab List-First Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Flip the mobile Chat tab from "lands on the most recent
conversation" to "lands on a session list; tapping a session pushes into
its conversation" — a real `chat/[id]` route replaces the current
single-screen-plus-formSheet architecture.

**Architecture:** Extract the existing conversation logic (message list,
composer, send/optimistic-update, drafts, realtime, delete) out of the
tab-root screen into a shared `ChatConversationView` component, consumed
by two thin pushed routes: `chat/[id]` (existing session) and `chat/new`
(picked an agent, no session yet — creates one lazily on first send, then
`router.replace`s to the real route). The tab root becomes a session-list
page. The old `chat-sessions` formSheet and its cross-screen picker store
are deleted — real navigation replaces the bridge they existed for.

**Tech Stack:** Expo Router (pushed Stack screens + inline `Stack.Screen`
header overrides, same pattern as `issue/[id].tsx`), TanStack Query,
react-i18next, NativeWind.

## Global Constraints

- No content enhancements to list rows — exactly what the current
  `chat-sessions.tsx` sheet shows today (agent avatar with presence dot,
  title, unread dot, archived label), nothing added.
- No changes to send/optimistic-update/draft/auto-markRead/realtime
  business logic — only where each piece is mounted changes.
- No changes to the tab-bar unread badge wiring
  (`useChatUnreadSessionCount` in `(tabs)/_layout.tsx`) — untouched by
  this plan.
- `AgentPickerSheet`, `ChatMessageList`, `ChatComposer`, `NoAgentBanner`,
  `OfflineBanner` are reused as-is, unmodified.
- This is a mobile-only UX divergence from web/desktop's dropdown-based
  session switcher (confirmed neither has a full session-list page
  either) — acceptable per `apps/mobile/CLAUDE.md`'s "Behavioral parity"
  rule since it changes UI/interaction, not product semantics (same
  sessions, same unread state, same deletion capability).
- Per root `CLAUDE.md`: since the old sheet route has no external
  consumers, delete it outright rather than keeping a dead path.
- Every new/moved source file must be `git ls-files`-tracked before its
  task's commit (apps/mobile/CLAUDE.md lesson #2).

---

### Task 1: Shared `ChatConversationView` component

**Files:**
- Create: `apps/mobile/components/chat/chat-conversation.tsx`
- Modify: `apps/mobile/components/chat/chat-title-button.tsx`

**Interfaces:**
- Consumes: `chatSessionsOptions`, `chatMessagesOptions`,
  `pendingChatTaskOptions`, `taskMessagesOptions`, `chatKeys` (all
  pre-existing, `@/data/queries/chat`), `agentListOptions`
  (`@/data/queries/agents`), `memberListOptions` (`@/data/queries/members`),
  `useCreateChatSession`/`useDeleteChatSession`/`useMarkChatSessionRead`
  (`@/data/mutations/chat`), `DRAFT_NEW_SESSION`/`useChatDraftsStore`
  (`@/data/stores/chat-drafts-store`), `useChatSessionRealtime`
  (`@/data/realtime/use-chat-session-realtime`, signature
  `(sessionId: string | null, onSessionDeleted?: () => void)`),
  `canAssignAgent` (`@/lib/can-assign-agent`),
  `useWorkspaceAgentAvailability` (`@/lib/workspace-agent-availability`),
  `useAgentPresence` (`@/lib/use-agent-presence`), `useChatSelectStore`
  (`@/data/chat-select-store`), `ChatMessageList`
  (`@/components/chat/chat-message-list`, props: `messages`, `loading`,
  `hasSessions`, `agentName?`, `onPickPrompt`, `pendingTask?`,
  `liveTaskMessages`, `availability`), `ChatComposer`
  (`@/components/chat/chat-composer`, props: `value`, `onChangeText`,
  `onSend(content, attachmentIds)`, `onStop`, `sending`, `disabled`,
  `disabledReason`), `NoAgentBanner` (`@/components/chat/no-agent-banner`,
  no props), `OfflineBanner` (`@/components/chat/offline-banner`, props:
  `agentName?`, `availability`).
- Produces: `export function ChatConversationView(props: { mode: "session"; sessionId: string } | { mode: "new"; agentId: string })`
  — Task 2's two route files render this directly.

- [ ] **Step 1: Make `ChatTitleButton`'s `onPress` optional**

Read `apps/mobile/components/chat/chat-title-button.tsx` first — it
currently requires `onPress: () => void` and always wraps content in a
`Pressable`. Replace the whole file with:

```tsx
/**
 * Centred title region for a chat screen's native Stack header — shows
 * the current agent's avatar + name and the session title/subtitle.
 * `onPress` is optional: `chat/[id]` and `chat/new` render this
 * non-interactively (there's no sheet left to open — the native back
 * button already returns to the session list).
 */
import { Pressable, View } from "react-native";
import { useTranslation } from "react-i18next";
import type { Agent, ChatSession } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";

interface Props {
  currentSession: ChatSession | null;
  currentAgent: Agent | null;
  onPress?: () => void;
}

export function ChatTitleButton({
  currentSession,
  currentAgent,
  onPress,
}: Props) {
  const { t } = useTranslation("chat");
  const agentName = currentAgent?.name ?? t("title_button.default_agent_name");
  const subtitle = currentSession?.title || t("title_button.new_chat_subtitle");

  const content = (
    <View className="flex-row items-center gap-2 px-2 py-1 rounded-lg">
      <ActorAvatar
        type={currentAgent ? "agent" : null}
        id={currentAgent?.id ?? null}
        size={24}
        showPresence
      />
      <View>
        <Text
          className="text-base font-semibold text-foreground"
          numberOfLines={1}
        >
          {agentName}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {subtitle}
        </Text>
      </View>
    </View>
  );

  if (!onPress) return content;

  return (
    <Pressable
      onPress={onPress}
      hitSlop={4}
      className="active:bg-secondary rounded-lg"
      accessibilityRole="button"
      accessibilityLabel={t("title_button.accessibility_label")}
    >
      {content}
    </Pressable>
  );
}
```

Note: the `▼` dropdown-affordance glyph is dropped along with the
tap-to-open-sheet behavior it signaled — there's nothing left to open.

- [ ] **Step 2: Build `ChatConversationView`**

Create `apps/mobile/components/chat/chat-conversation.tsx`:

```tsx
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
```

- [ ] **Step 3: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint components/chat/chat-conversation.tsx components/chat/chat-title-button.tsx
```

Expected: both commands exit 0. Making `onPress` optional is backward
compatible — the old `(tabs)/chat.tsx` still passes it today and keeps
compiling unchanged; it isn't touched until Task 3.

- [ ] **Step 4: Commit**

```bash
git add apps/mobile/components/chat/chat-conversation.tsx apps/mobile/components/chat/chat-title-button.tsx
git status --short   # confirm chat-conversation.tsx is tracked, not ignored
git commit -m "feat(mobile): add shared ChatConversationView component"
```

---

### Task 2: Pushed routes `chat/[id]` and `chat/new`

**Files:**
- Create: `apps/mobile/app/(app)/[workspace]/chat/[id].tsx`
- Create: `apps/mobile/app/(app)/[workspace]/chat/new.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`

**Interfaces:**
- Consumes: `ChatConversationView` (Task 1,
  `@/components/chat/chat-conversation`).
- Produces: the routes `chat/[id]` and `chat/new` that Task 3's list page
  navigates to.

- [ ] **Step 1: Create the existing-session route**

Create `apps/mobile/app/(app)/[workspace]/chat/[id].tsx`:

```tsx
/**
 * Chat conversation for an existing session. Thin route wrapper — all
 * rendering and logic lives in ChatConversationView, shared with
 * chat/new.tsx.
 */
import { useLocalSearchParams } from "expo-router";
import { ChatConversationView } from "@/components/chat/chat-conversation";

export default function ChatSessionPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <ChatConversationView mode="session" sessionId={id} />;
}
```

- [ ] **Step 2: Create the new-chat route**

Create `apps/mobile/app/(app)/[workspace]/chat/new.tsx`:

```tsx
/**
 * Chat conversation for a not-yet-created session — reached from the
 * session list's "+" button (or its agent picker) with a chosen agent
 * but no session yet. On first send, ChatConversationView creates the
 * session and router.replace()s to chat/[id].
 */
import { useLocalSearchParams } from "expo-router";
import { ChatConversationView } from "@/components/chat/chat-conversation";

export default function ChatNewPage() {
  const { agentId } = useLocalSearchParams<{ agentId: string }>();
  return <ChatConversationView mode="new" agentId={agentId} />;
}
```

- [ ] **Step 3: Register both routes**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, find this exact block:

```tsx
        {/* Chat session-switch sheet. */}
        <Stack.Screen name="chat-sessions" options={SHEET_OPTIONS} />
```

Add directly after it:

```tsx
        <Stack.Screen
          name="chat/[id]"
          options={{
            title: tChat("title_button.default_agent_name"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
        <Stack.Screen
          name="chat/new"
          options={{
            title: tChat("title_button.default_agent_name"),
            headerBackTitle: tCommon("nav.back"),
          }}
        />
```

(`chat-sessions` itself is removed in Task 4 — leave it in place for now
so this task's diff stays additive and independently testable.)

- [ ] **Step 4: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint "app/(app)/[workspace]/chat/[id].tsx" "app/(app)/[workspace]/chat/new.tsx" "app/(app)/[workspace]/_layout.tsx"
```

Expected: both commands exit 0.

- [ ] **Step 5: Commit**

```bash
git add "apps/mobile/app/(app)/[workspace]/chat/[id].tsx" "apps/mobile/app/(app)/[workspace]/chat/new.tsx" "apps/mobile/app/(app)/[workspace]/_layout.tsx"
git status --short   # confirm both new route files are tracked
git commit -m "feat(mobile): add chat/[id] and chat/new pushed routes"
```

---

### Task 3: Session-list landing page

**Files:**
- Modify: `apps/mobile/app/(app)/[workspace]/(tabs)/chat.tsx` (full
  rewrite)

**Interfaces:**
- Consumes: `chatSessionsOptions` (`@/data/queries/chat`),
  `agentListOptions` (`@/data/queries/agents`), `memberListOptions`
  (`@/data/queries/members`), `useDeleteChatSession`
  (`@/data/mutations/chat`), `canAssignAgent` (`@/lib/can-assign-agent`),
  `AgentPickerSheet` (`@/components/chat/agent-picker-sheet`, props:
  `visible`, `agents`, `currentAgentId`, `onPick(agent)`, `onClose`),
  `Header` (`@/components/ui/header`, props: `title`, `right`),
  `IconButton` (`@/components/ui/icon-button`, props: `name`, `iconSize?`,
  `onPress`, `accessibilityLabel`), pushes to `chat/[id]` and `chat/new`
  (Task 2).
- Produces: nothing consumed by a later task in this plan.

- [ ] **Step 1: Rewrite the Chat tab as the session list**

Replace the entire contents of
`apps/mobile/app/(app)/[workspace]/(tabs)/chat.tsx` with:

```tsx
/**
 * Chat tab — session list (landing page). Tapping a row pushes into that
 * session's conversation (chat/[id]); "+" starts a new one (agent picker
 * when the workspace has more than one usable agent, else straight to
 * chat/new for the sole agent). Long-press a row to delete. See
 * docs/superpowers/specs/2026-07-10-mobile-chat-tab-navigation-design.md.
 *
 * No "currently selected" checkmark on rows (the old chat-sessions sheet
 * had one, to reflect the single-screen chat's background session) —
 * there's no such concept here; each row is an independent navigation
 * target.
 */
import { useState } from "react";
import { Alert, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { Agent, ChatSession } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Header } from "@/components/ui/header";
import { IconButton } from "@/components/ui/icon-button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { AgentPickerSheet } from "@/components/chat/agent-picker-sheet";
import { chatSessionsOptions } from "@/data/queries/chat";
import { agentListOptions } from "@/data/queries/agents";
import { memberListOptions } from "@/data/queries/members";
import { useDeleteChatSession } from "@/data/mutations/chat";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { canAssignAgent } from "@/lib/can-assign-agent";
import { cn } from "@/lib/utils";

export default function ChatListPage() {
  const { t } = useTranslation("chat");
  const { t: tCommon } = useTranslation("common");
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);

  const [agentPickerOpen, setAgentPickerOpen] = useState(false);

  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const deleteSession = useDeleteChatSession();

  const memberRole = members.find((m) => m.user_id === userId)?.role;
  const availableAgents = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
  );

  const goNewChat = (agentId: string) => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/chat/new",
      params: { workspace: wsSlug, agentId },
    });
  };

  const handleNewPress = () => {
    if (availableAgents.length > 1) {
      setAgentPickerOpen(true);
      return;
    }
    if (availableAgents[0]) goNewChat(availableAgents[0].id);
  };

  const confirmDelete = (session: ChatSession) => {
    Alert.alert(
      t("delete_chat.title"),
      session.title || t("untitled_chat"),
      [
        { text: tCommon("cancel"), style: "cancel" },
        {
          text: t("delete_chat.confirm"),
          style: "destructive",
          onPress: () => deleteSession.mutate(session.id),
        },
      ],
      { cancelable: true },
    );
  };

  return (
    <View className="flex-1 bg-background">
      <Header
        title={tCommon("tabs.chat")}
        right={
          <IconButton
            name="add"
            iconSize={24}
            onPress={handleNewPress}
            accessibilityLabel={t("session_actions.new_chat_label")}
          />
        }
      />
      <ScrollView className="flex-1" showsVerticalScrollIndicator={false}>
        {sessions.length === 0 ? (
          <View className="px-4 py-8">
            <Text className="text-sm text-muted-foreground text-center">
              {t("sessions.empty")}
            </Text>
          </View>
        ) : (
          sessions.map((session) => {
            const archived = session.status === "archived";
            return (
              <Pressable
                key={session.id}
                onPress={() => {
                  if (!wsSlug) return;
                  router.push({
                    pathname: "/[workspace]/chat/[id]",
                    params: { workspace: wsSlug, id: session.id },
                  });
                }}
                onLongPress={() => confirmDelete(session)}
                className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
              >
                <View
                  className={cn(
                    "h-2 w-2 rounded-full",
                    session.has_unread ? "bg-primary" : "bg-transparent",
                  )}
                />
                <ActorAvatar
                  type="agent"
                  id={session.agent_id}
                  size={32}
                  showPresence
                />
                <View className="flex-1">
                  <Text
                    className={cn(
                      "text-sm text-foreground",
                      session.has_unread && "font-semibold",
                    )}
                    numberOfLines={1}
                  >
                    {session.title || t("untitled_chat")}
                  </Text>
                  {archived ? (
                    <Text className="text-xs text-muted-foreground mt-0.5">
                      {t("sessions.archived_label")}
                    </Text>
                  ) : null}
                </View>
              </Pressable>
            );
          })
        )}
      </ScrollView>

      <AgentPickerSheet
        visible={agentPickerOpen}
        agents={availableAgents}
        currentAgentId={null}
        onPick={(agent: Agent) => {
          setAgentPickerOpen(false);
          goNewChat(agent.id);
        }}
        onClose={() => setAgentPickerOpen(false)}
      />
    </View>
  );
}
```

- [ ] **Step 2: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint "app/(app)/[workspace]/(tabs)/chat.tsx"
```

Expected: both commands exit 0. Any remaining reference to
`ChatSessionActions` or `chat-session-picker-store` from the old
`chat.tsx` is gone now — `tsc` will flag `chat-sessions.tsx` and
`chat-session-picker-store.ts` themselves as now-orphaned (unreferenced
by anything except each other and `_layout.tsx`'s reset wiring) but they
still compile; Task 4 deletes them.

- [ ] **Step 3: Commit**

```bash
git add "apps/mobile/app/(app)/[workspace]/(tabs)/chat.tsx"
git commit -m "feat(mobile): rewrite Chat tab as the session list"
```

---

### Task 4: Remove the old sheet, picker store, and their wiring

**Files:**
- Delete: `apps/mobile/app/(app)/[workspace]/chat-sessions.tsx`
- Delete: `apps/mobile/data/stores/chat-session-picker-store.ts`
- Delete: `apps/mobile/components/chat/chat-session-actions.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/_layout.tsx`
- Modify: `apps/mobile/components/ui/header.tsx` (doc comment only)

**Interfaces:** none — this task only removes now-dead code.

- [ ] **Step 1: Delete the old sheet route**

```bash
git rm "apps/mobile/app/(app)/[workspace]/chat-sessions.tsx"
```

- [ ] **Step 2: Delete the picker store**

```bash
git rm apps/mobile/data/stores/chat-session-picker-store.ts
```

- [ ] **Step 2b: Delete the now-orphaned `ChatSessionActions` component**

After Task 3 rewrote `(tabs)/chat.tsx` to build its own header actions
inline (and Task 1's `ChatConversationView` built its own "⋯" action
inline), nothing imports `ChatSessionActions` anymore — confirm with:

```bash
grep -rn "chat-session-actions\|ChatSessionActions" apps/mobile --include="*.tsx" --include="*.ts"
```

Expected: only the component's own file and one doc-comment mention in
`apps/mobile/components/ui/header.tsx` (fixed in Step 2c below). Then:

```bash
git rm apps/mobile/components/chat/chat-session-actions.tsx
```

- [ ] **Step 2c: Fix the stale doc-comment example in `header.tsx`**

In `apps/mobile/components/ui/header.tsx`, find:

```tsx
 *   <Header title="Inbox" right={<HeaderActions />} />
 *   <Header center={<ChatTitleButton ... />} right={<ChatSessionActions ... />} />
```

Replace with:

```tsx
 *   <Header title="Inbox" right={<HeaderActions />} />
 *   <Header title="Chat" right={<IconButton name="add" ... />} />
```

- [ ] **Step 3: Remove the store's import and reset wiring**

In `apps/mobile/app/(app)/[workspace]/_layout.tsx`, find:

```tsx
import { useChatSessionPickerResetOnWorkspaceChange } from "@/data/stores/chat-session-picker-store";
```

Delete that line entirely.

Find:

```tsx
  useNewIssueDraftResetOnWorkspaceChange(matched?.id ?? null);
  useNewProjectDraftResetOnWorkspaceChange(matched?.id ?? null);
  useChatSessionPickerResetOnWorkspaceChange(matched?.id ?? null);
```

Replace with:

```tsx
  useNewIssueDraftResetOnWorkspaceChange(matched?.id ?? null);
  useNewProjectDraftResetOnWorkspaceChange(matched?.id ?? null);
```

- [ ] **Step 4: Remove the `chat-sessions` Stack.Screen registration**

In the same file, find:

```tsx
        {/* Chat session-switch sheet. */}
        <Stack.Screen name="chat-sessions" options={SHEET_OPTIONS} />
```

Delete both lines entirely (the comment and the `Stack.Screen`). The
`chat/[id]` and `chat/new` registrations Task 2 added right after this
block stay in place — after this deletion they become the first two
entries where `chat-sessions` used to be.

- [ ] **Step 5: Verify**

```bash
cd apps/mobile
pnpm exec tsc --noEmit -p .
pnpm exec eslint "app/(app)/[workspace]/_layout.tsx" components/ui/header.tsx
pnpm test -- parity
```

Expected: all three commands exit 0. `tsc` in particular confirms nothing
still imports the three deleted files.

- [ ] **Step 6: Commit**

```bash
git add -A
git status --short   # confirm chat-sessions.tsx, chat-session-picker-store.ts, and chat-session-actions.tsx show as deleted (D); _layout.tsx and header.tsx as modified (M)
git commit -m "chore(mobile): remove the old chat-sessions sheet, picker store, and now-unused ChatSessionActions"
```

---

### Task 5: Manual bilingual verification

**Files:** none (verification only).

**Interfaces:** none — this task only exercises Tasks 1-4's surface.

- [ ] **Step 1: Full-suite automated check**

```bash
cd apps/mobile
pnpm typecheck
pnpm lint
pnpm test
```

Expected: all three exit 0.

- [ ] **Step 2: Manual pass — English**

1. Tap the Chat tab → lands on the session list (not a conversation).
   Confirm rows show avatar (with presence dot), title, unread dot,
   archived label where applicable — same info as before, no missing
   sessions.
2. Tap a session → conversation screen renders with full history;
   composer works; send a message and confirm the optimistic bubble
   appears immediately, then resolves to the real response.
3. If the session had `has_unread`, confirm it clears (auto-markRead)
   and the tab-bar badge count drops accordingly.
4. Back out to the list → confirm the session's unread dot is gone and
   the tab-bar badge reflects it.
5. Tap "+" with more than one available agent → agent picker sheet →
   pick one → blank compose screen (`chat/new`) renders with that
   agent's name in the header. Type and send the first message → the
   screen transitions to the real session (still on the same screen, no
   visible flicker/reload) → back button from here returns to the list,
   and the just-created session now appears in the list with its title.
6. Tap "+" with exactly one available agent → skips the picker entirely,
   same blank-compose flow as above.
7. Open a session, delete it via the header "⋯" → confirm → lands back
   on the list, the deleted session is gone.
8. Long-press a session row on the list → delete confirm → row
   disappears without navigating anywhere.
9. Stop/cancel an in-flight agent task from an open conversation still
   works (unchanged code path, but confirm the UI still responds).

- [ ] **Step 3: Manual pass — Chinese**

Switch language to 简体中文 (Settings → Language) and repeat Step 2's
9 checks. No new translation keys were added in this plan — confirm the
reused `chat.json` keys (`sessions.empty`, `untitled_chat`,
`delete_chat.*`, `session_actions.new_chat_label`,
`title_button.default_agent_name`, `title_button.new_chat_subtitle`,
`disabled_reason.*`) still read correctly in their new locations.

- [ ] **Step 4: Confirm no regression on adjacent tabs/screens**

Tap through Inbox, My Issues, and More (Skills/Runtimes/Pinned/Issues/
Projects rows) — confirm none of them were disturbed by the
`_layout.tsx` edits in Tasks 2 and 4.

No commit for this task — it's verification-only. If any step surfaces a
defect, fix it in the task file it belongs to and re-run that task's
verification before moving on.
