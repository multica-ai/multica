/**
 * Centred, tappable title region for the Chat tab header. HarmonyOS port of
 * apps/mobile/components/chat/chat-title-button.tsx — iOS rendered it into
 * the native Stack header via `headerTitle`; here the ported <Header> takes
 * it as the `center` slot, so the interaction (tap opens the sessions +
 * agent picker sheet) is unchanged.
 *
 * Platform delta: the iOS ActorAvatar resolved agent identity + presence
 * overlay from the agent id alone; the ported one takes explicit identity —
 * callers pass `name` / `avatarUrl` from the resolved agent, and the
 * presence dot is omitted (use-agent-presence is not ported yet).
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { Agent, ChatSession } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { chatSessionDisplayTitle } from "@/lib/chat-session-title";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  currentSession: ChatSession | null;
  currentAgent: Agent | null;
  onPress: () => void;
}

export function ChatTitleButton({
  currentSession,
  currentAgent,
  onPress,
}: Props) {
  const c = useThemeColors();
  const agentName = currentAgent?.name ?? "Chat";
  const subtitle = chatSessionDisplayTitle(currentSession?.title);

  return (
    <Pressable
      onPress={onPress}
      hitSlop={4}
      style={({ pressed }) => [
        styles.button,
        pressed && { backgroundColor: c.secondary },
      ]}
      accessibilityRole="button"
      accessibilityLabel="Sessions and agent picker"
    >
      <ActorAvatar
        type={currentAgent ? "agent" : null}
        id={currentAgent?.id ?? null}
        name={currentAgent?.name ?? undefined}
        avatarUrl={currentAgent?.avatar_url ?? null}
        size={24}
      />
      <View style={styles.textCol}>
        <View style={styles.nameRow}>
          <Text
            style={[styles.name, { color: c.foreground }]}
            numberOfLines={1}
          >
            {agentName}
          </Text>
          <Text style={[styles.chevron, { color: c.mutedForeground }]}>▼</Text>
        </View>
        <Text
          style={[styles.subtitle, { color: c.mutedForeground }]}
          numberOfLines={1}
        >
          {subtitle}
        </Text>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // flex-row items-center gap-2 px-2 py-1 rounded-lg active:bg-secondary
  button: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 8,
  },
  textCol: { flexShrink: 1 },
  nameRow: { flexDirection: "row", alignItems: "center", gap: 4 },
  // text-base font-semibold
  name: { fontSize: 16, fontWeight: "600" },
  chevron: { fontSize: 12 },
  // text-xs text-muted-foreground
  subtitle: { fontSize: 12 },
});
