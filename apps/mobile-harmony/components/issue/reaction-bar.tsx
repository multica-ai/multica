/**
 * HarmonyOS port of apps/mobile/components/issue/reaction-bar.tsx —
 * reaction chip bar. Same `groupReactions` algorithm as the iOS file (which
 * mirrors web's packages/ui/components/common/reaction-bar.tsx) so counts
 * and "reacted by me" detection match web exactly — counts-must-agree
 * parity rule from apps/mobile/CLAUDE.md.
 *
 * Empty state: when there are zero reactions the bar renders nothing.
 * Adding a new reaction is not exposed here — the entry point is the
 * comment/issue long-press menu. Tapping an existing chip toggles the
 * current user's reaction via `onToggle`.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { Text } from "@/components/ui/text";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

interface ReactionItem {
  id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
}

interface GroupedReaction {
  emoji: string;
  count: number;
  reacted: boolean;
}

function groupReactions(
  reactions: ReactionItem[],
  currentUserId: string | undefined,
): GroupedReaction[] {
  const map = new Map<string, GroupedReaction>();
  for (const r of reactions) {
    let group = map.get(r.emoji);
    if (!group) {
      group = { emoji: r.emoji, count: 0, reacted: false };
      map.set(r.emoji, group);
    }
    group.count += 1;
    if (r.actor_type === "member" && r.actor_id === currentUserId) {
      group.reacted = true;
    }
  }
  return Array.from(map.values());
}

interface Props {
  reactions: ReactionItem[];
  currentUserId: string | undefined;
  onToggle: (emoji: string) => void;
}

export function ReactionBar({ reactions, currentUserId, onToggle }: Props) {
  const c = useThemeColors();
  const grouped = groupReactions(reactions, currentUserId);
  if (grouped.length === 0) return null;

  return (
    <View style={styles.row}>
      {grouped.map((g) => (
        <Pressable
          key={g.emoji}
          onPress={() => onToggle(g.emoji)}
          accessibilityRole="button"
          accessibilityLabel={`${g.emoji} ${g.count}`}
          style={({ pressed }) => [
            styles.chip,
            {
              borderColor: g.reacted
                ? withAlpha(c.brand, 0.3)
                : c.border,
              backgroundColor: g.reacted
                ? withAlpha(c.brand, 0.1)
                : c.background,
            },
            pressed ? { opacity: 0.8 } : null,
          ]}
        >
          <Text style={styles.emoji}>{g.emoji}</Text>
          <Text
            style={[
              styles.count,
              { color: g.reacted ? c.brand : c.mutedForeground },
            ]}
          >
            {g.count}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  // flex-row flex-wrap items-center gap-1.5
  row: {
    flexDirection: "row",
    flexWrap: "wrap",
    alignItems: "center",
    gap: 6,
  },
  // flex-row items-center gap-1 rounded-full border px-2 py-0.5
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    borderRadius: 999,
    borderWidth: 1,
    paddingHorizontal: 8,
    paddingVertical: 2,
  },
  // text-xs
  emoji: { fontSize: 12 },
  // text-xs tabular-nums
  count: { fontSize: 12, fontVariant: ["tabular-nums"] },
});
