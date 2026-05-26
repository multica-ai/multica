"use client";

import { useState } from "react";
import { Hash, Pencil, Trash2, Plus, Check, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import type { TimeEntryLabel } from "@/shared/types";
import { LabelColorPicker, LABEL_PRESET_COLORS } from "@/features/issues/components/label-color-picker";
import { useTimeEntryLabelsQuery, useTimeEntryLabelMutations } from "@/features/time-tracking";

const DEFAULT_NEW_COLOR = LABEL_PRESET_COLORS[5]?.hex ?? "#3b82f6";

interface EditingState {
  id: string;
  name: string;
  color: string;
}

/** TimeEntryLabelsTab provides workspace-scoped CRUD management for time-entry labels. */
export function TimeEntryLabelsTab() {
  const { data: labels = [], isLoading } = useTimeEntryLabelsQuery();
  const {
    createTimeEntryLabel,
    updateTimeEntryLabel,
    deleteTimeEntryLabel,
    creating,
    updating,
    deleting,
  } = useTimeEntryLabelMutations();

  const [newName, setNewName] = useState("");
  const [newColor, setNewColor] = useState(DEFAULT_NEW_COLOR);
  const [editing, setEditing] = useState<EditingState | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<TimeEntryLabel | null>(null);

  async function handleCreate() {
    const name = newName.trim();
    if (!name) return;
    try {
      await createTimeEntryLabel({ name, color: newColor });
      setNewName("");
      setNewColor(DEFAULT_NEW_COLOR);
      toast.success(`Label "${name}" created`);
    } catch {
      toast.error("Failed to create label");
    }
  }

  async function handleSaveEdit() {
    if (!editing) return;
    const name = editing.name.trim();
    if (!name) return;
    try {
      await updateTimeEntryLabel(editing.id, { name, color: editing.color });
      setEditing(null);
      toast.success("Label updated");
    } catch {
      toast.error("Failed to update label");
    }
  }

  async function handleDelete(label: TimeEntryLabel) {
    try {
      await deleteTimeEntryLabel(label.id);
      setConfirmDelete(null);
      toast.success(`Label "${label.name}" deleted`);
    } catch {
      toast.error("Failed to delete label");
    }
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Create Time Label</CardTitle>
          <CardDescription>
            Add a workspace label for classifying time records, including pomodoro sessions.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-3">
            <span
              className="inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
              style={{ borderColor: newColor, color: newColor }}
            >
              <Hash className="h-3 w-3" />
              <span>{newName || "preview"}</span>
            </span>
            <Input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="Label name"
              className="h-8 max-w-[220px]"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
          </div>
          <LabelColorPicker value={newColor} onChange={setNewColor} />
          <Button size="sm" onClick={handleCreate} disabled={!newName.trim() || creating}>
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            {creating ? "Creating..." : "Create Label"}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Workspace Time Labels</CardTitle>
          <CardDescription>
            {isLoading ? "Loading..." : `${labels.length} label${labels.length !== 1 ? "s" : ""}`}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {labels.length === 0 && !isLoading && (
            <p className="text-sm text-muted-foreground">No labels yet.</p>
          )}

          {labels.map((label) => {
            const isEditing = editing?.id === label.id;
            if (isEditing) {
              return (
                <div key={label.id} className="rounded-md border border-primary/40 bg-muted/40 p-3 space-y-3">
                  <div className="flex items-center gap-3">
                    <span
                      className="inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
                      style={{ borderColor: editing.color, color: editing.color }}
                    >
                      <Hash className="h-3 w-3" />
                      <span>{editing.name || "preview"}</span>
                    </span>
                    <Input
                      value={editing.name}
                      onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                      className="h-8 max-w-[220px]"
                      autoFocus
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleSaveEdit();
                        if (e.key === "Escape") setEditing(null);
                      }}
                    />
                  </div>
                  <LabelColorPicker
                    value={editing.color}
                    onChange={(color) => setEditing({ ...editing, color })}
                  />
                  <div className="flex gap-2">
                    <Button size="sm" onClick={handleSaveEdit} disabled={!editing.name.trim() || updating}>
                      <Check className="mr-1.5 h-3.5 w-3.5" />
                      {updating ? "Saving..." : "Save"}
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => setEditing(null)}>
                      <X className="mr-1.5 h-3.5 w-3.5" />
                      Cancel
                    </Button>
                  </div>
                </div>
              );
            }

            return (
              <div
                key={label.id}
                className="flex items-center justify-between rounded-md border border-transparent px-3 py-2 hover:bg-muted/50 group"
              >
                <span
                  className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
                  style={{ borderColor: label.color, color: label.color }}
                >
                  <Hash className="h-3 w-3 shrink-0" />
                  <span>{label.name}</span>
                </span>
                <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    onClick={() => setEditing({ id: label.id, name: label.name, color: label.color })}
                  >
                    <Pencil className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7 text-destructive hover:text-destructive"
                    onClick={() => setConfirmDelete(label)}
                    disabled={deleting}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            );
          })}
        </CardContent>
      </Card>

      <AlertDialog open={!!confirmDelete} onOpenChange={(open) => !open && setConfirmDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete label "{confirmDelete?.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              This removes the label from all time records. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => confirmDelete && handleDelete(confirmDelete)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
