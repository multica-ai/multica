import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  descriptionVersionOptions,
  descriptionVersionsOptions,
} from "@multica/core/issues/queries";
import { useRestoreDescriptionVersion } from "@multica/core/issues/mutations";
import type { DescriptionVersion } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { RotateCcw } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT, useTimeAgo } from "../../i18n";
import { DescriptionDiffView } from "./description-diff-view";

interface DescriptionHistoryDialogProps {
  issueId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  getActorName: (type: string, id: string) => string;
  /** Preselects a version — the timeline's "view changes" entry point. */
  initialVersionId?: string | null;
}

/**
 * Description version history: list on the left, diff on the right.
 *
 * The list carries no snapshots (the API omits them), so opening the panel on a
 * long-lived issue costs one small request; the two sides of the selected diff
 * are fetched by id.
 */
export function DescriptionHistoryDialog({
  issueId,
  open,
  onOpenChange,
  getActorName,
  initialVersionId,
}: DescriptionHistoryDialogProps) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const [selectedId, setSelectedId] = useState<string | null>(initialVersionId ?? null);

  const { data: versions, isLoading } = useQuery({
    ...descriptionVersionsOptions(issueId),
    enabled: open,
  });

  // Default to the newest version once the list lands. Keyed on the list rather
  // than on `open` so reopening the panel does not fight a selection the user
  // just made via the timeline entry point.
  useEffect(() => {
    if (!open) return;
    if (initialVersionId) {
      setSelectedId(initialVersionId);
      return;
    }
    setSelectedId((current) => current ?? versions?.[0]?.id ?? null);
  }, [open, initialVersionId, versions]);

  const selected = useMemo(
    () => versions?.find((v) => v.id === selectedId) ?? null,
    [versions, selectedId],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[80vh] w-[min(56rem,92vw)] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="border-b border-border px-4 py-3">
          <DialogTitle className="text-sm font-semibold">
            {t(($) => $.description_history.title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
          <div className="shrink-0 overflow-y-auto border-b border-border sm:max-h-none sm:w-64 sm:border-b-0 sm:border-r">
            {isLoading ? (
              <div className="space-y-2 p-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : versions && versions.length > 0 ? (
              <ul className="py-1">
                {versions.map((version) => (
                  <li key={version.id}>
                    <VersionRow
                      version={version}
                      selected={version.id === selectedId}
                      onSelect={() => setSelectedId(version.id)}
                      actorName={
                        version.actor_type && version.actor_id
                          ? getActorName(version.actor_type, version.actor_id)
                          : t(($) => $.description_history.unknown_actor)
                      }
                      timeAgoLabel={timeAgo(version.updated_at)}
                      originalLabel={t(($) => $.description_history.original_badge)}
                    />
                  </li>
                ))}
              </ul>
            ) : (
              <p className="p-4 text-xs text-muted-foreground">
                {t(($) => $.description_history.empty_history)}
              </p>
            )}
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            {selected ? (
              <VersionDiffPane
                issueId={issueId}
                version={selected}
                onRestored={() => onOpenChange(false)}
              />
            ) : (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.description_history.select_prompt)}
              </p>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function VersionRow({
  version,
  selected,
  onSelect,
  actorName,
  timeAgoLabel,
  originalLabel,
}: {
  version: DescriptionVersion;
  selected: boolean;
  onSelect: () => void;
  actorName: string;
  timeAgoLabel: string;
  originalLabel: string;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-2 px-3 py-2 text-left transition-colors hover:bg-muted/60",
        selected && "bg-muted",
      )}
    >
      {version.actor_type && version.actor_id ? (
        <ActorAvatar actorType={version.actor_type} actorId={version.actor_id} size="sm" />
      ) : (
        <span className="h-4 w-4 shrink-0 rounded-full bg-muted" />
      )}
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span className="truncate text-xs font-medium">{actorName}</span>
          {version.is_original === true && (
            <span className="shrink-0 rounded bg-muted px-1 py-0.5 text-[10px] font-medium text-muted-foreground">
              {originalLabel}
            </span>
          )}
        </span>
        <span className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
          <span>{timeAgoLabel}</span>
          {version.is_original !== true && (
            <DiffStat added={version.added_lines} removed={version.removed_lines} />
          )}
        </span>
      </span>
    </button>
  );
}

/**
 * Fetches both sides of the selected version's diff.
 *
 * The parent is the version this one was written against, so the pane always
 * answers "what did this edit change", not "how does this differ from now".
 */
function VersionDiffPane({
  issueId,
  version,
  onRestored,
}: {
  issueId: string;
  version: DescriptionVersion;
  onRestored: () => void;
}) {
  const { t } = useT("issues");
  const restore = useRestoreDescriptionVersion();
  const [confirming, setConfirming] = useState(false);

  const { data: current, isLoading: loadingCurrent } = useQuery(
    descriptionVersionOptions(issueId, version.id),
  );
  const { data: parent, isLoading: loadingParent } = useQuery({
    ...descriptionVersionOptions(issueId, version.parent_version_id ?? ""),
    enabled: version.parent_version_id != null,
  });

  // Reset the confirm affordance when the user picks a different version, or a
  // stray second click would restore something they were only browsing.
  useEffect(() => {
    setConfirming(false);
  }, [version.id]);

  if (loadingCurrent || (version.parent_version_id != null && loadingParent)) {
    return <Skeleton className="h-40 w-full" />;
  }

  const after = current?.content ?? "";
  const before = version.parent_version_id != null ? (parent?.content ?? "") : "";

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          {version.is_original === true
            ? t(($) => $.description_history.original_hint)
            : t(($) => $.description_history.diff_against_parent)}
        </p>
        <div className="flex items-center gap-2">
          {confirming ? (
            <>
              <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                {t(($) => $.description_history.cancel)}
              </Button>
              <Button
                size="sm"
                disabled={restore.isPending}
                onClick={() =>
                  restore.mutate(
                    { issueId, versionId: version.id },
                    { onSuccess: onRestored },
                  )
                }
              >
                {t(($) => $.description_history.confirm_restore)}
              </Button>
            </>
          ) : (
            <Button size="sm" variant="outline" onClick={() => setConfirming(true)}>
              <RotateCcw className="mr-1 h-3 w-3" />
              {t(($) => $.description_history.restore)}
            </Button>
          )}
        </div>
      </div>

      {confirming && (
        <p className="rounded-md border border-border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          {t(($) => $.description_history.restore_hint)}
        </p>
      )}

      <DescriptionDiffView before={before} after={after} />
    </div>
  );
}

/** Shared +N −M badge. Tabular so the numbers do not jitter between rows. */
export function DiffStat({
  added,
  removed,
  className,
}: {
  added: number;
  removed: number;
  className?: string;
}) {
  if (added === 0 && removed === 0) return null;
  return (
    <span className={cn("shrink-0 tabular-nums", className)}>
      {added > 0 && <span className="text-emerald-600 dark:text-emerald-400">+{added}</span>}
      {added > 0 && removed > 0 && " "}
      {removed > 0 && <span className="text-rose-600 dark:text-rose-400">−{removed}</span>}
    </span>
  );
}
