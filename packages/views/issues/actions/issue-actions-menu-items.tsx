"use client";

import { useCallback, useMemo, useRef, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowDown,
  ArrowUp,
  Calendar,
  CalendarClock,
  Check,
  CircleDot,
  CircleEqual,
  ExternalLink,
  FolderOpen,
  Hash,
  Link2,
  Network,
  Pin,
  PinOff,
  Pencil,
  Plus,
  Square,
  Trash2,
  Unlink,
  UserMinus,
} from "@multica/ui/icons";
import type { AgentTask, Issue } from "@multica/core/types";
import { resolveWorkdirCopyTarget } from "@multica/core/issues";
import { todayDateOnly, addDaysDateOnly } from "@multica/core/issues/date";
import { api } from "@multica/core/api";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import {
  PRIORITY_DISPLAY_ORDER,
  PRIORITY_CONFIG,
} from "@multica/core/issues/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueUnread, useMarkIssueRead, useMarkIssueUnread } from "@multica/core/issues/unread";
import { useActorName } from "@multica/core/workspace/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { stepHandsOff, workflowAllowsStatus, workflowStep } from "@multica/core/issue-workflows";
import { useModalStore } from "@multica/core/modals";
import { useIssueStatusOptions } from "../utils/status-options";
import { useStatusLabel } from "../utils/status-label";
import { issueKeys } from "@multica/core/issues/queries";
import { StatusIcon } from "../components/status-icon";
import { PriorityIcon } from "../components/priority-icon";
import { selectIssueTasks } from "../surface/activity";
import { AssigneePickerPanel } from "../components/pickers";
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
  /** Closes the surrounding menu. Menu items close it themselves; the
   *  assignee list is not made of menu items, so it calls this after a pick. */
  onClose: () => void;
  /** If set, leave the page after the issue is deleted (used by the detail
   *  page, which renders the issue being deleted). The delete modal goes back
   *  to the list the user came from and only falls back to this path when
   *  there is no in-app history. List surfaces leave it unset and stay put. */
  onDeletedFallbackPath?: string;
  /** `sidebar` is the trimmed menu for sidebar rows, where only the title is
   *  visible: it names the issue in a header, shows current values beside
   *  Status / Priority / Assignee, and keeps only triage and navigation
   *  entries — in the full menu's order, so the shared entries sit where
   *  users already expect them. */
  variant?: "full" | "sidebar";
  /** Sidebar variant: asks the parent to confirm stopping a running task. */
  onStopRun?: (task: AgentTask) => void;
}

