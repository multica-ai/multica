"use client";

import { useEffect, useMemo, useState } from "react";
import type { DateRange } from "react-day-picker";
import {
  CalendarRange,
  ChevronRight,
  Hash,
  ListTodo,
  Search,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  STATUS_CONFIG,
  PRIORITY_CONFIG,
  PRIORITY_ORDER,
} from "@/features/issues/config";
import { useIssuesListQuery, useWorkspaceLabelsQuery } from "@/features/issues/queries";
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
import { IssueLabelFilter } from "@/features/issues/components/issue-label-filter";
import { StatusIcon } from "@/features/issues/components";
import { PriorityIcon } from "@/features/issues/components/priority-icon";
import { ProjectPicker } from "@/features/projects/components/project-picker";
import { useWorkspaceStore, WorkspaceAvatar } from "@/features/workspace";

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

/** A single removable filter chip */
function FilterChip({
  icon,
  label,
  onRemove,
}: {
  icon?: React.ReactNode;
  label: string;
  onRemove: () => void;
}) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border bg-muted/60 px-2 py-0.5 text-xs text-foreground">
      {icon}
      {label}
      <button
        type="button"
        onClick={onRemove}
        className="ml-0.5 rounded-sm text-muted-foreground transition-colors hover:text-foreground"
        aria-label={`Remove ${label} filter`}
      >
        <X className="size-3" />
      </button>
    </span>
  );
}

