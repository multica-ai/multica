"use client";

import { forwardRef, memo, useCallback, useLayoutEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, ChevronUp } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueStatusCategory } from "@multica/core/issues";
import { issueDetailOptions, sidebarIssuesOptions } from "@multica/core/issues/queries";
import { SIDEBAR_FIRST_PAGE, SIDEBAR_PAGE_STEP, type SidebarIssuesRequest } from "@multica/core/issues/sidebar-list";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import { useIssueUnread } from "@multica/core/issues/unread";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AgentTask, Issue, IssueUnread, ListIssuesParams } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { StatusIcon } from "../../issues/components/status-icon";
import { StatusPicker } from "../../issues/components/pickers/status-picker";
import { IssueActionsContextMenu } from "../../issues/actions";
import { useIssueActions } from "../../issues/actions/use-issue-actions";
import { selectIssueTasks } from "../../issues/surface/activity";
import { useStatusLabel } from "../../issues/utils/status-label";
import { DeferredPopup } from "../../common/deferred-popup";
import { useT, useTimeAgo } from "../../i18n";
import { PANEL_ROW_CLASS, panelRowStateClass } from "../panel-nav";

/** A working agent's issue title: dimmer at rest and a wider highlight than
 *  the default shimmer, so the sweep reads at sidebar size. */
export const SIDEBAR_SHIMMER_CLASS =
  "[--shimmer-text-base:var(--faint-foreground)] [--shimmer-text-spread:22%]";

export const TREE_ROW_BUTTON_CLASS =
  "relative z-10 flex size-5 shrink-0 items-center justify-center rounded-md outline-none transition-[color,opacity] hover:text-foreground focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring";

interface TreeRowProps extends React.ComponentProps<"div"> {
  href: string;
  label: string;
  icon: React.ReactNode;
  active: boolean;
  /** Omit the expand props for a row that only opens its page. */
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  listId?: string;
  /** Label for the expand toggle; `aria-expanded` carries its state. */
  toggleLabel?: string;
  onPrefetch?: () => void;
  /** End of the row: a count, or a settings button. */
  trailing?: React.ReactNode;
  onNavigate?: (event: React.MouseEvent<HTMLAnchorElement>) => void;
  onNavigateInPlace?: () => void;
  rowRef?: React.Ref<HTMLDivElement>;
}

/**
 * A nav row that expands in place. The link stretches over the whole row so
 * the row still opens its page; the chevron, shown in the icon's place on
 * hover, only toggles.
 */
export const TreeRow = forwardRef<HTMLAnchorElement, TreeRowProps>(function TreeRow(
  {
    href,
    label,
    icon,
    active,
    expanded,
    onExpandedChange,
    listId,
    toggleLabel,
    onPrefetch,
    trailing,
    onNavigate,
    onNavigateInPlace,
    rowRef,
    className,
    ...rowProps
  },
  linkRef,
) {
  const onKeyDown = (event: React.KeyboardEvent) => {
    if (!onExpandedChange) return;
    if (event.key === "ArrowRight" && !expanded) {
      event.preventDefault();
      onExpandedChange(true);
    } else if (event.key === "ArrowLeft" && expanded) {
      event.preventDefault();
      onExpandedChange(false);
    }
  };

  return (
    <div
      {...rowProps}
      ref={rowRef}
      className={cn(
        PANEL_ROW_CLASS,
        panelRowStateClass(active),
        "group/row relative",
        expanded && !active && "text-foreground",
        className,
      )}
    >
      {onExpandedChange ? (
        // The toggle takes the icon's place on hover, so the label keeps the
        // row's width.
        <span className="relative flex size-4 shrink-0 items-center justify-center">
          <span className="flex group-hover/row:opacity-0 group-has-[:focus-visible]/row:opacity-0 [@media(pointer:coarse)]:opacity-0">
            {icon}
          </span>
          <button
            type="button"
            aria-label={toggleLabel}
            aria-expanded={expanded}
            aria-controls={expanded ? listId : undefined}
            onClick={() => onExpandedChange(!expanded)}
            onKeyDown={onKeyDown}
            onPointerDown={(event) => event.stopPropagation()}
            onPointerEnter={onPrefetch}
            className={cn(
              TREE_ROW_BUTTON_CLASS,
              "absolute text-muted-foreground opacity-0 transition-none group-hover/row:opacity-100 group-has-[:focus-visible]/row:opacity-100 [@media(pointer:coarse)]:opacity-100",
            )}
          >
            <ChevronRight
              className={cn(
                "size-3.5 transition-transform duration-150 ease-out motion-reduce:transition-none",
                expanded && "rotate-90",
              )}
            />
          </button>
        </span>
      ) : (
        icon
      )}
      <AppLink
        ref={linkRef}
        href={href}
        newTabTitle={label}
        draggable={false}
        aria-current={active ? "page" : undefined}
        onKeyDown={onKeyDown}
        onClick={onNavigate}
        onNavigateInPlace={onNavigateInPlace}
        className="min-w-0 flex-1 truncate outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:outline-2 focus-visible:after:outline-ring"
      >
        {label}
      </AppLink>
      {trailing}
    </div>
  );
});

