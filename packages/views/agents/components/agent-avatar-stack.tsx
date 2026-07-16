"use client";

import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { AVATAR_SIZE_PX, type AvatarSize } from "@multica/ui/lib/avatar-size";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";

interface AgentAvatarStackProps {
  // Agent ids to render, in display order. The component does NOT dedupe —
  // callers are expected to pass a unique list (`new Set(...)` upstream).
  agentIds: readonly string[];
  // Semantic avatar tier. Avatars overlap by ~30% so the visible spacing
  // scales naturally with size. Defaults match a compact toolbar / card-corner
  // density (sm / 20 px).
  size?: AvatarSize;
  // Maximum head count before collapsing the tail into a `+N` chip. Three
  // is the plan default — beyond that the stack visually crowds.
  max?: number;
  // `half` drops opacity to 50%. Used by IssueAgentActivityIndicator to
  // signal a queued-only state (no running task) — same heads, weakened
  // visual.
  opacity?: "full" | "half";
  // How the tail beyond `max` is signalled.
  //   - `count` (default): a `+N` chip. Precise, but it reads as a second
  //     number — next to a count of something else (the working chip counts
  //     issues, these heads are agents) that lands as a contradiction.
  //   - `fade`: the last visible head dims instead. Says "there are more"
  //     without putting a rival number on screen; the exact roster lives in
  //     the hover card (MUL-4884).
  overflow?: "count" | "fade";
  className?: string;
}

/**
 * Overlapping avatar group for agents. Pure presentational — no data
 * fetching, no hover handling. Wrap it in a HoverCardTrigger upstream
 * (IssueAgentActivityIndicator / WorkspaceAgentWorkingChip) to surface
 * per-agent detail.
 *
 * `agentIds` is the full input list. We render up to `max` heads; if the
 * input is longer, we drop the tail and append a `+N` overflow chip styled
 * to match the avatar dimensions.
 */
export function AgentAvatarStack({
  agentIds,
  size = "sm",
  max = 3,
  opacity = "full",
  overflow: overflowMode = "count",
  className,
}: AgentAvatarStackProps) {
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  if (agentIds.length === 0) return null;

  const visible = agentIds.slice(0, max);
  const overflow = agentIds.length - visible.length;
  const fadeTail = overflowMode === "fade" && overflow > 0;
  const px = AVATAR_SIZE_PX[size];
  // 30% overlap reads as "stacked" without obscuring the next avatar's icon.
  const overlap = Math.round(px * 0.3);

  return (
    <span
      className={cn(
        "inline-flex items-center",
        opacity === "half" && "opacity-50",
        className,
      )}
      style={{ paddingLeft: 0 }}
    >
      {visible.map((id, i) => (
        <span
          key={id}
          // Each subsequent head sits negative-margin over the previous so
          // the stack collapses horizontally instead of growing linearly.
          style={{ marginLeft: i === 0 ? 0 : -overlap }}
          className={cn(
            "ring-2 ring-background rounded-full inline-flex",
            // Dim the last head when the tail is hidden — the "there are
            // more" cue in `fade` mode.
            fadeTail && i === visible.length - 1 && "opacity-40",
          )}
        >
          <ActorAvatarBase
            name={getActorName("agent", id)}
            initials={getActorInitials("agent", id)}
            avatarUrl={getActorAvatarUrl("agent", id)}
            isAgent
            size={size}
          />
        </span>
      ))}
      {overflowMode === "count" && overflow > 0 && (
        <span
          style={{
            marginLeft: -overlap,
            width: px,
            height: px,
            fontSize: Math.max(9, Math.round(px * 0.45)),
          }}
          className="ring-2 ring-background rounded-full bg-muted text-muted-foreground inline-flex items-center justify-center font-medium tabular-nums"
          aria-label={`${overflow} more`}
        >
          +{overflow}
        </span>
      )}
    </span>
  );
}