export function IssueActionsMenuItems({
  issue,
  actions,
  primitives: P,
  onClose,
  onDeletedFallbackPath,
  variant = "full",
  onStopRun,
}: IssueActionsMenuItemsProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const sidebar = variant === "sidebar";
  const statusLabel = useStatusLabel(wsId);
  // Only the statuses the issue's workflow allows; entering a step that hands
  // the issue off confirms first, as in the status picker. (MUL-7420)
  const { options: statusOptions, workflow } = useIssueStatusOptions(wsId, issue.project_id, issue.status);
  const openModal = useModalStore((s) => s.open);
  const setStatus = (status: string) => {
    if (workflow && status !== issue.status && stepHandsOff(workflowStep(workflow, status))) {
      openModal("issue-workflow-handoff", { issueId: issue.id, identifier: issue.identifier, status });
      return;
    }
    updateField({ status });
  };
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const unread = useIssueUnread(wsId, sidebar ? issue.id : "");
  const { mutate: markRead } = useMarkIssueRead(wsId);
  const { mutate: markUnread } = useMarkIssueUnread(wsId);
  const {
    isPinned,
    updateField,
    openInNewTab,
    togglePin,
    copyLink,
    openCreateSubIssue,
    openRename,
    openSetParent,
    removeParent,
    openAddChild,
    openMarkDuplicate,
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
    enabled: !sidebar,
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

  const copyIdentifier = () => {
    void copyText(issue.identifier).then((ok) => {
      if (ok) {
        toast.success(t(($) => $.actions.identifier_copied, { identifier: issue.identifier }));
      }
    });
  };

  return (
    <>
      {sidebar ? (
        <>
          <div className="px-2 pt-1 pb-1.5">
            <div className="text-caption text-muted-foreground">{issue.identifier}</div>
            <div className="line-clamp-2 text-body font-medium break-words text-pretty">
              {issue.title}
            </div>
          </div>
          <P.Separator />
        </>
      ) : (
        <>
          {/* Rename leads the menu: it is the only entry that changes what the
              issue *is* rather than a property of it, and on list/board rows
              it is the only way to reach the title at all. */}
          <P.Item onClick={openRename}>
            <Pencil className="h-3.5 w-3.5" />
            {t(($) => $.actions.rename_issue)}
          </P.Item>
          {/* Top level rather than under Relations: on a long issue the
              sub-issues section's own "+" is scrolled far out of reach, and
              this menu is pinned in the header. */}
          <P.Item onClick={openCreateSubIssue}>
            <Plus className="h-3.5 w-3.5" />
            {t(($) => $.actions.create_sub_issue)}
          </P.Item>

          <P.Separator />
        </>
      )}

      {/* Status */}
      <P.Sub>
        <P.SubTrigger>
          <StatusIcon
            status={issue.status}
            category={categoryOf(issue.status)}
            color={colorOf(issue.status)}
            icon={iconOf(issue.status)}
            className="h-3.5 w-3.5"
          />
          <ItemLabel
            label={t(($) => $.actions.status)}
            value={sidebar ? statusLabel(issue.status) : undefined}
          />
        </P.SubTrigger>
        <P.SubContent>
          {/* The same set as the status picker: every entry point that can
              change a status must offer it, or a custom status is unreachable
              from the board's right-click menu. (MUL-6243) */}
          {statusOptions.map((option) => (
            <P.Item
              key={option.key}
              onClick={() => setStatus(option.key)}
            >
              <StatusIcon
                status={option.key}
                category={option.category}
                color={option.color}
                icon={option.icon}
                className="h-3.5 w-3.5"
              />
              {option.label}
              {issue.status === option.key && (
                <Check className="ml-auto size-3.5 text-muted-foreground" />
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Priority */}
      <P.Sub>
        <P.SubTrigger>
          <PriorityIcon priority={issue.priority} />
          <ItemLabel
            label={t(($) => $.actions.priority)}
            value={sidebar ? t(($) => $.priority[issue.priority]) : undefined}
          />
        </P.SubTrigger>
        <P.SubContent>
          {PRIORITY_DISPLAY_ORDER.map((p) => (
            <P.Item key={p} onClick={() => updateField({ priority: p })}>
              <span
                className={`inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-caption font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
              >
                <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                {t(($) => $.priority[p])}
              </span>
              {issue.priority === p && (
                <Check className="ml-auto size-3.5 text-muted-foreground" />
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Assignee — a submenu like Status and Priority, filled with the
          shared assignee picker's own list (members + agents + squads, with
          search and permission checks) rather than menu items of its own, so
          every entry point offers the same choices. (MUL-2157) */}
      <P.Sub>
        <P.SubTrigger>
          <UserMinus className="h-3.5 w-3.5" />
          <ItemLabel
            label={t(($) => $.actions.assignee)}
            value={sidebar ? <AssigneeName issue={issue} /> : undefined}
          />
        </P.SubTrigger>
        <P.SubContent className="w-64 p-0">
          <SubmenuPanel>
            <AssigneePickerPanel
              assigneeType={issue.assignee_type}
              assigneeId={issue.assignee_id}
              onUpdate={updateField}
              onPicked={onClose}
            />
          </SubmenuPanel>
        </P.SubContent>
      </P.Sub>

      {!sidebar && (
        <>
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
        </>
      )}

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
      {sidebar &&
        (unread ? (
          <P.Item onClick={() => markRead(issue.id)}>
            <Check className="h-3.5 w-3.5" />
            {t(($) => $.actions.mark_read)}
          </P.Item>
        ) : (
          <P.Item onClick={() => markUnread(issue.id)}>
            <CircleDot className="h-3.5 w-3.5" />
            {t(($) => $.actions.mark_unread)}
          </P.Item>
        ))}
      <P.Item onClick={copyLink}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_link)}
      </P.Item>
      {sidebar ? (
        <P.Item onClick={copyIdentifier}>
          <Hash className="h-3.5 w-3.5" />
          {t(($) => $.actions.copy_identifier)}
        </P.Item>
      ) : (
        <P.Item onClick={handleCopyWorkdirPath}>
          <FolderOpen className="h-3.5 w-3.5" />
          {workdirCopyTarget?.source === "durable_project_directory"
            ? t(($) => $.actions.copy_project_directory_path)
            : t(($) => $.actions.copy_workdir_path)}
        </P.Item>
      )}

      {sidebar ? (
        onStopRun && <StopRunItems issueId={issue.id} primitives={P} onStopRun={onStopRun} />
      ) : (
        <>
          <P.Separator />

          {/* Relationship actions live under "Relations" — a semantically
              explicit label (unlike the old "More") so the first level tells
              you what the submenu does. Holds parent/sub-issue links today,
              and will grow (blocks, duplicates, related) as we add more
              relation types. */}
          <P.Sub>
            <P.SubTrigger>
              <Network className="h-3.5 w-3.5" />
              {t(($) => $.actions.relations)}
            </P.SubTrigger>
            <P.SubContent>
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
              {/* Marking a duplicate cancels the issue. (MUL-7420) */}
              {workflowAllowsStatus(workflow, "cancelled") && (
                <P.Item onClick={openMarkDuplicate}>
                  <CircleEqual className="h-3.5 w-3.5" />
                  {t(($) => $.actions.mark_duplicate)}
                </P.Item>
              )}
            </P.SubContent>
          </P.Sub>

          {/* Manual plugin hooks. Rendered by the host rather than by the
              plugin because the trigger decides identity: a `manual` call acts
              as the person who picked it, so the entry has to live where the
              host can prove somebody did. Renders nothing when no plugin
              declares one. */}
          <PluginHookMenuItems issueId={issue.id} Item={P.Item} Separator={P.Separator} />
          {/* Modal surfaces open from here for the same reason: a modal is a
              third party's UI taking over the screen, so it opens because a
              person chose it, never on the plugin's own initiative. */}
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
      )}
    </>
  );
}

/** An entry's label, with the current value trailing it when given. The label
 *  takes the free space so a submenu chevron stays flush right. */
function ItemLabel({ label, value }: { label: string; value?: ReactNode }) {
  if (value === undefined) return <>{label}</>;
  return (
    <>
      <span className="flex-1">{label}</span>
      <span className="max-w-28 truncate text-muted-foreground">{value}</span>
    </>
  );
}

/** Hosts a search box and list inside a submenu. The menu's typeahead and
 *  arrow-key navigation would otherwise take the keys typed into the search
 *  box; Escape and Tab still reach the menu so it can close. */
function SubmenuPanel({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <div
      ref={ref}
      onKeyDown={(e) => {
        // Hold the height from the first keystroke: a shorter, filtered list
        // would re-position a submenu shifted to fit the viewport, moving the
        // search box while the user types in it.
        const el = ref.current;
        if (el && !el.style.minHeight) el.style.minHeight = `${el.offsetHeight}px`;
        if (e.key !== "Escape" && e.key !== "Tab") e.stopPropagation();
      }}
    >
      {children}
    </div>
  );
}

function AssigneeName({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  if (!issue.assignee_type || !issue.assignee_id) return <>{t(($) => $.actions.unassigned)}</>;
  return <>{getActorName(issue.assignee_type, issue.assignee_id)}</>;
}

const NO_TASKS: AgentTask[] = [];

/** One entry per running task — several agents can work an issue at once. */
function StopRunItems({
  issueId,
  primitives: P,
  onStopRun,
}: {
  issueId: string;
  primitives: MenuPrimitives;
  onStopRun: (task: AgentTask) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const selectRunning = useCallback(
    (snapshot: AgentTask[]) => selectIssueTasks(snapshot, issueId).running,
    [issueId],
  );
  const { data: running = NO_TASKS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    select: selectRunning,
  });
  if (running.length === 0) return null;
  return (
    <>
      <P.Separator />
      {running.map((task) => (
        <P.Item key={task.id} variant="destructive" onClick={() => onStopRun(task)}>
          <Square className="h-3.5 w-3.5" />
          {t(($) => $.actions.stop_agent_run, { name: getActorName("agent", task.agent_id) })}
        </P.Item>
      ))}
    </>
  );
}