/** Issues under an expanded row sit flush and a step smaller than the rows
 *  around them, so a group reads as one block. */
const TREE_CHILD_ROW_CLASS =
  "flex min-h-[26px] w-full items-center gap-2 rounded-lg px-2 py-[3px] text-left text-label transition-colors outline-none focus-visible:outline-2 focus-visible:outline-ring";

/** An issue row whose context menu is open stays lit as if hovered. The
 *  selected row keeps its own fill. */
export function issueRowMenuOpenClass(active: boolean) {
  return active ? undefined : "data-popup-open:bg-surface-hover data-popup-open:text-foreground";
}

const EMPTY_PARAMS: ListIssuesParams = {};
const EMPTY_ISSUES: Issue[] = [];

/**
 * `items` with the open item put back at its last place in `list` once it
 * drops out, until another item opens: an edit on its page (marking it done,
 * reassigning it, moving it) must not take away its row or move the
 * highlight elsewhere. `latest` is the open item's current data; without it
 * (deleted, or not loaded) nothing is kept.
 */
export function useKeptActiveItem<T extends { id: string }>(
  list: string,
  items: readonly T[],
  activeId: string | null,
  latest: T | undefined,
): readonly T[] {
  const index = activeId ? items.findIndex((item) => item.id === activeId) : -1;
  const held = useSidebarKeptStore((s) => s.held[list]);
  useLayoutEffect(() => {
    if (activeId && index !== -1) useSidebarKeptStore.getState().hold(list, activeId, index);
  }, [list, activeId, index]);
  if (index !== -1 || !activeId || !held || held.id !== activeId || !latest || latest.id !== activeId) return items;
  const at = Math.min(held.index, items.length);
  return [...items.slice(0, at), latest, ...items.slice(at)];
}

/**
 * The issues under an expanded row: the first page, then "Load more" in
 * steps. `params: null` means the row's settings can match nothing;
 * `undefined` that they are still resolving.
 */
