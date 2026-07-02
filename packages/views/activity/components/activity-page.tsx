"use client";

import { useMemo, useState, type ComponentType, type ReactNode } from "react";
import {
  Activity,
  Bot,
  Clock3,
  FolderKanban,
  ListTodo,
  Search,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueListOptions } from "@multica/core/issues/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, Issue, Project } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { PageHeader } from "../../layout/page-header";
import { StatusIcon } from "../../issues/components/status-icon";
import { ProjectIcon } from "../../projects/components/project-icon";

const EMPTY_ISSUES: Issue[] = [];
const EMPTY_PROJECTS: Project[] = [];
const EMPTY_AGENTS: Agent[] = [];
const ACTIVITY_ITEM_LIMIT = 200;
const PROJECT_WRAP_LIMIT = 24;

type IconComponent = ComponentType<{ className?: string }>;
type ActivityKind = "issue" | "project" | "agent";
type ActivityT = ReturnType<typeof useT<"activity">>["t"];

interface ActivityItem {
  id: string;
  kind: ActivityKind;
  href: string;
  title: string;
  action: string;
  context: string | null;
  timestamp: string;
  icon: IconComponent;
  node?: ReactNode;
  projectId?: string | null;
}

interface ActivityGroup {
  key: string;
  label: string;
  items: ActivityItem[];
}

interface ProjectWrapRow {
  id: string;
  project: Project | null;
  issues: Issue[];
  latestAt: string;
  issueCount: number;
  doneCount: number;
  resourceCount: number;
}

function isOpenIssue(issue: Issue): boolean {
  return issue.status !== "done" && issue.status !== "cancelled";
}

function nearSameTimestamp(a: string, b: string): boolean {
  return Math.abs(new Date(a).getTime() - new Date(b).getTime()) < 2_000;
}

function startOfLocalDay(date: Date): Date {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  return copy;
}

function dayLabel(timestamp: string, todayLabel: string, yesterdayLabel: string): string {
  const date = new Date(timestamp);
  const today = startOfLocalDay(new Date());
  const day = startOfLocalDay(date);
  const diffDays = Math.round((today.getTime() - day.getTime()) / 86_400_000);
  if (diffDays === 0) return todayLabel;
  if (diffDays === 1) return yesterdayLabel;
  return date.toLocaleDateString(undefined, {
    month: "long",
    day: "numeric",
    year: date.getFullYear() === today.getFullYear() ? undefined : "numeric",
  });
}

function issueDisplayTitle(issue: Issue): string {
  return issue.identifier ? `${issue.identifier} ${issue.title}` : issue.title;
}

