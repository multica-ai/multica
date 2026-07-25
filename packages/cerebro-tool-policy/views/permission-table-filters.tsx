"use client";

import type { ReactNode } from "react";
import { Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import {
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolPolicyRow,
} from "../core";
import {
  permissionType,
  type PermissionType,
} from "./capability-presentation";
import { SETTING_LABEL } from "./permission-attribution";

const DECISION_FILTERS: ToolEffectiveSetting[] = ["allow", "ask", "deny"];

export interface FilterState {
  search: string;
  types: Set<PermissionType>;
  decisions: Set<ToolEffectiveSetting>;
  showInherited: boolean;
  editLayer: ToolLayer;
}

// Each facet is OR within itself and AND across facets. An empty set means all.
export function filterRows(
  rows: ToolPolicyRow[],
  filters: FilterState,
): ToolPolicyRow[] {
  const query = filters.search.trim().toLowerCase();
  return rows.filter((row) => {
    if (
      query &&
      !`${row.title} ${row.tool_key} ${row.category}`
        .toLowerCase()
        .includes(query)
    ) {
      return false;
    }
    if (filters.types.size && !filters.types.has(permissionType(row))) {
      return false;
    }
    if (
      filters.decisions.size &&
      !filters.decisions.has(row.effective.setting)
    ) {
      return false;
    }
    if (!filters.showInherited && !row.layers[filters.editLayer]) return false;
    return true;
  });
}

export function CatalogHeader({
  shown,
  total,
  busy,
  onBulk,
}: {
  shown: number;
  total: number;
  busy: boolean;
  onBulk: (setting: "allow" | "deny") => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-base font-semibold">All tools</h3>
          <p className="max-w-xl text-sm text-muted-foreground">
            Permissions attach at the tool level; filter by type or decision.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground">
            {shown === total ? `${total} tools` : `${shown} of ${total} tools`}
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => onBulk("allow")}
          >
            Allow all
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => onBulk("deny")}
          >
            Deny all
          </Button>
        </div>
      </div>
    </div>
  );
}

export function FilterBar({
  search,
  onSearch,
  typeFacets,
  types,
  onToggleType,
  decisions,
  onToggleDecision,
  showInherited,
  onShowInherited,
  editLayerLabel,
}: {
  search: string;
  onSearch: (value: string) => void;
  typeFacets: PermissionType[];
  types: Set<PermissionType>;
  onToggleType: (type: PermissionType) => void;
  decisions: Set<ToolEffectiveSetting>;
  onToggleDecision: (decision: ToolEffectiveSetting) => void;
  showInherited: boolean;
  onShowInherited: (value: boolean) => void;
  editLayerLabel: string;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative w-full sm:max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => onSearch(event.target.value)}
            placeholder="Filter tools by name…"
            className="h-9 pl-9"
            aria-label="Filter tools"
          />
        </div>
      </div>

      {typeFacets.length > 1 ? (
        <FilterGroup label="Type">
          {typeFacets.map((type) => (
            <FilterChip
              key={type}
              active={types.has(type)}
              onClick={() => onToggleType(type)}
            >
              {type}
            </FilterChip>
          ))}
        </FilterGroup>
      ) : null}

      <div className="flex flex-wrap items-center gap-3">
        <FilterGroup label="Decision">
          {DECISION_FILTERS.map((decision) => (
            <FilterChip
              key={decision}
              active={decisions.has(decision)}
              onClick={() => onToggleDecision(decision)}
            >
              {SETTING_LABEL[decision]}
            </FilterChip>
          ))}
        </FilterGroup>
        <label className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
          <Checkbox
            checked={showInherited}
            onCheckedChange={(value) => onShowInherited(value === true)}
            aria-label="Show inherited"
          />
          Show inherited
          <span className="text-xs">
            (off = only {editLayerLabel} overrides)
          </span>
        </label>
      </div>
    </div>
  );
}

function FilterGroup({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      {children}
    </div>
  );
}

function FilterChip({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors",
        active
          ? "border-primary bg-primary/10 text-foreground"
          : "border-border bg-background text-muted-foreground hover:bg-muted",
      )}
    >
      {children}
    </button>
  );
}
