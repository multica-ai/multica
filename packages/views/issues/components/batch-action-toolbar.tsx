"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Ellipsis, Link2 } from "@multica/ui/icons";
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
import { ContextMenu, ContextMenuContent } from "@multica/ui/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useQuery } from "@tanstack/react-query";
import { useIssueDeletion } from "@multica/core/issues/use-issue-deletion";
import { useIsGuest } from "@multica/core/permissions";
import {
  isListPropertyType,
  type BatchIssueLabelOps,
  type BatchIssuePropertyOps,
  type Issue,
  type UpdateIssueRequest,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { commonIssueFields } from "@multica/core/issues/batch";
import { sharedWorkflowStatusKeys, useIssueWorkflows } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import { useBatchUpdateIssues, useBatchDeleteIssues } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { StatusPicker, PriorityPicker, AssigneePicker, DueDatePicker, StartDatePicker } from "./pickers";
import { BatchLabelPicker } from "./pickers/batch-label-picker";
import { CustomPropertyValueInput } from "./pickers/custom-property-picker";
import { BatchMenuItems, sharedPropertyValue, useCopyIssueLinks, type BatchEditor } from "./batch-menu-items";
import { useBatchResultToast } from "./batch-result-toast";
import { contextPrimitives, dropdownPrimitives } from "../../common/menu-primitives";
import { useRegisterIssueBatchMenu } from "../actions/issue-actions-context-menu";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { ProjectPicker } from "../../projects/components/project-picker";
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
  const { canDelete, offersDelete } = useIssueDeletion(wsId);
  // A guest (MUL-9420) edits no issue; its selection only copies links.
  const guest = useIsGuest(wsId);
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
  const [labelsOpen, setLabelsOpen] = useState(false);
  const [projectOpen, setProjectOpen] = useState(false);
  const [dueOpen, setDueOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [parentOpen, setParentOpen] = useState(false);
  const [editor, setEditor] = useState<BatchEditor | null>(null);
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
  const { report, reportError } = useBatchResultToast();
  const loading =
    surfaceActions?.isPending ?? (batchUpdate.isPending || batchDelete.isPending);
  const ids = selectedIssues.map((issue) => issue.id);

  // The same rule as commonIssueFields, for the nullable fields the bar adds:
  // undefined when the selection disagrees, so no row is checked.
  const sharedProject = shared(selectedIssues, (issue) => issue.project_id ?? null);
  const sharedDueDate = shared(selectedIssues, (issue) => issue.due_date ?? null);
  const sharedStartDate = shared(selectedIssues, (issue) => issue.start_date ?? null);

  useEffect(() => {
    if (count > 0) return;
    setStatusOpen(false);
    setPriorityOpen(false);
    setAssigneeOpen(false);
    setLabelsOpen(false);
    setProjectOpen(false);
    setDueOpen(false);
    setMoreOpen(false);
    setParentOpen(false);
    setEditor(null);
    setDeleteOpen(false);
    setMenuOpen(false);
  }, [count]);

  const selectOnly = (issueIds: string[]) => {
    clear();
    selection.select(issueIds);
  };

  const handleBatchUpdate = async (
    updates: Partial<UpdateIssueRequest>,
    ops: { labels?: BatchIssueLabelOps; properties?: BatchIssuePropertyOps } = {},
    { quiet = false }: { quiet?: boolean } = {},
  ): Promise<boolean> => {
    try {
      const response = surfaceActions
        ? await surfaceActions.batchUpdate(ids, updates, ops)
        : await batchUpdate.mutateAsync({ ids, updates, ...ops });
      const applied = report(response, count, {
        onSelectSkipped: selectOnly,
        successMessage: quiet ? null : undefined,
      });
      if (applied && clearSelectionOnApply && !quiet) clear();
      return applied;
    } catch (err) {
      reportError(err);
      return false;
    }
  };

  // Label toggles stay quiet on success: the picker stays open for the next
  // toggle and the rows themselves show the result.
  const handleLabels = (labels: BatchIssueLabelOps) => handleBatchUpdate({}, { labels }, { quiet: !labels.clear });
  const handleProperties = (properties: BatchIssuePropertyOps) => handleBatchUpdate({}, { properties });

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

  // An editor picked from a menu opens once that menu has closed, so the
  // menu's focus return does not dismiss it.
  const openEditor = (next: BatchEditor) => {
    setMoreOpen(false);
    setMenuOpen(false);
    setTimeout(() => setEditor(next), 0);
  };
  const openSetParent = () => {
    setMoreOpen(false);
    setMenuOpen(false);
    setParentOpen(true);
  };

  const menuProps = {
    issues: selectedIssues,
    common,
    projectId: sharedProjectId,
    allowedStatuses,
    disabled: loading,
    editable: !guest,
    offersDelete,
    canDelete: canDeleteSelection,
    onStatus: (status: string) => handleBatchStatus({ status }),
    onAssignee: handleBatchAssignee,
    onUpdate: (updates: Partial<UpdateIssueRequest>) => void handleBatchUpdate(updates),
    onLabels: handleLabels,
    onProperties: (properties: BatchIssuePropertyOps) => void handleProperties(properties),
    onEditor: openEditor,
    onSetParent: openSetParent,
    onDelete: () => {
      setMoreOpen(false);
      setMenuOpen(false);
      setDeleteOpen(true);
    },
  };

  const barButton = <Button variant="ghost" size="sm" disabled={loading} />;
  // Editors opened from a menu anchor on the "More" button.
  const editorAnchor = (
    <button
      type="button"
      tabIndex={-1}
      aria-hidden
      className="pointer-events-none absolute inset-0 overflow-hidden opacity-0"
    />
  );
  const closeEditor = (open: boolean) => {
    if (!open) setEditor(null);
  };

  const contextMenu = menuPoint && count > 1 && (
    <BatchContextMenu point={menuPoint} open={menuOpen} onOpenChange={setMenuOpen}>
      <BatchMenuItems
        primitives={contextPrimitives}
        variant="context"
        {...menuProps}
        onClearSelection={clear}
        onClose={() => setMenuOpen(false)}
      />
    </BatchContextMenu>
  );

  if (guest) {
    return (
      <>
        <CollectionBatchBar
          count={count}
          onClear={clear}
          placement={placement === "fixed-bottom" ? "fixed" : "inline"}
          label={t(($) => $.batch.selected, { count })}
        >
          <CopyLinksButton issues={selectedIssues} />
        </CollectionBatchBar>
        {contextMenu}
      </>
    );
  }

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
          triggerRender={barButton}
          trigger={t(($) => $.batch.status)}
          align="center"
        />

        {/* Priority */}
        <PriorityPicker
          priority={common.priority}
          onUpdate={handleBatchUpdate}
          open={priorityOpen}
          onOpenChange={setPriorityOpen}
          triggerRender={barButton}
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
          triggerRender={barButton}
          trigger={t(($) => $.batch.assignee)}
          align="center"
        />

        {/* Labels */}
        <BatchLabelPicker
          issues={selectedIssues}
          onApply={handleLabels}
          open={labelsOpen}
          onOpenChange={setLabelsOpen}
          triggerRender={barButton}
          trigger={t(($) => $.batch.labels)}
        />

        {/* Project */}
        <ProjectPicker
          projectId={sharedProject}
          onUpdate={handleBatchUpdate}
          open={projectOpen}
          onOpenChange={setProjectOpen}
          triggerRender={barButton}
          trigger={t(($) => $.batch.project)}
          align="center"
        />

        {/* Due date */}
        <DueDatePicker
          dueDate={sharedDueDate ?? null}
          onUpdate={handleBatchUpdate}
          open={dueOpen}
          onOpenChange={setDueOpen}
          triggerRender={barButton}
          trigger={t(($) => $.batch.due_date)}
          align="center"
        />

        {/* More: the remaining fields, copy links and delete */}
        <span className="relative inline-flex">
          <DropdownMenu open={moreOpen} onOpenChange={setMoreOpen}>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={loading}
                  aria-label={t(($) => $.batch.more)}
                  title={t(($) => $.batch.more)}
                />
              }
            >
              <Ellipsis className="size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" align="end" className="w-auto min-w-52">
              <BatchMenuItems
                primitives={dropdownPrimitives}
                variant="more"
                {...menuProps}
                onClose={() => setMoreOpen(false)}
              />
            </DropdownMenuContent>
          </DropdownMenu>
          {editor?.kind === "start_date" && (
            <StartDatePicker
              startDate={sharedStartDate ?? null}
              onUpdate={handleBatchUpdate}
              open
              onOpenChange={closeEditor}
              triggerRender={editorAnchor}
              align="end"
            />
          )}
          {editor?.kind === "due_date" && (
            <DueDatePicker
              dueDate={sharedDueDate ?? null}
              onUpdate={handleBatchUpdate}
              open
              onOpenChange={closeEditor}
              triggerRender={editorAnchor}
              align="end"
            />
          )}
          {editor?.kind === "property" && (
            <CustomPropertyValueInput
              key={editor.property.id}
              property={editor.property}
              value={sharedPropertyValue(selectedIssues, editor.property.id)}
              onChange={(value) => {
                const save = handleProperties(
                  value === undefined
                    ? { unset: [editor.property.id] }
                    : { set: { [editor.property.id]: value } },
                );
                // List editors hold their draft and lock input until the write
                // settles, and show a rejection beside the draft.
                if (!isListPropertyType(editor.property.type)) return;
                return save.then((applied) => {
                  if (!applied) throw new Error(t(($) => $.batch.update_failed));
                });
              }}
              open
              onOpenChange={closeEditor}
              triggerRender={editorAnchor}
            />
          )}
        </span>
      </CollectionBatchBar>

      {contextMenu}

      {parentOpen && (
        <IssuePickerModal
          open
          onOpenChange={setParentOpen}
          title={t(($) => $.batch.set_parent_title, { count })}
          description={t(($) => $.batch.set_parent_description, { count })}
          excludeIds={ids}
          onSelect={(parent) => void handleBatchUpdate({ parent_issue_id: parent.id })}
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

/** The value every issue shares, or undefined when they differ. */
function shared<T>(issues: readonly Issue[], pick: (issue: Issue) => T): T | undefined {
  const first = issues[0];
  if (!first) return undefined;
  const value = pick(first);
  return issues.every((issue) => pick(issue) === value) ? value : undefined;
}

/** A guest's whole batch bar: the one action its selection supports. */
function CopyLinksButton({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const copyLinks = useCopyIssueLinks(issues);
  return (
    <Button variant="ghost" size="sm" onClick={copyLinks}>
      <Link2 className="size-4" />
      {t(($) => $.batch.copy_links)}
    </Button>
  );
}

/** The batch menu at the cursor, for a right-click on a selected row. */
function BatchContextMenu({
  point,
  open,
  onOpenChange,
  children,
}: {
  point: { x: number; y: number };
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
}) {
  const anchor = useMemo(
    () => ({
      getBoundingClientRect: () => DOMRect.fromRect({ x: point.x, y: point.y, width: 0, height: 0 }),
    }),
    [point.x, point.y],
  );
  return (
    <ContextMenu open={open} onOpenChange={onOpenChange}>
      <ContextMenuContent anchor={anchor} className="w-auto min-w-52">
        {children}
      </ContextMenuContent>
    </ContextMenu>
  );
}
