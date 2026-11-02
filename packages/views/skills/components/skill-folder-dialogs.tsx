"use client";

import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Folder, Layers, Loader2 } from "@multica/ui/icons";
import type { SkillFolder, SkillFolderTree } from "@multica/core/api/schemas";
import { api, errorCode } from "@multica/core/api";
import { invalidateSkillPackageQueries } from "@multica/core/skills/package-queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { folderMoveDestinations } from "../lib/skill-folder-tree";

export type SkillFolderFormState =
  | { mode: "create"; parentId: string | null }
  | { mode: "rename"; folder: SkillFolder };

// Localize the tree contract's error codes
// (docs/engineering/labrastro-skill-packages.md): the server's `message` is
// English-only, so a coded failure renders fork copy instead. Errors without
// a known code fall back to the dialog's generic failure sentence.
function useFolderErrorMessage() {
  const { t } = useT("skill-packages");
  return (err: unknown, fallback: string): string => {
    switch (errorCode(err)) {
      case "name_conflict":
        return t(($) => $.folder_errors.name_conflict);
      case "folder_cycle":
        return t(($) => $.folder_errors.folder_cycle);
      case "invalid_name":
        return t(($) => $.folder_errors.invalid_name);
      case "forbidden":
        return t(($) => $.folder_errors.forbidden);
      case "not_found":
        return t(($) => $.folder_errors.not_found);
      case "managed_folder":
        return t(($) => $.folder_errors.managed_folder);
      case "preview_stale":
        return t(($) => $.folder_errors.preview_stale);
      case "permission_changed":
        return t(($) => $.folder_errors.permission_changed);
      default:
        return fallback;
    }
  };
}

