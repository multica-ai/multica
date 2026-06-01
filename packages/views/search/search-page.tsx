// CEREBRO-PATCH(search-page-1326): JEH-1326 — dedicated full-page search with scope tabs and filters
"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AtSign,
  Bot,
  CircleDot,
  FolderKanban,
  Info,
  Loader2,
  MessageCircle,
  Paperclip,
  Search,
  SlidersHorizontal,
  TerminalSquare,
  X,
} from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core";
import { useWorkspacePaths } from "@multica/core/paths";
import type { SearchIssueResult, SearchProjectResult } from "@multica/core/types";
import type { ChatSession } from "@multica/core/types/chat";
import { cerebroTasksListOptions } from "@multica/cerebro-tasks/core/queries";
import type { CerebroTask, TaskStatus, TaskType } from "@multica/cerebro-tasks/core";
import { DEFAULT_TASKS_FILTER } from "@multica/cerebro-tasks/core";
import {
  parseSearchQuery,
  type ActiveFilter,
  type FilterKey,
  type ParsedFilter,
} from "@multica/cerebro-search";
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
  activeFilters: ActiveFilter[];
}

const EMPTY_SEARCH_DATA: SearchData = { issues: [], projects: [], activeFilters: [] };

// CEREBRO-PATCH(search-page-filter-chips-2595): FIR-2595 trin 1 — inline
// `from:/assignee:/status:/project:/has:` filter chip rendering.
const FILTER_ICON: Record<FilterKey, ReactNode> = {
  from: <AtSign className="size-3" />,
  assignee: <AtSign className="size-3" />,
  status: <CircleDot className="size-3" />,
  project: <FolderKanban className="size-3" />,
  has: <Paperclip className="size-3" />,
};

