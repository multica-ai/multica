"use client";

import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
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
import {
  UI_EASE_OUT,
  UI_MOTION_DURATION,
} from "@multica/ui/lib/motion";
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
  const shouldReduceMotion = useReducedMotion() ?? false;

  // The authoritative selection is selectedIds ∩ the visible universe. Acting
  // on raw selectedIds while other consumers (Export selected, common fields)
  // intersect lets one "N selected" mean different sets — e.g. a realtime
  // update drops a selected row from the window, batch still mutates it but
  // export omits it. Count, pickers, and every action below share this set.
  const selectedIssues = useMemo(
    () => issues.filter((i) => selectedIds.has(i.id)),
    [issues, selectedIds],
  );
  const count = selectedIssues.length;

  // Reflect the real shared value of the selected issues in each picker; fall
  // back to an empty (no-checkmark) state when the selection is mixed, instead
  // of asserting a hardcoded default.
  const common = useMemo(
    () => commonIssueFields(selectedIssues),
    [selectedIssues],
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
  const ids = selectedIssues.map((issue) => issue.id);

  useEffect(() => {
    if (count > 0) return;
    setStatusOpen(false);
    setPriorityOpen(false);
    setAssigneeOpen(false);
    setDeleteOpen(false);
  }, [count]);

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

  // Batch status changes apply directly — no run-confirm modal (MUL-4155).
  // done/cancelled can never start a run, and a backlog → active promotion now
  // starts its run the same way a single-issue status change or the CLI does,
  // without an extra confirmation step (product decision on MUL-4155). The
  // status change was previously routed through the pre-trigger modal, which for
  // the common done/cancelled case only rendered a misleading "现在开始处理？ →
  // 不会开始处理" box. Agent/squad assignment still confirms via
  // handleBatchAssignee — that is the only batch action that should preview a
  // run fan-out.
  const handleBatchStatus = (updates: Partial<UpdateIssueRequest>) => {
    if (!updates.status) return;
    void handleBatchUpdate(updates);
  };

  const handleBatchAssignee = (updates: Partial<UpdateIssueRequest>) => {
    if ((updates.assignee_type === "agent" || updates.assignee_type === "squad") && updates.assignee_id) {
      // Backlog never starts a run on assign (parking lot), so if every selected
      // issue is in backlog the confirm modal would only render an empty "won't
      // start" box — apply directly, matching handleBatchStatus's backlog short-
      // circuit. A mixed selection still routes through the modal: the non-backlog
      // issues will trigger and need confirmation.
      const allBacklog = selectedIssues.every((i) => i.status === "backlog");
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
      <AnimatePresence initial={false}>
        {count > 0 && (
          <div
            key="issue-batch-toolbar"
            className={cn(
              "z-50",
              placement === "fixed-bottom"
                ? "fixed bottom-6 left-1/2 -translate-x-1/2"
                : "mb-2 w-fit",
            )}
          >
            <motion.div
              className="flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg"
              initial={{
                opacity: 0,
                transform: shouldReduceMotion
                  ? "translateY(0)"
                  : "translateY(8px)",
              }}
              animate={{
                opacity: 1,
                transform: "translateY(0)",
                transition: {
                  duration: UI_MOTION_DURATION.fast,
                  ease: UI_EASE_OUT,
                },
              }}
              exit={{
                opacity: 0,
                transform: shouldReduceMotion
                  ? "translateY(0)"
                  : "translateY(8px)",
                transition: {
                  duration: shouldReduceMotion
                    ? UI_MOTION_DURATION.fast
                    : UI_MOTION_DURATION.micro,
                  ease: UI_EASE_OUT,
                },
              }}
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
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      {count > 0 && <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
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
      </AlertDialog>}
    </>
  );
}
