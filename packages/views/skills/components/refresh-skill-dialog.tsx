"use client";

import { useState } from "react";
import { Loader2, RotateCw } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Skill, SkillSummary } from "@multica/core/types";
import { api } from "@multica/core/api";
import {
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import type { OriginInfo } from "../lib/origin";

/** Human name of the hosted source a refresh re-downloads from. */
export function useRefreshSourceLabel(origin: OriginInfo | null): string {
  const { t } = useT("skills");
  if (origin?.type === "clawhub") return t(($) => $.detail.refresh.source_clawhub);
  if (origin?.type === "skills_sh") return t(($) => $.detail.refresh.source_skills_sh);
  return t(($) => $.detail.refresh.source_github);
}

/**
 * Confirm-and-run dialog for `POST /api/skills/:id/refresh`. The server
 * replaces name, description, SKILL.md, and the full file set with the fresh
 * upstream state while preserving the skill id — so agent assignments,
 * labels, and the creator survive. Local edits do not, hence the confirm.
 */
export function RefreshSkillDialog({
  skill,
  origin,
  wsId,
  open,
  onOpenChange,
  onRefreshed,
}: {
  skill: SkillSummary;
  origin: OriginInfo | null;
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fired after caches are updated; the detail page adopts the version here. */
  onRefreshed?: (updated: Skill) => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const [refreshing, setRefreshing] = useState(false);
  const source = useRefreshSourceLabel(origin);

  const handleConfirm = async () => {
    setRefreshing(true);
    try {
      const updated = await api.refreshSkill(skill.id);
      if (updated.id === skill.id) {
        qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, updated);
      } else {
        // Schema fallback (malformed response): drop the stale cache instead
        // of seeding it with the empty placeholder.
        qc.invalidateQueries({
          queryKey: skillDetailOptions(wsId, skill.id).queryKey,
        });
      }
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.refresh.toast_success, { source }));
      onOpenChange(false);
      if (updated.id === skill.id) onRefreshed?.(updated);
    } catch (e) {
      toast.error(
        e instanceof Error && e.message
          ? e.message
          : t(($) => $.detail.refresh.toast_failed),
      );
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!refreshing) onOpenChange(v);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.detail.refresh.dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.detail.refresh.dialog.description, {
              name: skill.name,
              source,
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-md bg-warning/10 px-3 py-2 text-caption text-muted-foreground">
          {t(($) => $.detail.refresh.dialog.warning)}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={refreshing}
          >
            {t(($) => $.detail.refresh.dialog.cancel)}
          </Button>
          <Button type="button" onClick={handleConfirm} disabled={refreshing}>
            {refreshing ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.detail.refresh.dialog.refreshing)}
              </>
            ) : (
              <>
                <RotateCw className="h-3 w-3" />
                {t(($) => $.detail.refresh.dialog.confirm)}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