function IssueListFiltersRow({
  searchQuery,
  onSearchChange,
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
  // Read all view-store filters directly (this component is inside ViewStoreProvider)
  const statusFilters = useViewStore((s) => s.statusFilters);
  const toggleStatusFilter = useViewStore((s) => s.toggleStatusFilter);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const togglePriorityFilter = useViewStore((s) => s.togglePriorityFilter);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const toggleAssigneeFilter = useViewStore((s) => s.toggleAssigneeFilter);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const toggleCreatorFilter = useViewStore((s) => s.toggleCreatorFilter);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const labelFilterMode = useViewStore((s) => s.labelFilterMode);
  const toggleLabelFilter = useViewStore((s) => s.toggleLabelFilter);
  const setLabelFilterMode = useViewStore((s) => s.setLabelFilterMode);
  const clearLabelFilters = useViewStore((s) => s.clearLabelFilters);
  const clearViewFilters = useViewStore((s) => s.clearFilters);

  // Resolve actor display names
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const { data: workspaceLabels = [] } = useWorkspaceLabelsQuery();

  const getActorName = (type: "member" | "agent", id: string) => {
    if (type === "member") return members.find((m) => m.user_id === id)?.name ?? id;
    return agents.find((a) => a.id === id)?.name ?? id;
  };

  const activeDateFilterCount = [dueFrom, dueTo, startFrom, startTo, endFrom, endTo].filter(Boolean).length;
  const labelById = useMemo(() => new Map(workspaceLabels.map((label) => [label.id, label])), [workspaceLabels]);
  const hasViewFilters =
    statusFilters.length > 0 ||
    priorityFilters.length > 0 ||
    assigneeFilters.length > 0 ||
    creatorFilters.length > 0 ||
    labelFilters.length > 0;
  const hasServerFilters = Boolean(searchQuery.trim() || projectId || activeDateFilterCount > 0);
  const hasAnyFilters = hasViewFilters || hasServerFilters;

  const handleClearAll = () => {
    clearViewFilters();
    onReset();
  };

  return (
    <div className="border-b px-4 py-3 space-y-3">
      {/* Search + project + date row */}
      <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
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

          <IssueLabelFilter
            selectedIds={labelFilters}
            mode={labelFilterMode}
            onToggle={toggleLabelFilter}
            onModeChange={setLabelFilterMode}
            onClear={clearLabelFilters}
          />
        </div>
      </div>

      {/* Active filter chips */}
      {hasAnyFilters && (
        <div className="flex flex-wrap items-center gap-1.5">
          {/* Status chips */}
          {statusFilters.map((status) => (
            <FilterChip
              key={`status-${status}`}
              icon={<StatusIcon status={status} className="size-3" />}
              label={STATUS_CONFIG[status].label}
              onRemove={() => toggleStatusFilter(status)}
            />
          ))}

          {/* Priority chips */}
          {priorityFilters.map((priority) => (
            <FilterChip
              key={`priority-${priority}`}
              icon={
                <span className={`inline-flex ${PRIORITY_CONFIG[priority].badgeText}`}>
                  <PriorityIcon priority={priority} className="size-3" inheritColor />
                </span>
              }
              label={PRIORITY_CONFIG[priority].label}
              onRemove={() => togglePriorityFilter(priority)}
            />
          ))}

          {/* Assignee chips */}
          {assigneeFilters.map((filter) => (
            <FilterChip
              key={`assignee-${filter.type}-${filter.id}`}
              label={`Assignee: ${getActorName(filter.type, filter.id)}`}
              onRemove={() => toggleAssigneeFilter(filter)}
            />
          ))}

          {/* Creator chips */}
          {creatorFilters.map((filter) => (
            <FilterChip
              key={`creator-${filter.type}-${filter.id}`}
              label={`Creator: ${getActorName(filter.type, filter.id)}`}
              onRemove={() => toggleCreatorFilter(filter)}
            />
          ))}

          {labelFilters.map((labelId) => (
            <FilterChip
              key={`label-${labelId}`}
              icon={<Hash className="size-3" />}
              label={`Label: ${labelById.get(labelId)?.name ?? labelId}`}
              onRemove={() => toggleLabelFilter(labelId)}
            />
          ))}

          {labelFilters.length > 1 && labelFilterMode === "all" && (
            <FilterChip
              label="Match all labels"
              onRemove={() => setLabelFilterMode("any")}
            />
          )}

          {/* Date chips */}
          {(dueFrom || dueTo) && (
            <FilterChip
              label={`Due: ${formatDateRangeSummary(dueFrom, dueTo)}`}
              onRemove={() => {
                onDateChange("dueFrom", "");
                onDateChange("dueTo", "");
              }}
            />
          )}
          {(startFrom || startTo) && (
            <FilterChip
              label={`Start: ${formatDateRangeSummary(startFrom, startTo)}`}
              onRemove={() => {
                onDateChange("startFrom", "");
                onDateChange("startTo", "");
              }}
            />
          )}
          {(endFrom || endTo) && (
            <FilterChip
              label={`End: ${formatDateRangeSummary(endFrom, endTo)}`}
              onRemove={() => {
                onDateChange("endFrom", "");
                onDateChange("endTo", "");
              }}
            />
          )}

          <button
            type="button"
            onClick={handleClearAll}
            className="text-xs text-muted-foreground underline-offset-2 hover:underline"
          >
            Clear all
          </button>
        </div>
      )}

      {/* Stats row */}
      <div className="flex items-center text-xs text-muted-foreground">
        <span>
          {isSearching
            ? "Searching issues..."
            : `${visibleCount} visible · ${total} matched`}
        </span>
      </div>
    </div>
  );
}

function IssueListPageContent({
  archived = false,
}: {
  archived?: boolean;
}) {
  const workspace = useWorkspaceStore((state) => state.workspace);
  const scope = useIssuesScopeStore((state) => state.scope);
  const setViewMode = useViewStore((state) => state.setViewMode);
  const statusFilters = useViewStore((state) => state.statusFilters);
  const priorityFilters = useViewStore((state) => state.priorityFilters);
  const assigneeFilters = useViewStore((state) => state.assigneeFilters);
  const includeNoAssignee = useViewStore((state) => state.includeNoAssignee);
  const creatorFilters = useViewStore((state) => state.creatorFilters);
  const labelFilters = useViewStore((state) => state.labelFilters);
  const labelFilterMode = useViewStore((state) => state.labelFilterMode);

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
      ...(labelFilters.length > 0 ? { label_ids: [...labelFilters].sort(), label_match_mode: labelFilterMode } : {}),
      ...(dueFrom ? { due_from: dueFrom } : {}),
      ...(dueTo ? { due_to: dueTo } : {}),
      ...(startFrom ? { start_from: startFrom } : {}),
      ...(startTo ? { start_to: startTo } : {}),
      ...(endFrom ? { end_from: endFrom } : {}),
      ...(endTo ? { end_to: endTo } : {}),
      ...(archived ? { archived: true } : {}),
    };
  }, [archived, debouncedSearch, projectId, labelFilters, labelFilterMode, dueFrom, dueTo, startFrom, startTo, endFrom, endTo]);

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
    labelFilters,
    labelFilterMode,
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
    debouncedSearch || projectId || labelFilters.length > 0 || dueFrom || dueTo || startFrom || startTo || endFrom || endTo,
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
        <span className="text-sm font-medium">{archived ? "Archived Issues" : "Issues"}</span>
      </div>

      <IssueListFiltersRow
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
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
          <p className="text-sm">{archived ? "No archived issues" : hasServerFilters ? "No issues match your search" : "No issues match your current filters"}</p>
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

export function IssueListPage({ archived = false }: { archived?: boolean } = {}) {
  return (
    <ViewStoreProvider store={issueListViewStore}>
      <IssueListPageContent archived={archived} />
    </ViewStoreProvider>
  );
}
