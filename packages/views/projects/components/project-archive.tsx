"use client";

import { useCallback, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useWorkspaceId } from "@multica/core/hooks";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useArchiveProject, useRestoreProject } from "@multica/core/projects";
import type { Project, ProjectStatus } from "@multica/core/types";
import { useT } from "../../i18n";
import { CollectionTag } from "../../layout/collection-list";

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Inline marker for an archived project in lists, search results and pins. */
export function ArchivedProjectTag() {
  const { t } = useT("projects");
  return <CollectionTag>{t(($) => $.archive.badge)}</CollectionTag>;
}

function openIssueCount(rows: Project[]): number {
  return rows.reduce((sum, p) => sum + Math.max(0, p.issue_count - p.done_count), 0);
}

interface ArchiveRequestOptions {
  onArchived?: () => void;
  /** Skip the confirmation, for callers that already asked (the delete dialog). */
  immediate?: boolean;
}

/**
 * Archive flow shared by every surface that offers it. A project with no open
 * issues and no active autopilots archives immediately behind an undo toast;
 * otherwise a dialog first says what stays and what pauses. Render `dialog`
 * once next to the trigger.
 */
export function useArchiveProjects(): {
  requestArchive: (rows: Project[], options?: ArchiveRequestOptions) => void;
  dialog: ReactNode;
} {
  const { t } = useT("projects");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const archiveProject = useArchiveProject();
  const restoreProject = useRestoreProject();
  const [pending, setPending] = useState<{
    rows: Project[];
    autopilotCount: number;
    onArchived?: () => void;
  } | null>(null);
  const [archiving, setArchiving] = useState(false);

  const archive = useCallback(
    async (rows: Project[], onArchived?: () => void) => {
      const archived: Project[] = [];
      let paused = 0;
      try {
        for (const project of rows) {
          const res = await archiveProject.mutateAsync(project.id);
          archived.push(project);
          paused += res.paused_autopilot_ids.length;
        }
      } catch (err) {
        toast.error(errorMessage(err));
      }
      if (archived.length === 0) return;
      onArchived?.();
      const title =
        archived.length === 1
          ? t(($) => $.archive.toast_archived, { name: archived[0]!.title })
          : t(($) => $.archive.toast_archived_count, { count: archived.length });
      toast.success(title, {
        description: paused > 0 ? t(($) => $.archive.toast_paused, { count: paused }) : undefined,
        action: {
          label: t(($) => $.archive.undo),
          onClick: () => {
            for (const project of archived) restoreProject.mutate(project.id);
          },
        },
      });
    },
    [archiveProject, restoreProject, t],
  );

  const requestArchive = useCallback(
    (rows: Project[], options?: ArchiveRequestOptions) => {
      const targets = rows.filter((p) => !p.archived_at);
      if (targets.length === 0) return;
      if (options?.immediate) {
        void archive(targets, options.onArchived);
        return;
      }
      void (async () => {
        const ids = new Set(targets.map((p) => p.id));
        let autopilotCount = 0;
        try {
          const { autopilots } = await qc.ensureQueryData(autopilotListOptions(wsId));
          autopilotCount = autopilots.filter(
            (a) => a.status === "active" && !!a.project_id && ids.has(a.project_id),
          ).length;
        } catch {
          // The count only decides whether to confirm; the server pauses
          // whatever is active regardless.
        }
        if (openIssueCount(targets) === 0 && autopilotCount === 0) {
          await archive(targets, options?.onArchived);
          return;
        }
        setPending({ rows: targets, autopilotCount, onArchived: options?.onArchived });
      })();
    },
    [archive, qc, wsId],
  );

  const confirm = async () => {
    if (!pending || archiving) return;
    setArchiving(true);
    try {
      await archive(pending.rows, pending.onArchived);
      setPending(null);
    } finally {
      setArchiving(false);
    }
  };

  const openIssues = pending ? openIssueCount(pending.rows) : 0;
  const dialog = (
    <Dialog
      open={pending !== null}
      onOpenChange={(open) => {
        if (!open && !archiving) setPending(null);
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={!archiving}>
        <DialogHeader>
          <DialogTitle>
            {pending?.rows.length === 1
              ? t(($) => $.archive.dialog_title, { name: pending.rows[0]!.title })
              : t(($) => $.archive.dialog_title_count, { count: pending?.rows.length ?? 0 })}
          </DialogTitle>
          <DialogDescription>{t(($) => $.archive.dialog_description)}</DialogDescription>
        </DialogHeader>
        <ul className="list-disc space-y-1 pl-5 text-body text-muted-foreground">
          {openIssues > 0 && <li>{t(($) => $.archive.dialog_open_issues, { count: openIssues })}</li>}
          {pending && pending.autopilotCount > 0 && (
            <li>{t(($) => $.archive.dialog_autopilots, { count: pending.autopilotCount })}</li>
          )}
          <li>{t(($) => $.archive.dialog_restore_hint)}</li>
        </ul>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={archiving}
            onClick={() => setPending(null)}
          >
            {t(($) => $.archive.cancel)}
          </Button>
          <Button type="button" size="sm" disabled={archiving} aria-busy={archiving} onClick={() => void confirm()}>
            {archiving ? <Spinner className="size-3.5" aria-hidden="true" /> : <Archive className="size-3.5" />}
            {t(($) => $.archive.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  return { requestArchive, dialog };
}

/**
 * After a project is marked completed, offer to archive it from the toast.
 * Clicking the action is the confirmation, and the row may have left the
 * current scope by then, so it archives without a dialog; the result toast
 * still carries Undo.
 */
export function useOfferArchiveOnComplete() {
  const { t } = useT("projects");
  const { requestArchive } = useArchiveProjects();
  return useCallback(
    (project: Project, nextStatus: ProjectStatus) => {
      if (nextStatus !== "completed" || project.status === "completed" || project.archived_at) return;
      toast.success(t(($) => $.archive.toast_completed, { name: project.title }), {
        action: {
          label: t(($) => $.page.archive),
          onClick: () => requestArchive([{ ...project, status: nextStatus }], { immediate: true }),
        },
      });
    },
    [requestArchive, t],
  );
}

/** Restore with a confirmation toast; paused autopilots stay paused. */
export function useRestoreProjects() {
  const { t } = useT("projects");
  const restoreProject = useRestoreProject();
  return useCallback(
    (project: Project) => {
      restoreProject.mutate(project.id, {
        onSuccess: () => toast.success(t(($) => $.archive.toast_restored, { name: project.title })),
        onError: (err) => toast.error(errorMessage(err)),
      });
    },
    [restoreProject, t],
  );
}

/** Notice under the project page header while the project is archived. */
export function ArchivedProjectNotice({ project, className }: { project: Project; className?: string }) {
  const { t } = useT("projects");
  const restore = useRestoreProjects();
  return (
    <div
      role="status"
      className={cn(
        "flex items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-caption text-muted-foreground",
        className,
      )}
    >
      <Archive className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">{t(($) => $.archive.notice)}</span>
      <Button variant="outline" size="xs" onClick={() => restore(project)}>
        {t(($) => $.page.restore)}
      </Button>
    </div>
  );
}
