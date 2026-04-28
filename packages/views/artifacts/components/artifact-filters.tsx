"use client";

import { Search, X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import type { ArtifactKind, ArtifactScope } from "@multica/core/types";
import { KIND_LABELS, KindIcon } from "./kind-icon";

const ALL_KINDS: ArtifactKind[] = ["report", "plan", "decision", "diagram", "note"];

export interface ArtifactFilterState {
  q: string;
  kind?: ArtifactKind;
  scope?: ArtifactScope;
}

const SCOPE_LABELS: Record<ArtifactScope, string> = {
  all: "All",
  workspace: "Workspace",
  project: "Project",
  issue: "Issue",
};

export function ArtifactFilters({
  value,
  onChange,
  showScope = false,
  className,
}: {
  value: ArtifactFilterState;
  onChange: (next: ArtifactFilterState) => void;
  showScope?: boolean;
  className?: string;
}) {
  const update = (patch: Partial<ArtifactFilterState>) =>
    onChange({ ...value, ...patch });

  const toggleKind = (k: ArtifactKind) => {
    update({ kind: value.kind === k ? undefined : k });
  };

  const setScope = (s: ArtifactScope) => {
    update({ scope: s === "all" ? undefined : s });
  };

  const hasFilters =
    Boolean(value.q) || Boolean(value.kind) || Boolean(value.scope);

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={value.q}
          onChange={(e) => update({ q: e.target.value })}
          placeholder="Search artifacts…"
          className="h-9 pl-8 pr-8"
        />
        {value.q && (
          <button
            type="button"
            onClick={() => update({ q: "" })}
            className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-muted-foreground hover:bg-secondary hover:text-foreground"
            aria-label="Clear search"
          >
            <X className="size-3.5" />
          </button>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        {ALL_KINDS.map((k) => {
          const active = value.kind === k;
          return (
            <button
              key={k}
              type="button"
              onClick={() => toggleKind(k)}
              className={cn(
                "inline-flex items-center gap-1 rounded-full border px-2.5 py-1 text-xs transition-colors",
                active
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-border bg-card hover:bg-muted",
              )}
              aria-pressed={active}
            >
              <KindIcon kind={k} className="size-3.5" />
              {KIND_LABELS[k]}
            </button>
          );
        })}
        {hasFilters && (
          <button
            type="button"
            onClick={() => onChange({ q: "" })}
            className="ml-1 inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <X className="size-3.5" /> Clear
          </button>
        )}
      </div>

      {showScope && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 text-xs text-muted-foreground">Scope:</span>
          {(["all", "workspace", "project", "issue"] as ArtifactScope[]).map(
            (s) => {
              const active = (value.scope ?? "all") === s;
              return (
                <Badge
                  key={s}
                  variant={active ? "default" : "secondary"}
                  className="cursor-pointer text-xs font-normal"
                  render={
                    <button type="button" onClick={() => setScope(s)} />
                  }
                >
                  {SCOPE_LABELS[s]}
                </Badge>
              );
            },
          )}
        </div>
      )}
    </div>
  );
}

/**
 * Client-side filter applied on top of an already-fetched artifact list.
 * Useful for filtering project/issue lists where the API doesn't take filter
 * params; the workspace-level page passes the filter state to the API instead.
 */
export function applyArtifactFilter<T extends { kind: string; title: string; body: string }>(
  artifacts: T[],
  state: ArtifactFilterState,
): T[] {
  const q = state.q.trim().toLowerCase();
  return artifacts.filter((a) => {
    if (state.kind && a.kind !== state.kind) return false;
    if (q) {
      const hay = (a.title + " " + a.body).toLowerCase();
      if (!hay.includes(q)) return false;
    }
    return true;
  });
}