const FILTER_LABEL: Record<FilterKey, string> = {
  from: "From",
  assignee: "Assignee",
  status: "Status",
  project: "Project",
  has: "Has",
};
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
  // CEREBRO-PATCH(search-page-mobile-filter-2638): FIR-2638 — mobile filter panel toggle state.
  const [filterPanelOpen, setFilterPanelOpen] = useState(false);
  const activeFilterCount = [closedFilter !== "all", taskStatus !== "all", taskType !== "all", dateRange !== "all"].filter(Boolean).length;

  const trimmedQuery = query.trim();
  const includeClosed = closedFilter === "all";

  // CEREBRO-PATCH(search-page-parse-2595): parse inline filters from the raw
  // query so chips render the moment the user types `from:jesper` — without
  // waiting for the backend response. The text portion is shown as the free-
  // text part of the query above the chip row.
  const parsedQuery = useMemo(() => parseSearchQuery(query), [query]);
  const filterChipsEnabled = trimmedQuery.length > 0 || parsedQuery.filters.length > 0;

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
      // The cerebro FTS endpoint echoes back the resolved filter list so the
      // UI can show "miss" hints. Older server builds without FIR-2595 omit
      // the field — default to []. CEREBRO-PATCH(search-page-active-2595).
      const activeFilters = (issueRes as { filters?: ActiveFilter[] }).filters ?? [];
      return {
        issues: issueRes.issues,
        projects: projectRes.projects,
        activeFilters,
      };
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

  const searchData: SearchData = useMemo(() => {
    const data = globalSearch.data ?? EMPTY_SEARCH_DATA;
    if (dateRange === "all") return data;
    return {
      issues: data.issues.filter((issue) => matchesDateRange(issue, dateField, dateRange)),
      projects: data.projects.filter((project) => matchesDateRange(project, dateField, dateRange)),
      activeFilters: data.activeFilters,
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
                  placeholder="Search — try from:jesper status:todo project:lager"
                  className="h-10 pl-9 text-base md:text-sm"
                />
              </div>

              {/* CEREBRO-PATCH(search-page-filter-chip-row-2595): show parsed
                  filters as chips; "miss" gets a subtle dashed border so the
                  user sees a real filter was applied but resolved to nothing. */}
              {filterChipsEnabled && parsedQuery.filters.length > 0 && (
                <FilterChipRow
                  parsed={parsedQuery.filters}
                  active={searchData.activeFilters}
                  onRemove={(f) => {
                    setQuery(removeFilterFromQuery(query, f));
                  }}
                />
              )}

              {/* Hint row — only shown when there is no query at all so it
                  doesn't crowd the active-results page. */}
              {!trimmedQuery && parsedQuery.filters.length === 0 && (
                <FilterHintRow />
              )}

              <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                <Tabs value={scope} onValueChange={(value) => setScope(value as SearchScope)}>
                  {/* CEREBRO-PATCH(search-page-scope-scroll-2638): FIR-2638 — horizontal scroll on mobile instead of wrap. */}
                  <TabsList className="flex h-auto w-full flex-nowrap justify-start gap-1 overflow-x-auto rounded-lg p-1 no-scrollbar sm:w-fit">
                    <ScopeTab value="all" label="All" count={total} />
                    <ScopeTab value="issues" label="Issues" count={counts.issues} />
                    <ScopeTab value="projects" label="Projects" count={counts.projects} />
                    <ScopeTab value="tasks" label="Tasks" count={counts.tasks} />
                    <ScopeTab value="chats" label="Chats" count={counts.chats} />
                  </TabsList>
                </Tabs>

                {/* CEREBRO-PATCH(search-page-mobile-filter-toggle-2638): FIR-2638 — mobile Filters toggle button, hidden on xl. */}
                <button type="button" onClick={() => setFilterPanelOpen((v) => !v)}
                  className={cn("flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground hover:bg-muted xl:hidden", filterPanelOpen && "bg-muted")}>
                  <SlidersHorizontal className="size-3.5" />
                  {filterPanelOpen ? "Skjul filtre" : `Filtre${activeFilterCount > 0 ? ` (${activeFilterCount})` : ""}`}
                </button>

                {/* CEREBRO-PATCH(search-page-mobile-filter-panel-2638): FIR-2638 — hidden on mobile until toggled; always visible on xl. */}
                <div className={cn("grid grid-cols-1 gap-2 sm:grid-cols-2 xl:flex xl:flex-wrap xl:justify-end", !filterPanelOpen && "hidden xl:flex")}>
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
    // CEREBRO-PATCH(search-page-scope-tab-shrink-2638): FIR-2638 — shrink-0 replaces two-column mobile basis.
    <TabsTrigger value={value} className="shrink-0 justify-between px-2">
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

// CEREBRO-PATCH(search-page-filter-chip-row-2595): FIR-2595 trin 1 chip row.
function FilterChipRow({
  parsed,
  active,
  onRemove,
}: {
  parsed: ParsedFilter[];
  active: ActiveFilter[];
  onRemove: (filter: ParsedFilter) => void;
}) {
  // Pair each parsed chip with the backend echo when available so we can
  // render "miss" hints. We pair positionally per (key, value), tolerating
  // the case where the backend hasn't replied yet (active is []).
  const matchByKeyValue = useMemo(() => {
    const m = new Map<string, ActiveFilter["match"]>();
    for (const a of active) m.set(`${a.key}:${a.value.toLowerCase()}`, a.match);
    return m;
  }, [active]);

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
        Filters
      </span>
      {parsed.map((f, idx) => {
        const match = matchByKeyValue.get(`${f.key}:${f.value.toLowerCase()}`) ?? null;
        const isMiss = match === "miss";
        return (
          <span
            key={`${f.key}-${f.value}-${idx}`}
            className={cn(
              "inline-flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-xs",
              isMiss
                ? "border-dashed border-amber-400/60 text-amber-600 dark:text-amber-400"
                : "border-border text-foreground",
            )}
            title={isMiss ? "Filter resolved to no rows" : undefined}
          >
            {FILTER_ICON[f.key]}
            <span className="font-medium">{FILTER_LABEL[f.key]}:</span>
            <span className="truncate max-w-[16ch]">{f.value}</span>
            <button
              type="button"
              onClick={() => onRemove(f)}
              className="ml-0.5 -mr-0.5 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label={`Remove ${FILTER_LABEL[f.key]} filter ${f.value}`}
            >
              <X className="size-3" />
            </button>
          </span>
        );
      })}
    </div>
  );
}

// CEREBRO-PATCH(search-page-filter-hint-2595): empty-state syntax hint.
function FilterHintRow() {
  return (
    <div className="flex items-start gap-2 rounded-md border border-dashed bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
      <Info className="mt-0.5 size-3.5 shrink-0" />
      <div className="flex flex-col gap-0.5">
        <span>
          Combine free text with inline filters: <span className="font-mono text-foreground">from:</span>
          <span className="text-muted-foreground">name</span>{" "}
          <span className="font-mono text-foreground">assignee:</span>
          <span className="text-muted-foreground">me</span>{" "}
          <span className="font-mono text-foreground">status:</span>
          <span className="text-muted-foreground">todo</span>{" "}
          <span className="font-mono text-foreground">project:</span>
          <span className="text-muted-foreground">lager</span>{" "}
          <span className="font-mono text-foreground">has:attachment</span>
        </span>
        <span>
          Quote multi-word values: <span className="font-mono text-foreground">project:"my project"</span>.
        </span>
      </div>
    </div>
  );
}

// CEREBRO-PATCH(search-page-remove-filter-2595): strip a parsed filter token
// from the raw query when the user clicks ✕ on its chip. Preserves quoting
// and re-collapses whitespace so the input stays tidy.
function removeFilterFromQuery(raw: string, target: ParsedFilter): string {
  const escapedKey = target.key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const escapedValueLiteral = target.value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const valuePart = `(?:"${escapedValueLiteral}"|'${escapedValueLiteral}'|${escapedValueLiteral.replace(
    /\s+/g,
    "\\s*",
  )})`;
  const pattern = new RegExp(`(^|\\s)${escapedKey}:${valuePart}(?=\\s|$)`, "i");
  const stripped = raw.replace(pattern, "").replace(/\s+/g, " ").trim();
  return stripped;
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
