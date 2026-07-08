/**
 * Chat session-switch sheet — presented as a formSheet by the parent Stack.
 * Reads the session list from the chat cache and writes the user's pick
 * through a shared "active session" store so the chat tab picks it up on
 * dismiss.
 *
 * Why a tiny dedicated store: the chat tab's `activeSessionId` used to live
 * as a `useState` inside `chat.tsx`, but now that session picking happens
 * on a separate route screen, we need a cross-screen channel. Same minimum
 * pattern as `useNewIssueDraftStore` for the new-issue form.
 */
import { Alert, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { ChatSession } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { chatSessionsOptions } from "@/data/queries/chat";
import { useDeleteChatSession } from "@/data/mutations/chat";
import { useChatSessionPickerStore } from "@/data/stores/chat-session-picker-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";

export default function ChatSessionsRoute() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const activeSessionId = useChatSessionPickerStore((s) => s.activeSessionId);
  const requestSelect = useChatSessionPickerStore((s) => s.requestSelect);
  const deleteSession = useDeleteChatSession();

  const confirmDelete = (session: ChatSession) => {
    Alert.alert(
      "Delete this chat?",
      session.title || "Untitled chat",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => {
            deleteSession.mutate(session.id);
            // If we just deleted the active one, the chat tab clears its
            // local activeSessionId via the picker-store request.
            if (session.id === activeSessionId) {
              requestSelect(null);
            }
          },
        },
      ],
      { cancelable: true },
    );
  };

  return (
    <View className="flex-1">
      <View className="px-4 pt-4 pb-3">
        <Text className="text-base font-semibold text-foreground">Chats</Text>
      </View>
      <ScrollView className="flex-1" showsVerticalScrollIndicator={false}>
        {sessions.length === 0 ? (
          <View className="px-4 py-8">
            <Text className="text-sm text-muted-foreground text-center">
              No chats yet.
            </Text>
          </View>
        ) : (
          sessions.map((session) => {
            const selected = session.id === activeSessionId;
            const archived = session.status === "archived";
            return (
              <Pressable
                key={session.id}
                onPress={() => {
                  requestSelect(session.id);
                  router.back();
                }}
                onLongPress={() => confirmDelete(session)}
                className={cn(
                  "flex-row items-center gap-3 px-4 py-3 active:bg-secondary",
                  selected && "bg-secondary/60",
                )}
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
                    {session.title || "Untitled chat"}
                  </Text>
                  {archived ? (
                    <Text className="text-xs text-muted-foreground mt-0.5">
                      archived
                    </Text>
                  ) : null}
                </View>
                {selected ? (
                  <Text className="text-sm text-primary font-semibold">✓</Text>
                ) : null}
              </Pressable>
            );
          })
        )}
      </ScrollView>
    </View>
  );
}
