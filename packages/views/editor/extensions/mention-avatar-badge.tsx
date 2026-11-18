"use client";

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

import {
  MentionCliCornerBadge,
  type MentionCli,
} from "./mention-cli-brand";

// ---------------------------------------------------------------------------
// Mention avatar badge stack
//
// One component owns the badge geometry so no call site re-derives it:
//
//   ≥ 34px (36px row default) → CLI badge 17px, presence dot per the
//                               app-wide scale
//   ≥ 28px (floor)            → CLI badge 14px, same dot scale
//   <  28px                   → CLI badge DROPPED (CLI falls back to the
//                               side text tag), same dot scale
//
// the dot is agentStatusDotPx (6px standard / 4px
// small) — the exact same source AgentStatusDot renders with, so a third
// drift is impossible.
//
// Presence dot has three shapes, distinguished by more than hue:
//   online   solid green #3BA55D
//   offline  solid gray  #B9B7AC
//   disabled hollow gray ring with a 45° slash, row dims separately
//
// Both overlays are aria-hidden decoration; the row's aria-label carries the
// full state sentence. Badge/dot rims use ring-popover so dark theme swaps
// the white rim for the panel colour.
// ---------------------------------------------------------------------------

/** Presence shape shown at the avatar's bottom-right. */
export type MentionPresenceState = "online" | "offline" | "disabled";

const STATUS_DOT_COLORS: Record<MentionPresenceState, string> = {
  online: "#3BA55D",
  offline: "#B9B7AC",
  // Hollow + slash: the fill is the panel colour, ring and slash are gray.
  disabled: "#B9B7AC",
};

interface BadgeGeometry {
  cliBadgePx: number | null;
  dotPx: number;
}

// the dot IS the app-wide AgentStatusDot scale —
// same function, same numbers (6px standard / 4px small).
function badgeGeometry(avatarPx: number): BadgeGeometry {
  if (avatarPx >= 34) return { cliBadgePx: 17, dotPx: (avatarPx >= 24 ? 6 : 4) };
  if (avatarPx >= 28) return { cliBadgePx: 14, dotPx: (avatarPx >= 24 ? 6 : 4) };
  return { cliBadgePx: null, dotPx: (avatarPx >= 24 ? 6 : 4) };
}

export function mentionPresenceState(
  availability: string | null | undefined,
): MentionPresenceState {
  // `unstable` (runtime recently lost) conservatively reads as offline — the
  // agent may not be reachable. `archived` is the design's "disabled" shape.
  if (availability === "online") return "online";
  if (availability === "archived") return "disabled";
  return "offline";
}

function PresenceDot({ px, state }: { px: number; state: MentionPresenceState }) {
  if (state === "disabled") {
    return (
      <span
        aria-hidden
        style={{ width: px, height: px }}
        className="absolute bottom-0 right-0 inline-flex translate-x-[30%] translate-y-[30%] items-center justify-center rounded-full ring-1 ring-popover"
      >
        <span
          className="relative inline-flex h-full w-full items-center justify-center rounded-full"
          style={{ boxShadow: `inset 0 0 0 1.5px ${STATUS_DOT_COLORS.disabled}` }}
        >
          <span
            className="absolute h-[1.5px] w-[86%] rounded-full"
            style={{ backgroundColor: STATUS_DOT_COLORS.disabled, transform: "rotate(-45deg)" }}
          />
        </span>
      </span>
    );
  }
  return (
    <span
      aria-hidden
      style={{
        width: px,
        height: px,
        backgroundColor: STATUS_DOT_COLORS[state],
      }}
      className="absolute bottom-0 right-0 inline-flex translate-x-[30%] translate-y-[30%] rounded-full ring-1 ring-popover"
    />
  );
}

/**
 * Avatar + CLI corner badge (top-right) + presence dot (bottom-right).
 * Purely presentational: pass the rendered avatar as `children`.
 */
export function MentionAvatarBadge({
  avatarPx,
  cli,
  provider,
  presence,
  className,
  children,
}: {
  /** Rendered avatar diameter; drives the geometry tier. */
  avatarPx: number;
  cli: MentionCli;
  provider?: string | null;
  presence: MentionPresenceState;
  className?: string;
  children: ReactNode;
}) {
  const { cliBadgePx, dotPx } = badgeGeometry(avatarPx);
  return (
    <span className={cn("relative inline-flex shrink-0", className)}>
      {children}
      {cliBadgePx !== null && cli !== "unknown" && (
        <MentionCliCornerBadge
          cli={cli}
          provider={provider}
          sizePx={cliBadgePx}
        />
      )}
      <PresenceDot px={dotPx} state={presence} />
    </span>
  );
}
