"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Archive,
  ArchiveRestore,
  ArrowDown,
  ArrowUp,
  Calendar,
  CalendarClock,
  CopyPlus,
  FolderOpen,
  Link2,
  MoreHorizontal,
  Pin,
  PinOff,
  Plus,
  Trash2,
  UserMinus,
} from "lucide-react";
import type { AgentTask, Issue } from "@multica/core/types";
import { todayDateOnly, addDaysDateOnly } from "@multica/core/issues/date";
import { api } from "@multica/core/api";
import {
  ALL_STATUSES,
  PRIORITY_ORDER,
  PRIORITY_CONFIG,
} from "@multica/core/issues/config";
import { issueKeys } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { StatusIcon } from "../components/status-icon";
import { PriorityIcon } from "../components/priority-icon";
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
  ContextMenuSeparator,
} from "@multica/ui/components/ui/context-menu";
import { copyText } from "@multica/ui/lib/clipboard";
import type { UseIssueActionsResult } from "./use-issue-actions";
import { useT } from "../../i18n";

// Both Dropdown and Context menu wrappers expose an API-compatible surface
// (variant, inset, onClick, etc.). We bundle the primitives we need into a
// single object so `IssueActionsMenuItems` can render the same JSX for both.
export interface MenuPrimitives {
  Item: typeof DropdownMenuItem;
  Sub: typeof DropdownMenuSub;
  SubTrigger: typeof DropdownMenuSubTrigger;
  SubContent: typeof DropdownMenuSubContent;
  Separator: typeof DropdownMenuSeparator;
}

export const dropdownPrimitives: MenuPrimitives = {
  Item: DropdownMenuItem,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
  Separator: DropdownMenuSeparator,
};

// Context primitives are API-compatible with Dropdown primitives, but their
// TypeScript identities differ. Cast once here and call it a day — this is the
// single bridge between the two primitive sets.
export const contextPrimitives: MenuPrimitives = {
  Item: ContextMenuItem as unknown as typeof DropdownMenuItem,
  Sub: ContextMenuSub as unknown as typeof DropdownMenuSub,
  SubTrigger: ContextMenuSubTrigger as unknown as typeof DropdownMenuSubTrigger,
  SubContent: ContextMenuSubContent as unknown as typeof DropdownMenuSubContent,
  Separator: ContextMenuSeparator as unknown as typeof DropdownMenuSeparator,
};

interface IssueActionsMenuItemsProps {
  issue: Issue;
  actions: UseIssueActionsResult;
  primitives: MenuPrimitives;
  /** Called when the user clicks the Assignee menu item. The parent should
   *  close the surrounding menu and open the shared `AssigneePicker` popover.
   *  Decoupled this way so the same item can drive both the dropdown
   *  (3-dot button) and the context menu (right-click) wrappers. */
  onOpenAssignee: () => void;
  /** If set, navigate here after the issue is deleted (used by the detail page). */
  onDeletedNavigateTo?: string;
}

