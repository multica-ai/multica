"use client";

import { useEffect, useMemo, useState } from "react";
import type { DateRange } from "react-day-picker";
import { CalendarRange, ChevronRight, ListTodo, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ALL_STATUSES, STATUS_CONFIG } from "@/features/issues/config";
import { useIssuesListQuery } from "@/features/issues/queries";
import { useIssuesScopeStore } from "@/features/issues/stores/issues-scope-store";
import { registerViewStoreForWorkspaceSync } from "@/features/issues/stores/view-store";
import {
  ViewStoreProvider,
  useViewStore,
} from "@/features/issues/stores/view-store-context";
import { issueListViewStore } from "@/features/issues/stores/issue-list-view-store";
import { filterIssues } from "@/features/issues/utils/filter";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { IssueTaskStatusSync } from "@/features/issues/components/issue-task-status-sync";
import { IssuesHeader } from "@/features/issues/components/issues-header";
import { FlatIssueList } from "@/features/issues/components/flat-issue-list";
import { BatchActionToolbar } from "@/features/issues/components/batch-action-toolbar";
import { StatusIcon } from "@/features/issues/components";
import { ProjectPicker } from "@/features/projects/components/project-picker";
import { useWorkspaceStore, WorkspaceAvatar } from "@/features/workspace";
import type { IssueStatus } from "@/shared/types";

type DateFilterField = "due" | "start" | "end";

function parseDateOnly(value: string): Date | undefined {
  if (!value) return undefined;

  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) return undefined;

  return new Date(year, month - 1, day);
}

