"use client";

import { forwardRef, memo, useCallback, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, ChevronUp } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueStatusCategory } from "@multica/core/issues";
import { sidebarIssuesOptions } from "@multica/core/issues/queries";
import { SIDEBAR_FIRST_PAGE, SIDEBAR_PAGE_STEP, type SidebarIssuesRequest } from "@multica/core/issues/sidebar-list";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AgentTask, Issue, ListIssuesParams } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { StatusIcon } from "../../issues/components/status-icon";
import { IssueActionsContextMenu } from "../../issues/actions";
import { selectIssueTasks } from "../../issues/surface/activity";
import { useT } from "../../i18n";
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
  const [limit, setLimit] = useState(SIDEBAR_FIRST_PAGE);
  const query = useQuery({
    ...sidebarIssuesOptions(wsId, source, params ?? EMPTY_PARAMS, limit),
    enabled: !!params,
    placeholderData: keepPreviousData,
  });
  const issues = params ? (query.data?.issues ?? []) : [];
  const total = params ? (query.data?.total ?? 0) : 0;

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
                onKeyDown={onKeyDown}
              />
            </li>
          ))}
          {total > issues.length ? (
            <li>
              <button
                type="button"
                className={tailClass}
                disabled={query.isFetching}
                onKeyDown={onKeyDown}
                onClick={() => setLimit(issues.length + SIDEBAR_PAGE_STEP)}
              >
                <ChevronDown className="size-3.5" />
                {t(($) => $.panels.work.load_more)}{" "}
                <span className="tabular-nums">{total - issues.length}</span>
              </button>
            </li>
          ) : (
            limit > SIDEBAR_FIRST_PAGE &&
            issues.length > SIDEBAR_FIRST_PAGE && (
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

/** The title shimmers while an agent runs on the issue; queued agents show dimmed. */
const SidebarIssueRow = memo(function SidebarIssueRow({
  wsId,
  issue,
  href,
  active,
  statusColor,
  statusIcon,
  onKeyDown,
}: {
  wsId: string;
  issue: Issue;
  href: string;
  active: boolean;
  statusColor: string | null;
  statusIcon: string | null;
  onKeyDown: (event: React.KeyboardEvent) => void;
}) {
  const agents = useIssueAgentState(wsId, issue.id);

  return (
    <IssueActionsContextMenu issue={issue}>
      <AppLink
        href={href}
        newTabTitle={issue.title}
        aria-current={active ? "page" : undefined}
        onKeyDown={onKeyDown}
        className={cn(TREE_CHILD_ROW_CLASS, panelRowStateClass(active), issueRowMenuOpenClass(active))}
      >
        <StatusIcon
          status={issue.status}
          color={statusColor}
          icon={statusIcon}
          category={issueStatusCategory(issue) ?? undefined}
          className="size-3 shrink-0"
        />
        <ShimmerText active={!!agents?.running} className={cn("min-w-0 flex-1 truncate", SIDEBAR_SHIMMER_CLASS)}>
          {issue.title}
        </ShimmerText>
        {agents && <IssueAgentBadge agents={agents} />}
      </AppLink>
    </IssueActionsContextMenu>
  );
});
