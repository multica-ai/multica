"use client";

import { useMemo, useState } from "react";
import { Plus, MoreHorizontal, Pencil, Trash2, Tag } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
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
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import {
  LABEL_COLORS,
  MAX_LABELS_PER_WORKSPACE,
  MAX_LABEL_NAME_LEN,
  type LabelColor,
  type IssueLabel,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels/queries";
import {
  useCreateLabel,
  useUpdateLabel,
  useDeleteLabel,
} from "@multica/core/labels/mutations";
import { LabelChip, LabelColorDot } from "../../issues/components/label-chip";

// Local helper: synthesize a Pick<IssueLabel,'name'|'color'> for preview chips.
function previewLabel(name: string, color: LabelColor): Pick<IssueLabel, "name" | "color"> {
  return { name, color };
}

const DEFAULT_COLOR: LabelColor = "slate";

export function LabelsTab() {
  const wsId = useWorkspaceId();
  const { data: labels = [], isLoading } = useQuery(labelListOptions(wsId));
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<IssueLabel | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<IssueLabel | null>(null);

  const sorted = useMemo(
    () => [...labels].sort((a, b) => a.name.localeCompare(b.name)),
    [labels],
  );

  const reachedCap = labels.length >= MAX_LABELS_PER_WORKSPACE;

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">Labels</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Tags any workspace member can attach to issues for organization and filtering.
            <span className="ml-1">{labels.length}/{MAX_LABELS_PER_WORKSPACE} used.</span>
          </p>
        </div>
        <Button
          size="sm"
          disabled={reachedCap}
          onClick={() => {
            setEditing(null);
            setDialogOpen(true);
          }}
        >
          <Plus className="h-4 w-4" />
          New label
        </Button>
      </div>

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="px-4 py-6 text-sm text-muted-foreground">Loading…</div>
          ) : sorted.length === 0 ? (
            <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
              <Tag className="h-8 w-8 text-muted-foreground" />
              <div className="text-sm font-medium">No labels yet</div>
              <div className="text-xs text-muted-foreground max-w-xs">
                Create labels to tag issues with topics, themes, or workflows.
              </div>
            </div>
          ) : (
            <ul className="divide-y">
              {sorted.map((label) => (
                <LabelRow
                  key={label.id}
                  label={label}
                  onEdit={() => {
                    setEditing(label);
                    setDialogOpen(true);
                  }}
                  onDelete={() => setConfirmDelete(label)}
                />
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <LabelEditorDialog
        open={dialogOpen}
        onOpenChange={(o) => {
          setDialogOpen(o);
          if (!o) setEditing(null);
        }}
        label={editing}
        existingNames={labels.map((l) => l.name.toLowerCase())}
      />

      <DeleteLabelDialog
        label={confirmDelete}
        onOpenChange={(o) => {
          if (!o) setConfirmDelete(null);
        }}
      />
    </div>
  );
}

function LabelRow({
  label,
  onEdit,
  onDelete,
}: {
  label: IssueLabel;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <LabelChip label={previewLabel(label.name, label.color as LabelColor)} />
      <div className="flex-1 min-w-0">
        <div className="text-xs text-muted-foreground capitalize">{label.color}</div>
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button variant="ghost" size="icon-sm">
              <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
            </Button>
          }
        />
        <DropdownMenuContent align="end" className="w-auto">
          <DropdownMenuItem onClick={onEdit}>
            <Pencil className="h-3.5 w-3.5" />
            Edit
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onDelete} className="text-destructive">
            <Trash2 className="h-3.5 w-3.5" />
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  );
}

function LabelEditorDialog({
  open,
  onOpenChange,
  label,
  existingNames,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  label: IssueLabel | null;
  existingNames: string[];
}) {
  const create = useCreateLabel();
  const update = useUpdateLabel();
  const isEdit = !!label;

  const [name, setName] = useState(label?.name ?? "");
  const [color, setColor] = useState<LabelColor>((label?.color as LabelColor) ?? DEFAULT_COLOR);

  // Reset form when dialog opens/changes target.
  // Using a separate effect keeps inputs synced when switching between create and edit.
  useMemoReset(() => {
    setName(label?.name ?? "");
    setColor((label?.color as LabelColor) ?? DEFAULT_COLOR);
  }, [open, label?.id]);

  const trimmed = name.trim();
  const lower = trimmed.toLowerCase();
  const others = existingNames.filter((n) => n !== label?.name.toLowerCase());
  const duplicate = trimmed.length > 0 && others.includes(lower);
  const tooLong = trimmed.length > MAX_LABEL_NAME_LEN;
  const empty = trimmed.length === 0;
  const dirty = isEdit ? (trimmed !== label?.name || color !== label?.color) : true;
  const canSave = !empty && !duplicate && !tooLong && dirty;
  const busy = create.isPending || update.isPending;

  async function handleSave() {
    if (!canSave) return;
    try {
      if (isEdit && label) {
        await update.mutateAsync({ id: label.id, name: trimmed, color });
        toast.success("Label updated");
      } else {
        await create.mutateAsync({ name: trimmed, color });
        toast.success("Label created");
      }
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save label");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit label" : "New label"}</DialogTitle>
          <DialogDescription>
            Pick a name and color. Workspace members can attach this label to any issue.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium">Name</label>
            <Input
              autoFocus
              value={name}
              maxLength={MAX_LABEL_NAME_LEN + 16}
              placeholder="e.g. Urgent"
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && canSave) {
                  e.preventDefault();
                  void handleSave();
                }
              }}
            />
            <div className="flex justify-between text-xs">
              <span className={duplicate ? "text-destructive" : "text-muted-foreground"}>
                {duplicate
                  ? "A label with this name already exists"
                  : tooLong
                    ? `Maximum ${MAX_LABEL_NAME_LEN} characters`
                    : ""}
              </span>
              <span className={tooLong ? "text-destructive" : "text-muted-foreground"}>
                {trimmed.length}/{MAX_LABEL_NAME_LEN}
              </span>
            </div>
          </div>

          <div className="space-y-2">
            <label className="text-xs font-medium">Color</label>
            <div className="flex flex-wrap gap-2">
              {LABEL_COLORS.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => setColor(c)}
                  className={`flex items-center justify-center h-8 w-8 rounded-md border transition-colors ${
                    color === c ? "ring-2 ring-ring" : "hover:bg-muted"
                  }`}
                  aria-label={c}
                  title={c}
                >
                  <LabelColorDot color={c} />
                </button>
              ))}
            </div>
          </div>

          <div className="rounded-md border bg-muted/40 px-3 py-2">
            <div className="text-xs text-muted-foreground mb-1.5">Preview</div>
            <LabelChip label={previewLabel(trimmed || "Label name", color)} />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!canSave || busy}>
            {isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteLabelDialog({
  label,
  onOpenChange,
}: {
  label: IssueLabel | null;
  onOpenChange: (o: boolean) => void;
}) {
  const del = useDeleteLabel();
  const open = !!label;

  async function handleConfirm() {
    if (!label) return;
    try {
      await del.mutateAsync(label.id);
      toast.success("Label deleted");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete label");
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete label?</AlertDialogTitle>
          <AlertDialogDescription>
            {label ? (
              <>
                The label &ldquo;{label.name}&rdquo; will be removed from every issue it&apos;s
                attached to. This cannot be undone.
              </>
            ) : null}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={del.isPending}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleConfirm}
            disabled={del.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

// ---------------------------------------------------------------------------
// Tiny helper to reset form fields when dependencies change without pulling
// React.useEffect into the namespace twice (kept inline for clarity).
// ---------------------------------------------------------------------------

import { useEffect } from "react";
function useMemoReset(fn: () => void, deps: unknown[]) {
  useEffect(fn, deps); // eslint-disable-line react-hooks/exhaustive-deps
}
