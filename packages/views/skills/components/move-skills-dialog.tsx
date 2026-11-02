"use client";

import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Folder, Inbox, Loader2 } from "@multica/ui/icons";
import { toast } from "sonner";
import type { SkillFolderTree } from "@multica/core/api/schemas";
import { api } from "@multica/core/api";
import { invalidateSkillPackageQueries } from "@multica/core/skills/package-queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { skillMoveDestinations } from "../lib/skill-folder-tree";
import type { SkillRow } from "./skill-list-filter";

/**
 * Move one or more skills into a custom folder (or back to the root). Package
 * folders refuse custom content. Moves run sequentially with per-item failure
 * tolerance, like the batch update flow.
 */
export function MoveSkillsDialog({
  wsId,
  tree,
  rows,
  open,
  onOpenChange,
  onMoved,
}: {
  wsId: string;
  tree: SkillFolderTree;
  rows: SkillRow[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fired only after a fully successful run, so partial failures keep the selection. */
  onMoved?: () => void;
}) {
  const { t } = useT("skill-packages");
  const qc = useQueryClient();
  const destinations = useMemo(() => skillMoveDestinations(tree.folders), [tree.folders]);
  // null = uncategorized; undefined = nothing chosen yet.
  const [target, setTarget] = useState<string | null | undefined>(undefined);
  const [moving, setMoving] = useState(false);

  const handleOpenChange = (v: boolean) => {
    if (moving) return;
    if (!v) setTarget(undefined);
    onOpenChange(v);
  };

  const handleConfirm = async () => {
    if (target === undefined || moving) return;
    setMoving(true);
    let done = 0;
    let failed = 0;
    // Unreadable responses are not failures: the write may have committed.
    let unconfirmed = 0;
    try {
      for (const row of rows) {
        try {
          const result = await api.setSkillPlacement(wsId, row.skill.id, target ?? "");
          if (result === null) unconfirmed++;
          else done++;
        } catch {
          failed++;
        }
      }
      await invalidateSkillPackageQueries(qc, wsId);
      if (failed === 0 && unconfirmed === 0) {
        toast.success(t(($) => $.move_skills.done, { count: done }));
        handleOpenChange(false);
        onMoved?.();
      } else if (failed === 0) {
        toast.error(
          t(($) => $.move_skills.unconfirmed, {
            done,
            total: rows.length,
            count: unconfirmed,
          }),
        );
        handleOpenChange(false);
      } else {
        toast.error(
          done > 0
            ? t(($) => $.move_skills.partial, { done, total: rows.length })
            : t(($) => $.move_skills.failed),
        );
        handleOpenChange(false);
      }
    } finally {
      setMoving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.move_skills.title, { count: rows.length })}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <div className="text-caption text-muted-foreground">
            {t(($) => $.move_skills.destination)}
          </div>
          <div className="max-h-64 space-y-0.5 overflow-y-auto rounded-lg border bg-card p-1.5">
            <button
              type="button"
              aria-pressed={target === null}
              onClick={() => setTarget(null)}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-body transition-colors",
                target === null ? "bg-accent" : "hover:bg-accent/50",
              )}
            >
              <Inbox className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              {t(($) => $.move_skills.root_option)}
            </button>
            {destinations.map((dest) => (
              <button
                key={dest.id}
                type="button"
                aria-pressed={target === dest.id}
                onClick={() => setTarget(dest.id)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-body transition-colors",
                  target === dest.id ? "bg-accent" : "hover:bg-accent/50",
                )}
              >
                <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{dest.name}</span>
              </button>
            ))}
          </div>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => handleOpenChange(false)}
            disabled={moving}
          >
            {t(($) => $.move_skills.cancel)}
          </Button>
          <Button
            type="button"
            onClick={() => void handleConfirm()}
            disabled={target === undefined || moving || rows.length === 0}
            aria-busy={moving}
          >
            {moving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.move_skills.moving)}
              </>
            ) : (
              t(($) => $.move_skills.confirm)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
