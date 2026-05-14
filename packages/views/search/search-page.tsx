// CEREBRO-PATCH(search-page-1326): JEH-1326 — dedicated full-page search with scope tabs and filters
"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Bot,
  Loader2,
  MessageCircle,
  Search,
  SlidersHorizontal,
  TerminalSquare,
} from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core";
import { useWorkspacePaths } from "@multica/core/paths";
import type { SearchIssueResult, SearchProjectResult } from "@multica/core/types";
import type { ChatSession } from "@multica/core/types/chat";
import { cerebroTasksListOptions } from "@multica/cerebro-tasks/core/queries";
import type { CerebroTask, TaskStatus, TaskType } from "@multica/cerebro-tasks/core";
import { DEFAULT_TASKS_FILTER } from "@multica/cerebro-tasks/core";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "../layout/page-header";
import { useNavigation } from "../navigation";
import { StatusIcon } from "../issues/components";
import { ProjectIcon } from "../projects/components/project-icon";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { PROJECT_STATUS_CONFIG } from "@multica/core/projects/config";
import type { ProjectStatus } from "@multica/core/types";

type SearchScope = "all" | "issues" | "projects" | "tasks" | "chats";
type ClosedFilter = "open" | "all";
type TaskStatusFilter = "all" | TaskStatus;
type TaskTypeFilter = "all" | TaskType;
// CEREBRO-PATCH(search-page-date-filters-1326): JEH-1326 — full-page search date filtering controls.
type DateField = "created" | "activity";
type DateRange = "all" | "24h" | "7d" | "30d";

interface SearchData {
  issues: SearchIssueResult[];
  projects: SearchProjectResult[];
}

const EMPTY_SEARCH_DATA: SearchData = { issues: [], projects: [] };
const EMPTY_CHATS: ChatSession[] = [];
const EMPTY_TASKS: CerebroTask[] = [];

function normalize(text: string | null | undefined) {
  return (text ?? "").replace(/\s+/g, " ").trim();
}

function rangeStart(range: DateRange, now = Date.now()) {
  if (range === "all") return null;
  const days = range === "24h" ? 1 : range === "7d" ? 7 : 30;
  return new Date(now - days * 24 * 60 * 60 * 1000);
}

function activityAt(row: { updated_at?: string; completed_at?: string; started_at?: string; dispatched_at?: string; created_at?: string }) {
  return row.updated_at ?? row.completed_at ?? row.started_at ?? row.dispatched_at ?? row.created_at ?? null;
}

function matchesDateRange(
  row: { created_at?: string; updated_at?: string; completed_at?: string; started_at?: string; dispatched_at?: string },
  field: DateField,
  range: DateRange,
) {
  const start = rangeStart(range);
  if (!start) return true;
  const raw = field === "created" ? row.created_at : activityAt(row);
  if (!raw) return false;
  return new Date(raw).getTime() >= start.getTime();
}

function useUrlBackedQuery() {
  const navigation = useNavigation();
  const initial = navigation.searchParams.get("q") ?? "";
  const [query, setQuery] = useState(initial);

  useEffect(() => {
    setQuery(navigation.searchParams.get("q") ?? "");
  }, [navigation.searchParams]);

  useEffect(() => {
    const params = new URLSearchParams(navigation.searchParams);
    const trimmed = query.trim();
    if (trimmed) params.set("q", trimmed);
    else params.delete("q");
    const next = params.toString()
      ? `${navigation.pathname}?${params.toString()}`
      : navigation.pathname;
    const current = navigation.searchParams.toString()
      ? `${navigation.pathname}?${navigation.searchParams.toString()}`
      : navigation.pathname;
    if (next !== current) navigation.replace(next);
  }, [navigation, query]);

  return [query, setQuery] as const;
}

