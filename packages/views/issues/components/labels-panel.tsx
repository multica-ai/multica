"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Trash2, Plus, Pencil, Check, X } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Label as UILabel } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions, useCreateLabel, useUpdateLabel, useDeleteLabel } from "@multica/core/labels";
import type { Label } from "@multica/core/types";
import { LabelChip } from "../../labels/label-chip";
import { useIssuesT } from "../i18n";

/** Default color for brand-new labels. Everything else goes through the native picker. */
const DEFAULT_COLOR_DEFAULT = "#3b82f6";

/**
 * Workspace-wide labels management surface. Opened from the Manage labels…
 * footer in the label picker.
 */
export function LabelsPanel() {
  const t = useIssuesT();
  const wsId = useWorkspaceId();
  const { data: labels = [], isLoading } = useQuery(labelListOptions(wsId));

  const create = useCreateLabel();
  const update = useUpdateLabel();
  const del = useDeleteLabel();

  const [newName, setNewName] = useState("");
  const [newColor, setNewColor] = useState(DEFAULT_COLOR_DEFAULT);

  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [editColor, setEditColor] = useState("");
  const [editError, setEditError] = useState("");

  const [pendingDeletion, setPendingDeletion] = useState<Label | null>(null);

  const handleCreate = () => {
    const name = newName.trim();
    if (!name) return;
    create.mutate(
      { name, color: newColor },
      {
        onSuccess: () => {
          setNewName("");
          setNewColor(DEFAULT_COLOR_DEFAULT);
        },
        onError: (err: unknown) => {
          toast.error(err instanceof Error ? err.message : t.labels.createFailed);
        },
      },
    );
  };

  const startEdit = (label: Label) => {
    setEditingId(label.id);
    setEditName(label.name);
    setEditColor(label.color);
    setEditError("");
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditName("");
    setEditColor("");
    setEditError("");
  };

  const saveEdit = (id: string) => {
    const name = editName.trim();
    if (!name) {
      // Surface the reason the save didn't happen — previously this was a
      // silent no-op. Button is also disabled (below) but a visible message
      // beats a greyed-out button for telling the user WHY.
      setEditError(t.labels.nameRequired);
      return;
    }
    setEditError("");
    update.mutate(
      { id, name, color: editColor },
      {
        onSuccess: cancelEdit,
        onError: (err: unknown) => {
          toast.error(err instanceof Error ? err.message : t.labels.updateFailed);
        },
      },
    );
  };

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">
        {t.labels.panelHint}
      </p>

      {/* Create form — color swatch, name, Add button all in one row */}
      <div className="flex items-center gap-2">
        <ColorPalette value={newColor} onChange={setNewColor} compact />
        <Input
          id="label-new-name"
          placeholder={t.labels.newPlaceholder}
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") handleCreate();
          }}
          className="flex-1"
          maxLength={32}
          aria-label={t.labels.newLabelAriaLabel}
        />
        <Button onClick={handleCreate} disabled={!newName.trim() || create.isPending}>
          <Plus className="h-4 w-4 mr-1" />
          {t.labels.add}
        </Button>
      </div>

      {/* List — scrolls when labels exceed viewport */}
      <div className="space-y-2 max-h-[50vh] overflow-y-auto">
        {isLoading && <p className="text-sm text-muted-foreground">{t.labels.loading}</p>}
        {!isLoading && labels.length === 0 && (
          <p className="text-sm text-muted-foreground">{t.labels.empty}</p>
        )}
        {labels.map((label) => {
          const isEditing = editingId === label.id;
          const editNameEmpty = isEditing && !editName.trim();
          return (
            <div
              key={label.id}
              className="rounded-md border bg-card px-3 py-2"
            >
              <div className="flex items-center gap-3">
                {isEditing ? (
                  <>
                    <Input
                      value={editName}
                      onChange={(e) => {
                        setEditName(e.target.value);
                        if (e.target.value.trim()) setEditError("");
                      }}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") saveEdit(label.id);
                        if (e.key === "Escape") cancelEdit();
                      }}
                      className="flex-1 h-8"
                      maxLength={32}
                      autoFocus
                      aria-invalid={editNameEmpty}
                      aria-describedby={editError ? `label-edit-error-${label.id}` : undefined}
                    />
                    <ColorPalette
                      value={editColor}
                      onChange={setEditColor}
                      compact
                    />
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => saveEdit(label.id)}
                      disabled={editNameEmpty || update.isPending}
                      aria-label={t.labels.save}
                    >
                      <Check className="h-4 w-4" />
                    </Button>
                    <Button size="sm" variant="ghost" onClick={cancelEdit} aria-label={t.labels.cancel}>
                      <X className="h-4 w-4" />
                    </Button>
                  </>
                ) : (
                  <>
                    {/* min-w-0 on the label wrapper lets long names wrap without
                        pushing the hex/buttons off the right edge. */}
                    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                      <LabelChip label={label} fullName />
                      <span className="text-xs text-muted-foreground">
                        {label.color}
                      </span>
                    </div>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => startEdit(label)}
                      aria-label={t.labels.editAriaLabel(label.name)}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setPendingDeletion(label)}
                      aria-label={t.labels.deleteAriaLabel(label.name)}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </>
                )}
              </div>
              {isEditing && editError && (
                <p
                  id={`label-edit-error-${label.id}`}
                  className="mt-1.5 text-xs text-destructive"
                  role="alert"
                >
                  {editError}
                </p>
              )}
            </div>
          );
        })}
      </div>

      <AlertDialog open={!!pendingDeletion} onOpenChange={(o) => !o && setPendingDeletion(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t.labels.deleteTitle}</AlertDialogTitle>
            <AlertDialogDescription>
              {t.labels.deleteDescription(pendingDeletion?.name ?? "")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t.labels.cancel}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (!pendingDeletion) return;
                del.mutate(pendingDeletion.id, {
                  onSuccess: () => setPendingDeletion(null),
                  onError: (err: unknown) => {
                    toast.error(err instanceof Error ? err.message : t.labels.deleteFailed);
                  },
                });
              }}
            >
              {t.labels.deleteConfirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * Color picker — a single swatch that opens the browser's native color
 * picker. Full gamut, trusted UX, zero visual clutter. `focus-within` ring
 * makes keyboard focus visible despite the transparent `<input type="color">`.
 */
function ColorPalette({
  value,
  onChange,
  compact,
}: {
  value: string;
  onChange: (c: string) => void;
  compact?: boolean;
}) {
  const t = useIssuesT();
  const size = compact ? "h-7 w-7" : "h-9 w-9";
  return (
    <div className={compact ? "flex items-center" : "flex items-center gap-3"}>
      {!compact && <UILabel className="text-xs text-muted-foreground">{t.labels.color}</UILabel>}
      <label
        className={`relative inline-flex ${size} cursor-pointer items-center justify-center rounded-full border border-border shadow-sm transition-transform hover:scale-105 focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-1 focus-within:ring-offset-background`}
        style={{ backgroundColor: value }}
        aria-label={t.labels.pickColor}
        title={value}
      >
        <input
          type="color"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
        />
      </label>
      {!compact && (
        <span className="font-mono text-xs text-muted-foreground">{value}</span>
      )}
    </div>
  );
}
