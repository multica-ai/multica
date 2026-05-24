"use client";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface TimeEntryDeleteDialogProps {
  open: boolean;
  isLoading?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * Confirmation dialog for deleting a time entry.
 * Warns the user that the entry will be removed, with a short undo window.
 */
export function TimeEntryDeleteDialog({
  open,
  isLoading = false,
  onCancel,
  onConfirm,
}: TimeEntryDeleteDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete time entry</DialogTitle>
          <DialogDescription>
            This entry will be removed from your history. You'll have a few seconds to undo after deletion.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={isLoading}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={isLoading}>
            Confirm delete
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
