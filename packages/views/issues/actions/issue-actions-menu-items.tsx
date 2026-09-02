"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowDown,
  ArrowUp,
  Calendar,
  CalendarClock,
  ExternalLink,
  FolderOpen,
  Link2,
  Network,
  Pin,
  PinOff,
  Plus,
  Trash2,
  Unlink,
  UserMinus,
} from "lucide-react";
import type { Issue } from "@multica/core/types";
import { resolveWorkdirCopyTarget } from "@multica/core/issues";
import { todayDateOnly, addDaysDateOnly } from "@multica/core/issues/date";
import { api } from "@multica/core/api";
import {
  PRIORITY_DISPLAY_ORDER,
  PRIORITY_CONFIG,
} from "@multica/core/issues/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useStatusOptions } from "../utils/status-options";
import { issueKeys } from "@multica/core/issues/queries";
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
import { PluginHookMenuItems, PluginModalMenuItems } from "../../plugins";
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
  /** If set, leave the page after the issue is deleted (used by the detail
   *  page, which renders the issue being deleted). The delete modal goes back
   *  to the list the user came from and only falls back to this path when
   *  there is no in-app history. List surfaces leave it unset and stay put. */
  onDeletedFallbackPath?: string;
}

export function IssueActionsMenuItems({
  issue,
  actions,
  primitives: P,
  onOpenAssignee,
  onDeletedFallbackPath,
}: IssueActionsMenuItemsProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const statusOptions = useStatusOptions(wsId);
  const { categoryOf, colorOf } = useIssueStatuses(wsId);
  const {
    isPinned,
    updateField,
    openInNewTab,
    togglePin,
    copyLink,
    openCreateSubIssue,
    openSetParent,
    removeParent,
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
  const workdirCopyTarget = useMemo(
    () => resolveWorkdirCopyTarget(tasks),
    [tasks],
  );

  // Synchronous click handler — the awaited fetch in the previous version
  // dropped the browser's transient user activation, which made
  // navigator.clipboard.writeText() reject from the menu when the cache
  // was cold. We now read straight from the cached query result and write
  // to the clipboard inside the same task as the click.
  const handleCopyWorkdirPath = useCallback(() => {
    if (!workdirCopyTarget) {
      toast.error(t(($) => $.detail.workdir_path_unavailable));
      return;
    }
    void copyText(workdirCopyTarget.path).then((ok) => {
      if (!ok) {
        toast.error(t(($) => $.detail.workdir_path_copy_failed));
        return;
      }
      if (workdirCopyTarget.source === "durable_project_directory") {
        toast.success(
          workdirCopyTarget.branchName
            ? t(($) => $.detail.project_directory_path_copied_with_branch, {
                branch: workdirCopyTarget.branchName,
              })
            : t(($) => $.detail.project_directory_path_copied),
        );
        return;
      }
      toast.success(t(($) => $.detail.workdir_path_copied));
    });
  }, [workdirCopyTarget, t]);

  return (
    <>
      {/* Status */}
      <P.Sub>
        <P.SubTrigger>
          <StatusIcon
            status={issue.status}
            category={categoryOf(issue.status)}
            color={colorOf(issue.status)}
            className="h-3.5 w-3.5"
          />
          {t(($) => $.actions.status)}
        </P.SubTrigger>
        <P.SubContent>
          {/* Catalog-driven, like the picker and the filter: every entry point
              that can change a status must offer the same set, or a custom
              status is unreachable from the board's right-click menu. One flat
              list in canonical category order. (MUL-6243) */}
          {statusOptions.map((option) => (
            <P.Item
              key={option.key}
              onClick={() => updateField({ status: option.key })}
            >
              <StatusIcon
                status={option.key}
                category={option.category}
                color={option.color}
                className="h-3.5 w-3.5"
              />
              {option.label}
              {issue.status === option.key && (
                <span className="ml-auto text-caption text-muted-foreground">{"✓"}</span>
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
          {PRIORITY_DISPLAY_ORDER.map((p) => (
            <P.Item key={p} onClick={() => updateField({ priority: p })}>
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-caption font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
              >
                <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                {t(($) => $.priority[p])}
              </span>
              {issue.priority === p && (
                <span className="ml-auto text-caption text-muted-foreground">{"✓"}</span>
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

      {/* Leads the "do something with this issue itself" group: the only
          discoverable way to open an issue elsewhere for users who don't know
          modifier-click, so it sits above the copy actions. */}
      <P.Item onClick={openInNewTab}>
        <ExternalLink className="h-3.5 w-3.5" />
        {t(($) => $.actions.open_in_new_tab)}
      </P.Item>
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
      <P.Item onClick={handleCopyWorkdirPath}>
        <FolderOpen className="h-3.5 w-3.5" />
        {workdirCopyTarget?.source === "durable_project_directory"
          ? t(($) => $.actions.copy_project_directory_path)
          : t(($) => $.actions.copy_workdir_path)}
      </P.Item>

      <P.Separator />

      {/* Relationship actions live under "Relations" — a semantically explicit
          label (unlike the old "More") so the first level tells you what the
          submenu does. Holds parent/sub-issue links today, and will grow
          (blocks, duplicates, related) as we add more relation types. */}
      <P.Sub>
        <P.SubTrigger>
          <Network className="h-3.5 w-3.5" />
          {t(($) => $.actions.relations)}
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
          {issue.parent_issue_id && (
            <P.Item onClick={removeParent}>
              <Unlink className="h-3.5 w-3.5" />
              {t(($) => $.actions.remove_parent_issue)}
            </P.Item>
          )}
          <P.Item onClick={openAddChild}>
            <ArrowDown className="h-3.5 w-3.5" />
            {t(($) => $.actions.add_sub_issue)}
          </P.Item>
        </P.SubContent>
      </P.Sub>

      {/* Manual plugin hooks. Rendered by the host rather than by the plugin
          because the trigger decides identity: a `manual` call acts as the
          person who picked it, so the entry has to live where the host can
          prove somebody did. Renders nothing when no plugin declares one. */}
      <PluginHookMenuItems issueId={issue.id} Item={P.Item} Separator={P.Separator} />
      {/* Modal surfaces open from here for the same reason: a modal is a third
          party's UI taking over the screen, so it opens because a person chose
          it, never on the plugin's own initiative. */}
      <PluginModalMenuItems issueId={issue.id} Item={P.Item} />

      <P.Separator />

      <P.Item
        variant="destructive"
        onClick={() => openDeleteConfirm({ onDeletedFallbackPath })}
      >
        <Trash2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.delete_issue)}
      </P.Item>
    </>
  );
}
