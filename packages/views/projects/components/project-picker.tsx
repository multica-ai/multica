"use client";

// CEREBRO-PATCH(SA3-project-picker): L3-marked (cannot wrap cleanly).
// Two intertwined deviations from upstream:
//   1) Replaces ProjectIcon with inline icon/color-dot rendering
//   2) Adds RestrictedLock indicator after icon, in both trigger and items
// CEREBRO-PATCH(project-picker-search-and-select): FIR-2536.
//   3) Search-and-select via Popover (not DropdownMenu) with a filter input
//      at the top of the dropdown. Flattens the tree while filtering so
//      matches in nested sub-projects surface to the top level.
// No injection slot exists inside the trigger/item children — wrapping
// would require duplicating the entire JSX tree, eliminating upstream-tracking
// benefit. Resolution deferred to chunk 11 (likely path-alias shadow with
// cerebro-access/project-picker.tsx as the canonical fork).
import { useState } from "react";
import { Check, FolderKanban, Search, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
// CEREBRO-PATCH(project-picker-nesting-import): tree query instead of the flat
// list so the picker can show sub-projects (FIR-2487).
import { projectTreeOptions } from "@multica/core/projects/nesting";
import { getProjectColor } from "@multica/core/projects/config";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ProjectTreeItem, UpdateIssueRequest } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
// CEREBRO-PATCH(SA3-project-picker-imports): RestrictedLock indicator from cerebro-access
import { RestrictedLock } from "@multica/cerebro-access/views";
import { useT } from "../../i18n";

export function ProjectPicker({
  projectId,
  onUpdate,
  triggerRender,
  align = "start",
  defaultOpen = false,
}: {
  projectId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
  /** Open the popover on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(defaultOpen);
  // CEREBRO-PATCH(project-picker-search-and-select): FIR-2536.
  const [query, setQuery] = useState("");
  // CEREBRO-PATCH(project-picker-nesting): render the project tree so
  // sub-projects appear nested under their parent (FIR-2487). The tree query
  // is backward-safe — a workspace with no nesting yields a flat list of roots,
  // identical to the previous flat picker. Flatten in parent-then-children
  // order, carrying the nesting level so each row can be indented.
  const { data: tree = [] } = useQuery(projectTreeOptions(wsId));
  const projects: Array<ProjectTreeItem & { level: number }> = [];
  const flatten = (nodes: ProjectTreeItem[], level: number) => {
    for (const node of nodes) {
      projects.push({ ...node, level });
      if (node.children?.length) flatten(node.children, level + 1);
    }
  };
  flatten(tree, 0);
  const current = projects.find((p) => p.id === projectId);

  // CEREBRO-PATCH(project-picker-search-and-select): FIR-2536. Filter projects
  // by title. While a query is active we suppress the tree indentation so the
  // matches read as a flat list — nesting context isn't useful when you're
  // already targeting a specific name.
  const q = query.trim().toLowerCase();
  const filtered = q
    ? projects.filter((p) => p.title.toLowerCase().includes(q))
    : projects;
  const showIndent = !q;

  const closeAndReset = () => {
    setOpen(false);
    setQuery("");
  };

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setQuery("");
      }}
    >
      <PopoverTrigger
        className={triggerRender ? undefined : "flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden"}
        render={triggerRender}
      >
        {current?.icon ? (
          <span className="text-sm shrink-0">{current.icon}</span>
        ) : current?.color ? (
          <span className={cn("size-2.5 rounded-full shrink-0", getProjectColor(current.color).dot)} />
        ) : (
          <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {/* CEREBRO-PATCH(SA3-project-picker-trigger-lock): restricted-access lock indicator */}
        {current?.access === "restricted" && <RestrictedLock className="mr-1" />}
        <span className="truncate">{current ? current.title : t(($) => $.picker.no_project)}</span>
      </PopoverTrigger>
      <PopoverContent align={align} className="w-64 gap-0 p-0">
        <div className="flex items-center gap-1.5 border-b px-2 py-1.5">
          <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t(($) => $.picker.search_placeholder)}
            aria-label={t(($) => $.picker.search_aria)}
            autoFocus
            className="w-full bg-transparent text-base md:text-sm placeholder:text-muted-foreground outline-none"
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {filtered.map((proj) => (
            <button
              key={proj.id}
              type="button"
              onClick={() => {
                onUpdate({ project_id: proj.id });
                closeAndReset();
              }}
              className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent transition-colors cursor-pointer"
              // CEREBRO-PATCH(project-picker-nesting-indent): indent sub-projects
              // 14px per level so the hierarchy reads at a glance (FIR-2487).
              // Suppressed while filtering — matches read as a flat list.
              style={showIndent && proj.level > 0 ? { paddingLeft: 8 + proj.level * 14 } : undefined}
            >
              {proj.icon ? (
                <span className="shrink-0">{proj.icon}</span>
              ) : (
                <span className={cn("size-2 rounded-full shrink-0", getProjectColor(proj.color).dot)} />
              )}
              {/* CEREBRO-PATCH(SA3-project-picker-item-lock): restricted-access lock indicator */}
              {proj.access === "restricted" && <RestrictedLock />}
              <span className="flex-1 truncate">{proj.title}</span>
              {proj.id === projectId && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
            </button>
          ))}
          {filtered.length === 0 && q && (
            <div className="px-2 py-3 text-center text-xs text-muted-foreground">
              {t(($) => $.picker.no_results, { query })}
            </div>
          )}
          {projects.length === 0 && (
            <div className="px-2 py-1.5 text-xs text-muted-foreground">
              {t(($) => $.picker.empty)}
            </div>
          )}
        </div>
        {projectId && (
          <>
            <div className="border-t" />
            <button
              type="button"
              onClick={() => {
                onUpdate({ project_id: null });
                closeAndReset();
              }}
              className="flex w-full items-center gap-1.5 px-2 py-1.5 text-left text-sm hover:bg-accent transition-colors cursor-pointer"
            >
              <X className="h-3.5 w-3.5 text-muted-foreground" />
              {t(($) => $.picker.remove)}
            </button>
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}