export function SearchPage() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const navigation = useNavigation();
  const [query, setQuery] = useUrlBackedQuery();
  const [scope, setScope] = useState<SearchScope>("all");
  const [closedFilter, setClosedFilter] = useState<ClosedFilter>("all");
  const [taskStatus, setTaskStatus] = useState<TaskStatusFilter>("all");
  const [taskType, setTaskType] = useState<TaskTypeFilter>("all");
  const [dateField, setDateField] = useState<DateField>("activity");
  const [dateRange, setDateRange] = useState<DateRange>("all");

  const trimmedQuery = query.trim();
  const includeClosed = closedFilter === "all";

  const globalSearch = useQuery({
    queryKey: ["full-search", wsId, trimmedQuery, includeClosed],
    enabled: trimmedQuery.length > 0,
    queryFn: async () => {
      const [issueRes, projectRes] = await Promise.all([
        api.searchIssues({
          q: trimmedQuery,
          limit: 50,
          include_closed: includeClosed,
        }),
        api.searchProjects({
          q: trimmedQuery,
          limit: 25,
          include_closed: includeClosed,
        }),
      ]);
      return { issues: issueRes.issues, projects: projectRes.projects };
    },
    placeholderData: (prev) => prev,
  });

  const tasks = useQuery(
    cerebroTasksListOptions(wsId, {
      ...DEFAULT_TASKS_FILTER,
      range: "all",
      search: trimmedQuery,
      status: taskStatus === "all" ? null : taskStatus,
      type: taskType === "all" ? null : taskType,
      limit: 50,
    }),
  );

  const chats = useQuery({
    queryKey: ["full-search", wsId, "chats"],
    enabled: !!wsId,
    queryFn: () => api.listChatSessions({ status: "all" }),
    staleTime: 30 * 1000,
  });

  const searchData = useMemo(() => {
    const data = globalSearch.data ?? EMPTY_SEARCH_DATA;
    if (dateRange === "all") return data;
    return {
      issues: data.issues.filter((issue) => matchesDateRange(issue, dateField, dateRange)),
      projects: data.projects.filter((project) => matchesDateRange(project, dateField, dateRange)),
    };
  }, [dateField, dateRange, globalSearch.data]);
  const taskRows = useMemo(
    () => (tasks.data?.tasks ?? EMPTY_TASKS).filter((task) => matchesDateRange(task, dateField, dateRange)),
    [dateField, dateRange, tasks.data?.tasks],
  );
  const chatRows = useMemo(() => {
    const rows = chats.data ?? EMPTY_CHATS;
    const dated = rows.filter((chat) => matchesDateRange(chat, dateField, dateRange));
    if (!trimmedQuery) return dated.slice(0, 25);
    const q = trimmedQuery.toLowerCase();
    return dated
      .filter((chat) => chat.title.toLowerCase().includes(q))
      .slice(0, 25);
  }, [chats.data, dateField, dateRange, trimmedQuery]);

  const counts = {
    issues: searchData.issues.length,
    projects: searchData.projects.length,
    tasks: taskRows.length,
    chats: chatRows.length,
  };
  const total = counts.issues + counts.projects + counts.tasks + counts.chats;
  const isLoading =
    globalSearch.isFetching ||
    tasks.isFetching ||
    chats.isFetching;
  const showIssues = scope === "all" || scope === "issues";
  const showProjects = scope === "all" || scope === "projects";
  const showTasks = scope === "all" || scope === "tasks";
  const showChats = scope === "all" || scope === "chats";

  const open = (path: string) => navigation.push(path);

  return (
    <div className="flex h-full flex-col bg-background">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Search</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Find issues, projects, tasks, and chats across this workspace
          </p>
        </div>
        <div className="hidden items-center gap-2 text-xs text-muted-foreground sm:flex">
          {isLoading && <Loader2 className="size-3.5 animate-spin" />}
          <span>{total} results</span>
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4 sm:p-6">
          <div className="border-b bg-background pb-4">
            <div className="flex flex-col gap-3">
              <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  type="search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  autoFocus
                  placeholder="Search workspace context..."
                  className="h-10 pl-9 text-base md:text-sm"
                />
              </div>

              <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                <Tabs value={scope} onValueChange={(value) => setScope(value as SearchScope)}>
                  <TabsList className="flex h-auto w-full flex-wrap justify-start gap-1 rounded-lg p-1 sm:w-fit sm:flex-nowrap">
                    <ScopeTab value="all" label="All" count={total} />
                    <ScopeTab value="issues" label="Issues" count={counts.issues} />
                    <ScopeTab value="projects" label="Projects" count={counts.projects} />
                    <ScopeTab value="tasks" label="Tasks" count={counts.tasks} />
                    <ScopeTab value="chats" label="Chats" count={counts.chats} />
                  </TabsList>
                </Tabs>

                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:flex xl:flex-wrap xl:justify-end">
                  <FilterSelect
                    icon={<SlidersHorizontal className="size-3.5" />}
                    value={closedFilter}
                    onValueChange={(value) => setClosedFilter(value as ClosedFilter)}
                    items={[
                      ["all", "All statuses"],
                      ["open", "Open only"],
                    ]}
                  />
                  <FilterSelect
                    value={taskStatus}
                    onValueChange={(value) => setTaskStatus(value as TaskStatusFilter)}
                    items={[
                      ["all", "Any task status"],
                      ["queued", "Queued"],
                      ["dispatched", "Dispatched"],
                      ["running", "Running"],
                      ["completed", "Completed"],
                      ["failed", "Failed"],
                      ["cancelled", "Cancelled"],
                    ]}
                  />
                  <FilterSelect
                    value={taskType}
                    onValueChange={(value) => setTaskType(value as TaskTypeFilter)}
                    items={[
                      ["all", "Any task type"],
                      ["issue", "Issue tasks"],
                      ["chat", "Chat tasks"],
                    ]}
                  />
                  <FilterSelect
                    value={dateField}
                    onValueChange={(value) => setDateField(value as DateField)}
                    items={[
                      ["activity", "Last activity"],
                      ["created", "Created at"],
                    ]}
                  />
                  <FilterSelect
                    value={dateRange}
                    onValueChange={(value) => setDateRange(value as DateRange)}
                    items={[
                      ["all", "All time"],
                      ["24h", "Last 24 hours"],
                      ["7d", "Last 7 days"],
                      ["30d", "Last 30 days"],
                    ]}
                  />
                </div>
              </div>
            </div>
          </div>

          {!trimmedQuery && (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
              Search for a word, identifier, task title, or chat title. Filters stay available as the result set grows.
            </div>
          )}

          {trimmedQuery && total === 0 && !isLoading && (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
              No workspace context matched this search.
            </div>
          )}

          {trimmedQuery && (
            <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.7fr)]">
              <div className="flex min-w-0 flex-col gap-4">
                {showIssues && (
                  <ResultSection title="Issues" count={counts.issues} loading={globalSearch.isFetching}>
                    {searchData.issues.map((issue) => (
                      <button
                        key={issue.id}
                        type="button"
                        onClick={() => open(p.issueDetail(issue.id))}
                        className="flex w-full min-w-0 flex-col gap-1 rounded-lg border bg-background p-3 text-left transition-colors hover:bg-muted/50"
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <StatusIcon status={issue.status} className="size-4 shrink-0" />
                          <span className="shrink-0 text-xs text-muted-foreground">{issue.identifier}</span>
                          <span className="truncate text-sm font-medium">{issue.title}</span>
                          <span className={cn("ml-auto hidden shrink-0 text-xs sm:inline", STATUS_CONFIG[issue.status].iconColor)}>
                            {STATUS_CONFIG[issue.status].label}
                          </span>
                        </div>
                        {issue.matched_snippet && (
                          <p className="line-clamp-2 pl-6 text-xs text-muted-foreground">
                            {normalize(issue.matched_snippet)}
                          </p>
                        )}
                      </button>
                    ))}
                  </ResultSection>
                )}

                {showProjects && (
                  <ResultSection title="Projects" count={counts.projects} loading={globalSearch.isFetching}>
                    {searchData.projects.map((project) => (
                      <button
                        key={project.id}
                        type="button"
                        onClick={() => open(p.projectDetail(project.id))}
                        className="flex w-full min-w-0 flex-col gap-1 rounded-lg border bg-background p-3 text-left transition-colors hover:bg-muted/50"
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <ProjectIcon project={project} size="md" />
                          <span className="truncate text-sm font-medium">{project.title}</span>
                          <span className={cn("ml-auto hidden shrink-0 text-xs sm:inline", PROJECT_STATUS_CONFIG[project.status as ProjectStatus]?.color ?? "text-muted-foreground")}>
                            {PROJECT_STATUS_CONFIG[project.status as ProjectStatus]?.label ?? project.status}
                          </span>
                        </div>
                        {project.matched_snippet && (
                          <p className="line-clamp-2 pl-7 text-xs text-muted-foreground">
                            {normalize(project.matched_snippet)}
                          </p>
                        )}
                      </button>
                    ))}
                  </ResultSection>
                )}
              </div>

              <div className="flex min-w-0 flex-col gap-4">
                {showTasks && (
                  <ResultSection title="Tasks" count={counts.tasks} loading={tasks.isFetching}>
                    {taskRows.map((task) => (
                      <button
                        key={task.task_id}
                        type="button"
                        onClick={() =>
                          open(task.issue_id ? p.issueDetail(task.issue_id) : `${p.inbox()}?chat=${encodeURIComponent(task.chat_session_id ?? "")}`)
                        }
                        className="flex w-full min-w-0 flex-col gap-1 rounded-lg border bg-background p-3 text-left transition-colors hover:bg-muted/50"
                      >
                        <div className="flex min-w-0 items-center gap-2">
                          <TerminalSquare className="size-4 shrink-0 text-muted-foreground" />
                          <span className="truncate text-sm font-medium">
                            {normalize(task.task_title) || normalize(task.issue_title) || "Untitled task"}
                          </span>
                          <Badge variant="outline" className="ml-auto">
                            {task.status}
                          </Badge>
                        </div>
                        <div className="flex min-w-0 items-center gap-2 pl-6 text-xs text-muted-foreground">
                          <Bot className="size-3 shrink-0" />
                          <span className="truncate">{task.agent_name}</span>
                          {task.issue_number && <span className="shrink-0">#{task.issue_number}</span>}
                        </div>
                      </button>
                    ))}
                  </ResultSection>
                )}

                {showChats && (
                  <ResultSection title="Chats" count={counts.chats} loading={chats.isFetching}>
                    {chatRows.map((chat) => (
                      <button
                        key={chat.id}
                        type="button"
                        onClick={() => open(`${p.inbox()}?chat=${encodeURIComponent(chat.id)}`)}
                        className="flex w-full min-w-0 items-center gap-2 rounded-lg border bg-background p-3 text-left transition-colors hover:bg-muted/50"
                      >
                        <MessageCircle className="size-4 shrink-0 text-muted-foreground" />
                        <span className="min-w-0 flex-1 truncate text-sm font-medium">{chat.title}</span>
                        <Badge variant={chat.status === "archived" ? "secondary" : "outline"}>
                          {chat.status}
                        </Badge>
                      </button>
                    ))}
                  </ResultSection>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function ScopeTab({ value, label, count }: { value: SearchScope; label: string; count: number }) {
  return (
    <TabsTrigger value={value} className="basis-[calc(50%-0.125rem)] justify-between px-2 sm:basis-auto">
      <span>{label}</span>
      <span className="rounded bg-muted-foreground/10 px-1.5 text-[10px] text-muted-foreground">
        {count}
      </span>
    </TabsTrigger>
  );
}

function FilterSelect({
  icon,
  value,
  onValueChange,
  items,
}: {
  icon?: ReactNode;
  value: string;
  onValueChange: (value: string) => void;
  items: Array<[string, string]>;
}) {
  return (
    <Select value={value} onValueChange={(next) => next && onValueChange(next)}>
      <SelectTrigger className="h-8 w-full min-w-0 gap-2 xl:w-auto xl:min-w-36">
        {icon}
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map(([itemValue, label]) => (
          <SelectItem key={itemValue} value={itemValue}>
            {label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function ResultSection({
  title,
  count,
  loading,
  children,
}: {
  title: string;
  count: number;
  loading: boolean;
  children: ReactNode;
}) {
  return (
    <section className="min-w-0">
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-xs font-semibold uppercase tracking-normal text-muted-foreground">
          {title}
        </h2>
        <span className="text-xs text-muted-foreground">{count}</span>
        {loading && <Loader2 className="size-3 animate-spin text-muted-foreground" />}
      </div>
      {count > 0 ? (
        <div className="flex flex-col gap-2">{children}</div>
      ) : (
        <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          No matching {title.toLowerCase()}.
        </div>
      )}
    </section>
  );
}
