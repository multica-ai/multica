"use client";

import { Users } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "./actor-avatar";

/**
 * ActorMentionChip — the inline "avatar pill" for actor mentions
 * (member / agent / squad / @all) in rich-text. The single source of truth
 * for the avatar-chip look; both the Tiptap editor (MentionView) and readonly
 * markdown (ReadonlyContent) render it so composing and reading show the same
 * form.
 *
 * Type distinction is carried by the pill's background + border tint
 * (member=muted, agent=brand, squad=info, @all=warning) and by the avatar
 * shape/icons delegated to {@link ActorAvatar}. The chip is purely visual
 * confirmation — it has no click navigation. Callers wrap it in a hover card
 * and layer hover/focus hints via `className`.
 *
 * Size budget: must fit within a 14px / 1.625 prose line-box when used inline —
 * hence `py-0.5` + `text-xs` + a 14px avatar (same budget IssueChip proved).
 *
 * Focusability is caller-controlled (R14): the editor opts in (`focusable`)
 * so keyboard users get the hover card on focus; readonly leaves it off so a
 * long comment with N mentions does not inject N keyboard tab stops.
 */
export type ActorMentionType = "member" | "agent" | "squad" | "all";

export interface ActorMentionChipProps {
  type: ActorMentionType;
  /** Display name shown after the `@` prefix (e.g. "张三", "ReviewerBot"). */
  label: string;
  /** First-character initials for the avatar (derived by the caller). */
  initials: string;
  avatarUrl?: string | null;
  /** Extra classes — callers layer hover / focus hints here
   *  (e.g. `hover:bg-accent` for members in the editor). */
  className?: string;
  /** When true the chip is keyboard-focusable with a focus-visible ring
   *  (editor use). Readonly consumers leave it false (R14). */
  focusable?: boolean;
}

const BASE_CLASS =
  "actor-mention-chip inline-flex align-middle min-w-0 items-center gap-1 rounded-full border px-1.5 py-0.5 text-xs font-medium";

const TYPE_STYLES: Record<ActorMentionType, string> = {
  member: "bg-muted border-border",
  agent: "bg-brand/10 border-brand/20",
  squad: "bg-info/10 border-info/20",
  all: "bg-warning/10 border-warning/20",
};

/**
 * Per-type hover tint for consumers that opt into hover feedback (the editor
 * and readonly both use it). The chip's base background is the type-tint;
 * this layers a deeper tint on hover so R12's "background transitions to a
 * slightly deeper tint" is visible. For members the base is `bg-muted`, so the
 * hover tint is `bg-accent` (not `bg-muted`, which would equal the base and
 * show no transition).
 */
export const ACTOR_MENTION_HOVER_CLASS: Record<ActorMentionType, string> = {
  member: "hover:bg-accent transition-colors",
  agent: "hover:bg-brand/15 transition-colors",
  squad: "hover:bg-info/15 transition-colors",
  all: "hover:bg-warning/15 transition-colors",
};

function ariaLabelFor(type: ActorMentionType, label: string): string {
  return type === "all"
    ? "Mention: all workspace members"
    : `Mention: ${label}, ${type}`;
}

const AVATAR_SIZE = 14;

export function ActorMentionChip({
  type,
  label,
  initials,
  avatarUrl,
  className,
  focusable = false,
}: ActorMentionChipProps) {
  return (
    <span
      className={cn(
        BASE_CLASS,
        TYPE_STYLES[type],
        focusable &&
          "focus-visible:outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        className,
      )}
      tabIndex={focusable ? 0 : undefined}
      aria-label={ariaLabelFor(type, label)}
    >
      {type === "all" ? (
        // @all has no ActorAvatar mode; render a Users-icon tile so the chip
        // keeps the avatar + label anatomy. bg-muted matches ActorAvatar's
        // no-URL tile so it reads as a peer, text-warning ties it to the
        // @all warning identity.
        <span
          data-slot="avatar"
          aria-hidden
          className="inline-flex shrink-0 items-center justify-center rounded-full bg-muted text-warning"
          style={{ width: AVATAR_SIZE, height: AVATAR_SIZE }}
        >
          <Users style={{ width: AVATAR_SIZE * 0.55, height: AVATAR_SIZE * 0.55 }} />
        </span>
      ) : (
        <ActorAvatar
          name={label}
          initials={initials}
          avatarUrl={avatarUrl}
          isAgent={type === "agent"}
          isSquad={type === "squad"}
          size="xs"
        />
      )}
      <span data-slot="label" className="min-w-0 shrink truncate max-w-[8rem]">
        @{label}
      </span>
    </span>
  );
}
