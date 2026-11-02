"use client";

import { useCallback, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "@multica/ui/icons";
import { toast } from "sonner";
import type {
  SkillPackage,
  SkillPackageDeletePreview,
} from "@multica/core/api/schemas";
import { api } from "@multica/core/api";
import { invalidateSkillPackageQueries } from "@multica/core/skills/package-queries";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

type PreviewState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; preview: SkillPackageDeletePreview };

/**
 * Dissolve or delete a package, always behind the server's impact preview:
 * the affected skills and agents are listed before the confirm, and the
 * preview token guards the write. Cancel never touches the server.
 */
export function PackageRemoveDialog({
  wsId,
  pkg,
  mode,
  open,
  onOpenChange,
}: {
  wsId: string;
  pkg: SkillPackage;
  mode: "dissolve" | "delete";
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("skill-packages");
  const qc = useQueryClient();
  const [state, setState] = useState<PreviewState>({ status: "loading" });
  const [working, setWorking] = useState(false);

  const loadPreview = useCallback(async () => {
    setState({ status: "loading" });
    try {
      const preview = await api.getSkillPackageDeletePreview(wsId, pkg.id);
      if (preview === null) {
        setState({ status: "error", message: t(($) => $.remove.indeterminate) });
      } else {
        setState({ status: "ready", preview });
      }
    } catch (err) {
      setState({
        status: "error",
        message:
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.remove.indeterminate),
      });
    }
  }, [wsId, pkg.id, t]);

  useEffect(() => {
    if (open) void loadPreview();
  }, [open, loadPreview]);

  const handleConfirm = async () => {
    if (state.status !== "ready" || working) return;
    setWorking(true);
    try {
      const result =
        mode === "dissolve"
          ? await api.dissolveSkillPackage(wsId, pkg.id, state.preview.preview_id)
          : await api.deleteSkillPackage(wsId, pkg.id, state.preview.preview_id);
      if (result === null) {
        // Unreadable result: indeterminate — never claimed as failed or
        // written. The spent token is discarded and the impact preview
        // reloads, so any retry confirms against a fresh one.
        toast.error(t(($) => $.remove.unconfirmed));
        await loadPreview();
        return;
      }
      toast.success(
        result.dissolved ? t(($) => $.remove.dissolved) : t(($) => $.remove.deleted),
      );
      onOpenChange(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.remove.unconfirmed),
      );
      await loadPreview();
    } finally {
      setWorking(false);
      // Deletion commits per-skill before reporting: every settle path
      // refreshes the projections the write could have touched.
      await invalidateSkillPackageQueries(qc, wsId, { includeSkills: true });
    }
  };

  const ready = state.status === "ready" ? state.preview : null;
  const skillCount = ready?.skill_ids.length ?? 0;
  // can_delete gates only deletion (per-skill permission). Dissolution
  // needs package permission, which the menu already gated.
  const confirmAllowed =
    ready !== null && !working && (mode === "dissolve" || ready.can_delete);

  return (
    <AlertDialog
      open={open}
      onOpenChange={(v) => {
        if (!working) onOpenChange(v);
      }}
    >
      <AlertDialogContent className="sm:max-w-md">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {mode === "dissolve"
              ? t(($) => $.remove.dissolve_title)
              : t(($) => $.remove.delete_title)}
          </AlertDialogTitle>
          {ready && (
            <AlertDialogDescription>
              {mode === "dissolve"
                ? t(($) => $.remove.dissolve_description, { count: skillCount })
                : t(($) => $.remove.delete_description, { count: skillCount })}
            </AlertDialogDescription>
          )}
        </AlertDialogHeader>

        {state.status === "loading" && (
          <div className="flex items-center justify-center py-4">
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          </div>
        )}
        {state.status === "error" && (
          <div role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-caption text-destructive">
            {state.message}
          </div>
        )}
        {ready && (
          <div className="space-y-1.5">
            <div className="text-caption font-medium text-muted-foreground">
              {t(($) => $.remove.affected_agents)}
            </div>
            {ready.affected_agents.length === 0 ? (
              <div className="text-caption text-muted-foreground">
                {t(($) => $.remove.no_agents)}
              </div>
            ) : (
              <ul className="max-h-32 space-y-1 overflow-y-auto text-caption">
                {ready.affected_agents.map((agent) => (
                  <li key={`${agent.id}-${agent.skill_id}`} className="flex items-center gap-1.5">
                    <span className="truncate text-foreground">{agent.name}</span>
                    <span className="truncate text-muted-foreground">
                      · {agent.skill_name}
                    </span>
                  </li>
                ))}
              </ul>
            )}
            {!ready.can_delete && mode === "delete" && (
              <div className="rounded-md bg-warning/10 px-3 py-2 text-caption text-muted-foreground">
                {t(($) => $.remove.forbidden)}
              </div>
            )}
          </div>
        )}

        <AlertDialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={working}
          >
            {t(($) => $.remove.cancel)}
          </Button>
          {state.status === "error" && (
            <Button type="button" variant="outline" onClick={() => void loadPreview()}>
              {t(($) => $.tree.retry)}
            </Button>
          )}
          <Button
            type="button"
            variant={mode === "delete" ? "destructive" : "default"}
            onClick={handleConfirm}
            disabled={!confirmAllowed}
            aria-busy={working}
          >
            {working ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.remove.working)}
              </>
            ) : mode === "dissolve" ? (
              t(($) => $.remove.dissolve_confirm)
            ) : (
              t(($) => $.remove.delete_confirm)
            )}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