function toDateOnly(value: Date | undefined): string {
  if (!value) return "";

  const year = value.getFullYear();
  const month = `${value.getMonth() + 1}`.padStart(2, "0");
  const day = `${value.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function formatDateRangeSummary(from: string, to: string): string {
  if (!from && !to) return "Any time";

  const formatter = new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
  });

  const fromDate = parseDateOnly(from);
  const toDate = parseDateOnly(to);

  if (fromDate && toDate) {
    const fromLabel = formatter.format(fromDate);
    const toLabel = formatter.format(toDate);
    return fromLabel === toLabel ? fromLabel : `${fromLabel} - ${toLabel}`;
  }

  if (fromDate) return `From ${formatter.format(fromDate)}`;
  if (toDate) return `Until ${formatter.format(toDate)}`;
  return "Any time";
}

function getSelectedRange(from: string, to: string): DateRange | undefined {
  const fromDate = parseDateOnly(from);
  const toDate = parseDateOnly(to);

  if (!fromDate && !toDate) return undefined;

  return {
    from: fromDate,
    to: toDate ?? fromDate,
  };
}

function IssueListDateFilter({
  dueFrom,
  dueTo,
  startFrom,
  startTo,
  endFrom,
  endTo,
  onRangeChange,
}: {
  dueFrom: string;
  dueTo: string;
  startFrom: string;
  startTo: string;
  endFrom: string;
  endTo: string;
  onRangeChange: (field: DateFilterField, range: DateRange | undefined) => void;
}) {
  const [activeField, setActiveField] = useState<DateFilterField>("due");

  const fieldConfig: Array<{
    key: DateFilterField;
    label: string;
    from: string;
    to: string;
  }> = [
    { key: "due", label: "Due date", from: dueFrom, to: dueTo },
    { key: "start", label: "Start date", from: startFrom, to: startTo },
    { key: "end", label: "End date", from: endFrom, to: endTo },
  ];

  const fallbackConfig = fieldConfig[0]!;
  const activeConfig = fieldConfig.find((field) => field.key === activeField) ?? fallbackConfig;
  const activeDateFilterCount = fieldConfig.filter((field) => field.from || field.to).length;

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button variant="outline" size="sm" className="gap-2 text-muted-foreground">
            <CalendarRange className="size-4" />
            <span>{activeDateFilterCount > 0 ? `Dates (${activeDateFilterCount})` : "Dates"}</span>
          </Button>
        }
      />
      <PopoverContent align="end" className="w-[320px] p-0">
        <div className="border-b px-3 py-2.5">
          <span className="text-xs font-medium text-muted-foreground">
            Date filters
          </span>
          <div className="mt-2 grid gap-2">
            {fieldConfig.map((field) => {
              const active = field.key === activeField;
              return (
                <button
                  key={field.key}
                  type="button"
                  onClick={() => setActiveField(field.key)}
                  className={`rounded-md border px-3 py-2 text-left transition-colors ${
                    active
                      ? "border-primary bg-accent/40"
                      : "border-transparent hover:border-border hover:bg-accent/20"
                  }`}
                >
                  <div className="text-xs font-medium">{field.label}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {formatDateRangeSummary(field.from, field.to)}
                  </div>
                </button>
              );
            })}
          </div>
        </div>

        <Calendar
          mode="range"
          selected={getSelectedRange(activeConfig.from, activeConfig.to)}
          onSelect={(range) => onRangeChange(activeField, range)}
          numberOfMonths={1}
          initialFocus
        />

        <div className="flex items-center justify-between border-t px-3 py-2">
          <span className="text-xs text-muted-foreground">
            {formatDateRangeSummary(activeConfig.from, activeConfig.to)}
          </span>
          <Button
            variant="ghost"
            size="xs"
            disabled={!activeConfig.from && !activeConfig.to}
            onClick={() => onRangeChange(activeField, undefined)}
            className="text-muted-foreground hover:text-foreground"
          >
            Clear date
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function IssueListFiltersRow({
  searchQuery,
  onSearchChange,
  statusFilters,
  onToggleStatus,
  projectId,
  onProjectChange,
  dueFrom,
  dueTo,
  startFrom,
  startTo,
  endFrom,
  endTo,
  onDateChange,
  onReset,
  isSearching,
  total,
  visibleCount,
}: {
  searchQuery: string;
  onSearchChange: (value: string) => void;
  statusFilters: IssueStatus[];
  onToggleStatus: (status: IssueStatus) => void;
  projectId: string | null;
  onProjectChange: (value: string | null) => void;
  dueFrom: string;
  dueTo: string;
  startFrom: string;
  startTo: string;
  endFrom: string;
  endTo: string;
  onDateChange: (field: "dueFrom" | "dueTo" | "startFrom" | "startTo" | "endFrom" | "endTo", value: string) => void;
  onReset: () => void;
  isSearching: boolean;
  total: number;
  visibleCount: number;
}) {
  const activeDateFilterCount = [dueFrom, dueTo, startFrom, startTo, endFrom, endTo].filter(Boolean).length;
  const hasServerFilters = Boolean(searchQuery.trim() || projectId || activeDateFilterCount > 0);

  return (
    <div className="border-b px-4 py-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={searchQuery}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder="Search by title, description, issue number, or issue ID"
            className="h-9 pl-9 pr-9"
          />
          {searchQuery ? (
            <button
              type="button"
              onClick={() => onSearchChange("")}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
              aria-label="Clear search"
            >
              <X className="size-4" />
            </button>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <ProjectPicker
            projectId={projectId}
            onUpdate={(updates) => onProjectChange(updates.project_id ?? null)}
            align="start"
          />

          <IssueListDateFilter
            dueFrom={dueFrom}
            dueTo={dueTo}
            startFrom={startFrom}
            startTo={startTo}
            endFrom={endFrom}
            endTo={endTo}
            onRangeChange={(field, range) => {
              const nextFrom = toDateOnly(range?.from);
              const nextTo = toDateOnly(range?.to);

              switch (field) {
                case "due":
                  onDateChange("dueFrom", nextFrom);
                  onDateChange("dueTo", nextTo);
                  break;
                case "start":
                  onDateChange("startFrom", nextFrom);
                  onDateChange("startTo", nextTo);
                  break;
                case "end":
                  onDateChange("endFrom", nextFrom);
                  onDateChange("endTo", nextTo);
                  break;
              }
            }}
          />

          {hasServerFilters ? (
            <Button variant="ghost" size="sm" onClick={onReset}>
              Reset search
            </Button>
          ) : null}
        </div>
      </div>

      <div className="mt-3 flex items-center justify-between gap-2 text-xs text-muted-foreground">
        <span>
          {isSearching ? "Searching issues..." : `${visibleCount} visible · ${total} matched`}
        </span>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        {ALL_STATUSES.map((status) => {
          const selected = statusFilters.includes(status);
          return (
            <Button
              key={status}
              type="button"
              variant={selected ? "secondary" : "outline"}
              size="sm"
              className="h-8 gap-1.5 rounded-full px-3"
              onClick={() => onToggleStatus(status)}
            >
              <StatusIcon status={status} className="h-3.5 w-3.5" />
              <span>{STATUS_CONFIG[status].label}</span>
            </Button>
          );
        })}
      </div>
    </div>
  );
}

function IssueListPageContent() {
  const workspace = useWorkspaceStore((state) => state.workspace);
  const scope = useIssuesScopeStore((state) => state.scope);
  const setViewMode = useViewStore((state) => state.setViewMode);
  const statusFilters = useViewStore((state) => state.statusFilters);
  const toggleStatusFilter = useViewStore((state) => state.toggleStatusFilter);
  const priorityFilters = useViewStore((state) => state.priorityFilters);
  const assigneeFilters = useViewStore((state) => state.assigneeFilters);
  const includeNoAssignee = useViewStore((state) => state.includeNoAssignee);
  const creatorFilters = useViewStore((state) => state.creatorFilters);

  const [searchQuery, setSearchQuery] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [projectId, setProjectId] = useState<string | null>(null);
  const [dueFrom, setDueFrom] = useState("");
  const [dueTo, setDueTo] = useState("");
  const [startFrom, setStartFrom] = useState("");
  const [startTo, setStartTo] = useState("");
  const [endFrom, setEndFrom] = useState("");
  const [endTo, setEndTo] = useState("");

  useEffect(() => {
    registerViewStoreForWorkspaceSync(issueListViewStore);
  }, []);

  useEffect(() => {
    setViewMode("list");
  }, [setViewMode]);

  useEffect(() => {
    const timeoutId = window.setTimeout(() => {
      setDebouncedSearch(searchQuery.trim());
    }, 250);

    return () => window.clearTimeout(timeoutId);
  }, [searchQuery]);

  const queryParams = useMemo(() => {
    return {
      ...(debouncedSearch ? { search: debouncedSearch } : {}),
      ...(projectId ? { project_id: projectId } : {}),
      ...(dueFrom ? { due_from: dueFrom } : {}),
      ...(dueTo ? { due_to: dueTo } : {}),
      ...(startFrom ? { start_from: startFrom } : {}),
      ...(startTo ? { start_to: startTo } : {}),
      ...(endFrom ? { end_from: endFrom } : {}),
      ...(endTo ? { end_to: endTo } : {}),
    };
  }, [debouncedSearch, projectId, dueFrom, dueTo, startFrom, startTo, endFrom, endTo]);

  const issuesQuery = useIssuesListQuery(queryParams);
  const allIssues = issuesQuery.data?.issues ?? [];
  const total = issuesQuery.data?.total ?? 0;

  const scopedIssues = useMemo(() => {
    if (scope === "members") {
      return allIssues.filter((issue) => issue.assignee_type === "member");
    }

    if (scope === "agents") {
      return allIssues.filter((issue) => issue.assignee_type === "agent");
    }

    return allIssues;
  }, [allIssues, scope]);

  const issues = useMemo(
    () =>
      filterIssues(scopedIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
      }),
    [scopedIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters],
  );

  useEffect(() => {
    useIssueSelectionStore.getState().clear();
  }, [
    scope,
    debouncedSearch,
    projectId,
    dueFrom,
    dueTo,
    startFrom,
    startTo,
    endFrom,
    endTo,
    statusFilters,
    priorityFilters,
    assigneeFilters,
    includeNoAssignee,
    creatorFilters,
  ]);

  const resetServerFilters = () => {
    setSearchQuery("");
    setDebouncedSearch("");
    setProjectId(null);
    setDueFrom("");
    setDueTo("");
    setStartFrom("");
    setStartTo("");
    setEndFrom("");
    setEndTo("");
  };

  const hasServerFilters = Boolean(
    debouncedSearch || projectId || dueFrom || dueTo || startFrom || startTo || endFrom || endTo,
  );

  if (issuesQuery.isPending && allIssues.length === 0) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="border-b px-4 py-3">
          <Skeleton className="h-9 w-full" />
        </div>
        <div className="flex flex-1 min-h-0 flex-col gap-2 p-4">
          {Array.from({ length: 8 }).map((_, index) => (
            <Skeleton key={index} className="h-10 w-full rounded-lg" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueTaskStatusSync />

      <div className="border-b px-4 py-4 md:hidden">
        <h1 className="text-base font-semibold">Issues</h1>
        <p className="mt-1 text-xs text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </p>
      </div>

      <div className="hidden h-12 shrink-0 items-center gap-1.5 border-b px-4 md:flex">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">
          {workspace?.name ?? "Workspace"}
        </span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">Issues</span>
      </div>

      <IssueListFiltersRow
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
        statusFilters={statusFilters}
        onToggleStatus={toggleStatusFilter}
        projectId={projectId}
        onProjectChange={setProjectId}
        dueFrom={dueFrom}
        dueTo={dueTo}
        startFrom={startFrom}
        startTo={startTo}
        endFrom={endFrom}
        endTo={endTo}
        onDateChange={(field, value) => {
          switch (field) {
            case "dueFrom":
              setDueFrom(value);
              break;
            case "dueTo":
              setDueTo(value);
              break;
            case "startFrom":
              setStartFrom(value);
              break;
            case "startTo":
              setStartTo(value);
              break;
            case "endFrom":
              setEndFrom(value);
              break;
            case "endTo":
              setEndTo(value);
              break;
          }
        }}
        onReset={resetServerFilters}
        isSearching={issuesQuery.isFetching}
        total={total}
        visibleCount={issues.length}
      />

      <IssuesHeader
        scopedIssues={scopedIssues}
        hideViewToggle
      />

      {issues.length === 0 ? (
        <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 px-6 text-center text-muted-foreground">
          <ListTodo className="h-10 w-10 text-muted-foreground/40" />
          <p className="text-sm">{hasServerFilters ? "No issues match your search" : "No issues match your current filters"}</p>
          <p className="text-xs">
            {hasServerFilters
              ? "Try clearing one or more filters to broaden the result set."
              : "Adjust the current filters or create an issue to get started."}
          </p>
        </div>
      ) : (
        <div className="flex flex-1 min-h-0 flex-col">
          <FlatIssueList issues={issues} />
        </div>
      )}

      <BatchActionToolbar />
    </div>
  );
}

export function IssueListPage() {
  return (
    <ViewStoreProvider store={issueListViewStore}>
      <IssueListPageContent />
    </ViewStoreProvider>
  );
}