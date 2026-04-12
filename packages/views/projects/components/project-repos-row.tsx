"use client";

import { useMemo, useState } from "react";
import { Check, FolderGit2, FolderOpen, Plus, X } from "lucide-react";
import { useWorkspaceStore } from "@multica/core/workspace";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";

// PropRow-compatible row (must match the sibling PropRow in project-detail.tsx).
function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-8 items-start gap-2 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors py-1">
      <span className="mt-0.5 w-16 shrink-0 text-xs text-muted-foreground">
        {label}
      </span>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1 text-xs">
        {children}
      </div>
    </div>
  );
}

/**
 * ProjectReposRow renders a sidebar row that lets a user link workspace
 * repos to a project. The first repo in the list is the agent's default
 * start directory for any task in this project, so order matters.
 *
 * Interaction: click the "+" chip to open a popover with a filterable list
 * of workspace repos; toggling an entry adds or removes it. Remove existing
 * chips via the × button. All changes route through the single onChange
 * callback so the parent can persist via updateProject.
 */
export function ProjectReposRow({
  repoIds,
  onChange,
}: {
  projectId: string;
  repoIds: string[];
  onChange: (ids: string[]) => void;
}) {
  const workspace = useWorkspaceStore((s) => s.workspace);
  const allRepos = workspace?.repos ?? [];
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const linked = useMemo(
    () => repoIds.map((id) => allRepos.find((r) => r.id === id)).filter(Boolean),
    [repoIds, allRepos],
  );

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return allRepos;
    return allRepos.filter(
      (r) =>
        r.name.toLowerCase().includes(q) ||
        (r.local_path ?? "").toLowerCase().includes(q) ||
        (r.url ?? "").toLowerCase().includes(q),
    );
  }, [allRepos, filter]);

  const toggle = (id: string) => {
    if (repoIds.includes(id)) {
      onChange(repoIds.filter((x) => x !== id));
    } else {
      onChange([...repoIds, id]);
    }
  };

  return (
    <Row label="Repos">
      {linked.length === 0 && (
        <span className="text-muted-foreground">None linked</span>
      )}
      {linked.map(
        (r) =>
          r && (
            <Badge
              key={r.id}
              variant="secondary"
              className="h-5 gap-1 pr-1 text-[10px]"
            >
              {r.type === "local" ? (
                <FolderOpen className="h-2.5 w-2.5" />
              ) : (
                <FolderGit2 className="h-2.5 w-2.5" />
              )}
              {r.name}
              <button
                type="button"
                onClick={() => toggle(r.id)}
                className="ml-0.5 rounded-sm p-0.5 hover:bg-destructive/20 hover:text-destructive"
                aria-label={`Unlink ${r.name}`}
              >
                <X className="h-2.5 w-2.5" />
              </button>
            </Badge>
          ),
      )}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="inline-flex h-5 items-center gap-1 rounded-md border border-dashed px-1.5 text-[10px] text-muted-foreground hover:text-foreground"
            >
              <Plus className="h-2.5 w-2.5" />
              Add repo
            </button>
          }
        />
        <PopoverContent align="start" className="w-60 p-0">
          <div className="border-b px-2 py-1.5">
            <input
              type="text"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter repos..."
              className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
            />
          </div>
          <div className="max-h-60 overflow-y-auto p-1">
            {filtered.length === 0 && (
              <div className="px-2 py-3 text-center text-xs text-muted-foreground">
                No matches
              </div>
            )}
            {filtered.map((r) => {
              const active = repoIds.includes(r.id);
              return (
                <button
                  key={r.id}
                  type="button"
                  onClick={() => toggle(r.id)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors",
                    active
                      ? "bg-accent text-accent-foreground"
                      : "hover:bg-accent/50",
                  )}
                >
                  {r.type === "local" ? (
                    <FolderOpen className="h-3 w-3 shrink-0 text-muted-foreground" />
                  ) : (
                    <FolderGit2 className="h-3 w-3 shrink-0 text-muted-foreground" />
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{r.name}</div>
                    <div className="truncate text-[10px] text-muted-foreground">
                      {r.type === "local" ? r.local_path : r.url}
                    </div>
                  </div>
                  {active && <Check className="h-3 w-3 shrink-0" />}
                </button>
              );
            })}
          </div>
        </PopoverContent>
      </Popover>
    </Row>
  );
}
