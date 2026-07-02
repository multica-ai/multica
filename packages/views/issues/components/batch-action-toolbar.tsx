"use client";

import { useMemo, useState } from "react";
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
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { commonIssueFields } from "@multica/core/issues/batch";
import { useBatchUpdateIssues, useBatchDeleteIssues } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { StatusPicker, PriorityPicker, AssigneePicker } from "./pickers";
import { useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";
import { useIssueSurfaceActionsOptional } from "../surface/actions-context";
import { useIssueSurfaceSelection } from "../surface/selection-context";

export function BatchActionToolbar({
  issues,
  placement = "fixed-bottom",
}: {
  /**
   * The universe of selectable issues at this call site (the same list the
   * rows are rendered from). The toolbar filters it by the active surface
   * selection to reflect the real common status / priority / assignee of the
   * selected issues, mirroring how the skill list filters rows by `selectedIds`.
   */
  issues: Issue[];
  /**
   * "fixed-bottom" — floats at the bottom of the viewport (default; used by
   * full-screen issue lists).
   * "inline" — renders in normal flow so callers can place it adjacent to
   * the selected rows (used inside scrollable sections like sub-issues).
   */
  placement?: "fixed-bottom" | "inline";
}) {
  const { t } = useT("issues");
  const selection = useIssueSurfaceSelection();
  const selectedIds = selection.selectedIds;
  const clear = selection.clear;
  const count = selectedIds.size;

  // Reflect the real shared value of the selected issues in each picker; fall
  // back to an empty (no-checkmark) state when the selection is mixed, instead
  // of asserting a hardcoded default.
  const common = useMemo(
    () => commonIssueFields(issues.filter((i) => selectedIds.has(i.id))),
    [issues, selectedIds],
  );

  const [statusOpen, setStatusOpen] = useState(false);
  const [priorityOpen, setPriorityOpen] = useState(false);
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const surfaceActions = useIssueSurfaceActionsOptional();
  const batchUpdate = useBatchUpdateIssues();
  const batchDelete = useBatchDeleteIssues();
  const openModal = useModalStore((s) => s.open);
  const loading =
    surfaceActions?.isPending ?? (batchUpdate.isPending || batchDelete.isPending);

  if (count === 0) return null;

  const ids = Array.from(selectedIds);

  const handleBatchUpdate = async (updates: Partial<UpdateIssueRequest>) => {
    try {
      if (surfaceActions) {
        await surfaceActions.batchUpdate(ids, updates);
      } else {
        await batchUpdate.mutateAsync({ ids, updates });
      }
      toast.success(t(($) => $.batch.update_success, { count }));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.batch.update_failed),
      );
    }
  };

  // Status and agent/squad assignment can fan out runs across the selection, so
  // route them through the pre-trigger confirm modal (aggregate "将启动 N 个" +
  // collective handoff note for assign + 暂不开始). The modal applies the batch
  // itself. Priority, member assign, and unassign never start a run — direct.
  const handleBatchStatus = (updates: Partial<UpdateIssueRequest>) => {
    if (!updates.status) return;
    // Backlog is the parking lot — a move into backlog never starts a run
    // (server/internal/service/issue_trigger.go), so the confirm modal would
    // only render an empty "won't start" box with a single Apply button. Apply
    // directly, matching the single-issue status path.
    if (updates.status === "backlog") {
      void handleBatchUpdate(updates);
      return;
    }
    openModal("issue-run-confirm", { issueIds: ids, mode: "status", status: updates.status });
  };

  const handleBatchAssignee = (updates: Partial<UpdateIssueRequest>) => {
    if ((updates.assignee_type === "agent" || updates.assignee_type === "squad") && updates.assignee_id) {
      // Backlog never starts a run on assign (parking lot), so if every selected
      // issue is in backlog the confirm modal would only render an empty "won't
      // start" box — apply directly, matching handleBatchStatus's backlog short-
      // circuit. A mixed selection still routes through the modal: the non-backlog
      // issues will trigger and need confirmation. An empty intersection (selected
      // ids not in `issues`) falls through to the modal — safer than skipping.
      const selected = issues.filter((i) => selectedIds.has(i.id));
      const allBacklog = selected.length > 0 && selected.every((i) => i.status === "backlog");
      if (!allBacklog) {
        openModal("issue-run-confirm", {
          issueIds: ids,
          mode: "assign",
          assigneeType: updates.assignee_type,
          assigneeId: updates.assignee_id,
        });
        return;
      }
    }
    void handleBatchUpdate(updates);
  };

  const handleBatchDelete = async () => {
    try {
      if (surfaceActions) {
        await surfaceActions.batchDelete(ids);
      } else {
        await batchDelete.mutateAsync(ids);
      }
      clear();
      toast.success(t(($) => $.batch.delete_success, { count }));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.batch.delete_failed),
      );
    } finally {
      setDeleteOpen(false);
    }
  };

  return (
    <>
      <div
        className={cn(
          "z-50 flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg",
          placement === "fixed-bottom"
            ? "fixed bottom-6 left-1/2 -translate-x-1/2"
            : "mb-2 w-fit",
        )}
      >
        <div className="flex items-center gap-1.5 pl-1 pr-2 border-r mr-1">
          <span className="text-sm font-medium">{t(($) => $.batch.selected, { count })}</span>
          <button
            type="button"
            onClick={clear}
            className="rounded p-0.5 hover:bg-accent transition-colors"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        {/* Status */}
        <StatusPicker
          status={common.status}
          onUpdate={handleBatchStatus}
          open={statusOpen}
          onOpenChange={setStatusOpen}
          triggerRender={<Button variant="ghost" size="sm" disabled={loading} />}
          trigger={t(($) => $.batch.status)}
          align="center"
        />

        {/* Priority */}
        <PriorityPicker
          priority={common.priority}
          onUpdate={handleBatchUpdate}
          open={priorityOpen}
          onOpenChange={setPriorityOpen}
          triggerRender={<Button variant="ghost" size="sm" disabled={loading} />}
          trigger={t(($) => $.batch.priority)}
          align="center"
        />

        {/* Assignee */}
        <AssigneePicker
          assigneeType={common.assignee?.type ?? null}
          assigneeId={common.assignee?.id ?? null}
          mixed={common.assignee === null}
          onUpdate={handleBatchAssignee}
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          triggerRender={<Button variant="ghost" size="sm" disabled={loading} />}
          trigger={t(($) => $.batch.assignee)}
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
          {t(($) => $.batch.delete)}
        </Button>
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.batch.delete_dialog_title, { count })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.batch.delete_dialog_desc, { count })}
              <span className="mt-2 block text-xs text-muted-foreground/80">
                {t(($) => $.batch.delete_dialog_warning)}
              </span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.batch.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleBatchDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t(($) => $.batch.delete)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
