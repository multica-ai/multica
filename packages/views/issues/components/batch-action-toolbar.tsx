"use client";

import { useState } from "react";
import { X, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
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
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import type { UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useBatchUpdateIssues, useBatchDeleteIssues } from "@multica/core/issues/mutations";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";
import { AssigneePicker } from "./pickers";

export function BatchActionToolbar() {
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const clear = useIssueSelectionStore((s) => s.clear);
  const count = selectedIds.size;

  const [statusOpen, setStatusOpen] = useState(false);
  const [priorityOpen, setPriorityOpen] = useState(false);
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const batchUpdate = useBatchUpdateIssues();
  const batchDelete = useBatchDeleteIssues();
  const loading = batchUpdate.isPending || batchDelete.isPending;

  if (count === 0) return null;

  const ids = Array.from(selectedIds);

  const handleBatchUpdate = async (updates: Partial<UpdateIssueRequest>) => {
    try {
      await batchUpdate.mutateAsync({ ids, updates });
      toast.success(`Updated ${count} issue${count > 1 ? "s" : ""}`);
    } catch {
      toast.error("Failed to update issues");
    }
  };

  const handleBatchDelete = async () => {
    try {
      await batchDelete.mutateAsync(ids);
      clear();
      toast.success(`Deleted ${count} issue${count > 1 ? "s" : ""}`);
    } catch {
      toast.error("Failed to delete issues");
    } finally {
      setDeleteOpen(false);
    }
  };

  return (
    <>
      <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
        <div className="flex items-center gap-1.5 pl-1 pr-2 border-r mr-1">
          <span className="text-sm font-medium">{count} selected</span>
          <button
            type="button"
            onClick={clear}
            className="rounded p-0.5 hover:bg-accent transition-colors"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        {/* Status */}
        <Popover open={statusOpen} onOpenChange={setStatusOpen}>
          <PopoverTrigger
            render={
              <Button variant="ghost" size="sm" disabled={loading} />
            }
          >
            <StatusIcon status="todo" className="h-3.5 w-3.5 mr-1" />
            Status
          </PopoverTrigger>
          <PopoverContent align="center" className="w-44 p-1">
            {ALL_STATUSES.map((s) => {
              const cfg = STATUS_CONFIG[s];
              return (
                <button
                  key={s}
                  type="button"
                  onClick={() => {
                    handleBatchUpdate({ status: s });
                    setStatusOpen(false);
                  }}
                  className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${cfg.hoverBg} transition-colors`}
                >
                  <StatusIcon status={s} className="h-3.5 w-3.5" />
                  <span>{cfg.label}</span>
                </button>
              );
            })}
          </PopoverContent>
        </Popover>

        {/* Priority */}
        <Popover open={priorityOpen} onOpenChange={setPriorityOpen}>
          <PopoverTrigger
            render={
              <Button variant="ghost" size="sm" disabled={loading} />
            }
          >
            <PriorityIcon priority="high" className="mr-1" />
            Priority
          </PopoverTrigger>
          <PopoverContent align="center" className="w-44 p-1">
            {PRIORITY_ORDER.map((p) => {
              const cfg = PRIORITY_CONFIG[p];
              return (
                <button
                  key={p}
                  type="button"
                  onClick={() => {
                    handleBatchUpdate({ priority: p });
                    setPriorityOpen(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${cfg.badgeBg} ${cfg.badgeText}`}>
                    <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                    {cfg.label}
                  </span>
                </button>
              );
            })}
          </PopoverContent>
        </Popover>

        {/* Assignee */}
        <AssigneePicker
          assigneeType={null}
          assigneeId={null}
          onUpdate={handleBatchUpdate}
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          triggerRender={<Button variant="ghost" size="sm" disabled={loading} />}
          trigger="Assignee"
          align="center"
        />

        {/* Delete */}
        <Button
          variant="ghost"
          size="sm"
          disabled={loading}
          onClick={() => setDeleteOpen(true)}
          className="text-destructive hover:text-destructive"
        >
          <Trash2 className="size-3.5 mr-1" />
          Delete
        </Button>
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {count} issue{count > 1 ? "s" : ""}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete the
              selected issue{count > 1 ? "s" : ""} and all associated data.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleBatchDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

