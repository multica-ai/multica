"use client";

import { useCallback, useRef, useState, type ReactNode } from "react";
import { onlineManager, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check, Folder, FolderMinus, FolderPlus, Pencil, Plus, Trash2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import type { Project, WSEventType } from "@multica/core/types";
import { PillButton } from "@multica/views/common/pill-button";
import type { ProjectFoldersExtension } from "@multica/views/extensions";
import { CollectionRowMenu } from "@multica/views/layout/collection-list";
import { folderKeys, useCreateFolder, useDeleteFolder, useFileProject, useRenameFolder, type ProjectFolder } from "./data";
import { useFoldersT } from "./i18n";
import { useFolderDialogStore, useProjectFoldersState } from "./state";

const openDialog = useFolderDialogStore.getState().open;

function AddMenuItems() {
  const t = useFoldersT();
  return (
    <DropdownMenuItem onClick={() => openDialog({ kind: "create" })}>
      <FolderPlus className="size-3.5" />
      {t("new_folder")}
    </DropdownMenuItem>
  );
}

function FolderMenu({ folder }: { folder: ProjectFolder }) {
  const t = useFoldersT();
  const { canFile } = useProjectFoldersState(useWorkspaceId());
  return (
    <CollectionRowMenu
      label={t("folder_actions", { name: folder.name })}
      contentClassName="w-auto min-w-52"
      triggerHidden
    >
      {canFile && (
        <>
          <DropdownMenuItem onClick={() => useModalStore.getState().open("create-project", { folderId: folder.id })}>
            <Plus className="size-3.5" />
            {t("new_project")}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
        </>
      )}
      <DropdownMenuItem onClick={() => openDialog({ kind: "rename", folder })}>
        <Pencil className="size-3.5" />
        {t("rename")}
      </DropdownMenuItem>
      <DropdownMenuItem variant="destructive" onClick={() => openDialog({ kind: "delete", folder })}>
        <Trash2 className="size-3.5" />
        {t("delete")}
      </DropdownMenuItem>
    </CollectionRowMenu>
  );
}

/** "Move to folder" in a project's menu: every folder, the current one checked. */
function ProjectMenuItems({ project }: { project: Project }) {
  const t = useFoldersT();
  const wsId = useWorkspaceId();
  const { folders, folderOf } = useProjectFoldersState(wsId);
  const { mutate: file } = useFileProject(wsId);
  const current = folderOf.get(project.id) ?? null;

  const move = (folder: ProjectFolder | null) => {
    if ((folder?.id ?? null) === current) return;
    file(
      { projectId: project.id, folderId: folder?.id ?? null },
      { onSuccess: () => folder && toast.success(t("toast_moved", { name: folder.name })) },
    );
  };

  let items: ReactNode = folders.map((folder) => (
    <DropdownMenuItem key={folder.id} onClick={() => move(folder)}>
      <Folder className="size-3.5" />
      <span className="min-w-0 flex-1 truncate">{folder.name}</span>
      {folder.id === current && <Check className="size-3.5 shrink-0" />}
    </DropdownMenuItem>
  ));
  if (current) {
    items = (
      <>
        {items}
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => move(null)}>
          <FolderMinus className="size-3.5" />
          {t("remove_from_folder")}
        </DropdownMenuItem>
      </>
    );
  }

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <Folder className="size-3.5" />
        <span className="flex-1">{t("move_to_folder")}</span>
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 max-w-64 overflow-y-auto">
        {items}
        {folders.length > 0 && !current && <DropdownMenuSeparator />}
        <DropdownMenuItem onClick={() => openDialog({ kind: "create", fileProjectId: project.id })}>
          <FolderPlus className="size-3.5" />
          {t("new_folder_ellipsis")}
        </DropdownMenuItem>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}

