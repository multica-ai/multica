import { useMemo } from "react";
import { Pressable, Text, View } from "react-native";
import * as Haptics from "expo-haptics";
import type { Reaction } from "@multica/core/types";
import { useToggleCommentReaction } from "@multica/core/issues/mutations";

import { IconSymbol } from "@/components/ui/icon-symbol";

interface Props {
  issueId: string;
  commentId: string;
  reactions: Reaction[];
  currentUserId?: string;
  onAddPress: () => void;
}

// Interactive reaction strip for a single comment row. Tap an existing chip to
// toggle (own reactions show with a brand-tinted ring); tap `+` to open the
// EmojiQuickPicker sheet. Mirrors web's <ReactionBar> contract but built with
// raw RN primitives — no shared package since the desktop component is DOM-only.
export function CommentReactionBar({
  issueId,
  commentId,
  reactions,
  currentUserId,
  onAddPress,
}: Props) {
  const toggle = useToggleCommentReaction(issueId);

  // Group raw rows by emoji so each chip shows count + whether the current
  // member already reacted with it (drives the brand-tinted "own" style).
  const grouped = useMemo(() => {
    const map = new Map<
      string,
      { count: number; mine?: Reaction; first: Reaction }
    >();
    for (const r of reactions ?? []) {
      const existing = map.get(r.emoji);
      if (existing) {
        existing.count += 1;
        if (
          !existing.mine &&
          r.actor_type === "member" &&
          r.actor_id === currentUserId
        ) {
          existing.mine = r;
        }
      } else {
        map.set(r.emoji, {
          count: 1,
          mine:
            r.actor_type === "member" && r.actor_id === currentUserId
              ? r
              : undefined,
          first: r,
        });
      }
    }
    return Array.from(map.entries());
  }, [reactions, currentUserId]);

  const handleToggle = (emoji: string, mine?: Reaction) => {
    Haptics.selectionAsync().catch(() => {});
    toggle.mutate({ commentId, emoji, existing: mine });
  };

  if (grouped.length === 0) {
    // No reactions yet — render nothing here. The `+` entry point in this case
    // lives in the long-press ActionSheet ("React"), so a permanent floating
    // button would just add visual noise to every comment.
    return null;
  }

  return (
    <View className="flex-row flex-wrap gap-1.5 mt-2">
      {grouped.map(([emoji, { count, mine }]) => (
        <Pressable
          key={emoji}
          onPress={() => handleToggle(emoji, mine)}
          hitSlop={4}
          className={
            mine
              ? "flex-row items-center gap-1 px-2 py-0.5 rounded-full bg-brand/10 border border-brand/40"
              : "flex-row items-center gap-1 px-2 py-0.5 rounded-full bg-foreground/5 border border-transparent"
          }
        >
          <Text className="text-base">{emoji}</Text>
          <Text
            className={
              mine
                ? "text-brand text-xs font-medium"
                : "text-muted-foreground text-xs"
            }
          >
            {count}
          </Text>
        </Pressable>
      ))}
      <Pressable
        onPress={onAddPress}
        hitSlop={4}
        className="flex-row items-center px-2 py-0.5 rounded-full bg-foreground/5 active:bg-foreground/10"
      >
        {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
        <IconSymbol
          name={"face.smiling" as any}
          size={14}
          color="hsl(240 4% 46%)"
        />
        <Text className="text-muted-foreground text-xs ml-1">+</Text>
      </Pressable>
    </View>
  );
}