export function ActivityPage() {
  const { t } = useT("activity");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const [projectFilter, setProjectFilter] = useState("all");
  const [query, setQuery] = useState("");

  const issuesQuery = useQuery(issueListOptions(wsId));
  const projectsQuery = useQuery(projectListOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));

  const issues = issuesQuery.data ?? EMPTY_ISSUES;
  const projects = projectsQuery.data ?? EMPTY_PROJECTS;
  const agents = agentsQuery.data ?? EMPTY_AGENTS;

  const projectById = useMemo(
    () => new Map(projects.map((project) => [project.id, project] as const)),
    [projects],
  );

  const activityItems = useMemo(() => {
    const issueItems: ActivityItem[] = issues.map((issue) => {
      const project = issue.project_id ? projectById.get(issue.project_id) : null;
      const created = nearSameTimestamp(issue.created_at, issue.updated_at);
      const action =
        issue.status === "done"
          ? t(($) => $.activity.actions.issue_done)
          : issue.status === "blocked"
            ? t(($) => $.activity.actions.issue_blocked)
            : created
              ? t(($) => $.activity.actions.issue_created)
              : t(($) => $.activity.actions.issue_updated);
      return {
        id: `issue:${issue.id}`,
        kind: "issue" as const,
        href: paths.issueDetail(issue.id),
        title: issueDisplayTitle(issue),
        action,
        context: project?.title ?? null,
        timestamp: issue.updated_at,
        icon: ListTodo,
        node: <StatusIcon status={issue.status} className="size-3.5" />,
        projectId: issue.project_id,
      };
    });

    const projectItems: ActivityItem[] = projects.map((project) => ({
      id: `project:${project.id}`,
      kind: "project" as const,
      href: paths.projectDetail(project.id),
      title: project.title,
      action: nearSameTimestamp(project.created_at, project.updated_at)
        ? t(($) => $.activity.actions.project_created)
        : t(($) => $.activity.actions.project_updated),
      context:
        project.issue_count > 0
          ? t(($) => $.activity.issue_count, { count: project.issue_count })
          : null,
      timestamp: project.updated_at,
      icon: FolderKanban,
      node: <ProjectIcon project={project} size="sm" />,
      projectId: project.id,
    }));

    const agentItems: ActivityItem[] = agents.map((agent) => ({
      id: `agent:${agent.id}`,
      kind: "agent" as const,
      href: paths.agentDetail(agent.id),
      title: agent.name,
      action: agent.archived_at
        ? t(($) => $.activity.actions.agent_archived)
        : nearSameTimestamp(agent.created_at, agent.updated_at)
          ? t(($) => $.activity.actions.agent_created)
          : t(($) => $.activity.actions.agent_updated),
      context: t(($) => $.status.agent[agent.status]),
      timestamp: agent.archived_at ?? agent.updated_at,
      icon: Bot,
      projectId: null,
    }));

    return [...issueItems, ...projectItems, ...agentItems]
      .toSorted((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
  }, [agents, issues, paths, projectById, projects, t]);

  const filteredActivityItems = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return activityItems
      .filter((item) => projectFilter === "all" || item.projectId === projectFilter)
      .filter((item) => {
        if (!normalizedQuery) return true;
        const haystack = `${item.title} ${item.context ?? ""} ${item.action}`.toLowerCase();
        return haystack.includes(normalizedQuery);
      })
      .slice(0, ACTIVITY_ITEM_LIMIT);
  }, [activityItems, projectFilter, query]);

  const activityGroups = useMemo(() => {
    const groups = new Map<string, ActivityGroup>();
    for (const item of filteredActivityItems) {
      const date = startOfLocalDay(new Date(item.timestamp));
      const key = date.toISOString();
      const label = dayLabel(
        item.timestamp,
        t(($) => $.dates.today),
        t(($) => $.dates.yesterday),
      );
      const group = groups.get(key) ?? { key, label, items: [] };
      group.items.push(item);
      groups.set(key, group);
    }
    return Array.from(groups.values());
  }, [filteredActivityItems, t]);

  const projectWrap = useMemo<ProjectWrapRow[]>(() => {
    const rows = new Map<string, ProjectWrapRow>();
    for (const project of projects) {
      rows.set(project.id, {
        id: project.id,
        project,
        issues: [],
        latestAt: project.updated_at,
        issueCount: project.issue_count,
        doneCount: project.done_count,
        resourceCount: project.resource_count,
      });
    }

    const unassignedId = "__unassigned__";
    for (const issue of issues) {
      const id = issue.project_id ?? unassignedId;
      const project = issue.project_id ? projectById.get(issue.project_id) ?? null : null;
      const current =
        rows.get(id) ??
        ({
          id,
          project,
          issues: [],
          latestAt: issue.updated_at,
          issueCount: 0,
          doneCount: 0,
          resourceCount: 0,
        } satisfies ProjectWrapRow);
      current.issues.push(issue);
      current.latestAt =
        new Date(issue.updated_at).getTime() > new Date(current.latestAt).getTime()
          ? issue.updated_at
          : current.latestAt;
      if (!project) {
        current.issueCount += 1;
        if (issue.status === "done") current.doneCount += 1;
      }
      rows.set(id, current);
    }

    return Array.from(rows.values())
      .filter((row) => row.project || row.issues.length > 0)
      .toSorted((a, b) => new Date(b.latestAt).getTime() - new Date(a.latestAt).getTime())
      .slice(0, PROJECT_WRAP_LIMIT);
  }, [issues, projectById, projects]);

  const activityLoading =
    issuesQuery.isLoading || projectsQuery.isLoading || agentsQuery.isLoading;
  const activeAgentCount = agents.filter((agent) => agent.status !== "offline").length;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <Tabs defaultValue="activity" className="min-h-0 flex-1 gap-0">
        <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-y-1.5 px-5 py-1.5 sm:py-0">
          <div className="flex min-w-0 items-center gap-2">
            <Activity className="size-4 shrink-0 text-muted-foreground" />
            <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
          </div>
          <ActivityToolbar
            projects={projects}
            projectFilter={projectFilter}
            onProjectFilterChange={setProjectFilter}
            query={query}
            onQueryChange={setQuery}
            t={t}
          />
        </PageHeader>

        <div className="min-h-0 flex-1 overflow-auto">
          <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-6">
            <div>
              <h2 className="font-heading text-xl font-semibold tracking-normal text-foreground">
                {t(($) => $.activity.title)}
              </h2>
              <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
                {t(($) => $.subtitle)}
              </p>
            </div>

            <TabsContent value="activity" className="mt-0">
              <ActivityTimeline
                groups={activityGroups}
                loading={activityLoading}
                timeAgo={timeAgo}
                activeAgentCount={activeAgentCount}
                t={t}
              />
            </TabsContent>

            <TabsContent value="wrapup" className="mt-0">
              <ProjectWrapList
                rows={projectWrap}
                loading={projectsQuery.isLoading || issuesQuery.isLoading}
                timeAgo={timeAgo}
                paths={paths}
                t={t}
              />
            </TabsContent>
          </div>
        </div>
      </Tabs>
    </div>
  );
}