/** The folder a new project is filed in; shown once there is a folder to file it in. */
function CreateProjectField({
  value,
  onChange,
}: {
  value: string | null;
  onChange: (folderId: string | null) => void;
}) {
  const t = useFoldersT();
  const { folders, canFile } = useProjectFoldersState(useWorkspaceId());
  if (folders.length === 0 || !canFile) return null;
  const current = folders.find((folder) => folder.id === value);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <PillButton>
            {current ? <Folder className="size-3" /> : <FolderMinus className="size-3" />}
            <span className="max-w-[10rem] truncate">{current?.name ?? t("no_folder")}</span>
          </PillButton>
        }
      />
      <DropdownMenuContent align="start" className="max-h-72 w-auto min-w-48 max-w-64 overflow-y-auto">
        <DropdownMenuItem onClick={() => onChange(null)}>
          <FolderMinus className="size-3.5" />
          <span className="min-w-0 flex-1 truncate">{t("no_folder")}</span>
          {!current && <Check className="size-3.5 shrink-0" />}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {folders.map((folder) => (
          <DropdownMenuItem key={folder.id} onClick={() => onChange(folder.id)}>
            <Folder className="size-3.5" />
            <span className="min-w-0 flex-1 truncate">{folder.name}</span>
            {folder.id === current?.id && <Check className="size-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Creates a folder, or renames one. A create from a project's "Move to
 * folder" menu also files that project in the new folder.
 */
function FolderNameDialog({
  folder,
  fileProjectId,
  onClose,
}: {
  folder?: ProjectFolder;
  fileProjectId?: string;
  onClose: () => void;
}) {
  const t = useFoldersT();
  const wsId = useWorkspaceId();
  const seedName = folder?.name ?? "";
  const [name, setName] = useState(seedName);
  const inputRef = useRef<HTMLInputElement>(null);
  const createFolder = useCreateFolder(wsId);
  const renameFolder = useRenameFolder(wsId);
  const { mutate: file } = useFileProject(wsId);
  const pending = createFolder.isPending || renameFolder.isPending;

  const handleSubmit = async () => {
    if (pending) return;
    const trimmed = name.trim();
    if (!trimmed) {
      toast.error(t("name_required"));
      return;
    }
    if (folder && trimmed === seedName.trim()) {
      onClose();
      return;
    }
    if (!onlineManager.isOnline()) {
      toast.error(t("toast_failed"));
      return;
    }
    try {
      if (folder) {
        await renameFolder.mutateAsync({ id: folder.id, name: trimmed });
      } else {
        const created = await createFolder.mutateAsync(trimmed);
        if (fileProjectId) file({ projectId: fileProjectId, folderId: created.id });
        toast.success(t("toast_created"));
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t("toast_failed"));
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent
        className="sm:max-w-md"
        initialFocus={() => {
          inputRef.current?.select();
          return inputRef.current;
        }}
      >
        <DialogHeader>
          <DialogTitle>{folder ? t("rename_title") : t("create_title")}</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void handleSubmit();
          }}
        >
          <Input
            ref={inputRef}
            value={name}
            maxLength={64}
            disabled={pending}
            placeholder={t("name_placeholder")}
            aria-label={t("name_placeholder")}
            onChange={(event) => setName(event.target.value)}
          />
        </form>
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" disabled={pending} onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={pending}
            aria-busy={pending}
            onClick={() => void handleSubmit()}
          >
            {pending && <Spinner className="size-3.5" aria-hidden="true" />}
            {folder ? t("rename_submit") : t("create_submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteFolderDialog({ folder, onClose }: { folder: ProjectFolder; onClose: () => void }) {
  const t = useFoldersT();
  const deleteFolder = useDeleteFolder(useWorkspaceId());
  const pending = deleteFolder.isPending;

  const handleDelete = async () => {
    try {
      await deleteFolder.mutateAsync(folder.id);
      onClose();
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t("toast_delete_failed"));
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={!pending}>
        <DialogHeader>
          <DialogTitle>{t("delete_title", { name: folder.name })}</DialogTitle>
          <DialogDescription>{t("delete_description")}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" size="sm" disabled={pending} onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            disabled={pending}
            aria-busy={pending}
            onClick={() => void handleDelete()}
          >
            {pending && <Spinner className="size-3.5" aria-hidden="true" />}
            {t("delete_confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const REFRESH_EVENTS = [
  "project_folder:created",
  "project_folder:updated",
  "project_folder:deleted",
  "project_folder:filed",
  // Visibility changes decide which filed projects the caller may see.
  "project:updated",
  "project:deleted",
] as WSEventType[];

function RefreshOn({ event, refresh }: { event: WSEventType; refresh: () => void }) {
  useWSEvent(event, refresh);
  return null;
}

/** The folder dialogs, and keeping folders in sync with other members. */
function Host() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const dialog = useFolderDialogStore((s) => s.dialog);
  const close = useFolderDialogStore((s) => s.close);
  const refresh = useCallback(() => {
    void qc.invalidateQueries({ queryKey: folderKeys.list(wsId) });
  }, [qc, wsId]);
  useWSReconnect(refresh);

  return (
    <>
      {REFRESH_EVENTS.map((event) => (
        <RefreshOn key={event} event={event} refresh={refresh} />
      ))}
      {dialog?.kind === "create" && <FolderNameDialog fileProjectId={dialog.fileProjectId} onClose={close} />}
      {dialog?.kind === "rename" && <FolderNameDialog folder={dialog.folder} onClose={close} />}
      {dialog?.kind === "delete" && <DeleteFolderDialog folder={dialog.folder} onClose={close} />}
    </>
  );
}

export const projectFoldersExtension: ProjectFoldersExtension = {
  useProjectFolders: useProjectFoldersState,
  AddMenuItems,
  FolderMenu,
  ProjectMenuItems,
  CreateProjectField,
  Host,
};
