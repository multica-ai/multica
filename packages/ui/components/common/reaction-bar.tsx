"use client";

import { useEffect, useRef } from "react";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { QuickEmojiPicker } from "./quick-emoji-picker";

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

interface ReactionBarProps {
  reactions: ReactionItem[];
  currentUserId?: string;
  onToggle: (emoji: string) => void;
  getActorName: (type: string, id: string) => string;
  className?: string;
  showPicker?: boolean;
}

function ReactionBar({
  reactions,
  currentUserId,
  onToggle,
  getActorName,
  className,
  showPicker = true,
}: ReactionBarProps) {
  const grouped = groupReactions(reactions, currentUserId);
  // Track emojis seen on mount so only newly added reactions pop in.
  const seenRef = useRef<Set<string> | null>(null);
  if (seenRef.current === null) {
    seenRef.current = new Set(grouped.map((g) => g.emoji));
  }
  useEffect(() => {
    const next = new Set(grouped.map((g) => g.emoji));
    seenRef.current = next;
  });

  if (!showPicker && grouped.length === 0) return null;

  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      {grouped.map((g) => {
        const isNew = !seenRef.current?.has(g.emoji);
        return (
          <Tooltip key={g.emoji}>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={() => onToggle(g.emoji)}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-caption transition-[background-color,border-color,color,scale] duration-100 ease-[cubic-bezier(0.23,1,0.32,1)] hover:bg-brand/15 active:scale-95 motion-reduce:transition-none motion-reduce:active:scale-100",
                    g.reacted
                      ? "border-brand/30 bg-brand/8 text-brand"
                      : "border-brand/10 bg-brand/4 text-muted-foreground",
                    isNew && "animate-in fade-in zoom-in-95 duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:animate-none",
                  )}
                >
                  <span>{g.emoji}</span>
                  <NumberFlow value={g.count} className="leading-none" />
                </button>
              }
            />
            <TooltipContent side="top">
              {g.actors.map((a) => getActorName(a.type, a.id)).join(", ")}
            </TooltipContent>
          </Tooltip>
        );
      })}
      {showPicker && <QuickEmojiPicker onSelect={onToggle} />}
    </div>
  );
}

export { ReactionBar, type ReactionBarProps, type ReactionItem };
