/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/issue/[id]/comment/
 * [commentId]/emoji-picker.tsx — full emoji picker for a comment reaction,
 * opened from the per-comment long-press menu's "React…" row. Mirrors web's
 * emoji-mart picker product semantics: mobile must offer the full emoji
 * set, not only the quick picks.
 *
 * Reads the comment from the timeline cache to detect an already-applied
 * reaction by the current user, then fires `useToggleCommentReaction` with
 * the right `existing` value so re-tapping an active emoji removes it
 * (matches web behaviour and the inline ReactionBar toggle semantics).
 *
 * Platform substitution: the iOS route embedded `rn-emoji-keyboard`, which
 * is not available on this matrix (AGENTS.md). The replacement is a pure
 * JS unicode emoji grid — a quick-emoji row on top (lib/quick-emojis) and
 * static category tabs below (Smileys / Gestures / People / Objects /
 * Symbols). Single-codepoint emoji are preferred so glyphs render reliably
 * across system emoji fonts. A search bar was considered and skipped —
 * emoji search needs names-per-emoji data we don't ship.
 */
import React, { useCallback, useMemo, useState } from "react";
import { FlatList, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { Reaction } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueTimelineOptions } from "@/data/queries/issues";
import { useToggleCommentReaction } from "@/data/mutations/issues";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { QUICK_EMOJIS } from "@/lib/quick-emojis";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  issueId: string;
  commentId: string;
  visible: boolean;
  onClose: () => void;
}

/** One emoji category tab. Kept as static arrays — no data dependency. */
const CATEGORY_TABS: { label: string; emojis: string[] }[] = [
  {
    label: "Smileys",
    emojis: [
      "😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "😊",
      "😇", "🥰", "😍", "🤩", "😘", "😗", "😚", "😙", "🥲", "😋",
      "😛", "😜", "🤪", "😝", "🤑", "🤗", "🤭", "🤫", "🤔", "🤐",
      "🤨", "😐", "😑", "😶", "😏", "😒", "🙄", "😬", "🤥", "😌",
      "😔", "😪", "🤤", "😴", "😷", "🤒", "🤕", "🤢", "🤮", "🥵",
      "🥶", "🥴", "😵", "🤯", "🤠", "🥳", "🥸", "😎", "🤓", "🧐",
      "😕", "😟", "🙁", "😮", "😯", "😲", "😳", "🥺", "😦", "😧",
      "😨", "😰", "😥", "😢", "😭", "😱", "😖", "😣", "😞", "😓",
      "😩", "😫", "🥱", "😤", "😡", "😠", "🤬", "😈", "👿", "💀",
    ],
  },
  {
    label: "Gestures",
    emojis: [
      "👍", "👎", "👌", "✌️", "🤞", "🤟", "🤘", "🤙", "👈", "👉",
      "👆", "🖕", "👇", "☝️", "✋", "🤚", "🖐", "🖖", "👋", "🤝",
      "🙏", "✍️", "💪", "👏", "🙌", "👐", "🤲", "🤜", "🤛", "✊",
      "👊", "🫶", "🤳", "💁", "🙆", "🙅", "🙋", "🤦", "🤷", "🙇",
    ],
  },
  {
    label: "People",
    emojis: [
      "👶", "🧒", "👦", "👧", "🧑", "👱", "👨", "🧔", "👩", "🧓",
      "👴", "👵", "🙍", "🙎", "🚶", "🧍", "🏃", "💃", "🕺", "🧑‍⚕️",
      "🧑‍🎓", "🧑‍🏫", "🧑‍🔧", "🧑‍💻", "🧑‍🔬", "🧑‍🎨", "👮", "🕵️", "💂", "👷",
      "🤴", "👸", "🥷", "🧙", "🧚", "🧛", "🧜", "🧝", "🧞", "🧟",
      "👋", "🤲", "🫂",
    ],
  },
  {
    label: "Objects",
    emojis: [
      "❤️", "🧡", "💛", "💚", "💙", "💜", "🖤", "🤍", "🤎", "💔",
      "❣️", "💕", "💞", "💓", "💗", "💖", "💘", "💝", "💯", "💥",
      "💫", "💦", "💨", "🕳", "💬", "💭", "🔥", "⭐", "🌟", "✨",
      "⚡", "🎉", "🎊", "🎈", "🎁", "🏆", "🥇", "🎯", "🎨", "🧩",
      "🔧", "🔨", "⚙️", "📎", "📌", "📝", "📄", "📅", "⏰", "💻",
      "🖥️", "📱", "⌨️", "🖱️", "💾", "📷", "🎥", "🔍", "🔑", "🔒",
      "💡", "📦", "🚀", "🛠️", "🧪", "🧲", "💊", "🧬",
    ],
  },
  {
    label: "Symbols",
    emojis: [
      "✅", "❌", "❓", "❗", "⁉️", "⚠️", "🚫", "♻️", "🔐", "🆒",
      "🔵", "🟠", "🟡", "🟢", "🟣", "⚫", "⚪", "🟤", "🟥", "🟧",
      "🟨", "🟩", "🟦", "🟪", "⬛", "⬜", "🔺", "🔻", "💠", "🔶",
      "🔷", "🔸", "🔹", "♦️", "♠️", "♥️", "♣️", "🗨️", "🗯️", "👁️",
      "🤖", "👋", "🌍", "🌙", "☀️", "🌈", "☂️", "❄️", "🌊", "🌸",
    ],
  },
];

