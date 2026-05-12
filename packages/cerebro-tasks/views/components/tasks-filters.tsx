"use client";

import { useQuery } from "@tanstack/react-query";
import { agentListOptions } from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { useCerebroTasksStore } from "../../core/store";
import type { TaskStatus, TaskTimeRange, TaskType } from "../../core/types";

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
  const setAgentId = useCerebroTasksStore((s) => s.setAgentId);
  const setStatus = useCerebroTasksStore((s) => s.setStatus);
  const setType = useCerebroTasksStore((s) => s.setType);
  const setRange = useCerebroTasksStore((s) => s.setRange);
  const reset = useCerebroTasksStore((s) => s.reset);

  const agents = useQuery(agentListOptions(wsId));
  const agentOptions = agents.data ?? [];

  const hasFilters = !!agentId || !!status || !!type || range !== "all";

  return (
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
