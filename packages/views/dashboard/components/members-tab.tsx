"use client";

import { useId, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronRight,
  CircleDashed,
  EyeOff,
  MessageSquare,
  UserMinus,
} from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { useWorkspacePaths, issueRouteRef } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  dashboardMemberTasksOptions,
  dashboardUsageByMemberOptions,
  type DashboardHistoryLimitParam,
} from "@multica/core/dashboard";
import type { Agent, DashboardMemberTask } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { formatTokens } from "../../runtimes/utils";
import { useT, useTimeAgo } from "../../i18n";
import {
  aggregateMemberUsage,
  RESTRICTED_AGENTS_ROW_ID,
  summarizeMemberUsage,
  UNATTRIBUTED_MEMBER_ID,
  type MemberDashboardRow,
} from "../utils";

// The server's memberTaskLimit; only used in the hint copy.
const MEMBER_TASK_LIMIT = 20;

const MEMBER_GRID_STYLE = {
  minWidth: "fit-content",
  gridTemplateColumns: "minmax(10rem, 1.6fr) minmax(6rem, 1fr) 5rem 5rem 7rem",
} satisfies CSSProperties;

const TASK_GRID_STYLE = {
  minWidth: "fit-content",
  gridTemplateColumns: "minmax(12rem, 2fr) minmax(8rem, 1fr) 5rem 5rem 6rem",
} satisfies CSSProperties;

const GRID = "grid items-center gap-3";

interface MembersTabScope {
  wsId: string;
  days: number;
  projectId: string | null;
  viewTZ: string;
  historyLimit: DashboardHistoryLimitParam;
  squadId: string | null;
}

/**
 * The dashboard's "By member" tab: usage split by each run's accountable
 * member, so a shared agent's spend lands on the people it worked for. The
 * server scopes the data (a caller below admin only gets their own row, and
 * other people's private agents and chats never reach the client); nothing
 * here is a permission check. It reads the same range, project and squad
 * filters as the other tabs and is only fetched once the tab is open.
 */
