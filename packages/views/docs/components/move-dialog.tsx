"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { BookOpen, Folder } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { canMoveFolder, docTreeOptions, useMoveDoc, useMoveDocFolder } from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import type { DocFolder } from "@multica/core/types";
import { useT } from "../../i18n";

export type MoveTarget = { kind: "doc"; id: string; title: string } | { kind: "folder"; id: string; title: string };

/** Folders in tree order with their depth, for a flat indented picker. */
function flattenFolders(folders: readonly DocFolder[]): { folder: DocFolder; depth: number }[] {
  const ids = new Set(folders.map((f) => f.id));
  const byParent = new Map<string | null, DocFolder[]>();
  for (const f of folders) {
    const parent = f.parent_id && ids.has(f.parent_id) ? f.parent_id : null;
    byParent.set(parent, [...(byParent.get(parent) ?? []), f]);
  }
  const out: { folder: DocFolder; depth: number }[] = [];
  const seen = new Set<string>();
  const walk = (parent: string | null, depth: number) => {
    const kids = [...(byParent.get(parent) ?? [])].sort((a, b) =>
      a.title.localeCompare(b.title, undefined, { sensitivity: "base", numeric: true }),
    );
    for (const f of kids) {
      if (seen.has(f.id)) continue;
      seen.add(f.id);
      out.push({ folder: f, depth });
      walk(f.id, depth + 1);
    }
  };
  walk(null, 0);
  return out;
}

export function MoveDialog({
  target,
  onOpenChange,
}: {
  target: MoveTarget | null;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const moveDoc = useMoveDoc();
  const moveFolder = useMoveDocFolder();
  const [selected, setSelected] = useState<string | null | undefined>(undefined);

  const folders = useMemo(() => tree?.folders ?? [], [tree]);
  const rows = useMemo(() => flattenFolders(folders), [folders]);
  const current =
    target?.kind === "doc"
      ? (tree?.docs.find((d) => d.id === target.id)?.folder_id ?? null)
      : target
        ? (folders.find((f) => f.id === target.id)?.parent_id ?? null)
        : null;

  const allowed = (folderId: string | null) =>
    !target || target.kind === "doc" || (folderId !== target.id && canMoveFolder(folders, target.id, folderId));

  const close = () => {
    setSelected(undefined);
    onOpenChange(false);
  };

  const submit = () => {
    if (!target || selected === undefined || selected === current) return close();
    const onError = (error: unknown) =>
      toast.error(error instanceof Error ? error.message : t(($) => $.errors.move));
    if (target.kind === "doc") moveDoc.mutate({ id: target.id, folderId: selected }, { onError });
    else moveFolder.mutate({ id: target.id, parentId: selected }, { onError });
    close();
  };

  const choice = selected === undefined ? current : selected;
  const option = (id: string | null, label: string, icon: React.ReactNode, depth = 0) => {
    const disabled = !allowed(id);
    return (
      <button
        key={id ?? "root"}
        type="button"
        role="option"
        aria-selected={choice === id}
        disabled={disabled}
        onClick={() => setSelected(id)}
        style={{ paddingLeft: 8 + depth * 16 }}
        className={cn(
          "flex h-8 w-full items-center gap-2 rounded-md pr-2 text-left text-body outline-none transition-colors focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-40",
          choice === id ? "bg-surface-selected text-surface-selected-foreground" : "hover:bg-surface-hover",
        )}
      >
        <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {id === current && <span className="shrink-0 text-caption text-faint-foreground">{t(($) => $.move.current)}</span>}
      </button>
    );
  };

  return (
    <Dialog open={!!target} onOpenChange={(open) => (open ? onOpenChange(true) : close())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="truncate">{t(($) => $.move.title, { title: target?.title ?? "" })}</DialogTitle>
        </DialogHeader>
        <div role="listbox" aria-label={t(($) => $.move.destination)} className="max-h-80 overflow-y-auto">
          {option(null, t(($) => $.move.root), <BookOpen />)}
          {rows.map(({ folder, depth }) => option(folder.id, folder.title, <Folder />, depth + 1))}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            {t(($) => $.actions.cancel)}
          </Button>
          <Button onClick={submit} disabled={selected === undefined || selected === current}>
            {t(($) => $.move.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
