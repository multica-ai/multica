// TECH-3413 #5 / TECH-3502 #3 — the filter builder: stack conditions (AND) for
// a "filter" box. Each active condition is a removable chip; "+ Condition" adds
// boolean/type predicates, and "Project" opens a searchable picker (it does NOT
// list every project as a flat menu) with an "include sub-projects" toggle.
// All copy is English.
"use client";

import { useState } from "react";
import { Plus, X, FolderOpen, Check } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@multica/ui/components/ui/command";
import type { Project } from "@multica/core/types";
import type { FilterCondition, FilterField, EntryKindFilter } from "../layout";

export interface FilterBuilderProps {
  filters: FilterCondition[];
  projects: Project[];
  onChange: (next: FilterCondition[]) => void;
}

// Pure-boolean predicates — toggled on/off from the "+ Condition" menu.
const BOOLEAN_FIELDS: Array<{ field: FilterField; label: string }> = [
  { field: "unread", label: "Unread" },
  { field: "read", label: "Read" },
  { field: "mentioned", label: "Mentions me" },
  { field: "pinned", label: "Pinned" },
  { field: "muted", label: "Muted" },
  { field: "running", label: "Agents working" },
];

// Read/Unread contradict each other, so picking one drops the other.
const OPPOSITE: Partial<Record<FilterField, FilterField>> = {
  unread: "read",
  read: "unread",
};

const KIND_OPTIONS: Array<{ value: EntryKindFilter; label: string }> = [
  { value: "issue", label: "Issues" },
  { value: "chat", label: "Agent chats" },
  { value: "channel", label: "Channels & DMs" },
];

function conditionLabel(cond: FilterCondition, projects: Project[]): string {
  switch (cond.field) {
    case "unread":
      return "Unread";
    case "read":
      return "Read";
    case "mentioned":
      return "Mentions me";
    case "pinned":
      return "Pinned";
    case "muted":
      return "Muted";
    case "running":
      return "Agents working";
    case "kind": {
      const label = KIND_OPTIONS.find((k) => k.value === cond.entryKind)?.label ?? cond.entryKind;
      return `Type: ${label ?? "any"}`;
    }
    case "project": {
      const title = projects.find((p) => p.id === cond.projectId)?.title ?? "unknown";
      return cond.includeSubprojects ? `Project: ${title} + sub-projects` : `Project: ${title}`;
    }
    default:
      return cond.field;
  }
}

export function FilterBuilder({ filters, projects, onChange }: FilterBuilderProps) {
  const [projectOpen, setProjectOpen] = useState(false);

  const hasField = (field: FilterField) => filters.some((c) => c.field === field);

  const toggleBoolean = (field: FilterField) => {
    if (hasField(field)) {
      onChange(filters.filter((c) => c.field !== field));
      return;
    }
    const opp = OPPOSITE[field];
    const base = opp ? filters.filter((c) => c.field !== opp) : filters;
    onChange([...base, { field }]);
  };

  // Only one "type" condition makes sense (a row has exactly one kind).
  const activeKind = filters.find((c) => c.field === "kind")?.entryKind;
  const setKind = (entryKind: EntryKindFilter) => {
    const without = filters.filter((c) => c.field !== "kind");
    onChange(activeKind === entryKind ? without : [...without, { field: "kind", entryKind }]);
  };

  // Only one project narrow at a time (matches the old behaviour).
  const projectCond = filters.find((c) => c.field === "project");
  const setProject = (
    projectId: string | undefined,
    includeSubprojects = projectCond?.includeSubprojects ?? false,
  ) => {
    const without = filters.filter((c) => c.field !== "project");
    onChange(projectId ? [...without, { field: "project", projectId, includeSubprojects }] : without);
  };
  const toggleSubprojects = () => {
    if (!projectCond?.projectId) return;
    setProject(projectCond.projectId, !projectCond.includeSubprojects);
  };

  const removeAt = (index: number) => onChange(filters.filter((_, i) => i !== index));

  return (
    <div className="flex flex-wrap items-center gap-1.5 px-3 py-2">
      {filters.length === 0 && (
        <span className="text-[11px] text-muted-foreground">No filters — showing everything.</span>
      )}

      {filters.map((cond, i) => (
        <span
          key={`${cond.field}:${cond.projectId ?? cond.entryKind ?? ""}:${i}`}
          className="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2 py-0.5 text-[11px] text-foreground"
        >
          {cond.field === "project" ? (
            <button
              type="button"
              onClick={toggleSubprojects}
              className="hover:underline"
              title={
                cond.includeSubprojects
                  ? "Click to match only this project"
                  : "Click to also match sub-projects"
              }
            >
              {conditionLabel(cond, projects)}
            </button>
          ) : (
            conditionLabel(cond, projects)
          )}
          <button
            type="button"
            onClick={() => removeAt(i)}
            className="rounded-full text-muted-foreground hover:text-foreground"
            aria-label={`Remove filter ${conditionLabel(cond, projects)}`}
          >
            <X className="size-3" />
          </button>
        </span>
      ))}

      {/* + Condition — booleans and the single "type" narrow. */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-full border border-dashed border-border px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
              title="Add condition"
            />
          }
        >
          <Plus className="size-3" /> Condition
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-52">
          <DropdownMenuGroup>
            <DropdownMenuLabel>Status</DropdownMenuLabel>
            {BOOLEAN_FIELDS.map((opt) => (
              <DropdownMenuItem key={opt.field} onClick={() => toggleBoolean(opt.field)}>
                {opt.label} {hasField(opt.field) ? "✓" : ""}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>Type</DropdownMenuLabel>
            {KIND_OPTIONS.map((opt) => (
              <DropdownMenuItem key={opt.value} onClick={() => setKind(opt.value)}>
                {opt.label} {activeKind === opt.value ? "✓" : ""}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Project — searchable picker, not a flat list of every project. */}
      {projects.length > 0 && (
        <Popover open={projectOpen} onOpenChange={setProjectOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="inline-flex items-center gap-1 rounded-full border border-dashed border-border px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
                title="Filter by project"
              />
            }
          >
            <FolderOpen className="size-3" /> Project
          </PopoverTrigger>
          <PopoverContent align="start" className="w-64 p-0">
            <Command>
              <CommandInput placeholder="Search projects…" />
              <CommandList>
                <CommandEmpty>No projects found.</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    value="__none__ no project clear"
                    onSelect={() => {
                      setProject(undefined);
                      setProjectOpen(false);
                    }}
                  >
                    <span className="text-muted-foreground">No project filter</span>
                    {!projectCond?.projectId && <Check className="ml-auto size-3.5" />}
                  </CommandItem>
                  {projects.map((p) => (
                    <CommandItem
                      key={p.id}
                      value={`${p.title} ${p.id}`}
                      onSelect={() => {
                        setProject(p.id);
                        setProjectOpen(false);
                      }}
                    >
                      <span className="truncate">{p.title}</span>
                      {projectCond?.projectId === p.id && <Check className="ml-auto size-3.5" />}
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
              {projectCond?.projectId && (
                <div className="border-t border-border p-2">
                  <button
                    type="button"
                    onClick={toggleSubprojects}
                    className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-[12px] hover:bg-accent"
                  >
                    <span
                      className={`flex size-4 items-center justify-center rounded border ${
                        projectCond.includeSubprojects
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-border"
                      }`}
                    >
                      {projectCond.includeSubprojects && <Check className="size-3" />}
                    </span>
                    Include sub-projects
                  </button>
                </div>
              )}
            </Command>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}
