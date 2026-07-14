"use client";

import type { TaskAttribution } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/** First + last initial, for the avatar fallback when there's no picture. */
function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const first = parts[0];
  if (!first) return "?";
  const last = parts[parts.length - 1];
  if (parts.length === 1 || !last) return first.slice(0, 2).toUpperCase();
  return (first.charAt(0) + last.charAt(0)).toUpperCase();
}

/**
 * AttributionBadge renders who an agent run is accountable to (MUL-4302 §9):
 * the "on behalf of <member>" provenance, with the resolution source in a
 * tooltip and a cautionary tone ONLY when the named human may not be the real
 * responsible person — a fallback guess (owner_fallback). A backfilled
 * attribution is a historical, after-the-fact record (non-realtime, not
 * compliance-grade), but that does not make the displayed name wrong, so it
 * earns no warning tone; its historical origin still shows in the tooltip and
 * the raw `source` field (MUL-4768).
 *
 * Two shapes, both silent when no responsible member resolved (MUL-4765):
 *  - `variant="badge"` (default): the full "on behalf of <name>" chip. Renders
 *    nothing when there's no accountable member, so an unassigned run reads as
 *    plain rather than a warning.
 *  - `variant="avatar"`: just the accountable member's avatar, with the name +
 *    source in a hover tooltip. Compact enough for a dense task row. Renders
 *    nothing when there's no accountable member — an avatar-only surface has
 *    nothing meaningful to show for an unattributed run.
 *
 * Renders nothing when the task has no attribution at all (older backends) —
 * the caller should optional-chain `task.attribution`.
 */
export function AttributionBadge({
  attribution,
  className,
  variant = "badge",
}: {
  attribution?: TaskAttribution;
  className?: string;
  variant?: "badge" | "avatar";
}) {
  const { t } = useT("issues");
  if (!attribution) return null;

  // Human-readable resolution source, defaulting to the raw label so a
  // server-added source degrades gracefully instead of showing blank.
  let sourceLabel: string;
  switch (attribution.source) {
    case "direct_human":
      sourceLabel = t(($) => $.execution_log.attribution.source_direct_human);
      break;
    case "delegation":
      sourceLabel = t(($) => $.execution_log.attribution.source_delegation);
      break;
    case "comment_source":
      sourceLabel = t(($) => $.execution_log.attribution.source_comment_source);
      break;
    case "trigger_owner":
      sourceLabel = t(($) => $.execution_log.attribution.source_trigger_owner);
      break;
    case "rule_owner":
      sourceLabel = t(($) => $.execution_log.attribution.source_rule_owner);
      break;
    case "owner_fallback":
      sourceLabel = t(($) => $.execution_log.attribution.source_owner_fallback);
      break;
    case "backfill":
      sourceLabel = t(($) => $.execution_log.attribution.source_backfill);
      break;
    case "unattributed":
      sourceLabel = t(($) => $.execution_log.attribution.source_unattributed);
      break;
    default:
      sourceLabel = attribution.source;
  }

  // The backend's `precise` flag is an attribution-*coverage* health bit:
  // owner_fallback, backfill, and unattributed all fail it. But coverage is an
  // ops metric, not a reader-facing signal. The only thing a viewer of "on
  // behalf of <name>" cares about is whether that named human might NOT actually
  // be who's responsible — true only for a fallback guess (owner_fallback:
  // nothing resolved, so we defaulted to the agent owner). Backfill is a
  // historical, after-the-fact record (non-realtime, not compliance-grade), but
  // that does not make the displayed name wrong — so it warrants no warning tone;
  // its historical origin stays visible in the tooltip and the raw `source` field
  // (MUL-4768). The cautionary tone therefore fires for any non-precise source
  // EXCEPT backfill; keeping the `precise === false` base means a future unknown
  // degraded source still warns (fail-safe) instead of silently reading as
  // confident.
  const uncertain =
    attribution.precise === false && attribution.source !== "backfill";
  const initiator = attribution.initiator;

  // Avatar-only shape: just the accountable member's face, with the name +
  // source in a hover tooltip. Nothing to show without an accountable member.
  if (variant === "avatar") {
    if (!initiator) return null;
    const name = initiator.name || t(($) => $.execution_log.attribution.someone);
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              className={cn(
                "inline-flex shrink-0",
                // A subtle ring flags a fallback guess so an owner-fallback face
                // never silently reads as a confidently resolved responsible member.
                uncertain && "rounded-full ring-1 ring-warning/60",
                className
              )}
            >
              <ActorAvatar
                name={name}
                initials={initialsOf(name)}
                avatarUrl={initiator.avatar_url}
                size="xs"
              />
            </span>
          }
        />
        <TooltipContent>
          <div className="flex flex-col">
            <span>
              {t(($) => $.execution_log.attribution.on_behalf_of, { name })}
            </span>
            <span
              className={cn(
                "text-[11px]",
                uncertain ? "text-warning" : "text-muted-foreground"
              )}
            >
              {sourceLabel}
            </span>
          </div>
        </TooltipContent>
      </Tooltip>
    );
  }

  // No resolved responsible member: render nothing rather than a warning chip.
  // An empty accountable member is a normal state (e.g. an unassigned task), not
  // something to flag — so the badge variant stays silent, matching the avatar
  // variant above (MUL-4765).
  if (!initiator) return null;

  const name = initiator.name || t(($) => $.execution_log.attribution.someone);
  return (
    <Badge
      variant="outline"
      className={cn(
        "max-w-40 min-w-0 gap-1 font-normal",
        uncertain ? "text-warning" : "text-muted-foreground",
        className
      )}
      title={sourceLabel}
    >
      <ActorAvatar
        name={name}
        initials={initialsOf(name)}
        avatarUrl={initiator.avatar_url}
        size="xs"
        className="shrink-0"
      />
      <span className="min-w-0 truncate">
        {t(($) => $.execution_log.attribution.on_behalf_of, { name })}
      </span>
    </Badge>
  );
}
