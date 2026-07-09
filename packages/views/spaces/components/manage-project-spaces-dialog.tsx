"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ApiError,
  ProjectSpaceConflictResponseSchema,
  parseWithFallback,
  type ProjectSpaceConflict,
  type ProjectSpaceConflictResponse,
} from "@multica/core/api";
import { activeSpaceListOptions } from "@multica/core/spaces/queries";
import { useUpdateProject } from "@multica/core/projects/mutations";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { SpaceMultiPicker } from "./space-picker";
import { SpaceIcon } from "./space-icon";
import { PillButton } from "../../common/pill-button";
import { useT } from "../../i18n";

/**
 * Project → Space membership editor, reached from the project's 3-dot menu
 * (kept out of the always-visible property row on purpose: unlike
 * Status/Priority, removing a Space here can strand issues and needs its
 * own confirm step, so it doesn't belong at the same "click and done"
 * altitude as the property row).
 *
 * Two-step flow in one dialog: pick the space set, save. If the server
 * reports issues stranded by a Space leaving the set (409
 * project_space_has_issues), the dialog swaps to a per-Space "move to"
 * picker — mirroring, in the reverse direction, the
 * SpaceProjectConflictDialog shown when an issue's space isn't part of its
 * project.
 */
export function ManageProjectSpacesDialog({
  open,
  onOpenChange,
  wsId,
  projectId,
  spaceIds,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  wsId: string;
  projectId: string;
  spaceIds: string[];
}) {
  const { t } = useT("projects");
  const { data: spaces = [] } = useQuery(activeSpaceListOptions(wsId));
  const updateProject = useUpdateProject();

  const [selected, setSelected] = useState<string[]>(spaceIds);
  const [conflicts, setConflicts] = useState<ProjectSpaceConflict[] | null>(null);
  const [targets, setTargets] = useState<Record<string, string>>({});

  // Reset to the project's current space set each time the dialog opens —
  // stale local edits from a previous open (or an abandoned conflict step)
  // must not leak into the next one.
  useEffect(() => {
    if (open) {
      setSelected(spaceIds);
      setConflicts(null);
      setTargets({});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Excludes conflicting spaces defensively, not just spaces already absent
  // from `selected`: a Space reported as conflicting is, by construction,
  // one being removed, so it should never appear as its own move target —
  // even if the two ever drifted out of sync.
  const conflictingSpaceIds = useMemo(
    () => new Set((conflicts ?? []).map((c) => c.space_id)),
    [conflicts],
  );
  const remainingSpaces = useMemo(
    () => spaces.filter((space) => selected.includes(space.id) && !conflictingSpaceIds.has(space.id)),
    [spaces, selected, conflictingSpaceIds],
  );

  const totalConflictingIssues = useMemo(
    () => (conflicts ?? []).reduce((sum, c) => sum + c.issue_count, 0),
    [conflicts],
  );

  const handleSave = async () => {
    try {
      await updateProject.mutateAsync({ id: projectId, space_ids: selected });
      toast.success(t(($) => $.manage_spaces.toast_updated));
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        const parsed = parseWithFallback<ProjectSpaceConflictResponse | null>(
          err.body,
          ProjectSpaceConflictResponseSchema,
          null,
          { endpoint: "PUT /api/projects/:id (project_space_has_issues)" },
        );
        if (parsed) {
          setConflicts(parsed.spaces_with_issues);
          // Computed fresh from `parsed` rather than the `remainingSpaces`
          // memo: that memo still reflects pre-conflict state in this same
          // synchronous handler (setConflicts above hasn't re-rendered yet),
          // so reading it here would offer a conflicting Space as its own
          // move target. Default every conflicting Space to the first
          // still-selected, non-conflicting Space so the picker never opens
          // on an empty selection.
          const conflictingIds = new Set(parsed.spaces_with_issues.map((c) => c.space_id));
          const firstRemaining = spaces.find(
            (space) => selected.includes(space.id) && !conflictingIds.has(space.id),
          )?.id;
          if (firstRemaining) {
            setTargets(
              Object.fromEntries(parsed.spaces_with_issues.map((c) => [c.space_id, firstRemaining])),
            );
          }
          return;
        }
      }
      toast.error(t(($) => $.manage_spaces.toast_failed));
    }
  };

  const handleConfirmMove = async () => {
    try {
      await updateProject.mutateAsync({
        id: projectId,
        space_ids: selected,
        space_reassignments: targets,
      });
      toast.success(t(($) => $.manage_spaces.toast_updated));
      onOpenChange(false);
    } catch {
      toast.error(t(($) => $.manage_spaces.toast_failed));
    }
  };

  const isConflictStep = conflicts !== null;
  const allTargetsChosen = (conflicts ?? []).every((c) => !!targets[c.space_id]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {isConflictStep ? (
          <>
            <DialogTitle>{t(($) => $.manage_spaces.conflict_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.manage_spaces.conflict_body, { count: totalConflictingIssues })}
            </DialogDescription>
            <div className="flex flex-col gap-2">
              {conflicts?.map((conflict) => (
                <div key={conflict.space_id} className="flex items-center justify-between gap-2 text-sm">
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">
                    {t(($) => $.manage_spaces.conflict_row, {
                      count: conflict.issue_count,
                      space: conflict.space_key,
                    })}
                  </span>
                  <Select
                    value={targets[conflict.space_id] ?? ""}
                    onValueChange={(value) => {
                      if (!value) return;
                      setTargets((prev) => ({ ...prev, [conflict.space_id]: value }));
                    }}
                  >
                    <SelectTrigger size="sm" className="w-40">
                      <SelectValue placeholder={t(($) => $.manage_spaces.conflict_target_placeholder)}>
                        {(() => {
                          const target = remainingSpaces.find((space) => space.id === targets[conflict.space_id]);
                          return target ? (
                            <span className="flex items-center gap-1.5 truncate">
                              <SpaceIcon space={target} />
                              {target.name}
                            </span>
                          ) : null;
                        })()}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent align="end">
                      {remainingSpaces.map((space) => (
                        <SelectItem key={space.id} value={space.id}>
                          <span className="flex items-center gap-1.5">
                            <SpaceIcon space={space} />
                            {space.name}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ))}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setConflicts(null)}>
                {t(($) => $.manage_spaces.back)}
              </Button>
              <Button
                onClick={handleConfirmMove}
                disabled={!allTargetsChosen || updateProject.isPending}
              >
                {updateProject.isPending
                  ? t(($) => $.manage_spaces.saving)
                  : t(($) => $.manage_spaces.confirm_move)}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogTitle>{t(($) => $.manage_spaces.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.manage_spaces.description)}</DialogDescription>
            <SpaceMultiPicker
              spaceIds={selected}
              onChange={setSelected}
              triggerRender={<PillButton className="w-fit" />}
              align="start"
            />
            {selected.length === 0 && (
              <p className="text-xs text-destructive">{t(($) => $.manage_spaces.empty)}</p>
            )}
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t(($) => $.manage_spaces.cancel)}
              </Button>
              <Button onClick={handleSave} disabled={selected.length === 0 || updateProject.isPending}>
                {updateProject.isPending ? t(($) => $.manage_spaces.saving) : t(($) => $.manage_spaces.save)}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
