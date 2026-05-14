"use client";

import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Columns3, Search } from "lucide-react";
import { agentListOptions } from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { useCerebroTasksStore } from "../../core/store";
import { COLUMN_DEFS, type ColumnId, type TaskStatus, type TaskTimeRange, type TaskType } from "../../core/types";

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
];

interface TasksFiltersProps {
  wsId: string;
}

export function TasksFilters({ wsId }: TasksFiltersProps) {
  const agentId = useCerebroTasksStore((s) => s.agentId);
  const status = useCerebroTasksStore((s) => s.status);
  const type = useCerebroTasksStore((s) => s.type);
  const range = useCerebroTasksStore((s) => s.range);
  const search = useCerebroTasksStore((s) => s.search);
  const visibleColumns = useCerebroTasksStore((s) => s.visibleColumns);
  const setAgentId = useCerebroTasksStore((s) => s.setAgentId);
  const setStatus = useCerebroTasksStore((s) => s.setStatus);
  const setType = useCerebroTasksStore((s) => s.setType);
  const setRange = useCerebroTasksStore((s) => s.setRange);
  const setSearch = useCerebroTasksStore((s) => s.setSearch);
  const setColumnVisible = useCerebroTasksStore((s) => s.setColumnVisible);
  const reset = useCerebroTasksStore((s) => s.reset);

  const agents = useQuery(agentListOptions(wsId));
  const agentOptions = agents.data ?? [];

  const hasFilters = !!agentId || !!status || !!type || range !== "24h" || !!search;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <div className="relative flex-1 max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            placeholder="Søg i tasks…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-7 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <ColumnToggle visibleColumns={visibleColumns} onToggle={setColumnVisible} />
        {hasFilters && (
          <button
            type="button"
            onClick={reset}
            className="ml-auto rounded-md border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
          >
            Nulstil
          </button>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <FilterGroup label="Agent">
          <select
            aria-label="Filter on agent"
            value={agentId ?? ""}
            onChange={(e) => setAgentId(e.target.value === "" ? null : e.target.value)}
            className="h-7 rounded-md border bg-background px-2 text-xs"
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

        <FilterGroup label="Status">
          <PillRow
            ariaLabel="Filter on status"
            options={STATUSES}
            value={status ?? "all"}
            onChange={(v) => setStatus(v === "all" ? null : (v as TaskStatus))}
          />
        </FilterGroup>

        <FilterGroup label="Type">
          <PillRow
            ariaLabel="Filter on type"
            options={TYPES}
            value={type ?? "all"}
            onChange={(v) => setType(v === "all" ? null : (v as TaskType))}
          />
        </FilterGroup>

        <FilterGroup label="Range">
          <PillRow
            ariaLabel="Filter on time range"
            options={RANGES}
            value={range}
            onChange={(v) => setRange(v as TaskTimeRange)}
          />
        </FilterGroup>
      </div>
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

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
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
      className="inline-flex items-center rounded-md border bg-background p-0.5"
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
