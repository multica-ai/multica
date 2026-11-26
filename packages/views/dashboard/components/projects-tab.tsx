"use client";

import { useId, useMemo, useState, type CSSProperties } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, CircleDashed, EyeOff } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { useCurrentMember } from "@multica/core/permissions";
import {
  dashboardProjectTasksOptions,
  dashboardUsageByProjectOptions,
  type DashboardHistoryLimitParam,
} from "@multica/core/dashboard";
import type { Agent } from "@multica/core/types";
import { ProjectIcon } from "../../projects/components/project-icon";
import { formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  aggregateProjectUsage,
  NO_PROJECT_ID,
  RESTRICTED_PROJECTS_ID,
  type ProjectDashboardRow,
} from "../utils";
import { RecentRuns } from "./members-tab";

const PROJECT_GRID_STYLE = {
  minWidth: "fit-content",
  gridTemplateColumns: "minmax(10rem, 1.6fr) minmax(6rem, 1fr) 5rem 5rem 7rem",
} satisfies CSSProperties;

const GRID = "grid items-center gap-3";

interface ProjectsTabScope {
  wsId: string;
  days: number;
  projectId: string | null;
  viewTZ: string;
  historyLimit: DashboardHistoryLimitParam;
  squadId: string | null;
}

/**
 * The dashboard's "By project" tab: usage split by each run's project (its
 * issue's), with the runs outside any project in their own bucket. The server
 * scopes the data like "By member" (a caller below admin only gets their own
 * runs) and folds private projects the viewer cannot see into one bucket that
 * names none of them; nothing here is a permission check. It reads the same
 * range, project and squad filters as the other tabs and is only fetched once
 * the tab is open.
 */
export function ProjectsTab({
  agents,
  projects,
  ...scope
}: ProjectsTabScope & { agents: Agent[]; projects: { id: string; icon: string | null }[] }) {
  const { t } = useT("usage");
  const { role } = useCurrentMember(scope.wsId);
  const usageQuery = useQuery(
    dashboardUsageByProjectOptions(
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
    () => (usageQuery.data ? aggregateProjectUsage(usageQuery.data) : []),
    [usageQuery.data],
  );
  const maxTokens = useMemo(() => rows.reduce((m, r) => Math.max(m, r.tokens), 0), [rows]);
  const namedCount = rows.filter(
    (r) => r.projectId !== NO_PROJECT_ID && r.projectId !== RESTRICTED_PROJECTS_ID,
  ).length;
  const icons = useMemo(() => new Map(projects.map((p) => [p.id, p])), [projects]);
  const isPrivileged = role === "owner" || role === "admin";

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
          <h4 className="text-body font-semibold">{t(($) => $.projects.title)}</h4>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.projects.description)}
          </p>
          {role !== null && !isPrivileged ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.members.own_only)}
            </p>
          ) : null}
        </div>
        <span className="shrink-0 text-caption text-muted-foreground">
          {t(($) => $.projects.caption, { count: namedCount })}
        </span>
      </div>
      {rows.length === 0 ? (
        <p className="px-4 py-8 text-center text-caption text-muted-foreground">
          {t(($) => $.projects.no_data)}
        </p>
      ) : (
        <div
          role="region"
          aria-label={t(($) => $.projects.title)}
          tabIndex={0}
          className="overflow-x-auto overscroll-x-contain"
        >
          <div
            className={`${GRID} border-b px-4 py-2 text-caption font-medium text-muted-foreground`}
            style={PROJECT_GRID_STYLE}
          >
            <span>{t(($) => $.projects.header_project)}</span>
            <span />
            <span className="text-right">{t(($) => $.members.header_tokens)}</span>
            <span className="text-right">{t(($) => $.members.header_cost)}</span>
            <span className="text-right">{t(($) => $.members.header_tasks)}</span>
          </div>
          <ul aria-label={t(($) => $.projects.title)} className="divide-y">
            {rows.map((row) => (
              <ProjectRow
                key={row.projectId}
                row={row}
                icon={icons.get(row.projectId)?.icon ?? null}
                maxTokens={maxTokens}
                expanded={expanded === row.projectId}
                onToggle={() =>
                  setExpanded((cur) => (cur === row.projectId ? null : row.projectId))
                }
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

function ProjectRow({
  row,
  icon,
  maxTokens,
  expanded,
  onToggle,
  scope,
  agents,
}: {
  row: ProjectDashboardRow;
  icon: string | null;
  maxTokens: number;
  expanded: boolean;
  onToggle: () => void;
  scope: ProjectsTabScope;
  agents: Agent[];
}) {
  const { t } = useT("usage");
  const panelId = useId();
  const isNoProject = row.projectId === NO_PROJECT_ID;
  // The server names no private project the viewer cannot see, so there is
  // nothing to expand into.
  const isRestricted = row.projectId === RESTRICTED_PROJECTS_ID;
  const name = isRestricted
    ? t(($) => $.projects.restricted)
    : isNoProject
      ? t(($) => $.projects.no_project)
      : row.title;
  const pct = maxTokens > 0 ? Math.min(100, (row.tokens / maxTokens) * 100) : 0;
  const Chevron = expanded ? ChevronDown : ChevronRight;

  const cells = (
    <>
      <span className="flex min-w-0 items-center gap-2">
        {isRestricted ? (
          <span aria-hidden="true" className="size-3.5 shrink-0" />
        ) : (
          <Chevron aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        {isRestricted || isNoProject ? (
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
            {isRestricted ? (
              <EyeOff aria-hidden="true" className="h-3 w-3" />
            ) : (
              <CircleDashed aria-hidden="true" className="h-3 w-3" />
            )}
          </span>
        ) : (
          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center">
            <ProjectIcon project={{ icon }} size="md" />
          </span>
        )}
        <TruncatedTooltip content={name}>
          <span
            className={`block min-w-0 truncate text-body font-medium ${isRestricted || isNoProject ? "italic text-muted-foreground" : ""}`}
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
    </>
  );

  if (isRestricted) {
    return (
      <li className={`${GRID} px-4 py-2`} style={PROJECT_GRID_STYLE}>
        {cells}
      </li>
    );
  }
  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-controls={expanded ? panelId : undefined}
        aria-label={
          expanded
            ? t(($) => $.projects.hide_tasks, { name })
            : t(($) => $.projects.show_tasks, { name })
        }
        className={`${GRID} w-full px-4 py-2 text-left hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none`}
        style={PROJECT_GRID_STYLE}
      >
        {cells}
      </button>
      {expanded ? (
        <div id={panelId} className="border-t bg-muted/30 px-4 py-3">
          <ProjectTasks projectKey={row.projectId} scope={scope} agents={agents} />
        </div>
      ) : null}
    </li>
  );
}

function ProjectTasks({
  projectKey,
  scope,
  agents,
}: {
  projectKey: string;
  scope: ProjectsTabScope;
  agents: Agent[];
}) {
  const query = useQuery(
    dashboardProjectTasksOptions(
      scope.wsId,
      scope.days,
      projectKey,
      scope.viewTZ,
      scope.historyLimit,
      scope.squadId,
    ),
  );
  return <RecentRuns query={query} agents={agents} />;
}