function ActivityToolbar({
  projects,
  projectFilter,
  onProjectFilterChange,
  query,
  onQueryChange,
  t,
}: {
  projects: Project[];
  projectFilter: string;
  onProjectFilterChange: (value: string) => void;
  query: string;
  onQueryChange: (value: string) => void;
  t: ActivityT;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <TabsList>
        <TabsTrigger value="activity" className="px-2.5">
          {t(($) => $.tabs.activity)}
        </TabsTrigger>
        <TabsTrigger value="wrapup" className="px-2.5">
          {t(($) => $.tabs.wrapup)}
        </TabsTrigger>
      </TabsList>

      <NativeSelect
        size="sm"
        value={projectFilter}
        onChange={(event) => onProjectFilterChange(event.target.value)}
        className="w-44"
      >
        <NativeSelectOption value="all">
          {t(($) => $.filters.all_projects)}
        </NativeSelectOption>
        {projects.map((project) => (
          <NativeSelectOption key={project.id} value={project.id}>
            {project.title}
          </NativeSelectOption>
        ))}
      </NativeSelect>

      <Button
        type="button"
        variant="outline"
        size="sm"
        aria-disabled="true"
        tabIndex={-1}
        className="pointer-events-none text-muted-foreground"
      >
        {t(($) => $.filters.everyone)}
      </Button>

      <label className="relative w-52">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder={t(($) => $.filters.filter_placeholder)}
          className="h-7 pl-8 text-sm"
        />
      </label>
    </div>
  );
}

