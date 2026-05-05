"use client";

import { useMemo, useState } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useToggleChannelReaction } from "@multica/core/channels";
import type { ChannelReaction } from "@multica/core/types";
import { Smile, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

// Curated emoji palette. The picker is intentionally tiny — Phase 4
// doesn't ship a full emoji search; if/when that's needed, swap the
// PopoverContent body for an emoji-mart instance. Today's UX matches
// what most agent-comment surfaces in this codebase do.
const QUICK_EMOJIS = ["👍", "👎", "😄", "🎉", "❤️", "🚀", "👀", "🙏"];

interface MessageReactionsProps {
  channelId: string;
  messageId: string;
  reactions: ChannelReaction[];
}

interface GroupedReaction {
  emoji: string;
  count: number;
  /** Whether the current actor's id appears among this emoji's reactors. */
  byMe: boolean;
}

function groupByEmoji(reactions: ChannelReaction[], selfId: string | null): GroupedReaction[] {
  const map = new Map<string, GroupedReaction>();
  for (const r of reactions) {
    const cur = map.get(r.emoji);
    const byMe = selfId != null && r.actor_type === "member" && r.actor_id === selfId;
    if (cur) {
      cur.count++;
      cur.byMe = cur.byMe || byMe;
    } else {
      map.set(r.emoji, { emoji: r.emoji, count: 1, byMe });
    }
  }
  // Stable order: first-seen wins. Object iteration order is insertion
  // order, so just convert.
  return Array.from(map.values());
}

/**
 * MessageReactions — chip row under each message + an "add reaction"
 * picker. Click an existing chip toggles your reaction; click the +
 * button opens an 8-emoji palette.
 *
 * Optimistic updates live in the toggle mutation; the WS handler in
 * use-realtime-sync invalidates this cache when other actors react,
 * so chips stay in sync without a refetch on the optimistic path.
 */
export function MessageReactions({ channelId, messageId, reactions }: MessageReactionsProps) {
  const selfId = useAuthStore((s) => s.user?.id ?? null);
  const toggle = useToggleChannelReaction(channelId);
  const [pickerOpen, setPickerOpen] = useState(false);

  const groups = useMemo(() => groupByEmoji(reactions, selfId), [reactions, selfId]);

  const handleToggle = (emoji: string, currentlyReacted: boolean) => {
    toggle.mutate({ messageId, emoji, currentlyReacted });
  };

  const handlePick = (emoji: string) => {
    const existing = groups.find((g) => g.emoji === emoji);
    handleToggle(emoji, !!existing?.byMe);
    setPickerOpen(false);
  };

  if (groups.length === 0) {
    // No reactions yet — render only the add button on hover. The
    // hover-reveal is implemented via group-hover at the parent level
    // (see MessageRow); here we render the button unconditionally so
    // the parent hover styling can hide it when not in use.
    return (
      <div className="mt-1 opacity-0 transition-opacity group-hover:opacity-100">
        <ReactionPicker open={pickerOpen} onOpenChange={setPickerOpen} onPick={handlePick} />
      </div>
    );
  }

  return (
    <div className="mt-1 flex flex-wrap items-center gap-1">
      {groups.map((g) => (
        <button
          key={g.emoji}
          type="button"
          onClick={() => handleToggle(g.emoji, g.byMe)}
          className={[
            "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs transition-colors",
            g.byMe
              ? "border-primary bg-primary/10 text-foreground"
              : "border-border bg-muted/40 text-muted-foreground hover:border-foreground/30 hover:text-foreground",
          ].join(" ")}
          aria-pressed={g.byMe}
          aria-label={`${g.emoji} (${g.count})`}
        >
          <span className="text-sm leading-none">{g.emoji}</span>
          <span className="tabular-nums">{g.count}</span>
        </button>
      ))}
      <ReactionPicker open={pickerOpen} onOpenChange={setPickerOpen} onPick={handlePick} />
    </div>
  );
}

interface ReactionPickerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPick: (emoji: string) => void;
}

function ReactionPicker({ open, onOpenChange, onPick }: ReactionPickerProps) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        render={
          <Button
            size="sm"
            variant="ghost"
            className="h-6 w-6 p-0 text-muted-foreground"
            aria-label="Add reaction"
          >
            {open ? <Plus className="h-3.5 w-3.5" /> : <Smile className="h-3.5 w-3.5" />}
          </Button>
        }
      />
      <PopoverContent className="w-auto p-1">
        <div className="flex gap-0.5">
          {QUICK_EMOJIS.map((e) => (
            <button
              key={e}
              type="button"
              onClick={() => onPick(e)}
              className="rounded-md p-1.5 text-base hover:bg-muted"
              aria-label={`React with ${e}`}
            >
              {e}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}
