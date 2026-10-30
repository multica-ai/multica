"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertCircle,
  FileCode,
  FileText,
  Folder,
  FolderInput,
  FolderPlus,
  MoreHorizontal,
  Plus,
  Trash2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  docTreeOptions,
  folderAncestry,
  folderChildren,
  useDeleteDocFolder,
  useRenameDocFolder,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DocFolder } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { AppLink, useNavigation } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { MoveDialog, type MoveTarget } from "./move-dialog";
import { useDocCommands } from "./use-doc-commands";

function FolderTitle({ folder }: { folder: DocFolder }) {
  const { t } = useT("docs");
  const rename = useRenameDocFolder();
  const [value, setValue] = useState(folder.title);
  useEffect(() => setValue(folder.title), [folder.title]);
  const commit = () => {
    const title = value.trim();
    if (!title || title === folder.title) {
      setValue(folder.title);
      return;
    }
    rename.mutate(
      { id: folder.id, title },
      { onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.rename)) },
    );
  };
  return (
    <input
      value={value}
      maxLength={200}
      aria-label={t(($) => $.tree.folder_name)}
      onChange={(event) => setValue(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.nativeEvent.isComposing) event.currentTarget.blur();
        if (event.key === "Escape") {
          setValue(folder.title);
          event.currentTarget.blur();
        }
      }}
      className="prose-document-title w-full bg-transparent text-foreground outline-none"
    />
  );
}

export function DocFolderPage({ folderId }: { folderId: string }) {
  const { t } = useT("docs");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: tree, isPending } = useQuery(docTreeOptions(wsId));
  const { newDoc, newFolder } = useDocCommands();
  const deleteFolder = useDeleteDocFolder();
  const [moveTarget, setMoveTarget] = useState<MoveTarget | null>(null);
  const [naming, setNaming] = useState(false);
  const [newName, setNewName] = useState("");

  const folder = tree?.folders.find((f) => f.id === folderId);
  if (isPending) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-6 w-1/3" />
        <Skeleton className="h-4 w-1/2" />
      </div>
    );
  }
  if (!tree || !folder) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-center">
        <AlertCircle className="size-8 text-faint-foreground" />
        <p className="text-body font-medium">{t(($) => $.not_found.folder)}</p>
        <AppLink href={p.docs()} className="text-caption text-brand hover:underline">
          {t(($) => $.not_found.back)}
        </AppLink>
      </div>
    );
  }

  const ancestors = folderAncestry(tree.folders, folder.parent_id);
  const children = folderChildren(tree, folder.id);
  const empty = children.folders.length === 0 && children.docs.length === 0;

  const submitNew = () => {
    const title = newName.trim();
    setNaming(false);
    setNewName("");
    if (title) newFolder(title, folder.id);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: p.docs(), label: t(($) => $.kb) },
          ...ancestors.map((f) => ({ href: p.docFolder(f.id), label: f.title, className: "min-w-0 max-w-40 truncate" })),
        ]}
        leaf={<span className="min-w-0 truncate text-foreground">{folder.title}</span>}
        actions={
          <>
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button size="xs" variant="outline" />}>
                <Plus className="size-3.5" />
                {t(($) => $.actions.new)}
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto min-w-44">
                <DropdownMenuItem onClick={() => newDoc("markdown", folder.id)}>
                  <FileText />
                  {t(($) => $.actions.new_doc)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => newDoc("html", folder.id)}>
                  <FileCode />
                  {t(($) => $.actions.new_html_doc)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setNaming(true)}>
                  <FolderPlus />
                  {t(($) => $.actions.new_folder)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<IconButton label={t(($) => $.actions.more)} tooltipSide="bottom" className="text-muted-foreground" />}
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto min-w-44">
                <DropdownMenuItem onClick={() => setMoveTarget({ kind: "folder", id: folder.id, title: folder.title })}>
                  <FolderInput />
                  {t(($) => $.actions.move)}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  variant="destructive"
                  disabled={!empty}
                  onClick={() =>
                    deleteFolder.mutate(folder.id, {
                      onSuccess: () => push(folder.parent_id ? p.docFolder(folder.parent_id) : p.docs()),
                      onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.errors.delete)),
                    })
                  }
                >
                  <Trash2 />
                  {empty ? t(($) => $.actions.delete) : t(($) => $.tree.delete_folder_not_empty)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-6 pt-12 pb-16 sm:px-10">
          <div className="mb-6 flex items-center gap-3">
            <Folder className="size-7 shrink-0 text-muted-foreground" />
            <FolderTitle folder={folder} />
          </div>
          <ul className="space-y-px">
            {naming && (
              <li className="flex h-10 items-center gap-3 rounded-lg bg-surface-hover px-3">
                <Folder className="size-4 shrink-0 text-muted-foreground" />
                <input
                  autoFocus
                  value={newName}
                  maxLength={200}
                  placeholder={t(($) => $.tree.folder_name)}
                  aria-label={t(($) => $.tree.folder_name)}
                  onChange={(event) => setNewName(event.target.value)}
                  onBlur={submitNew}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && !event.nativeEvent.isComposing) submitNew();
                    if (event.key === "Escape") {
                      setNewName("");
                      setNaming(false);
                    }
                  }}
                  className="min-w-0 flex-1 bg-transparent text-body outline-none placeholder:text-faint-foreground"
                />
              </li>
            )}
            {children.folders.map((f) => (
              <li key={f.id}>
                <AppLink
                  href={p.docFolder(f.id)}
                  className="flex h-10 items-center gap-3 rounded-lg px-3 text-body hover:bg-surface-hover"
                >
                  <Folder className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{f.title}</span>
                </AppLink>
              </li>
            ))}
            {children.docs.map((doc) => (
              <li key={doc.id}>
                <AppLink
                  href={p.docDetail(doc.id)}
                  className="flex h-10 items-center gap-3 rounded-lg px-3 text-body hover:bg-surface-hover"
                >
                  <span className="text-muted-foreground [&_svg]:size-4">
                    {doc.format === "html" ? <FileCode /> : <FileText />}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{doc.title || t(($) => $.untitled)}</span>
                  {doc.unread && <span className="size-1.5 shrink-0 rounded-full bg-brand" />}
                  <ActorAvatar actorType={doc.updated_by_type} actorId={doc.updated_by_id} size="xs" />
                  <span className="w-20 shrink-0 text-right text-caption text-faint-foreground">{timeAgo(doc.updated_at)}</span>
                </AppLink>
              </li>
            ))}
          </ul>
          {empty && !naming && (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <p className="text-body text-muted-foreground">{t(($) => $.folder.empty)}</p>
              <Button size="xs" variant="outline" onClick={() => newDoc("markdown", folder.id)}>
                <Plus className="size-3.5" />
                {t(($) => $.actions.new_doc)}
              </Button>
            </div>
          )}
        </div>
      </div>
      <MoveDialog target={moveTarget} onOpenChange={(open) => !open && setMoveTarget(null)} />
    </div>
  );
}