const NUM_COLUMNS = 8;

export function EmojiPickerSheet({
  issueId,
  commentId,
  visible,
  onClose,
}: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id);
  const toggle = useToggleCommentReaction(issueId);
  const [tab, setTab] = useState(0);

  const { data: timeline = [] } = useQuery(issueTimelineOptions(wsId, issueId));
  const entry = useMemo(
    () => timeline.find((e) => e.id === commentId) ?? null,
    [timeline, commentId],
  );

  const reactions = useMemo<Reaction[]>(
    () => (entry?.reactions ?? []) as Reaction[],
    [entry?.reactions],
  );

  const onSelect = useCallback(
    (emoji: string) => {
      const existing = reactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggle.mutate({ commentId, emoji, existing });
      onClose();
    },
    [reactions, userId, toggle, commentId, onClose],
  );

  const emojis = CATEGORY_TABS[tab].emojis;

  return (
    <BottomSheet visible={visible} onClose={onClose} maxHeightRatio={0.7}>
      <View style={styles.header}>
        <Text style={[styles.title, { color: c.foreground }]}>Add Reaction</Text>
      </View>

      {/* Quick row — the same curated set as the long-press menu's inline
          row, so muscle memory carries between the two surfaces. */}
      <View style={styles.quickRow}>
        {QUICK_EMOJIS.map((emoji) => (
          <Pressable
            key={emoji}
            onPress={() => onSelect(emoji)}
            accessibilityRole="button"
            accessibilityLabel={`React with ${emoji}`}
            style={({ pressed }) => [
              styles.quickCell,
              pressed ? { backgroundColor: c.secondary } : null,
            ]}
          >
            <Text style={styles.emoji}>{emoji}</Text>
          </Pressable>
        ))}
      </View>

      {/* Category tabs */}
      <View style={[styles.tabs, { borderBottomColor: c.border }]}>
        {CATEGORY_TABS.map((category, index) => {
          const active = index === tab;
          return (
            <Pressable
              key={category.label}
              onPress={() => setTab(index)}
              accessibilityRole="tab"
              accessibilityLabel={category.label}
              accessibilityState={{ selected: active }}
              style={[
                styles.tab,
                active ? { backgroundColor: withAlpha(c.brand, 0.12) } : null,
              ]}
            >
              <Text
                style={[
                  styles.tabLabel,
                  { color: active ? c.brand : c.mutedForeground },
                ]}
              >
                {category.label}
              </Text>
            </Pressable>
          );
        })}
      </View>

      <FlatList
        data={emojis}
        keyExtractor={(emoji, i) => `${emoji}-${i}`}
        numColumns={NUM_COLUMNS}
        style={styles.grid}
        renderItem={({ item }) => (
          <Pressable
            onPress={() => onSelect(item)}
            accessibilityRole="button"
            accessibilityLabel={`React with ${item}`}
            style={({ pressed }) => [
              styles.cell,
              pressed ? { backgroundColor: c.secondary } : null,
            ]}
          >
            <Text style={styles.emoji}>{item}</Text>
          </Pressable>
        )}
      />
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  // px-4 pt-3 pb-2
  header: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 8 },
  // text-lg font-semibold
  title: { fontSize: 18, fontWeight: "600" },
  quickRow: {
    flexDirection: "row",
    paddingHorizontal: 12,
    paddingBottom: 4,
  },
  quickCell: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingVertical: 8,
    borderRadius: 8,
  },
  // text-2xl-ish grid glyph
  emoji: { fontSize: 26, lineHeight: 32 },
  tabs: {
    flexDirection: "row",
    paddingHorizontal: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
    gap: 4,
  },
  tab: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: 999,
  },
  // text-xs font-medium
  tabLabel: { fontSize: 12, fontWeight: "500" },
  grid: { maxHeight: 320 },
  cell: {
    flex: 1,
    aspectRatio: 1,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 8,
    margin: 2,
  },
});
