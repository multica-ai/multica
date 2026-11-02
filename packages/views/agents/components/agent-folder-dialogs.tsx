"use client";

// Labrastro fork (OL-1030): folder form/move/delete dialogs for the agents
// page, plus the agent move dialog. Mirrors the skills-page dialogs; the
// server contract is docs/engineering/labrastro-agent-folders.md.

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Folder, Inbox, Loader2 } from "@multica/ui/icons";
import { toast } from "sonner";
import { api, errorCode } from "@multica/core/api";
import { invalidateAgentFolderQueries } from "@multica/core/agents/folder-queries";
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
import {
  folderPathLabel,
  type FolderExplorerFolder,
} from "../../common/folder-explorer";
import type { Agent } from "@multica/core/types";

export type AgentFolderFormState =
  | { mode: "create"; parentId: string | null }
  | { mode: "rename"; folder: FolderExplorerFolder };

// Localize the folder contract's error codes
// (docs/engineering/labrastro-agent-folders.md): the server's `message` is
// English-only, so a coded failure renders fork copy instead. Errors without
// a known code fall back to the dialog's generic failure sentence.
function useFolderErrorMessage() {
  const { t } = useT("agent-folders");
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
      default:
        return fallback;
    }
  };
}

export function AgentFolderFormDialog({
  wsId,
  state,
  onCreated,
  onClose,
}: {
  wsId: string;
  state: AgentFolderFormState;
  onCreated?: (parentId: string | null) => void;
  onClose: () => void;
}) {
  const { t } = useT("agent-folders");
  const qc = useQueryClient();
  const folderError = useFolderErrorMessage();
  const [name, setName] = useState(
    state.mode === "rename" ? state.folder.name : "",
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed || saving) return;
    setSaving(true);
    setError("");
    try {
      const saved =
        state.mode === "create"
          ? await api.createAgentFolder(wsId, {
              name: trimmed,
              parentId: state.parentId ?? "",
            })
          : await api.updateAgentFolder(wsId, state.folder.id, {
              name: trimmed,
            });
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
      await invalidateAgentFolderQueries(qc, wsId);
      setSaving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {state.mode === "create"
              ? t(($) => $.folder_form.create_title)
              : t(($) => $.folder_form.rename_title)}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <Label
            htmlFor="agent-folder-name"
            className="text-caption text-muted-foreground"
          >
            {t(($) => $.folder_form.name_label)}
          </Label>
          <Input
            id="agent-folder-name"
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
          {error && (
            <p role="alert" className="text-caption text-destructive">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={onClose}
            disabled={saving}
          >
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

function FolderDestinationList({
  folders,
  destinations,
  target,
  onSelect,
}: {
  folders: readonly FolderExplorerFolder[];
  destinations: readonly FolderExplorerFolder[];
  target: string | null;
  onSelect: (folderId: string) => void;
}) {
  return (
    <>
      {destinations.map((destination) => (
        <button
          key={destination.id}
          type="button"
          aria-pressed={target === destination.id}
          onClick={() => onSelect(destination.id)}
          className={cn(
            "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-body transition-colors",
            target === destination.id ? "bg-accent" : "hover:bg-accent/50",
          )}
        >
          <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">
            {folderPathLabel(folders, destination.id)}
          </span>
        </button>
      ))}
    </>
  );
}

/** Move a custom folder or a squad folder into the root or a custom folder.
 *  Squad folders use the squad placement endpoint; custom folders PATCH
 *  their parent. */
export function AgentFolderMoveDialog({
  wsId,
  folders,
  destinations,
  folder,
  onClose,
}: {
  wsId: string;
  folders: readonly FolderExplorerFolder[];
  /** Valid custom-folder destinations (cycle-free for custom moves). */
  destinations: readonly FolderExplorerFolder[];
  folder: FolderExplorerFolder;
  onClose: () => void;
}) {
  const { t } = useT("agent-folders");
  const qc = useQueryClient();
  const folderError = useFolderErrorMessage();
  const [target, setTarget] = useState<string | null>(null);
  const [moving, setMoving] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    if (moving) return;
    setMoving(true);
    setError("");
    try {
      const moved = folder.managed
        ? await api.setAgentSquadPlacement(wsId, folder.id, target ?? "")
        : await api.updateAgentFolder(wsId, folder.id, {
            parentId: target ?? "",
          });
      if (moved === null) {
        // Unreadable response: the write may have committed.
        setError(t(($) => $.folder_move.unconfirmed));
        return;
      }
      onClose();
    } catch (err) {
      setError(folderError(err, t(($) => $.folder_move.failed)));
    } finally {
      await invalidateAgentFolderQueries(qc, wsId);
      setMoving(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !moving) onClose();
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {folder.managed
              ? t(($) => $.folder_move.squad_title)
              : t(($) => $.folder_move.title)}
          </DialogTitle>
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
            <Inbox className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            {t(($) => $.folder_move.root_option)}
          </button>
          <FolderDestinationList
            folders={folders}
            destinations={destinations}
            target={target}
            onSelect={setTarget}
          />
        </div>
        {error && (
          <p role="alert" className="text-caption text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={onClose}
            disabled={moving}
          >
            {t(($) => $.folder_move.cancel)}
          </Button>
          <Button
            type="button"
            onClick={() => void submit()}
            disabled={moving}
            aria-busy={moving}
          >
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

export function AgentFolderDeleteDialog({
  wsId,
  folder,
  onDeleted,
  onClose,
}: {
  wsId: string;
  folder: FolderExplorerFolder;
  onDeleted: () => void;
  onClose: () => void;
}) {
  const { t } = useT("agent-folders");
  const qc = useQueryClient();
  const folderError = useFolderErrorMessage();
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    if (deleting) return;
    setDeleting(true);
    setError("");
    try {
      const result = await api.deleteAgentFolder(wsId, folder.id);
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
      await invalidateAgentFolderQueries(qc, wsId);
      setDeleting(false);
    }
  };

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !deleting) onClose();
      }}
    >
      <AlertDialogContent className="sm:max-w-sm">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.folder_delete.title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.folder_delete.description, { name: folder.name })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && (
          <p role="alert" className="text-caption text-destructive">
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={onClose}
            disabled={deleting}
          >
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

/**
 * Move (or additionally place) one or more agents into a custom folder or
 * back to the root. Squad folders are never a destination; squad members
 * keep their squad folder either way — only the custom placement changes.
 * Moves run sequentially with per-item failure tolerance.
 */
export function MoveAgentsDialog({
  wsId,
  folders,
  destinations,
  agents,
  hasSquadMembers,
  variant,
  open,
  onOpenChange,
  onMoved,
}: {
  wsId: string;
  folders: readonly FolderExplorerFolder[];
  destinations: readonly FolderExplorerFolder[];
  agents: Agent[];
  hasSquadMembers: boolean;
  /** "place" is the inside-a-squad-folder wording: the agent stays in the
   *  squad folder and additionally gets a custom placement. */
  variant: "move" | "place";
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Fired only after a fully successful run, so partial failures keep the selection. */
  onMoved?: () => void;
}) {
  const { t } = useT("agent-folders");
  const qc = useQueryClient();
  // null = root (placement cleared); undefined = nothing chosen yet.
  const [target, setTarget] = useState<string | null | undefined>(undefined);
  const [moving, setMoving] = useState(false);

  const handleOpenChange = (value: boolean) => {
    if (moving) return;
    if (!value) setTarget(undefined);
    onOpenChange(value);
  };

  const handleConfirm = async () => {
    if (target === undefined || moving) return;
    setMoving(true);
    let done = 0;
    let failed = 0;
    // Unreadable responses are not failures: the write may have committed.
    let unconfirmed = 0;
    try {
      for (const agent of agents) {
        try {
          const result = await api.setAgentPlacement(
            wsId,
            agent.id,
            target ?? "",
          );
          if (result === null) unconfirmed++;
          else done++;
        } catch {
          failed++;
        }
      }
      await invalidateAgentFolderQueries(qc, wsId);
      if (failed === 0 && unconfirmed === 0) {
        toast.success(
          variant === "place"
            ? t(($) => $.move_agents.place_done, { count: done })
            : t(($) => $.move_agents.done, { count: done }),
        );
        handleOpenChange(false);
        onMoved?.();
      } else if (failed === 0) {
        toast.error(
          variant === "place"
            ? t(($) => $.move_agents.place_unconfirmed, {
                done,
                total: agents.length,
                count: unconfirmed,
              })
            : t(($) => $.move_agents.unconfirmed, {
                done,
                total: agents.length,
                count: unconfirmed,
              }),
        );
        handleOpenChange(false);
      } else {
        toast.error(
          done > 0
            ? variant === "place"
              ? t(($) => $.move_agents.place_partial, {
                  done,
                  total: agents.length,
                })
              : t(($) => $.move_agents.partial, {
                  done,
                  total: agents.length,
                })
            : variant === "place"
              ? t(($) => $.move_agents.place_failed)
              : t(($) => $.move_agents.failed),
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
            {variant === "place"
              ? t(($) => $.move_agents.place_title)
              : t(($) => $.move_agents.title, { count: agents.length })}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-1.5">
          <div className="text-caption text-muted-foreground">
            {t(($) => $.move_agents.destination)}
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
              {t(($) => $.move_agents.root_option)}
            </button>
            <FolderDestinationList
              folders={folders}
              destinations={destinations}
              target={target ?? null}
              onSelect={(folderId) => setTarget(folderId)}
            />
          </div>
          {hasSquadMembers && target !== undefined && (
            <p className="text-caption text-muted-foreground">
              {target === null
                ? t(($) => $.move_agents.squad_member_root_note)
                : t(($) => $.move_agents.squad_member_note)}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => handleOpenChange(false)}
            disabled={moving}
          >
            {t(($) => $.move_agents.cancel)}
          </Button>
          <Button
            type="button"
            onClick={() => void handleConfirm()}
            disabled={target === undefined || moving || agents.length === 0}
            aria-busy={moving}
          >
            {moving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.move_agents.moving)}
              </>
            ) : variant === "place" ? (
              t(($) => $.move_agents.place_confirm)
            ) : (
              t(($) => $.move_agents.confirm)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
