"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, FileText, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  projectNoteOptions,
  projectNotesOptions,
  useCreateProjectNote,
  useDeleteProjectNote,
  useUpdateProjectNote,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ProjectNoteSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

// Project Notes sidebar section.
//
// Notes are markdown notepads owned by Multica (contrast with
// ProjectResourcesSection, whose rows only point at external systems). The list
// endpoint returns titles and byte sizes but no bodies, so opening a note is a
// separate fetch — that is what keeps a project with dozens of long notes cheap
// to render.

// formatSize renders the server-computed byte size for a note row. The CLI
// table uses IEC units (KiB/MiB) via its shared formatBytes; the web UI keeps
// the shorter KB/MB labels that the rest of the product's copy uses.
function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "-";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function NoteRow({
  note,
  onOpen,
  onDelete,
}: {
  note: ProjectNoteSummary;
  onOpen: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("projects");
  return (
    <div className="group flex items-center gap-1.5 rounded-md px-2 py-1 text-caption hover:bg-accent/70">
      <button
        type="button"
        onClick={onOpen}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        <FileText className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{note.title}</span>
        <span className="shrink-0 text-micro text-muted-foreground">
          {formatSize(note.body_size)}
        </span>
      </button>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon"
              className="size-6 shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
              onClick={onDelete}
            >
              <Trash2 className="size-3" />
            </Button>
          }
        />
        <TooltipContent>{t(($) => $.notes.remove_tooltip)}</TooltipContent>
      </Tooltip>
    </div>
  );
}

// NoteEditorDialog owns the body fetch and the local draft.
//
// The parent mounts this only while a note is open (`editingId !== ""`), so
// closing unmounts it and the draft dies with the component — that is what
// guarantees opening a different note never shows the previous body. The effect
// below therefore only has to handle the initial load, not a switch.
function NoteEditorDialog({
  projectId,
  noteId,
  onClose,
}: {
  projectId: string;
  noteId: string;
  onClose: () => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: note } = useQuery(projectNoteOptions(wsId, projectId, noteId));
  const updateNote = useUpdateProjectNote(wsId, projectId);
  const [draft, setDraft] = useState("");
  const [loaded, setLoaded] = useState(false);

  // Seed the draft once the body arrives. Guarded by `loaded` rather than by
  // comparing ids so a background refetch cannot overwrite unsaved edits.
  useEffect(() => {
    if (!loaded && note && note.id !== "") {
      setDraft(note.body_md);
      setLoaded(true);
    }
  }, [note, loaded]);

  const handleSave = () => {
    // Refuse to save before the body has arrived. draft starts as "" and the
    // PATCH replaces the body wholesale, so saving during the load window would
    // wipe the note's contents. The button is disabled too; this is the guard
    // for a programmatic call or a stale click.
    if (!loaded) return;
    updateNote.mutate(
      { noteId, data: { body_md: draft } },
      {
        onSuccess: () => {
          toast.success(t(($) => $.notes.toast_saved));
          onClose();
        },
        onError: () => toast.error(t(($) => $.notes.toast_save_failed)),
      },
    );
  };

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{note?.title ?? ""}</DialogTitle>
        </DialogHeader>
        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t(($) => $.notes.body_placeholder)}
          className="min-h-[360px] font-mono text-caption"
          disabled={!loaded}
        />
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t(($) => $.notes.cancel)}
          </Button>
          <Button onClick={handleSave} disabled={!loaded || updateNote.isPending}>
            {t(($) => $.notes.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectNotesSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [editingId, setEditingId] = useState("");
  const [pendingDelete, setPendingDelete] = useState<ProjectNoteSummary | null>(
    null,
  );

  const { data: notes = [] } = useQuery(projectNotesOptions(wsId, projectId));
  const createNote = useCreateProjectNote(wsId, projectId);
  const deleteNote = useDeleteProjectNote(wsId, projectId);

  const handleCreate = () => {
    const title = newTitle.trim();
    if (title === "") {
      toast.error(t(($) => $.notes.title_required));
      return;
    }
    createNote.mutate(
      { title },
      {
        onSuccess: (created) => {
          toast.success(t(($) => $.notes.toast_created));
          setNewTitle("");
          setCreating(false);
          // Drop straight into the editor: a note created with no body is only
          // useful once something is in it.
          setEditingId(created.id);
        },
        onError: () => toast.error(t(($) => $.notes.toast_create_failed)),
      },
    );
  };

  const confirmDelete = () => {
    if (!pendingDelete) return;
    const id = pendingDelete.id;
    setPendingDelete(null);
    deleteNote.mutate(id, {
      onSuccess: () => toast.success(t(($) => $.notes.toast_removed)),
      onError: () => toast.error(t(($) => $.notes.toast_remove_failed)),
    });
  };

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.notes.section_header)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>

      {open && (
        <div className="pl-2 space-y-1.5">
          {notes.length === 0 && !creating && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.notes.empty)}
            </p>
          )}

          {notes.length > 0 && (
            <div className="max-h-64 space-y-0.5 overflow-y-auto pr-1">
              {notes.map((note) => (
                <NoteRow
                  key={note.id}
                  note={note}
                  onOpen={() => setEditingId(note.id)}
                  onDelete={() => setPendingDelete(note)}
                />
              ))}
            </div>
          )}

          {creating ? (
            <div className="space-y-1.5 px-2">
              <input
                autoFocus
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleCreate();
                  if (e.key === "Escape") {
                    setCreating(false);
                    setNewTitle("");
                  }
                }}
                placeholder={t(($) => $.notes.title_placeholder)}
                className="h-8 w-full rounded-md border bg-transparent px-2 text-caption outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
              />
              <div className="flex gap-1.5">
                <Button
                  size="sm"
                  className="h-7 text-caption"
                  onClick={handleCreate}
                  disabled={createNote.isPending}
                >
                  {t(($) => $.notes.create_submit)}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 text-caption"
                  onClick={() => {
                    setCreating(false);
                    setNewTitle("");
                  }}
                >
                  {t(($) => $.notes.cancel)}
                </Button>
              </div>
            </div>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-caption text-muted-foreground hover:text-foreground"
              onClick={() => setCreating(true)}
            >
              <Plus className="size-3" />
              {t(($) => $.notes.add_button)}
            </Button>
          )}

          <p className="px-2 text-micro leading-snug text-muted-foreground">
            {t(($) => $.notes.agent_hint)}
          </p>
        </div>
      )}

      {editingId !== "" && (
        <NoteEditorDialog
          projectId={projectId}
          noteId={editingId}
          onClose={() => setEditingId("")}
        />
      )}

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(next) => !next && setPendingDelete(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.notes.delete_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.notes.delete_confirm_body)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.notes.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete}>
              {t(($) => $.notes.delete_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