export function SkillFolderFormDialog({
  wsId,
  state,
  onCreated,
  onClose,
}: {
  wsId: string;
  state: SkillFolderFormState;
  onCreated?: (parentId: string | null) => void;
  onClose: () => void;
}) {
  const { t } = useT("skill-packages");
  const qc = useQueryClient();
  const folderError = useFolderErrorMessage();
  const [name, setName] = useState(state.mode === "rename" ? state.folder.name : "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed || saving) return;
    // The server validates names with an uncoded 400 (1–255 UTF-8 bytes, no
    // NUL or line breaks), so check client-side to show the localized reason.
    const tooLong = new TextEncoder().encode(trimmed).length > 255;
    const hasControl =
      trimmed.includes("\0") || trimmed.includes("\n") || trimmed.includes("\r");
    if (tooLong || hasControl) {
      setError(t(($) => $.folder_errors.invalid_name));
      return;
    }
    setSaving(true);
    setError("");
    try {
      const saved =
        state.mode === "create"
          ? await api.createSkillFolder(wsId, { name: trimmed, parent_id: state.parentId ?? "" })
          : await api.updateSkillFolder(wsId, state.folder.id, { name: trimmed });
      if (saved === null) {
        // Unreadable response: the write may have committed. Show an
        // indeterminate result instead of claiming success or failure.
        setError(t(($) => $.folder_form.unconfirmed));
        return;
      }
      if (state.mode === "create") onCreated?.(state.parentId);
      onClose();
    } catch (err) {
      setError(folderError(err, t(($) => $.folder_form.failed)));
    } finally {
      // The tree refreshes on every settled write, including unreadable and
      // failed responses (the server may have committed anyway).
      await invalidateSkillPackageQueries(qc, wsId);
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {state.mode === "create"
              ? t(($) => $.folder_form.create_title)
              : t(($) => $.folder_form.rename_title)}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label htmlFor="skill-folder-name" className="text-caption text-muted-foreground">
            {t(($) => $.folder_form.name_label)}
          </Label>
          <Input
            id="skill-folder-name"
            autoFocus
            value={name}
            onChange={(event) => {
              setName(event.target.value);
              setError("");
            }}
            placeholder={t(($) => $.folder_form.name_placeholder)}
            onKeyDown={(event) => {
              if (event.key === "Enter") void submit();
            }}
          />
          {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose} disabled={saving}>
            {t(($) => $.folder_form.cancel)}
          </Button>
          <Button
            type="button"
            onClick={() => void submit()}
            disabled={!name.trim() || saving}
            aria-busy={saving}
          >
            {saving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.folder_form.saving)}
              </>
            ) : state.mode === "create" ? (
              t(($) => $.folder_form.create)
            ) : (
              t(($) => $.folder_form.save)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function SkillFolderMoveDialog({
  wsId,
  tree,
  folder,
  onClose,
}: {
  wsId: string;
  tree: SkillFolderTree;
  folder: SkillFolder;
  onClose: () => void;
}) {
  const { t } = useT("skill-packages");
  const qc = useQueryClient();
  const folderError = useFolderErrorMessage();
  const destinations = useMemo(
    () => folderMoveDestinations(tree.folders, folder.id),
    [tree.folders, folder.id],
  );
  const [target, setTarget] = useState<string | null>(null);
  const [moving, setMoving] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    if (moving) return;
    setMoving(true);
    setError("");
    try {
      const moved = await api.updateSkillFolder(wsId, folder.id, { parent_id: target ?? "" });
      if (moved === null) {
        // Unreadable response: the write may have committed.
        setError(t(($) => $.folder_move.unconfirmed));
        return;
      }
      onClose();
    } catch (err) {
      setError(folderError(err, t(($) => $.folder_move.failed)));
    } finally {
      await invalidateSkillPackageQueries(qc, wsId);
      setMoving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !moving) onClose(); }}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t(($) => $.folder_move.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.folder_move.description, { name: folder.name })}
          </DialogDescription>
        </DialogHeader>
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
            <Layers className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            {t(($) => $.folder_move.root_option)}
          </button>
          {destinations.map((destination) => (
            <button
              key={destination.id}
              type="button"
              aria-pressed={target === destination.id}
              onClick={() => setTarget(destination.id)}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-body transition-colors",
                target === destination.id ? "bg-accent" : "hover:bg-accent/50",
              )}
            >
              <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{destination.name}</span>
            </button>
          ))}
        </div>
        {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose} disabled={moving}>
            {t(($) => $.folder_move.cancel)}
          </Button>
          <Button type="button" onClick={() => void submit()} disabled={moving} aria-busy={moving}>
            {moving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.folder_move.moving)}
              </>
            ) : (
              t(($) => $.folder_move.confirm)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function SkillFolderDeleteDialog({
  wsId,
  folder,
  onDeleted,
  onClose,
}: {
  wsId: string;
  folder: SkillFolder;
  onDeleted: () => void;
  onClose: () => void;
}) {
  const { t } = useT("skill-packages");
  const qc = useQueryClient();
  const folderError = useFolderErrorMessage();
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    if (deleting) return;
    setDeleting(true);
    setError("");
    try {
      const result = await api.deleteSkillFolder(wsId, folder.id);
      if (result === null) {
        // Unreadable response: the delete may have committed.
        setError(t(($) => $.folder_delete.unconfirmed));
        return;
      }
      onDeleted();
      onClose();
    } catch (err) {
      setError(folderError(err, t(($) => $.folder_delete.failed)));
    } finally {
      await invalidateSkillPackageQueries(qc, wsId);
      setDeleting(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(open) => { if (!open && !deleting) onClose(); }}>
      <AlertDialogContent className="sm:max-w-sm">
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.folder_delete.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.folder_delete.description, { name: folder.name })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
        <AlertDialogFooter>
          <Button type="button" variant="ghost" onClick={onClose} disabled={deleting}>
            {t(($) => $.folder_delete.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => void submit()}
            disabled={deleting}
            aria-busy={deleting}
          >
            {deleting ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.folder_delete.deleting)}
              </>
            ) : (
              t(($) => $.folder_delete.confirm)
            )}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
