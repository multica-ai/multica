"use client";

// NoteFolderCreateDialog (TECH-3690) — name-and-create a notes folder from
// anywhere, not just the folder rail. The rail is hidden on mobile while a note
// is open, so folder creation also lives in the "New note" split-button menu;
// this dialog is the shared surface both entry points use. The new folder is
// created inside `parentId` (the folder you're currently in) so folders nest.
import * as React from "react";
import { FolderPlus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useCreateArtifactFolder } from "@multica/cerebro-artifacts/core";
import type { ArtifactFolder } from "@multica/core/types";

export function NoteFolderCreateDialog({
  open,
  onOpenChange,
  parentId,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  parentId: string | null;
  onCreated?: (folder: ArtifactFolder) => void;
}) {
  const createFolder = useCreateArtifactFolder();
  const [name, setName] = React.useState("");

  React.useEffect(() => {
    if (open) setName("");
  }, [open]);

  const submit = () => {
    const trimmed = name.trim();
    if (!trimmed || createFolder.isPending) return;
    createFolder.mutate(
      { name: trimmed, parent_id: parentId, kind: "note" },
      {
        onSuccess: (folder) => {
          onOpenChange(false);
          onCreated?.(folder);
        },
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v && createFolder.isPending) return;
        onOpenChange(v);
      }}
    >
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <FolderPlus className="size-4" />
            New folder
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-1.5 py-1">
          <Label htmlFor="note-folder-name">Folder name</Label>
          <Input
            id="note-folder-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Weekly reviews"
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={createFolder.isPending}
          >
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name.trim() || createFolder.isPending}>
            Create folder
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
