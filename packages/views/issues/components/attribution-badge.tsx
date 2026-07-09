"use client";

import type { TaskAttribution } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
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
 * AttributionBadge renders the "on behalf of <member>" chip for an agent run
 * (MUL-4302 §9): who the run is accountable to, with the resolution source as a
 * tooltip and a distinct warning tone for degraded (non-precise) attribution.
 * Renders nothing when the task has no attribution (older backends) — the caller
 * should optional-chain `task.attribution`.
 */
export function AttributionBadge({
  attribution,
  className,
}: {
  attribution?: TaskAttribution;
  className?: string;
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

  // Degraded attribution (owner_fallback / backfill / unattributed) is marked
  // distinctly so it never reads as a compliance-grade "who is responsible".
  const degraded = attribution.precise === false;
  const initiator = attribution.initiator;

  if (!initiator) {
    return (
      <Badge
        variant="outline"
        className={cn("gap-1 font-normal text-warning", className)}
        title={sourceLabel}
      >
        {t(($) => $.execution_log.attribution.unattributed)}
      </Badge>
    );
  }

  const name = initiator.name || t(($) => $.execution_log.attribution.someone);
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1 font-normal",
        degraded ? "text-warning" : "text-muted-foreground",
        className
      )}
      title={sourceLabel}
    >
      <ActorAvatar
        name={name}
        initials={initialsOf(name)}
        avatarUrl={initiator.avatar_url}
        size={14}
      />
      <span>{t(($) => $.execution_log.attribution.on_behalf_of, { name })}</span>
    </Badge>
  );
}
