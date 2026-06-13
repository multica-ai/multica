// TECH-3413 #5 — the filter builder: stack conditions (AND) for a "filter"
// section instead of the old fixed toggles. Each active condition is a chip
// you can remove; "+ Betingelse" opens a menu to add more. Empty = show all.
"use client";

import { Plus, X } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import type { Project } from "@multica/core/types";
import type { FilterCondition, FilterField } from "../layout";

export interface FilterBuilderProps {
  filters: FilterCondition[];
  projects: Project[];
  onChange: (next: FilterCondition[]) => void;
}

const BOOLEAN_FIELDS: Array<{ field: FilterField; label: string }> = [
  { field: "unread", label: "Ulæst" },
  { field: "mentioned", label: "Nævner mig" },
  { field: "pinned", label: "Pinned" },
];

function conditionLabel(cond: FilterCondition, projects: Project[]): string {
  switch (cond.field) {
    case "unread":
      return "Ulæst";
    case "mentioned":
      return "Nævner mig";
    case "pinned":
      return "Pinned";
    case "project": {
      const title = projects.find((p) => p.id === cond.projectId)?.title;
      return `Projekt: ${title ?? "ukendt"}`;
    }
    default:
      return cond.field;
  }
}

export function FilterBuilder({ filters, projects, onChange }: FilterBuilderProps) {
  const hasBoolean = (field: FilterField) => filters.some((c) => c.field === field);

  const toggleBoolean = (field: FilterField) => {
    onChange(
      hasBoolean(field)
        ? filters.filter((c) => c.field !== field)
        : [...filters, { field }],
    );
  };

  const setProject = (projectId: string | undefined) => {
    const withoutProject = filters.filter((c) => c.field !== "project");
    onChange(projectId ? [...withoutProject, { field: "project", projectId }] : withoutProject);
  };

  const removeAt = (index: number) => {
    onChange(filters.filter((_, i) => i !== index));
  };

  const activeProjectId = filters.find((c) => c.field === "project")?.projectId;

  return (
    <div className="flex flex-wrap items-center gap-1.5 px-3 py-2">
      {filters.length === 0 && (
        <span className="text-[11px] text-muted-foreground">
          Ingen filtre — viser alt.
        </span>
      )}
      {filters.map((cond, i) => (
        <span
          key={`${cond.field}:${cond.projectId ?? ""}:${i}`}
          className="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2 py-0.5 text-[11px] text-foreground"
        >
          {conditionLabel(cond, projects)}
          <button
            type="button"
            onClick={() => removeAt(i)}
            className="rounded-full text-muted-foreground hover:text-foreground"
            aria-label={`Fjern filter ${conditionLabel(cond, projects)}`}
          >
            <X className="size-3" />
          </button>
        </span>
      ))}

      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-full border border-dashed border-border px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
              title="Tilføj betingelse"
            />
          }
        >
          <Plus className="size-3" /> Betingelse
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-52">
          <DropdownMenuGroup>
            <DropdownMenuLabel>Betingelse</DropdownMenuLabel>
            {BOOLEAN_FIELDS.map((opt) => (
              <DropdownMenuItem key={opt.field} onClick={() => toggleBoolean(opt.field)}>
                {opt.label} {hasBoolean(opt.field) ? "✓" : ""}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          {projects.length > 0 && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Projekt</DropdownMenuLabel>
                <DropdownMenuItem onClick={() => setProject(undefined)}>
                  Intet projekt {!activeProjectId ? "✓" : ""}
                </DropdownMenuItem>
                {projects.map((p) => (
                  <DropdownMenuItem key={p.id} onClick={() => setProject(p.id)}>
                    {p.title} {activeProjectId === p.id ? "✓" : ""}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
