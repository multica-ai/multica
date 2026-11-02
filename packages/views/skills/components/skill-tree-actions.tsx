"use client";

// Labrastro fork (OL-104): folder-tree row and batch actions for the skills
// list. Upstream `skill-list-actions.tsx` mounts these with one line each and
// carries only the optional `treeActions` field on its shared context.

import { FolderInput, Unlink as PackageX } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { SkillActionsContext } from "./skill-list-actions";
import type { SkillRow } from "./skill-list-filter";

/** Folder-tree extras on `SkillActionsContext`. Present when the workspace
 *  folder tree loaded; absent callers keep the pre-tree flat behavior. */
export interface SkillTreeActions {
  /** Package/source association for a skill, when placed by a package. */
  placementFor: (skillId: string) => { package_id: string | null } | null;
  /** Open the move-to-folder dialog for the given rows. */
  onMove: (rows: SkillRow[]) => void;
  /** Detach a packaged skill from its package (keeps folder + skill). */
  onDetach: (row: SkillRow) => void;
}

/** Row kebab items: move (or the disabled packaged hint plus detach). */
export function SkillTreeRowMenuItems({
  row,
  ctx,
}: {
  row: SkillRow;
  ctx: SkillActionsContext;
}) {
  const { t: tPkg } = useT("skill-packages");
  const treeActions = ctx.treeActions;
  const isPackaged = treeActions?.placementFor(row.skill.id)?.package_id != null;

  return (
    <>
      {treeActions && row.canEdit && !isPackaged && (
        <DropdownMenuItem onClick={() => treeActions.onMove([row])}>
          <FolderInput className="size-3.5" />
          {tPkg(($) => $.move_skills.title, { count: 1 })}
        </DropdownMenuItem>
      )}
      {treeActions && row.canEdit && isPackaged && (
        <>
          {/* The server refuses to move a packaged skill (409
              managed_skill): it must be detached first. */}
          <DropdownMenuItem disabled>
            <FolderInput className="size-3.5" />
            <span className="flex min-w-0 flex-col">
              <span>{tPkg(($) => $.move_skills.title, { count: 1 })}</span>
              <span className="text-caption text-muted-foreground">
                {tPkg(($) => $.move_skills.packaged_hint)}
              </span>
            </span>
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => treeActions.onDetach(row)}>
            <PackageX className="size-3.5" />
            {tPkg(($) => $.detach.action)}
          </DropdownMenuItem>
        </>
      )}
    </>
  );
}

/** Batch toolbar move button; disabled with a reason tooltip when blocked. */
export function SkillTreeBatchMoveAction({
  rows,
  ctx,
}: {
  rows: SkillRow[];
  ctx: SkillActionsContext;
}) {
  const { t: tPkg } = useT("skill-packages");
  const treeActions = ctx.treeActions;
  if (!treeActions) return null;

  const allDeletable = rows.every((r) => r.canEdit);
  // A mixed batch containing any packaged skill cannot move: the server
  // rejects those items, so the whole action is disabled with the reason.
  const anyPackaged = rows.some(
    (r) => treeActions.placementFor(r.skill.id)?.package_id != null,
  );
  const moveDisabled = !allDeletable || anyPackaged;
  const moveHint = anyPackaged
    ? tPkg(($) => $.move_skills.packaged_hint)
    : tPkg(($) => $.move_skills.no_permission);

  const moveButton = (
    <Button
      variant="ghost"
      size="sm"
      disabled={moveDisabled}
      onClick={() => treeActions?.onMove(rows)}
      className={cn(moveDisabled && "pointer-events-none")}
    >
      <FolderInput className="mr-1 size-3.5" />
      {tPkg(($) => $.move_skills.confirm)}
    </Button>
  );

  return !moveDisabled ? (
    moveButton
  ) : (
    <Tooltip>
      <TooltipTrigger
        render={<span className="inline-flex">{moveButton}</span>}
      />
      <TooltipContent side="top">
        {moveHint}
      </TooltipContent>
    </Tooltip>
  );
}
