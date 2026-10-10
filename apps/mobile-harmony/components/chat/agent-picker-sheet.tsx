/**
 * Agent picker — sheet listing agents the current user can assign / chat
 * with. HarmonyOS port of apps/mobile/components/chat/agent-picker-sheet.tsx.
 * Shown when the user taps `+ New Chat` and the workspace has more than one
 * usable agent; with exactly one, the chat screen skips this sheet and goes
 * straight to the blank state for that agent.
 *
 * Filtering is delegated to the caller (the screen passes a pre-filtered
 * `agents` list) so the same filter logic — archived + canAssignAgentToIssue
 * + order — stays in one place.
 *
 * Platform replacement: iOS used a transparent Modal with a centered card;
 * the standing sheet primitive on this platform is <BottomSheet> (the
 * formSheet/modal stand-in), so the same content renders as a bottom sheet.
 * The ActorAvatar takes explicit identity (no directory lookup on this
 * slice) and the presence overlay is omitted (use-agent-presence is not
 * ported yet).
 */
import { ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { Agent } from "@multica/core/types";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";

interface Props {
  visible: boolean;
  agents: Agent[];
  currentAgentId: string | null;
  onPick: (agent: Agent) => void;
  onClose: () => void;
}

export function AgentPickerSheet({
  visible,
  agents,
  currentAgentId,
  onPick,
  onClose,
}: Props) {
  const c = useThemeColors();
  const s = styles(c);

  return (
    <BottomSheet visible={visible} onClose={onClose} avoidKeyboard={false}>
      <View style={s.header}>
        <Text style={s.title}>Choose an agent</Text>
      </View>
      <ScrollView
        style={staticStyles.list}
        contentContainerStyle={staticStyles.listContent}
        showsVerticalScrollIndicator={false}
      >
        {agents.length === 0 ? (
          <View style={staticStyles.empty}>
            <Text style={s.emptyText}>No agents available.</Text>
          </View>
        ) : (
          agents.map((agent) => {
            const selected = agent.id === currentAgentId;
            const runtimeBound = isAgentRuntimeBound(agent);
            return (
              <Pressable
                key={agent.id}
                disabled={!runtimeBound}
                onPress={() => {
                  onPick(agent);
                  onClose();
                }}
                style={({ pressed }) => [
                  staticStyles.row,
                  selected && { backgroundColor: withAlpha(c.secondary, 0.6) },
                  pressed && !selected && { backgroundColor: c.secondary },
                  !runtimeBound && staticStyles.disabledRow,
                ]}
              >
                <ActorAvatar
                  type="agent"
                  id={agent.id}
                  name={agent.name}
                  avatarUrl={agent.avatar_url}
                  size={32}
                />
                <View style={staticStyles.rowMain}>
                  <Text style={s.agentName} numberOfLines={1}>
                    {agent.name}
                  </Text>
                  {agent.description ? (
                    <Text style={s.agentDescription} numberOfLines={1}>
                      {agent.description}
                    </Text>
                  ) : null}
                </View>
                {!runtimeBound ? (
                  <Text style={s.runtimeLabel}>Needs runtime</Text>
                ) : null}
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
    // px-4 py-3 border-b border-border
    header: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 1,
      borderBottomColor: c.border,
    },
    // text-base font-semibold text-foreground
    title: { fontSize: 16, fontWeight: "600", color: c.foreground },
    // text-sm text-muted-foreground text-center
    emptyText: {
      fontSize: 14,
      color: c.mutedForeground,
      textAlign: "center",
    },
    // text-sm font-medium text-foreground
    agentName: { fontSize: 14, fontWeight: "500", color: c.foreground },
    // text-xs text-muted-foreground mt-0.5
    agentDescription: {
      fontSize: 12,
      color: c.mutedForeground,
      marginTop: 2,
    },
    // text-xs font-medium text-warning
    runtimeLabel: { fontSize: 12, fontWeight: "500", color: c.warning },
    // text-sm text-primary font-semibold
    checkMark: { fontSize: 14, color: c.primary, fontWeight: "600" },
  });

// Theme-independent layout styles.
const staticStyles = StyleSheet.create({
  // max-h-96 (iOS cap — ~6 rows before scrolling)
  list: { maxHeight: 384 },
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
  // !runtimeBound && opacity-50
  disabledRow: { opacity: 0.5 },
  rowMain: { flex: 1, minWidth: 0 },
});