function ActivityTimeline({
  groups,
  loading,
  timeAgo,
  activeAgentCount,
  t,
}: {
  groups: ActivityGroup[];
  loading: boolean;
  timeAgo: (dateStr: string) => string;
  activeAgentCount: number;
  t: ActivityT;
}) {
  if (loading) return <ListSkeleton />;
  if (groups.length === 0) {
    return (
      <EmptyState
        icon={Clock3}
        title={t(($) => $.activity.empty_title)}
        body={t(($) => $.activity.empty_body)}
      />
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {groups.map((group) => (
        <section key={group.key} className="border-b last:border-b-0">
          <DayDivider
            label={group.label}
            sideText={t(($) => $.activity.active_agents, {
              count: activeAgentCount,
            })}
          />
          <div className="divide-y">
            {group.items.map((item) => (
              <ActivityRow key={item.id} item={item} timeAgo={timeAgo} t={t} />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function DayDivider({ label, sideText }: { label: string; sideText: string }) {
  return (
    <div className="flex items-center justify-between gap-3 bg-muted/30 px-3 py-2">
      <div className="text-xs font-medium uppercase text-muted-foreground">
        {label}
      </div>
      <div className="text-xs text-muted-foreground">{sideText}</div>
    </div>
  );
}

function ActivityRow({
  item,
  timeAgo,
  t,
}: {
  item: ActivityItem;
  timeAgo: (dateStr: string) => string;
  t: ActivityT;
}) {
  const Icon = item.icon;
  return (
    <div className="grid grid-cols-[4.5rem_2rem_minmax(0,1fr)] gap-3 px-3 py-3 transition-colors hover:bg-muted/35">
      <time className="pt-0.5 text-right text-xs tabular-nums text-muted-foreground">
        {timeAgo(item.timestamp)}
      </time>
      <div className="relative flex justify-center">
        <div className="flex size-7 items-center justify-center rounded-md border border-border bg-background text-muted-foreground">
          {item.node ?? <Icon className="size-3.5" />}
        </div>
      </div>
      <div className="min-w-0">
        <div className="text-sm leading-snug text-foreground">
          <span className="font-medium">{item.action}</span>{" "}
          <AppLink
            href={item.href}
            className="font-medium text-foreground underline-offset-2 hover:text-brand hover:underline"
          >
            {item.title}
          </AppLink>
          {item.context && (
            <span className="text-muted-foreground"> · {item.context}</span>
          )}
        </div>
        <div className="mt-1 text-xs text-muted-foreground">
          {t(($) => $.activity.kind[item.kind])}
        </div>
      </div>
    </div>
  );
}

function ProjectWrapList({
  rows,
  loading,
  timeAgo,
  paths,
  t,
}: {
  rows: ProjectWrapRow[];
  loading: boolean;
  timeAgo: (dateStr: string) => string;
  paths: ReturnType<typeof useWorkspacePaths>;
  t: ActivityT;
}) {
  if (loading) return <ListSkeleton />;
  if (rows.length === 0) {
    return (
      <EmptyState
        icon={FolderKanban}
        title={t(($) => $.wrapup.empty_title)}
        body={t(($) => $.wrapup.empty_body)}
      />
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-end justify-between gap-3">
        <div>
          <div className="text-xs font-medium uppercase text-muted-foreground">
            {t(($) => $.wrapup.date_label)}
          </div>
          <h3 className="mt-1 text-sm font-semibold leading-tight">
            {t(($) => $.wrapup.section_title)}
          </h3>
        </div>
        <div className="text-xs text-muted-foreground">
          {t(($) => $.wrapup.project_count, { count: rows.length })}
        </div>
      </div>
      <div className="overflow-hidden rounded-lg border bg-card">
        <div className="divide-y">
          {rows.map((row) => (
            <ProjectWrapRowItem
              key={row.id}
              row={row}
              timeAgo={timeAgo}
              paths={paths}
              t={t}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function ProjectWrapRowItem({
  row,
  timeAgo,
  paths,
  t,
}: {
  row: ProjectWrapRow;
  timeAgo: (dateStr: string) => string;
  paths: ReturnType<typeof useWorkspacePaths>;
  t: ActivityT;
}) {
  const openCount = row.project
    ? Math.max(0, row.issueCount - row.doneCount)
    : row.issues.filter(isOpenIssue).length;
  const recentIssues = row.issues
    .toSorted((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
    .slice(0, 5);
  const title = row.project?.title ?? t(($) => $.wrapup.no_project);
  const href = row.project ? paths.projectDetail(row.project.id) : paths.issues();

  return (
    <section className="grid gap-3 px-3 py-4 transition-colors hover:bg-muted/35 md:grid-cols-[2rem_minmax(0,1fr)]">
      <div className="hidden md:flex md:justify-center">
        <div className="flex size-7 items-center justify-center rounded-md border bg-background text-muted-foreground">
          {row.project ? (
            <ProjectIcon project={row.project} size="sm" />
          ) : (
            <FolderKanban className="size-3.5" />
          )}
        </div>
      </div>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <AppLink
            href={href}
            className="inline-flex max-w-full items-center text-sm font-medium leading-tight text-foreground underline-offset-2 hover:text-brand hover:underline"
          >
            <span className="truncate">{title}</span>
          </AppLink>
          <span className="text-xs text-muted-foreground">{timeAgo(row.latestAt)}</span>
        </div>
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>{t(($) => $.wrapup.open_issues, { count: openCount })}</span>
          <span>{t(($) => $.wrapup.done_issues, { count: row.doneCount })}</span>
          {row.resourceCount > 0 && (
            <span>{t(($) => $.wrapup.resources, { count: row.resourceCount })}</span>
          )}
        </div>
        {recentIssues.length > 0 && (
          <div className="mt-3 space-y-1.5">
            {recentIssues.map((issue) => (
              <AppLink
                key={issue.id}
                href={paths.issueDetail(issue.id)}
                className="grid grid-cols-[1.5rem_minmax(0,1fr)] items-start gap-2 text-sm leading-snug text-foreground hover:text-brand"
              >
                <StatusIcon status={issue.status} className="mt-0.5 size-3.5 shrink-0" />
                <span className="min-w-0 truncate">{issueDisplayTitle(issue)}</span>
              </AppLink>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

function EmptyState({
  icon: Icon,
  title,
  body,
}: {
  icon: IconComponent;
  title: string;
  body: string;
}) {
  return (
    <div className="flex min-h-48 flex-col items-center justify-center rounded-lg border bg-card px-4 py-10 text-center">
      <Icon className="size-5 text-muted-foreground" />
      <div className="mt-3 text-sm font-semibold">{title}</div>
      <div className="mt-1 max-w-sm text-sm text-muted-foreground">{body}</div>
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {Array.from({ length: 5 }).map((_, index) => (
        <div key={index} className="grid grid-cols-[4.5rem_2rem_minmax(0,1fr)] gap-3 border-b px-3 py-3 last:border-b-0">
          <Skeleton className="mt-0.5 h-4 w-12 justify-self-end" />
          <Skeleton className="size-7 rounded-full" />
          <div className="min-w-0 space-y-2">
            <Skeleton className="h-4 w-3/5" />
            <Skeleton className="h-4 w-2/5" />
          </div>
        </div>
      ))}
    </div>
  );
}
