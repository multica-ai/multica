"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, CircleDot, Link2, Trash2, UserMinus, X } from "@multica/ui/icons";
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
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@multica/ui/components/ui/context-menu";
import { copyText } from "@multica/ui/lib/clipboard";
import { PRIORITY_CONFIG, PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import { useWorkspacePaths } from "@multica/core/paths";
import { useQuery } from "@tanstack/react-query";
import { useIssueDeletion } from "@multica/core/issues/use-issue-deletion";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { commonIssueFields } from "@multica/core/issues/batch";
import { sharedWorkflowStatusKeys, useIssueWorkflows } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { useBatchUpdateIssues, useBatchDeleteIssues } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { StatusPicker, PriorityPicker, AssigneePicker, AssigneePickerPanel } from "./pickers";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";
import { useIssueStatusOptions } from "../utils/status-options";
import { SubmenuPanel } from "../actions/issue-actions-menu-items";
import { useRegisterIssueBatchMenu } from "../actions/issue-actions-context-menu";
import { useNavigation } from "../../navigation";
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
  const { canDelete } = useIssueDeletion(wsId);
  const canDeleteSelection = selectedIssues.every(canDelete);
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
  const [menuPoint, setMenuPoint] = useState<{ x: number; y: number } | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const selectedRef = useRef(selectedIssues);
  selectedRef.current = selectedIssues;
  useRegisterIssueBatchMenu(
    useMemo(
      () => ({
        covers: (issueId: string) =>
          selectedRef.current.length > 1 && selectedRef.current.some((issue) => issue.id === issueId),
        openAt: (point: { x: number; y: number }) => {
          setMenuPoint(point);
          setMenuOpen(true);
        },
      }),
      [],
    ),
  );
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
    setMenuOpen(false);
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
    if (!canDeleteSelection) return;
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
          disabled={loading || !canDeleteSelection}
          title={!canDeleteSelection ? t(($) => $.batch.delete_restricted) : undefined}
          onClick={() => setDeleteOpen(true)}
          className="text-destructive hover:text-destructive"
        >
          <Trash2 className="size-3.5 mr-1" />
          {t(($) => $.batch.delete)}
        </Button>
      </CollectionBatchBar>

      {menuPoint && count > 1 && (
        <BatchContextMenu
          point={menuPoint}
          open={menuOpen}
          onOpenChange={setMenuOpen}
          issues={selectedIssues}
          projectId={sharedProjectId}
          allowedStatuses={allowedStatuses}
          common={common}
          disabled={loading}
          canDelete={canDeleteSelection}
          onStatus={(status) => handleBatchStatus({ status })}
          onPriority={(priority) => void handleBatchUpdate({ priority })}
          onAssignee={handleBatchAssignee}
          onClear={clear}
          onDelete={() => setDeleteOpen(true)}
        />
      )}

      {count > 0 && <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.batch.delete_dialog_title, { count })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.batch.delete_dialog_desc, { count })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.batch.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              disabled={loading || !canDeleteSelection}
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

/** The batch bar's actions at the cursor, for a right-click on a selected row. */
function BatchContextMenu({
  point,
  open,
  onOpenChange,
  issues,
  projectId,
  allowedStatuses,
  common,
  disabled,
  canDelete,
  onStatus,
  onPriority,
  onAssignee,
  onClear,
  onDelete,
}: {
  point: { x: number; y: number };
  open: boolean;
  onOpenChange: (open: boolean) => void;
  issues: Issue[];
  projectId: string | null;
  allowedStatuses: ReadonlySet<string> | null;
  common: ReturnType<typeof commonIssueFields>;
  disabled: boolean;
  canDelete: boolean;
  onStatus: (status: string) => void;
  onPriority: (priority: Issue["priority"]) => void;
  onAssignee: (updates: Partial<UpdateIssueRequest>) => void;
  onClear: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("issues");
  const { t: tLayout } = useT("layout");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { getShareableUrl } = useNavigation();
  const { options } = useIssueStatusOptions(wsId, projectId ?? undefined, common.status);
  const statusOptions = allowedStatuses ? options.filter((o) => allowedStatuses.has(o.key)) : options;
  const count = issues.length;
  const anchor = useMemo(
    () => ({
      getBoundingClientRect: () => DOMRect.fromRect({ x: point.x, y: point.y, width: 0, height: 0 }),
    }),
    [point.x, point.y],
  );
  const copyLinks = () => {
    const links = issues.map((issue) => getShareableUrl(paths.issueDetail(issue.identifier || issue.id)));
    void copyText(links.join("\n")).then((ok) => {
      if (ok) toast.success(t(($) => $.batch.links_copied, { count }));
    });
  };

  return (
    <ContextMenu open={open} onOpenChange={onOpenChange}>
      <ContextMenuContent anchor={anchor} className="w-auto min-w-52">
        <div className="px-2 pt-1 pb-1.5 text-caption text-muted-foreground">
          {t(($) => $.batch.selected, { count })}
        </div>
        <ContextMenuSeparator />
        <ContextMenuSub>
          <ContextMenuSubTrigger disabled={disabled}>
            {common.status ? (
              <StatusIcon status={common.status} className="h-3.5 w-3.5" />
            ) : (
              <CircleDot className="h-3.5 w-3.5" />
            )}
            {t(($) => $.batch.status)}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            {statusOptions.map((option) => (
              <ContextMenuItem key={option.key} onClick={() => onStatus(option.key)}>
                <StatusIcon
                  status={option.key}
                  category={option.category}
                  color={option.color}
                  icon={option.icon}
                  className="h-3.5 w-3.5"
                />
                {option.label}
                {common.status === option.key && <Check className="ml-auto size-3.5 text-muted-foreground" />}
              </ContextMenuItem>
            ))}
          </ContextMenuSubContent>
        </ContextMenuSub>
        <ContextMenuSub>
          <ContextMenuSubTrigger disabled={disabled}>
            <PriorityIcon priority={common.priority ?? "none"} />
            {t(($) => $.batch.priority)}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            {PRIORITY_DISPLAY_ORDER.map((p) => (
              <ContextMenuItem key={p} onClick={() => onPriority(p)}>
                <span
                  className={`inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-caption font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
                >
                  <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                  {t(($) => $.priority[p])}
                </span>
                {common.priority === p && <Check className="ml-auto size-3.5 text-muted-foreground" />}
              </ContextMenuItem>
            ))}
          </ContextMenuSubContent>
        </ContextMenuSub>
        <ContextMenuSub>
          <ContextMenuSubTrigger disabled={disabled}>
            <UserMinus className="h-3.5 w-3.5" />
            {t(($) => $.batch.assignee)}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent className="w-64 p-0">
            <SubmenuPanel>
              <AssigneePickerPanel
                assigneeType={common.assignee?.type ?? null}
                assigneeId={common.assignee?.id ?? null}
                onUpdate={onAssignee}
                onPicked={() => onOpenChange(false)}
              />
            </SubmenuPanel>
          </ContextMenuSubContent>
        </ContextMenuSub>
        <ContextMenuSeparator />
        <ContextMenuItem onClick={copyLinks}>
          <Link2 className="h-3.5 w-3.5" />
          {t(($) => $.batch.copy_links)}
        </ContextMenuItem>
        <ContextMenuItem onClick={onClear}>
          <X className="h-3.5 w-3.5" />
          {tLayout(($) => $.collection.clear_selection)}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem
          variant="destructive"
          disabled={disabled || !canDelete}
          title={!canDelete ? t(($) => $.batch.delete_restricted) : undefined}
          onClick={onDelete}
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t(($) => $.batch.delete_count, { count })}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
