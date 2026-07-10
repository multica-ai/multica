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
