"use client";

import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { QuickEmojiPicker } from "@/components/common/quick-emoji-picker";
import { useActorName } from "@/features/workspace";

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
  actors: { type: string; id: string }[];
}

function groupReactions(reactions: ReactionItem[], currentUserId?: string): GroupedReaction[] {
  const map = new Map<string, GroupedReaction>();
  for (const r of reactions) {
    let group = map.get(r.emoji);
    if (!group) {
      group = { emoji: r.emoji, count: 0, reacted: false, actors: [] };
      map.set(r.emoji, group);
    }
    group.count++;
    group.actors.push({ type: r.actor_type, id: r.actor_id });
    if (r.actor_type === "member" && r.actor_id === currentUserId) {
      group.reacted = true;
    }
  }
  return Array.from(map.values());
}

export function ReactionBar({
  reactions,
  currentUserId,
  onToggle,
  className,
  hideAddButton,
}: {
  reactions: ReactionItem[];
  currentUserId?: string;
  onToggle: (emoji: string) => void;
  className?: string;
  hideAddButton?: boolean;
}) {
  const grouped = groupReactions(reactions, currentUserId);
  const { getActorName } = useActorName();

  return (
    <div className={`flex flex-wrap items-center gap-1.5 ${className ?? ""}`}>
      {grouped.map((g) => (
        <Tooltip key={g.emoji}>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={() => onToggle(g.emoji)}
                className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs transition-colors hover:bg-brand/15 ${
                  g.reacted
                    ? "border-brand/30 bg-brand/8 text-brand"
                    : "border-brand/10 bg-brand/4 text-muted-foreground"
                }`}
              >
                <span>{g.emoji}</span>
                <span>{g.count}</span>
              </button>
            }
          />
          <TooltipContent side="top">
            {g.actors.map((a) => getActorName(a.type, a.id)).join(", ")}
          </TooltipContent>
        </Tooltip>
      ))}
      {!hideAddButton && <QuickEmojiPicker onSelect={onToggle} />}
    </div>
  );
}
