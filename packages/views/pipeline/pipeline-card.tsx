"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import type { Initiative } from "@multica/core/pipeline";
import { useT } from "../i18n";

/**
 * One initiative on the board. A button, not a draggable: a card moves only
 * when the server moves it, so the board offers no drag handle.
 */
export function PipelineCard({
  initiative,
  squadName,
  selected,
  onSelect,
}: {
  initiative: Initiative;
  /** The owner squad's name, or null when it has no owner. */
  squadName: string | null;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("pipeline");
  const stalled = initiative.sla_state === "stalled";
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full min-w-0 flex-col gap-2 rounded-lg border bg-card p-3 text-left hover:bg-accent",
        selected && "border-primary bg-accent ring-1 ring-primary/40",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="min-w-0 truncate text-caption text-muted-foreground">{initiative.identifier}</span>
        {stalled ? <Badge variant="destructive">{t(($) => $.card.stalled)}</Badge> : null}
      </div>
      <span className="line-clamp-2 text-body font-medium">{initiative.title}</span>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
        <span className="min-w-0 truncate">{squadName ?? t(($) => $.card.no_owner)}</span>
        <span>{t(($) => $.card.days_in_stage, { count: initiative.days_in_stage })}</span>
        <span>
          {t(($) => $.card.artifacts, {
            present: initiative.artifacts_present,
            required: initiative.artifacts_required,
          })}
        </span>
      </div>
    </button>
  );
}