export function MembersTab({
  agents,
  ...scope
}: MembersTabScope & { agents: Agent[] }) {
  const { t } = useT("usage");
  const { role } = useCurrentMember(scope.wsId);
  const { data: members = [] } = useQuery(memberListOptions(scope.wsId));
  const usageQuery = useQuery(
    dashboardUsageByMemberOptions(
      scope.wsId,
      scope.days,
      scope.projectId,
      scope.viewTZ,
      scope.historyLimit,
      scope.squadId,
    ),
  );
  const [expanded, setExpanded] = useState<string | null>(null);

  const rows = useMemo(
    () => (usageQuery.data ? aggregateMemberUsage(usageQuery.data) : []),
    [usageQuery.data],
  );
  const maxTokens = useMemo(() => rows.reduce((m, r) => Math.max(m, r.tokens), 0), [rows]);
  const namedCount = rows.filter((r) => r.userId !== UNATTRIBUTED_MEMBER_ID).length;
  const memberNames = useMemo(
    () => new Map(members.map((m) => [m.user_id, m.name])),
    [members],
  );
  const isPrivileged = role === "owner" || role === "admin";

  const nameOf = (userId: string): string => {
    if (userId === UNATTRIBUTED_MEMBER_ID) return t(($) => $.members.unattributed);
    return memberNames.get(userId) ?? t(($) => $.members.former_member);
  };

  if (usageQuery.isPending) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-10 rounded-lg" />
        <Skeleton className="h-48 rounded-lg" />
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b px-4 pt-4 pb-3">
        <div className="min-w-0 space-y-1">
          <h4 className="text-body font-semibold">{t(($) => $.members.title)}</h4>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.members.description)}
          </p>
          {role !== null && !isPrivileged ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.members.own_only)}
            </p>
          ) : null}
        </div>
        <span className="shrink-0 text-caption text-muted-foreground">
          {t(($) => $.members.caption, { count: namedCount })}
        </span>
      </div>
      {rows.length === 0 ? (
        <p className="px-4 py-8 text-center text-caption text-muted-foreground">
          {t(($) => $.members.no_data)}
        </p>
      ) : (
        <div
          role="region"
          aria-label={t(($) => $.members.title)}
          tabIndex={0}
          className="overflow-x-auto overscroll-x-contain"
        >
          <div
            className={`${GRID} border-b px-4 py-2 text-caption font-medium text-muted-foreground`}
            style={MEMBER_GRID_STYLE}
          >
            <span>{t(($) => $.members.header_member)}</span>
            <span />
            <span className="text-right">{t(($) => $.members.header_tokens)}</span>
            <span className="text-right">{t(($) => $.members.header_cost)}</span>
            <span className="text-right">{t(($) => $.members.header_tasks)}</span>
          </div>
          <ul aria-label={t(($) => $.members.title)} className="divide-y">
            {rows.map((row) => (
              <MemberRow
                key={row.userId}
                row={row}
                name={nameOf(row.userId)}
                isKnownMember={memberNames.has(row.userId)}
                maxTokens={maxTokens}
                expanded={expanded === row.userId}
                onToggle={() => setExpanded((cur) => (cur === row.userId ? null : row.userId))}
                scope={scope}
                agents={agents}
              />
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function MemberRow({
  row,
  name,
  isKnownMember,
  maxTokens,
  expanded,
  onToggle,
  scope,
  agents,
}: {
  row: MemberDashboardRow;
  name: string;
  isKnownMember: boolean;
  maxTokens: number;
  expanded: boolean;
  onToggle: () => void;
  scope: MembersTabScope;
  agents: Agent[];
}) {
  const { t } = useT("usage");
  const panelId = useId();
  const isUnattributed = row.userId === UNATTRIBUTED_MEMBER_ID;
  const pct = maxTokens > 0 ? Math.min(100, (row.tokens / maxTokens) * 100) : 0;
  const Chevron = expanded ? ChevronDown : ChevronRight;

  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-controls={expanded ? panelId : undefined}
        aria-label={
          expanded
            ? t(($) => $.members.hide_tasks, { name })
            : t(($) => $.members.show_tasks, { name })
        }
        className={`${GRID} w-full px-4 py-2 text-left hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none`}
        style={MEMBER_GRID_STYLE}
      >
        <span className="flex min-w-0 items-center gap-2">
          <Chevron aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
          {isKnownMember ? (
            <ActorAvatar actorType="member" actorId={row.userId} size="md" profileLink={false} />
          ) : (
            <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
              {isUnattributed ? (
                <CircleDashed aria-hidden="true" className="h-3 w-3" />
              ) : (
                <UserMinus aria-hidden="true" className="h-3 w-3" />
              )}
            </span>
          )}
          <TruncatedTooltip content={name}>
            <span
              className={`block min-w-0 truncate text-body font-medium ${isKnownMember ? "" : "italic text-muted-foreground"}`}
            >
              {name}
            </span>
          </TruncatedTooltip>
        </span>
        <span aria-hidden="true" className="relative h-2 overflow-hidden rounded-full bg-muted">
          <span
            className="block h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
            style={{ width: `${pct}%` }}
          />
        </span>
        <span className="text-right text-caption font-medium tabular-nums text-foreground">
          {formatTokens(row.tokens)}
        </span>
        <span className="text-right text-caption tabular-nums text-muted-foreground">
          ${row.cost.toFixed(2)}
        </span>
        <span className="text-right text-caption tabular-nums text-muted-foreground">
          {row.taskCount}
        </span>
      </button>
      {expanded ? (
        <div id={panelId} className="border-t bg-muted/30 px-4 py-3">
          <MemberTasks userId={row.userId} scope={scope} agents={agents} />
        </div>
      ) : null}
    </li>
  );
}

function MemberTasks({
  userId,
  scope,
  agents,
}: {
  userId: string;
  scope: MembersTabScope;
  agents: Agent[];
}) {
  const query = useQuery(
    dashboardMemberTasksOptions(
      scope.wsId,
      scope.days,
      scope.projectId,
      scope.viewTZ,
      scope.historyLimit,
      scope.squadId,
      userId,
    ),
  );
  return <RecentRuns query={query} agents={agents} />;
}

/** The expanded list of recent runs, shared with the "By project" tab. */
export function RecentRuns({
  query,
  agents,
}: {
  query: { isPending: boolean; isError: boolean; data?: DashboardMemberTask[] };
  agents: Agent[];
}) {
  const { t } = useT("usage");
  if (query.isPending) return <Skeleton className="h-24 rounded-md" />;
  if (query.isError || !query.data) {
    return (
      <p role="alert" className="text-caption text-muted-foreground">
        {t(($) => $.members.recent_error)}
      </p>
    );
  }
  const tasks = query.data;

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h5 className="text-caption font-semibold">{t(($) => $.members.recent_title)}</h5>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.members.recent_hint, { limit: MEMBER_TASK_LIMIT })}
        </span>
      </div>
      {tasks.length === 0 ? (
        <p className="py-2 text-caption text-muted-foreground">
          {t(($) => $.members.recent_empty)}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <div
            className={`${GRID} py-1 text-caption font-medium text-muted-foreground`}
            style={TASK_GRID_STYLE}
          >
            <span>{t(($) => $.members.header_task)}</span>
            <span>{t(($) => $.members.header_agent)}</span>
            <span className="text-right">{t(($) => $.members.header_tokens)}</span>
            <span className="text-right">{t(($) => $.members.header_cost)}</span>
            <span className="text-right">{t(($) => $.members.header_time)}</span>
          </div>
          <ul aria-label={t(($) => $.members.recent_title)} className="divide-y">
            {tasks.map((task) => (
              <MemberTaskRow key={task.task_id} task={task} agents={agents} />
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function MemberTaskRow({ task, agents }: { task: DashboardMemberTask; agents: Agent[] }) {
  const { t } = useT("usage");
  const timeAgo = useTimeAgo();
  const wsPaths = useWorkspacePaths();
  const { tokens, cost } = summarizeMemberUsage(task.usage);

  let label: ReactNode;
  if (task.kind === "issue" && task.issue_id) {
    const title = task.issue_title ?? "";
    const text = task.issue_identifier ? `${task.issue_identifier} ${title}` : title;
    label = (
      <TruncatedTooltip content={text}>
        <AppLink
          href={wsPaths.issueDetail(
            issueRouteRef({ id: task.issue_id, identifier: task.issue_identifier }),
          )}
          className="block min-w-0 truncate text-body hover:underline"
        >
          {task.issue_identifier ? (
            <span className="mr-1.5 text-muted-foreground tabular-nums">
              {task.issue_identifier}
            </span>
          ) : null}
          {title}
        </AppLink>
      </TruncatedTooltip>
    );
  } else if (task.kind === "chat") {
    // The title and link only arrive for the chat's own creator.
    const inner = (
      <span className="flex min-w-0 items-center gap-1.5">
        <MessageSquare aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{task.chat_title || t(($) => $.members.chat_run)}</span>
      </span>
    );
    label = task.chat_session_id ? (
      <AppLink
        href={wsPaths.chatSession(task.chat_session_id)}
        className="block min-w-0 text-body hover:underline"
      >
        {inner}
      </AppLink>
    ) : (
      <span className="block min-w-0 text-body text-muted-foreground">{inner}</span>
    );
  } else {
    label = (
      <span className="block truncate text-body text-muted-foreground">
        {t(($) => $.members.generic_run)}
      </span>
    );
  }

  const agentName =
    task.agent_id === RESTRICTED_AGENTS_ROW_ID
      ? undefined
      : agents.find((a) => a.id === task.agent_id)?.name;

  return (
    <li className={`${GRID} py-1.5`} style={TASK_GRID_STYLE}>
      <span className="min-w-0">{label}</span>
      <span className="flex min-w-0 items-center gap-1.5">
        {agentName ? (
          <>
            <ActorAvatar actorType="agent" actorId={task.agent_id} size="sm" />
            <span className="truncate text-caption">{agentName}</span>
          </>
        ) : (
          <>
            <span className="flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <EyeOff aria-hidden="true" className="h-2.5 w-2.5" />
            </span>
            <span className="truncate text-caption italic text-muted-foreground">
              {t(($) => $.members.other_agents)}
            </span>
          </>
        )}
      </span>
      <span className="text-right text-caption tabular-nums">{formatTokens(tokens)}</span>
      <span className="text-right text-caption tabular-nums text-muted-foreground">
        ${cost.toFixed(2)}
      </span>
      <span className="text-right text-caption text-muted-foreground">
        {task.last_used_at ? timeAgo(task.last_used_at) : "—"}
      </span>
    </li>
  );
}
