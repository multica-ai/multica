/**
 * Reaction chip bar. Mobile RN port of
 * `packages/ui/components/common/reaction-bar.tsx`. Same `groupReactions`
 * algorithm so counts and "reacted by me" detection match web exactly —
 * counts-must-agree parity rule from apps/mobile/CLAUDE.md.
 *
 * Empty state: when there are zero reactions the bar renders nothing.
 * Adding a new reaction is intentionally NOT exposed here — that entry
 * point will be a long-press on the comment / issue body in a follow-up.
 * Tapping an existing chip still toggles the current user's reaction
 * (add/remove) via `onToggle`.
 */
import { Pressable, View } from "react-native";
import { Text } from "@/components/ui/text";
import { cn } from "@/lib/utils";

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
  className?: string;
}

export function ReactionBar({
  reactions,
  currentUserId,
  onToggle,
  className,
}: Props) {
  const grouped = groupReactions(reactions, currentUserId);
  if (grouped.length === 0) return null;

  return (
    <View
      className={cn("flex-row flex-wrap items-center gap-1.5", className)}
    >
      {grouped.map((g) => (
        <Pressable
          key={g.emoji}
          onPress={() => onToggle(g.emoji)}
          className={cn(
            "flex-row items-center gap-1 rounded-full border px-2 py-0.5",
            g.reacted
              ? "border-brand/30 bg-brand/10"
              : "border-border bg-background",
          )}
        >
          <Text className="text-xs">{g.emoji}</Text>
          <Text
            className={cn(
              "text-xs tabular-nums",
              g.reacted ? "text-brand" : "text-muted-foreground",
            )}
          >
            {g.count}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}
