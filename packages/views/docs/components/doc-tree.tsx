"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  FileText,
  Folder,
  FolderInput,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  docTreeOptions,
  useCreateDoc,
  useCreateDocFolder,
  useRenameDocFolder,
  useUpdateDoc,
  useDeleteDoc,
  useDeleteDocFolder,
  removeDocDraft,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DocSummary, DocFolder } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { MoveDialog, type MoveTarget } from "./move-dialog";

type Item =
  | { kind: "doc"; item: DocSummary }
  | { kind: "folder"; item: DocFolder };
export function DeleteDocDialog({
  target,
  onOpenChange,
}: {
  target: Item | null;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { pathname, push } = useNavigation();
  const docDelete = useDeleteDoc();
  const folderDelete = useDeleteDocFolder();
  const pending = docDelete.isPending || folderDelete.isPending;
  const confirm = async () => {
    if (!target) return;
    try {
      if (target.kind === "doc") {
        await docDelete.mutateAsync(target.item.id);
        removeDocDraft(`${wsId}:${target.item.id}`);
      } else await folderDelete.mutateAsync(target.item.id);
      onOpenChange(false);
      if (
        pathname === p.docFolder(target.item.id) ||
        pathname.startsWith(p.docDetail(target.item.id))
      )
        push(p.docs());
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.errors.delete),
      );
    }
  };
  return (
    <AlertDialog
      open={!!target}
      onOpenChange={(open) => {
        if (!pending) onOpenChange(open);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {target?.kind === "folder"
              ? t(($) => $.delete.folder_title)
              : t(($) => $.delete.title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {target?.kind === "folder"
              ? t(($) => $.delete.folder_description)
              : t(($) => $.delete.description, {
                  title: target?.item.title ?? "",
                })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>
            {t(($) => $.actions.cancel)}
          </AlertDialogCancel>
          <Button
            variant="destructive"
            disabled={pending}
            onClick={() => void confirm()}
          >
            {t(($) => $.delete.confirm)}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function DocTree() {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { pathname, push } = useNavigation();
  const tree = useQuery(docTreeOptions(wsId));
  const createDoc = useCreateDoc();
  const createFolder = useCreateDocFolder();
  const renameFolder = useRenameDocFolder();
  const updateDoc = useUpdateDoc();
  const [form, setForm] = useState<{
    kind: "doc" | "folder";
    parentId: string | null;
    target?: Item;
  } | null>(null);
  const [name, setName] = useState("");
  const [moving, setMoving] = useState<MoveTarget | null>(null);
  const [deleting, setDeleting] = useState<Item | null>(null);
  const pending =
    createDoc.isPending ||
    createFolder.isPending ||
    renameFolder.isPending ||
    updateDoc.isPending;
  const openForm = (
    kind: "doc" | "folder",
    parentId: string | null,
    target?: Item,
  ) => {
    setName(target?.item.title ?? "");
    setForm({ kind, parentId, target });
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!form || !name.trim()) return;
    try {
      if (form.target?.kind === "doc")
        await updateDoc.mutateAsync({
          id: form.target.item.id,
          title: name.trim(),
          baseRevision: form.target.item.revision,
        });
      else if (form.target?.kind === "folder")
        await renameFolder.mutateAsync({
          id: form.target.item.id,
          title: name.trim(),
        });
      else if (form.kind === "folder")
        await createFolder.mutateAsync({
          title: name.trim(),
          parentId: form.parentId,
        });
      else {
        const doc = await createDoc.mutateAsync({
          title: name.trim(),
          folderId: form.parentId,
        });
        push(p.docDetail(doc.id));
      }
      setForm(null);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.errors.save),
      );
    }
  };
  const menu = (target: Item) => (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={t(($) => $.tree.more, { title: target.item.title })}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {target.kind === "folder" && (
          <>
            <DropdownMenuItem onClick={() => openForm("doc", target.item.id)}>
              <Plus />
              {t(($) => $.actions.new_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => openForm("folder", target.item.id)}
            >
              <Folder />
              {t(($) => $.actions.new_folder)}
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuItem onClick={() => openForm(target.kind, null, target)}>
          <Pencil />
          {t(($) => $.actions.rename)}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            setMoving({
              kind: target.kind,
              id: target.item.id,
              title: target.item.title,
            })
          }
        >
          <FolderInput />
          {t(($) => $.actions.move)}
        </DropdownMenuItem>
        <DropdownMenuItem
          variant="destructive"
          onClick={() => setDeleting(target)}
        >
          <Trash2 />
          {t(($) => $.actions.delete)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
  const branch = (
    parent: string | null,
    ancestors: Set<string>,
  ): React.ReactNode => (
    <ul className="space-y-0.5">
      {tree.data?.folders
        .filter((f) => f.parentId === parent && !ancestors.has(f.id))
        .map((folder) => (
          <li key={folder.id}>
            <details open>
              <summary className="flex cursor-pointer items-center gap-1 rounded-md px-1 py-1 text-body hover:bg-surface-hover">
                <Folder className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{folder.title}</span>
                {menu({ kind: "folder", item: folder })}
              </summary>
              <div className="ml-3 border-l pl-2">
                {branch(folder.id, new Set([...ancestors, folder.id]))}
              </div>
            </details>
          </li>
        ))}
      {tree.data?.docs
        .filter((d) => d.folderId === parent)
        .map((doc) => (
          <li
            key={doc.id}
            className={`flex items-center gap-1 rounded-md px-1 py-1 text-body ${pathname === p.docDetail(doc.id) ? "bg-surface-selected text-surface-selected-foreground" : "hover:bg-surface-hover"}`}
          >
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <AppLink
              href={p.docDetail(doc.id)}
              className="min-w-0 flex-1 truncate"
            >
              {doc.title || t(($) => $.untitled)}
            </AppLink>
            {menu({ kind: "doc", item: doc })}
          </li>
        ))}
    </ul>
  );
  return (
    <>
      <div className="flex h-12 shrink-0 items-center justify-between px-3">
        <AppLink href={p.docs()} className="text-body font-medium">
          {t(($) => $.kb)}
        </AppLink>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t(($) => $.actions.new)}
              />
            }
          >
            <Plus className="size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onClick={() => openForm("doc", null)}>
              {t(($) => $.actions.new_doc)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => openForm("folder", null)}>
              {t(($) => $.actions.new_folder)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className="min-h-0 flex-1 overflow-auto px-2 pb-3">
        {tree.isError ? (
          <Button variant="ghost" onClick={() => void tree.refetch()}>
            {t(($) => $.editor.retry)}
          </Button>
        ) : tree.isPending ? (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.history.loading)}
          </span>
        ) : (
          branch(null, new Set())
        )}
        {tree.data && !tree.data.docs.length && !tree.data.folders.length && (
          <p className="p-2 text-caption text-muted-foreground">
            {t(($) => $.tree.empty)}
          </p>
        )}
      </div>
      <Dialog
        open={!!form}
        onOpenChange={(open) => {
          if (!open && !pending) setForm(null);
        }}
      >
        <DialogContent>
          <form onSubmit={submit}>
            <DialogHeader>
              <DialogTitle>
                {form?.target
                  ? t(($) => $.actions.rename)
                  : form?.kind === "folder"
                    ? t(($) => $.actions.new_folder)
                    : t(($) => $.actions.new_doc)}
              </DialogTitle>
            </DialogHeader>
            <label className="my-4 block text-body">
              {t(($) => $.editor.title_label)}
              <Input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={form?.kind === "folder" ? 200 : 300}
              />
            </label>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => setForm(null)}
              >
                {t(($) => $.actions.cancel)}
              </Button>
              <Button type="submit" disabled={pending || !name.trim()}>
                {form?.target
                  ? t(($) => $.actions.rename)
                  : t(($) => $.actions.new)}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <MoveDialog
        target={moving}
        onOpenChange={(open) => {
          if (!open) setMoving(null);
        }}
      />
      <DeleteDocDialog
        target={deleting}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
      />
    </>
  );
}
