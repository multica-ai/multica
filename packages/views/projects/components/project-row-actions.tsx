"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, ArchiveRestore, ExternalLink, Link2, Pencil, Pin, PinOff, Trash2 } from "@multica/ui/icons";
import { copyText } from "@multica/ui/lib/clipboard";
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
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { ProjectDeletionOfflineError, useDeleteProject } from "@multica/core/projects";
import { canDeleteProject } from "@multica/core/projects/deletion-permissions";
import type { Project } from "@multica/core/types";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useT } from "../../i18n";
import { useIntentNavigate, useNavigation } from "../../navigation";
import { CollectionRowMenu } from "../../layout/collection-list";
import { useArchiveProjects, useRestoreProjects } from "./project-archive";

export function DeleteProjectsDialog({
  rows,
  open,
  onOpenChange,
  onDeleted,
  onArchiveInstead,
}: {
  rows: Project[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
  /** Offers archiving as the reversible alternative; omitted when every row is already archived. */
  onArchiveInstead?: () => void;
}) {
  const { t } = useT("projects");
  const canArchiveInstead = !!onArchiveInstead && rows.some((p) => !p.archived_at);
  const deleteProject = useDeleteProject();
  const [deleting, setDeleting] = useState(false);

  const handleDelete = async () => {
    if (deleting) return;
    setDeleting(true);
    try {
      for (const project of rows) {
        await deleteProject.mutateAsync(project.id);
      }
      onOpenChange(false);
      onDeleted?.();
    } catch (err) {
      toast.error(
        err instanceof ProjectDeletionOfflineError
          ? t(($) => $.delete_dialog.offline_error)
          : err instanceof Error ? err.message : String(err),
      );
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!deleting) onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={!deleting}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.delete_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.delete_dialog.description)}
          </DialogDescription>
        </DialogHeader>
        {canArchiveInstead && (
          <div className="flex items-start gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-caption text-muted-foreground">
            <Archive className="mt-0.5 size-3.5 shrink-0" />
            <span>{t(($) => $.delete_dialog.archive_hint)}</span>
          </div>
        )}
        <DialogFooter>
          {canArchiveInstead && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="sm:mr-auto"
              disabled={deleting}
              onClick={() => {
                onOpenChange(false);
                onArchiveInstead?.();
              }}
            >
              <Archive className="size-3.5" />
              {t(($) => $.delete_dialog.archive_instead)}
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={deleting}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.delete_dialog.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            disabled={deleting}
            aria-busy={deleting}
            onClick={handleDelete}
          >
            {deleting && <Spinner className="size-3.5" aria-hidden="true" />}
            {t(($) => $.delete_dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * A project's row menu, shared by the projects page (table rows, ⋯ and
 * right-click, and cards) and the sidebar. `leading` items go first, for
 * actions only one surface offers; `triggerHidden` is for rows that open it
 * only on right-click.
 */
export function ProjectRowActions({
  project,
  pinned,
  canDelete,
  leading,
  trailing,
  triggerHidden,
  finalFocus,
}: {
  project: Project;
  pinned: boolean;
  canDelete: boolean;
  leading?: ReactNode;
  /** After the project's own items, e.g. the surrounding comment's submenu. */
  trailing?: ReactNode;
  triggerHidden?: boolean;
  finalFocus?: () => boolean;
}) {
  const { t } = useT("projects");
  const { t: tCommon } = useT("common");
  const { t: tLayout } = useT("layout");
  const wsPaths = useWorkspacePaths();
  const intentNavigate = useIntentNavigate();
  const { getShareableUrl } = useNavigation();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const { requestArchive, dialog: archiveDialog } = useArchiveProjects();
  const restore = useRestoreProjects();

  const togglePin = () => {
    if (pinned) deletePin.mutate({ itemType: "project", itemId: project.id });
    else createPin.mutate({ item_type: "project", item_id: project.id });
  };
  const copyLink = async () => {
    if (await copyText(getShareableUrl(wsPaths.projectDetail(project.id)))) {
      toast.success(tLayout(($) => $.rail.link_copied));
    }
  };

  return (
    <>
      <CollectionRowMenu
        label={t(($) => $.page.row_menu)}
        contentClassName="w-auto min-w-52"
        triggerHidden={triggerHidden}
        finalFocus={finalFocus}
      >
        {leading && (
          <>
            {leading}
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuItem
          onClick={() =>
            useModalStore
              .getState()
              .open("project-rename", { projectId: project.id, title: project.title })
          }
        >
          <Pencil className="size-3.5" />
          {t(($) => $.page.rename)}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            intentNavigate(
              wsPaths.projectDetail(project.id),
              "foreground-tab",
              project.title,
            )
          }
        >
          <ExternalLink className="size-3.5" />
          {tCommon(($) => $.navigation.open_in_new_tab)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={togglePin}>
          {pinned ? (
            <PinOff className="size-3.5" />
          ) : (
            <Pin className="size-3.5" />
          )}
          {pinned ? t(($) => $.page.unpin) : t(($) => $.page.pin)}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void copyLink()}>
          <Link2 className="size-3.5" />
          {tLayout(($) => $.rail.copy_link)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {project.archived_at ? (
          <DropdownMenuItem onClick={() => restore(project)}>
            <ArchiveRestore className="size-3.5" />
            {t(($) => $.page.restore)}
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onClick={() => requestArchive([project])}>
            <Archive className="size-3.5" />
            {t(($) => $.page.archive)}
          </DropdownMenuItem>
        )}
        {canDelete && (
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 className="size-3.5" />
            {t(($) => $.page.delete)}
          </DropdownMenuItem>
        )}
        {trailing}
      </CollectionRowMenu>

      <DeleteProjectsDialog
        rows={[project]}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onArchiveInstead={() => requestArchive([project], { immediate: true })}
      />
      {archiveDialog}
    </>
  );
}

/**
 * ProjectRowActions for a surface that does not already hold the viewer's
 * pins and permissions: it reads them from the shared caches.
 */
export function ProjectMenu({
  project,
  leading,
  trailing,
  triggerHidden,
  finalFocus,
}: {
  project: Project;
  leading?: ReactNode;
  trailing?: ReactNode;
  triggerHidden?: boolean;
  finalFocus?: () => boolean;
}) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: pins = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!wsId && !!userId,
  });
  const role = members.find((m) => m.user_id === userId)?.role ?? null;
  const pinned = pins.some((pin) => pin.item_type === "project" && pin.item_id === project.id);

  return (
    <ProjectRowActions
      project={project}
      pinned={pinned}
      canDelete={canDeleteProject(project, userId, role, agents)}
      leading={leading}
      trailing={trailing}
      triggerHidden={triggerHidden}
      finalFocus={finalFocus}
    />
  );
}
