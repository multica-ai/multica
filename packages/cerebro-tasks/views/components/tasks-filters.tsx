"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Columns3, Search, SlidersHorizontal, X } from "lucide-react";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import { issueListOptions } from "@multica/core/issues/queries";
import { cn } from "@multica/ui/lib/utils";
import { useCerebroTasksStore } from "../../core/store";
import {
  COLUMN_DEFS,
  type ColumnId,
  type GroupBy,
  type TaskStatus,
  type TaskTimeRange,
  type TaskType,
} from "../../core/types";

const STATUSES: { value: TaskStatus | "all"; label: string }[] = [
  { value: "all", label: "Alle" },
  { value: "queued", label: "Queued" },
  { value: "dispatched", label: "Dispatched" },
  { value: "running", label: "Running" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
];

const TYPES: { value: TaskType | "all"; label: string }[] = [
  { value: "all", label: "Alle" },
  { value: "issue", label: "Issue" },
  { value: "chat", label: "Chat" },
];

const RANGES: { value: TaskTimeRange; label: string }[] = [
  { value: "all", label: "Alle" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "custom", label: "Custom" },
];

const GROUP_BY_OPTIONS: { value: GroupBy; label: string }[] = [
  { value: "none", label: "Ingen" },
  { value: "issue", label: "Issue" },
  { value: "project", label: "Projekt" },
  { value: "parent_issue", label: "Parent issue" },
];

interface TasksFiltersProps {
  wsId: string;
}

export function TasksFilters({ wsId }: TasksFiltersProps) {
  const agentId = useCerebroTasksStore((s) => s.agentId);
  const issueId = useCerebroTasksStore((s) => s.issueId);
  const projectId = useCerebroTasksStore((s) => s.projectId);
  const status = useCerebroTasksStore((s) => s.status);
  const type = useCerebroTasksStore((s) => s.type);
  const range = useCerebroTasksStore((s) => s.range);
  const customFrom = useCerebroTasksStore((s) => s.customFrom);
  const customTo = useCerebroTasksStore((s) => s.customTo);
  const search = useCerebroTasksStore((s) => s.search);
  const groupBy = useCerebroTasksStore((s) => s.groupBy);
  const visibleColumns = useCerebroTasksStore((s) => s.visibleColumns);
  const setAgentId = useCerebroTasksStore((s) => s.setAgentId);
  const setIssueId = useCerebroTasksStore((s) => s.setIssueId);
  const setProjectId = useCerebroTasksStore((s) => s.setProjectId);
  const setStatus = useCerebroTasksStore((s) => s.setStatus);
  const setType = useCerebroTasksStore((s) => s.setType);
  const setRange = useCerebroTasksStore((s) => s.setRange);
  const setCustomFrom = useCerebroTasksStore((s) => s.setCustomFrom);
  const setCustomTo = useCerebroTasksStore((s) => s.setCustomTo);
  const setSearch = useCerebroTasksStore((s) => s.setSearch);
  const setGroupBy = useCerebroTasksStore((s) => s.setGroupBy);
  const setColumnVisible = useCerebroTasksStore((s) => s.setColumnVisible);
  const reset = useCerebroTasksStore((s) => s.reset);
  const [draftSearch, setDraftSearch] = useState(search);

  useEffect(() => {
    setDraftSearch(search);
  }, [search]);

  useEffect(() => {
    const nextSearch = draftSearch.trim();
    const timer = window.setTimeout(() => {
      if (nextSearch !== search) setSearch(nextSearch);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [draftSearch, search, setSearch]);

  const agents = useQuery(agentListOptions(wsId));
  const projects = useQuery(projectListOptions(wsId));
  const issues = useQuery(issueListOptions(wsId));
  const agentOptions = agents.data ?? [];
  const projectOptions = projects.data ?? [];
  const issueOptions = issues.data ?? [];

  const hasFilters =
    !!agentId ||
    !!issueId ||
    !!projectId ||
    !!status ||
    !!type ||
    range !== "24h" ||
    !!search ||
    groupBy !== "none";

  return (
    <div className="flex flex-col gap-3 rounded-md border bg-background p-2 sm:border-0 sm:p-0">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[180px] flex-1 sm:max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            placeholder="Søg i tasks…"
            value={draftSearch}
            onChange={(e) => setDraftSearch(e.target.value)}
            className="h-7 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <ColumnToggle visibleColumns={visibleColumns} onToggle={setColumnVisible} />
        {hasFilters && (
          <button
            type="button"
            onClick={reset}
            className="rounded-md border px-2 py-1 text-xs text-muted-foreground hover:text-foreground sm:ml-auto"
          >
            Nulstil
          </button>
        )}
      </div>

      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:gap-3">
        <div className="-mx-2 flex items-center gap-1.5 overflow-x-auto px-2 pb-1 sm:mx-0 sm:overflow-visible sm:px-0 sm:pb-0">
          <FilterGroup label="Status" compact>
            <PillRow
              ariaLabel="Filter on status"
              options={STATUSES}
              value={status ?? "all"}
              onChange={(v) => setStatus(v === "all" ? null : (v as TaskStatus))}
            />
          </FilterGroup>
        </div>

        <div className="-mx-2 flex items-center gap-1.5 overflow-x-auto px-2 pb-1 sm:mx-0 sm:overflow-visible sm:px-0 sm:pb-0">
          <FilterGroup label="Range" compact>
            <PillRow
              ariaLabel="Filter on time range"
              options={RANGES}
              value={range}
              onChange={(v) => setRange(v as TaskTimeRange)}
            />
          </FilterGroup>
        </div>
      </div>

      <details className="group sm:hidden">
        <summary className="flex h-8 cursor-pointer list-none items-center justify-between rounded-md border px-2 text-xs font-medium">
          <span className="inline-flex items-center gap-1.5">
            <SlidersHorizontal className="h-3.5 w-3.5" />
            Flere filtre
          </span>
          <span className="text-muted-foreground group-open:hidden">Vis</span>
          <span className="hidden text-muted-foreground group-open:inline">Skjul</span>
        </summary>
        <div className="mt-2 flex flex-col gap-2">
          <AdvancedFilters
            agentOptions={agentOptions}
            projectOptions={projectOptions}
            issueOptions={issueOptions}
            agentId={agentId}
            issueId={issueId}
            projectId={projectId}
            type={type}
            range={range}
            customFrom={customFrom}
            customTo={customTo}
            groupBy={groupBy}
            setAgentId={setAgentId}
            setIssueId={setIssueId}
            setProjectId={setProjectId}
            setType={setType}
            setCustomFrom={setCustomFrom}
            setCustomTo={setCustomTo}
            setGroupBy={setGroupBy}
          />
        </div>
      </details>

      <div className="hidden flex-wrap items-center gap-2 sm:flex">
        <AdvancedFilters
          agentOptions={agentOptions}
          projectOptions={projectOptions}
          issueOptions={issueOptions}
          agentId={agentId}
          issueId={issueId}
          projectId={projectId}
          type={type}
          range={range}
          customFrom={customFrom}
          customTo={customTo}
          groupBy={groupBy}
          setAgentId={setAgentId}
          setIssueId={setIssueId}
          setProjectId={setProjectId}
          setType={setType}
          setCustomFrom={setCustomFrom}
          setCustomTo={setCustomTo}
          setGroupBy={setGroupBy}
        />
      </div>
    </div>
  );
}

interface Issue {
  id: string;
  title?: string;
  number?: number;
}

function AdvancedFilters({
  agentOptions,
  projectOptions,
  issueOptions,
  agentId,
  issueId,
  projectId,
  type,
  range,
  customFrom,
  customTo,
  groupBy,
  setAgentId,
  setIssueId,
  setProjectId,
  setType,
  setCustomFrom,
  setCustomTo,
  setGroupBy,
}: {
  agentOptions: Array<{ id: string; name: string; archived_at?: string | null }>;
  projectOptions: Array<{ id: string; title: string }>;
  issueOptions: Issue[];
  agentId: string | null;
  issueId: string | null;
  projectId: string | null;
  type: TaskType | null;
  range: TaskTimeRange;
  customFrom: string | null;
  customTo: string | null;
  groupBy: GroupBy;
  setAgentId: (id: string | null) => void;
  setIssueId: (id: string | null) => void;
  setProjectId: (id: string | null) => void;
  setType: (type: TaskType | null) => void;
  setCustomFrom: (date: string | null) => void;
  setCustomTo: (date: string | null) => void;
  setGroupBy: (groupBy: GroupBy) => void;
}) {
  return (
    <>
        <FilterGroup label="Agent">
          <select
            aria-label="Filter on agent"
            value={agentId ?? ""}
            onChange={(e) => setAgentId(e.target.value === "" ? null : e.target.value)}
            className="h-7 min-w-0 rounded-md border bg-background px-2 text-xs"
          >
            <option value="">Alle agenter</option>
            {agentOptions
              .filter((a) => !a.archived_at)
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
        </FilterGroup>

        <FilterGroup label="Issue">
          <IssueCombobox
            issues={issueOptions}
            value={issueId}
            onChange={setIssueId}
          />
        </FilterGroup>

        <FilterGroup label="Projekt">
          <select
            aria-label="Filter on project"
            value={projectId ?? ""}
            onChange={(e) => setProjectId(e.target.value === "" ? null : e.target.value)}
            className="h-7 min-w-0 rounded-md border bg-background px-2 text-xs"
          >
            <option value="">Alle projekter</option>
            {projectOptions.map((p) => (
              <option key={p.id} value={p.id}>
                {p.title}
              </option>
            ))}
          </select>
        </FilterGroup>

        <FilterGroup label="Type">
          <PillRow
            ariaLabel="Filter on type"
            options={TYPES}
            value={type ?? "all"}
            onChange={(v) => setType(v === "all" ? null : (v as TaskType))}
          />
        </FilterGroup>

        {range === "custom" && (
          <FilterGroup label="Dato">
            <div className="flex items-center gap-1">
              <input
                type="date"
                aria-label="Fra dato"
                value={customFrom ? customFrom.slice(0, 10) : ""}
                onChange={(e) =>
                  setCustomFrom(e.target.value ? new Date(e.target.value).toISOString() : null)
                }
                className="h-7 rounded-md border bg-background px-2 text-xs"
              />
              <span className="text-xs text-muted-foreground">—</span>
              <input
                type="date"
                aria-label="Til dato"
                value={customTo ? customTo.slice(0, 10) : ""}
                onChange={(e) => {
                  if (e.target.value) {
                    const d = new Date(e.target.value);
                    d.setHours(23, 59, 59, 999);
                    setCustomTo(d.toISOString());
                  } else {
                    setCustomTo(null);
                  }
                }}
                className="h-7 rounded-md border bg-background px-2 text-xs"
              />
            </div>
          </FilterGroup>
        )}

        <FilterGroup label="Gruppér på">
          <PillRow
            ariaLabel="Group by"
            options={GROUP_BY_OPTIONS}
            value={groupBy}
            onChange={(v) => setGroupBy(v as GroupBy)}
          />
        </FilterGroup>
    </>
  );
}

function IssueCombobox({
  issues,
  value,
  onChange,
}: {
  issues: Issue[];
  value: string | null;
  onChange: (id: string | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const selectedIssue = value ? issues.find((i) => i.id === value) : null;
  const displayValue = selectedIssue
    ? `#${selectedIssue.number} ${selectedIssue.title ?? ""}`.trim()
    : "";

  const filtered = query.trim()
    ? issues.filter(
        (i) =>
          i.title?.toLowerCase().includes(query.toLowerCase()) ||
          String(i.number).includes(query),
      )
    : issues;

  const visibleOptions = filtered.slice(0, 50);

  function selectIssue(id: string | null) {
    onChange(id);
    setQuery("");
    setOpen(false);
  }

  return (
    <div className="relative" ref={containerRef}>
      <div className="relative flex h-7 items-center">
        <input
          ref={inputRef}
          type="text"
          placeholder="Alle issues"
          value={open ? query : displayValue}
          onChange={(e) => setQuery(e.target.value)}
          onFocus={() => {
            setOpen(true);
            setQuery("");
          }}
          onBlur={() => {
            setTimeout(() => setOpen(false), 150);
          }}
          className="h-7 w-full min-w-[160px] rounded-md border bg-background pl-2 pr-6 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring sm:w-44"
        />
        {value && (
          <button
            type="button"
            onMouseDown={(e) => {
              e.preventDefault();
              selectIssue(null);
            }}
            className="absolute right-1 flex h-4 w-4 items-center justify-center rounded-sm text-muted-foreground hover:text-foreground"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {open && (
        <div className="absolute left-0 top-full z-20 mt-1 max-h-48 w-[min(20rem,calc(100vw-2rem))] overflow-y-auto rounded-md border bg-popover shadow-md sm:w-64">
          <div
            className="px-2 py-1 text-xs text-muted-foreground hover:bg-accent cursor-pointer"
            onMouseDown={(e) => {
              e.preventDefault();
              selectIssue(null);
            }}
          >
            Alle issues
          </div>
          {visibleOptions.length === 0 ? (
            <div className="px-2 py-1 text-xs text-muted-foreground">Ingen resultater</div>
          ) : (
            visibleOptions.map((issue) => (
              <div
                key={issue.id}
                onMouseDown={(e) => {
                  e.preventDefault();
                  selectIssue(issue.id);
                }}
                className={cn(
                  "flex cursor-pointer items-center gap-1.5 px-2 py-1 text-xs hover:bg-accent",
                  value === issue.id && "bg-accent",
                )}
              >
                <span className="shrink-0 rounded-sm bg-muted px-1 py-0.5 text-[10px] font-medium text-muted-foreground">
                  #{issue.number}
                </span>
                <span className="truncate">{issue.title}</span>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ColumnToggle({
  visibleColumns,
  onToggle,
}: {
  visibleColumns: Record<ColumnId, boolean>;
  onToggle: (col: ColumnId, visible: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-label="Vælg kolonner"
        className={cn(
          "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs text-muted-foreground hover:text-foreground",
          open && "bg-muted text-foreground",
        )}
      >
        <Columns3 className="h-3 w-3" />
        Kolonner
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute left-0 top-full z-20 mt-1 min-w-[140px] rounded-md border bg-popover p-1.5 shadow-md">
            {COLUMN_DEFS.map((col) => (
              <label
                key={col.id}
                className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1 text-xs hover:bg-accent"
              >
                <input
                  type="checkbox"
                  checked={visibleColumns[col.id]}
                  onChange={(e) => onToggle(col.id, e.target.checked)}
                  className="h-3 w-3 accent-primary"
                />
                {col.label}
              </label>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function FilterGroup({
  label,
  children,
  compact = false,
}: {
  label: string;
  children: React.ReactNode;
  compact?: boolean;
}) {
  return (
    <div className={cn("flex min-w-0 items-center gap-1.5", !compact && "max-sm:justify-between")}>
      <span className="shrink-0 text-[11px] uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      {children}
    </div>
  );
}

interface PillRowProps<T extends string> {
  ariaLabel: string;
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
}

function PillRow<T extends string>({ ariaLabel, options, value, onChange }: PillRowProps<T>) {
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className="inline-flex min-w-max items-center rounded-md border bg-background p-0.5"
    >
      {options.map((opt) => {
        const active = value === opt.value;
        return (
          <button
            key={opt.value}
            role="radio"
            aria-checked={active}
            type="button"
            onClick={() => onChange(opt.value)}
            className={cn(
              "rounded-sm px-2 py-0.5 text-[11px] font-medium transition-colors",
              active
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
