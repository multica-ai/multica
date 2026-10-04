/**
 * Chat session-switch sheet — HarmonyOS port of
 * apps/mobile/app/(app)/[workspace]/chat-sessions.tsx. iOS presented this
 * route as a formSheet; here the same content mounts in a <BottomSheet>
 * owned by the chat screen (no route push).
 *
 * Reads the session list from the chat cache and writes the user's pick
 * through the shared "active session" store so the chat screen picks it up
 * on dismiss — the store channel semantics are kept verbatim from iOS:
 *
 *   - `activeSessionId` — mirrored from the chat screen so the sheet can
 *     render the current selection's check mark.
 *   - `selectRequest` — the sheet writes the id (or null) the user picked;
 *     the chat screen `useEffect`s on it, applies it, then consumes it.
 *     One-shot; deleting the active session here pushes a `null` request
 *     exactly like iOS.
 *
 * Platform deltas: `router.back()` → the `onClose` callback; long-press
 * delete keeps the iOS Alert.alert confirm (same copy / Cancel /
 * destructive pair); agent identity resolves from the agents query because
 * the ported ActorAvatar has no directory lookup, and its presence overlay
 * is omitted (use-agent-presence is not ported yet).
 */
import { Alert, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { ChatSession } from "@multica/core/types";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { chatSessionsOptions } from "@/data/queries/chat";
import { agentListOptions } from "@/data/queries/agents";
import { useDeleteChatSession } from "@/data/mutations/chat";
import { useChatSessionPickerStore } from "@/data/stores/chat-session-picker-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { chatSessionDisplayTitle } from "@/lib/chat-session-title";

interface Props {
  visible: boolean;
  onClose: () => void;
}

export function ChatSessionsSheet({ visible, onClose }: Props) {
  const c = useThemeColors();
  const s = styles(c);
  const wsId = useWorkspaceStore((st) => st.currentWorkspaceId);
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  // The iOS formSheet resolved agent avatars from the session's agent_id
  // through ActorAvatar's directory lookup; that layer isn't ported, so
  // join against the agents list (same cache the chat screen uses).
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const activeSessionId = useChatSessionPickerStore((st) => st.activeSessionId);
  const requestSelect = useChatSessionPickerStore((st) => st.requestSelect);
  const deleteSession = useDeleteChatSession();

  const confirmDelete = (session: ChatSession) => {
    Alert.alert(
      "Delete this chat?",
      chatSessionDisplayTitle(session.title),
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => {
            deleteSession.mutate(session.id);
            // If we just deleted the active one, the chat screen clears its
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
    <BottomSheet visible={visible} onClose={onClose} avoidKeyboard={false}>
      <View style={s.header}>
        <Text style={s.title}>Chats</Text>
      </View>
      <ScrollView
        style={staticStyles.list}
        contentContainerStyle={staticStyles.listContent}
        showsVerticalScrollIndicator={false}
      >
        {sessions.length === 0 ? (
          <View style={staticStyles.empty}>
            <Text style={s.emptyText}>No chats yet.</Text>
          </View>
        ) : (
          sessions.map((session) => {
            const selected = session.id === activeSessionId;
            const archived = session.status === "archived";
            const agent = agents.find((a) => a.id === session.agent_id);
            return (
              <Pressable
                key={session.id}
                onPress={() => {
                  requestSelect(session.id);
                  onClose();
                }}
                onLongPress={() => confirmDelete(session)}
                delayLongPress={500}
                style={({ pressed }) => [
                  staticStyles.row,
                  selected && {
                    backgroundColor: withAlpha(c.secondary, 0.6),
                  },
                  pressed && !selected && { backgroundColor: c.secondary },
                ]}
              >
                {/* h-2 w-2 unread dot — primary when has_unread */}
                <View
                  style={[
                    staticStyles.unreadDot,
                    {
                      backgroundColor: session.has_unread
                        ? c.primary
                        : "transparent",
                    },
                  ]}
                />
                <ActorAvatar
                  type="agent"
                  id={session.agent_id}
                  name={agent?.name}
                  avatarUrl={agent?.avatar_url ?? null}
                  size={32}
                />
                <View style={staticStyles.rowMain}>
                  <Text
                    style={[
                      s.sessionTitle,
                      session.has_unread && s.sessionTitleUnread,
                    ]}
                    numberOfLines={1}
                  >
                    {chatSessionDisplayTitle(session.title)}
                  </Text>
                  {archived ? (
                    <Text style={s.archivedLabel}>archived</Text>
                  ) : null}
                </View>
                {selected ? (
                  <Text style={s.checkMark}>✓</Text>
                ) : null}
              </Pressable>
            );
          })
        )}
      </ScrollView>
    </BottomSheet>
  );
}

const styles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    // px-4 pt-4 pb-3 (title block)
    header: {
      paddingHorizontal: 16,
      paddingTop: 12,
      paddingBottom: 12,
    },
    // text-base font-semibold text-foreground
    title: { fontSize: 16, fontWeight: "600", color: c.foreground },
    // text-sm text-muted-foreground text-center
    emptyText: {
      fontSize: 14,
      color: c.mutedForeground,
      textAlign: "center",
    },
    // text-sm text-foreground
    sessionTitle: { fontSize: 14, color: c.foreground },
    // has_unread → font-semibold
    sessionTitleUnread: { fontWeight: "600" },
    // text-xs text-muted-foreground mt-0.5
    archivedLabel: {
      fontSize: 12,
      color: c.mutedForeground,
      marginTop: 2,
    },
    // text-sm text-primary font-semibold
    checkMark: { fontSize: 14, color: c.primary, fontWeight: "600" },
  });

const staticStyles = StyleSheet.create({
  // Cap long session lists so the sheet stays content-sized for short ones.
  list: { maxHeight: 432 },
  listContent: { paddingBottom: 8 },
  // px-4 py-8
  empty: { paddingHorizontal: 16, paddingVertical: 32 },
  // flex-row items-center gap-3 px-4 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // h-2 w-2 rounded-full
  unreadDot: { width: 8, height: 8, borderRadius: 4 },
  rowMain: { flex: 1, minWidth: 0 },
});
