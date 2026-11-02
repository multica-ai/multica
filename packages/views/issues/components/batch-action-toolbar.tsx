"use client";

import { useEffect, useMemo, useState } from "react";
import { Trash2 } from "@multica/ui/icons";
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
import { useQuery } from "@tanstack/react-query";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { commonIssueFields } from "@multica/core/issues/batch";
import { sharedWorkflowStatusKeys, useIssueWorkflows } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { useBatchUpdateIssues, useBatchDeleteIssues } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { StatusPicker, PriorityPicker, AssigneePicker } from "./pickers";
import { useT } from "../../i18n";
import { CollectionBatchBar } from "../../layout/collection-list";
import { useIssueSurfaceActionsOptional } from "../surface/actions-context";
import { useIssueSurfaceSelection } from "../surface/selection-context";
import { useRegisterCommandBarContext } from "../../search/register-context";

export function BatchActionToolbar({
  issues,
  placement = "fixed-bottom",
  clearSelectionOnApply = false,
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
  /**
   * Drop the selection once a status / priority / assignee change fully
   * applies. Off for full-screen lists, where the selection stays so several
   * edits can be chained; on for sub-issues, where a batch edit is a one-shot
   * dispatch and a lingering inline toolbar reads as unfinished. A partial or
   * failed write keeps the selection so the user can retry.
   */
  clearSelectionOnApply?: boolean;
}) {
  const { t } = useT("issues");
  const selection = useIssueSurfaceSelection();
  const selectedIds = selection.selectedIds;
  const clear = selection.clear;

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

  useRegisterCommandBarContext(
    useMemo(
      () => (count > 0 ? { kind: "issues", issues: selectedIssues, clearSelection: clear } : null),
      [count, selectedIssues, clear],
    ),
  );

  // Reflect the real shared value of the selected issues in each picker; fall
  // back to an empty (no-checkmark) state when the selection is mixed, instead
  // of asserting a hardcoded default.
  const common = useMemo(
    () => commonIssueFields(selectedIssues),
    [selectedIssues],
  );

  // A selection inside one project offers that project's workflow statuses;
  // one spanning projects offers the statuses every selected issue's
  // workflow allows. (MUL-7420)
  const sharedProjectId = useMemo(() => {
    const first = selectedIssues[0]?.project_id ?? null;
    return first && selectedIssues.every((i) => i.project_id === first) ? first : null;
  }, [selectedIssues]);
  const wsId = useWorkspaceId();
  const { workflows } = useIssueWorkflows(wsId);
  const { data: projects } = useQuery({ ...projectListOptions(wsId), enabled: count > 0 && !sharedProjectId });
  const allowedStatuses = useMemo(
    () => (sharedProjectId ? null : sharedWorkflowStatusKeys(workflows, projects, selectedIssues)),
    [sharedProjectId, workflows, projects, selectedIssues],
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
      const { updated } = surfaceActions
        ? await surfaceActions.batchUpdate(ids, updates)
        : await batchUpdate.mutateAsync({ ids, updates });
      if (updated < count) {
        toast.warning(t(($) => $.batch.update_partial, { updated, count }));
      } else {
        if (clearSelectionOnApply) clear();
        toast.success(t(($) => $.batch.update_success, { count }));
      }
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
      // Only the fixed Backlog key parks work; custom Unstarted does not.
      const allBacklog = selectedIssues.every((i) => i.status === "backlog");
      if (!allBacklog) {
        openModal("issue-run-confirm", {
          issueIds: ids,
          mode: "assign",
          assigneeType: updates.assignee_type,
          assigneeId: updates.assignee_id,
          onApplied: clearSelectionOnApply ? clear : undefined,
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
      <CollectionBatchBar
        count={count}
        onClear={clear}
        placement={placement === "fixed-bottom" ? "fixed" : "inline"}
        label={t(($) => $.batch.selected, { count })}
      >
        {/* Status */}
        <StatusPicker
          status={common.status}
          // No shared project spans projects (or none): not the Default-only
          // list of a single issue outside a project. (MUL-8121)
          projectId={sharedProjectId ?? undefined}
          allowedStatuses={allowedStatuses}
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
      </CollectionBatchBar>

      {count > 0 && <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.batch.delete_dialog_title, { count })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.batch.delete_dialog_desc, { count })}
              <span className="mt-2 block text-caption text-muted-foreground">
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