export function IssueActionsMenuItems({
  issue,
  actions,
  primitives: P,
  onOpenAssignee,
  onDeletedNavigateTo,
}: IssueActionsMenuItemsProps) {
  const { t } = useT("issues");
  const {
    isPinned,
    canDelete,
    updateField,
    togglePin,
    copyLink,
    openCreateIssueFromCurrent,
    openCreateSubIssue,
    openSetParent,
    openAddChild,
      openDeleteConfirm,
  } = actions;

  // Subscribe to the issue's task list so the cache is warm by the time the
  // user clicks "Copy local workdir path". The query only fires while the
  // menu is open (Base UI portals the menu content lazily) — list views
  // that wrap every row in IssueActionsContextMenu pay nothing until the
  // menu actually opens.
  //
  // The query shares its key with ExecutionLogSection, so navigating from
  // the issue detail page is a free cache hit.
  const { data: tasks } = useQuery({
    queryKey: issueKeys.tasks(issue.id),
    queryFn: () => api.listTasksByIssue(issue.id),
    staleTime: 30_000,
  });

  const { getAgentName } = useActorName();

  // One sub-item per task that has a workdir, newest first — mirrors the old
  // "latest" semantics (latest sits at the top) while exposing every agent's
  // path instead of collapsing to a single one.
  const workdirTasks = useMemo(() => workdirTasksDesc(tasks), [tasks]);

  // Synchronous click handler — the awaited fetch in the previous version
  // dropped the browser's transient user activation, which made
  // navigator.clipboard.writeText() reject from the menu when the cache
  // was cold. We copy the cached path inside the same task as the click.
  const handleCopyWorkdir = useCallback(
    (workDir: string) => {
      void copyText(workDir).then((ok) => {
        if (ok) toast.success(t(($) => $.detail.workdir_path_copied));
        else toast.error(t(($) => $.detail.workdir_path_copy_failed));
      });
    },
    [t],
  );

  return (
    <>
      {/* Status */}
      <P.Sub>
        <P.SubTrigger>
          <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
          {t(($) => $.actions.status)}
        </P.SubTrigger>
        <P.SubContent>
          {ALL_STATUSES.map((s) => (
            <P.Item key={s} onClick={() => updateField({ status: s })}>
              <StatusIcon status={s} className="h-3.5 w-3.5" />
              {t(($) => $.status[s])}
              {issue.status === s && (
                <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Priority */}
      <P.Sub>
        <P.SubTrigger>
          <PriorityIcon priority={issue.priority} />
          {t(($) => $.actions.priority)}
        </P.SubTrigger>
        <P.SubContent>
          {PRIORITY_ORDER.map((p) => (
            <P.Item key={p} onClick={() => updateField({ priority: p })}>
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
              >
                <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                {t(($) => $.priority[p])}
              </span>
              {issue.priority === p && (
                <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Assignee — closes this menu and hands off to the shared
          AssigneePicker (members + agents + squads, with search and
          permission checks). Keeps a single source of truth for the
          assignee UX across detail sidebar, board cards, and right-click /
          3-dot menus. */}
      <P.Item onClick={onOpenAssignee}>
        <UserMinus className="h-3.5 w-3.5" />
        {t(($) => $.actions.assignee)}
      </P.Item>

      {/* Start date */}
      <P.Sub>
        <P.SubTrigger>
          <CalendarClock className="h-3.5 w-3.5" />
          {t(($) => $.actions.start_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ start_date: todayDateOnly() })}>
            {t(($) => $.actions.start_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ start_date: addDaysDateOnly(1) })}>
            {t(($) => $.actions.start_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ start_date: addDaysDateOnly(7) })}>
            {t(($) => $.actions.start_next_week)}
          </P.Item>
          {issue.start_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ start_date: null })}>
                {t(($) => $.actions.start_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      {/* Due date */}
      <P.Sub>
        <P.SubTrigger>
          <Calendar className="h-3.5 w-3.5" />
          {t(($) => $.actions.due_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ due_date: todayDateOnly() })}>
            {t(($) => $.actions.due_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: addDaysDateOnly(1) })}>
            {t(($) => $.actions.due_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: addDaysDateOnly(7) })}>
            {t(($) => $.actions.due_next_week)}
          </P.Item>
          {issue.due_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ due_date: null })}>
                {t(($) => $.actions.due_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      <P.Item onClick={togglePin}>
        {isPinned ? (
          <PinOff className="h-3.5 w-3.5" />
        ) : (
          <Pin className="h-3.5 w-3.5" />
        )}
        {isPinned ? t(($) => $.actions.unpin_from_sidebar) : t(($) => $.actions.pin_to_sidebar)}
      </P.Item>
      <P.Item onClick={copyLink}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_link)}
      </P.Item>
      <P.Sub>
        <P.SubTrigger>
          <FolderOpen className="h-3.5 w-3.5" />
          {t(($) => $.actions.copy_workdir_path)}
        </P.SubTrigger>
        <P.SubContent>
          {workdirTasks.length === 0 ? (
            <P.Item disabled>
              {t(($) => $.detail.workdir_path_unavailable)}
            </P.Item>
          ) : (
            workdirTasks.map((task) => (
              <P.Item
                key={task.id}
                onClick={() => handleCopyWorkdir(task.work_dir as string)}
              >
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-sm">
                    {getAgentName(task.agent_id)}
                  </span>
                  {task.relative_work_dir && (
                    <span className="truncate text-xs text-muted-foreground">
                      {task.relative_work_dir}
                    </span>
                  )}
                </div>
              </P.Item>
            ))
          )}
        </P.SubContent>
      </P.Sub>
      <P.Item onClick={openCreateIssueFromCurrent}>
        <CopyPlus className="h-3.5 w-3.5" />
        {t(($) => $.actions.create_from_issue)}
      </P.Item>

      <P.Separator />

      {/* Lower-frequency relationship actions live under "More" and will grow
          (blocks, duplicates, related) as we add more relation types. */}
      <P.Sub>
        <P.SubTrigger>
          <MoreHorizontal className="h-3.5 w-3.5" />
          {t(($) => $.actions.more)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={openCreateSubIssue}>
            <Plus className="h-3.5 w-3.5" />
            {t(($) => $.actions.create_sub_issue)}
          </P.Item>
          <P.Item onClick={openSetParent}>
            <ArrowUp className="h-3.5 w-3.5" />
            {t(($) => $.actions.set_parent_issue)}
          </P.Item>
          <P.Item onClick={openAddChild}>
            <ArrowDown className="h-3.5 w-3.5" />
            {t(($) => $.actions.add_sub_issue)}
          </P.Item>
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      {/* Archive actions — only available for terminal issues */}
      {!issue.archived_at && (issue.status === "done" || issue.status === "cancelled") && (
        <P.Item onClick={actions.openArchiveConfirm}>
          <Archive className="h-3.5 w-3.5" />
          {t(($) => $.actions.archive_issue)}
        </P.Item>
      )}
      {issue.archived_at && (
        <P.Item onClick={actions.openUnarchiveConfirm}>
          <ArchiveRestore className="h-3.5 w-3.5" />
          {t(($) => $.actions.unarchive_issue)}
        </P.Item>
      )}

      {canDelete && (
        <P.Item
          variant="destructive"
          onClick={() => openDeleteConfirm({ onDeletedNavigateTo })}
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t(($) => $.actions.delete_issue)}
        </P.Item>
      )}
    </>
  );
}

// Tasks that actually have a workdir, newest first, deduplicated by
// (agent_id, work_dir). A single agent reuses the same working copy across
// every run on one issue, so its repeated tasks all report an identical
// work_dir — collapsing them keeps one row per distinct agent+path instead
// of listing the same copy-target N times. Tasks without a work_dir (queued
// but never dispatched, or older backends) are dropped — there is nothing to
// copy for them.
function workdirTasksDesc(tasks: AgentTask[] | undefined): AgentTask[] {
  if (!tasks?.length) return [];
  const sorted = tasks
    .filter((task) => !!task.work_dir)
    .sort((a, b) => b.created_at.localeCompare(a.created_at));
  const seen = new Set<string>();
  return sorted.filter((task) => {
    const key = `${task.agent_id} ${task.work_dir}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}