export function SidebarIssueList({
  id,
  wsId,
  source,
  params,
  activeIssueId,
  onCollapse,
}: {
  id: string;
  wsId: string;
  source: string;
  params: SidebarIssuesRequest | null | undefined;
  activeIssueId: string | null;
  onCollapse: () => void;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const statusCatalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const [limit, setLimit] = useState(SIDEBAR_FIRST_PAGE);
  const query = useQuery({
    ...sidebarIssuesOptions(wsId, source, params ?? EMPTY_PARAMS, limit),
    enabled: !!params,
    placeholderData: keepPreviousData,
  });
  const listed = params ? (query.data?.issues ?? EMPTY_ISSUES) : EMPTY_ISSUES;
  const total = params ? (query.data?.total ?? 0) : 0;
  // The list no longer carries a kept issue; the detail cache follows its
  // edits, and dropping it on delete ends the keep.
  const { data: activeIssue } = useQuery({
    ...issueDetailOptions(wsId, activeIssueId ?? ""),
    enabled: false,
  });
  const issues = useKeptActiveItem(source, listed, activeIssueId, activeIssue);

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key !== "ArrowLeft") return;
    event.preventDefault();
    onCollapse();
  };

  const tailClass = cn(TREE_CHILD_ROW_CLASS, panelRowStateClass(false), "gap-1.5 min-h-6 text-caption text-faint-foreground");

  return (
    <ul id={id} className="mb-2 ml-5 border-l border-surface-border pl-1.5">
      {params === undefined || (params && query.isPending) ? (
        <li className="flex h-[26px] items-center gap-2 px-2">
          <div className="size-3 shrink-0 rounded-full bg-muted" />
          <div className="h-3 w-24 rounded-xs bg-muted" />
        </li>
      ) : query.isError || issues.length === 0 ? (
        <li className="flex h-[26px] items-center px-2 text-caption text-faint-foreground">
          {query.isError
            ? t(($) => $.panels.work.project_issues_error)
            : t(($) => $.panels.work.project_issues_empty)}
        </li>
      ) : (
        <>
          {issues.map((issue) => (
            <li key={issue.id}>
              <SidebarIssueRow
                wsId={wsId}
                issue={issue}
                href={p.issueDetail(issue.id)}
                active={issue.id === activeIssueId}
                statusColor={statusCatalog.colorOf(issue.status)}
                statusIcon={statusCatalog.iconOf(issue.status)}
                statusLabel={statusLabel(issue.status)}
                onKeyDown={onKeyDown}
              />
            </li>
          ))}
          {total > listed.length ? (
            <li>
              <button
                type="button"
                className={tailClass}
                disabled={query.isFetching}
                onKeyDown={onKeyDown}
                onClick={() => setLimit(listed.length + SIDEBAR_PAGE_STEP)}
              >
                <ChevronDown className="size-3.5" />
                {t(($) => $.panels.work.load_more)}{" "}
                <span className="tabular-nums">{total - listed.length}</span>
              </button>
            </li>
          ) : (
            limit > SIDEBAR_FIRST_PAGE &&
            listed.length > SIDEBAR_FIRST_PAGE && (
              <li>
                <button type="button" className={tailClass} onKeyDown={onKeyDown} onClick={() => setLimit(SIDEBAR_FIRST_PAGE)}>
                  <ChevronUp className="size-3.5" />
                  {t(($) => $.panels.work.show_less)}
                </button>
              </li>
            )
          )}
        </>
      )}
    </ul>
  );
}

type IssueAgentState = { running: boolean; agentIds: string[] } | null;

/** The agents running on an issue, or queued for it when none is running yet. */
export function useIssueAgentState(wsId: string, issueId: string): IssueAgentState {
  const select = useCallback(
    (snapshot: AgentTask[]): IssueAgentState => {
      const { running, queued } = selectIssueTasks(snapshot, issueId);
      const tasks = running.length > 0 ? running : queued;
      if (tasks.length === 0) return null;
      return { running: running.length > 0, agentIds: [...new Set(tasks.map((task) => task.agent_id))] };
    },
    [issueId],
  );
  const { data = null } = useQuery({ ...agentTaskSnapshotOptions(wsId), select });
  return data;
}

export function IssueAgentBadge({ agents }: { agents: NonNullable<IssueAgentState> }) {
  const { t } = useT("issues");
  return (
    <>
      <AgentAvatarStack agentIds={agents.agentIds} size="xs" max={2} opacity={agents.running ? "full" : "half"} />
      <span className="sr-only">
        {agents.running
          ? t(($) => $.agent_activity.status_running)
          : t(($) => $.agent_activity.status_queued)}
      </span>
    </>
  );
}

/** The link of an issue row whose status icon sits beside it: it stretches
 *  over the whole row, under the icon's button. */
export const ISSUE_ROW_LINK_CLASS =
  "flex min-w-0 flex-1 items-center gap-2 outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:outline-2 focus-visible:after:outline-ring";

/**
 * An issue row's status icon, which opens the status picker. The picker
 * mounts on the first click, so a long tree pays for none of them.
 */
export function SidebarIssueStatus({
  issue,
  color,
  icon,
  label,
  className,
}: {
  issue: Issue;
  color: string | null;
  icon: string | null;
  label: string;
  className: string;
}) {
  const statusIcon = (
    <StatusIcon
      status={issue.status}
      color={color}
      icon={icon}
      category={issueStatusCategory(issue) ?? undefined}
      className={cn(className, "shrink-0")}
    />
  );
  const button = (
    <button
      type="button"
      aria-label={label}
      // A pinned row is draggable; pressing its icon must not pick it up.
      onPointerDown={(event) => event.stopPropagation()}
      className="relative z-10 -m-1 flex shrink-0 items-center justify-center rounded-md p-1 outline-none transition-colors hover:bg-surface-selected focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-surface-selected"
    />
  );
  return (
    <DeferredPopup trigger={statusIcon} triggerRender={button} tooltip={label}>
      {(open, onOpenChange) => (
        <SidebarStatusPicker
          issue={issue}
          open={open}
          onOpenChange={onOpenChange}
          trigger={statusIcon}
          triggerRender={button}
          tooltip={label}
        />
      )}
    </DeferredPopup>
  );
}

function SidebarStatusPicker({
  issue,
  ...picker
}: {
  issue: Issue;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: React.ReactNode;
  triggerRender: React.ReactElement;
  tooltip: string;
}) {
  const { updateField } = useIssueActions(issue);
  return (
    <StatusPicker
      {...picker}
      status={issue.status}
      onUpdate={updateField}
      projectId={issue.project_id}
      issue={issue}
      align="start"
    />
  );
}

/** An agent result the viewer has not seen yet. It takes the avatar's place
 *  once the run ends, so the two never show together. */
export function IssueUnreadDot({ unread }: { unread: IssueUnread }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const label =
    unread.outcome === "manual" || !unread.agent_id
      ? t(($) => $.agent_activity.unread_manual)
      : unread.outcome === "failed"
        ? t(($) => $.agent_activity.unread_failed, { agent: getActorName("agent", unread.agent_id), time: timeAgo(unread.created_at) })
        : t(($) => $.agent_activity.unread_completed, { agent: getActorName("agent", unread.agent_id), time: timeAgo(unread.created_at) });
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span role="img" aria-label={label} className="flex size-4 shrink-0 items-center justify-center">
            <span className="size-2 rounded-full bg-brand" />
          </span>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** The title shimmers while an agent runs on the issue; queued agents show
 *  dimmed. Once the run ends, an unread result shows a dot and a brighter title. */
const SidebarIssueRow = memo(function SidebarIssueRow({
  wsId,
  issue,
  href,
  active,
  statusColor,
  statusIcon,
  statusLabel,
  onKeyDown,
}: {
  wsId: string;
  issue: Issue;
  href: string;
  active: boolean;
  statusColor: string | null;
  statusIcon: string | null;
  statusLabel: string;
  onKeyDown: (event: React.KeyboardEvent) => void;
}) {
  const agents = useIssueAgentState(wsId, issue.id);
  const unread = useIssueUnread(wsId, issue.id);

  return (
    <IssueActionsContextMenu issue={issue}>
      <div className={cn(TREE_CHILD_ROW_CLASS, panelRowStateClass(active), issueRowMenuOpenClass(active), "relative")}>
        <SidebarIssueStatus
          issue={issue}
          color={statusColor}
          icon={statusIcon}
          label={statusLabel}
          className="size-3"
        />
        <AppLink
          href={href}
          newTabTitle={issue.title}
          aria-current={active ? "page" : undefined}
          onKeyDown={onKeyDown}
          className={ISSUE_ROW_LINK_CLASS}
        >
          <ShimmerText
            active={!!agents?.running}
            className={cn("min-w-0 flex-1 truncate", SIDEBAR_SHIMMER_CLASS, !agents && unread && "text-foreground")}
          >
            {issue.title}
          </ShimmerText>
          {agents ? <IssueAgentBadge agents={agents} /> : unread && <IssueUnreadDot unread={unread} />}
        </AppLink>
      </div>
    </IssueActionsContextMenu>
  );
});
