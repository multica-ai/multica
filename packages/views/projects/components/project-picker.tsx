"use client";

// CEREBRO-PATCH(SA3-project-picker): L3-marked (cannot wrap cleanly).
// Two intertwined deviations from upstream:
//   1) Replaces ProjectIcon with inline icon/color-dot rendering
//   2) Adds RestrictedLock indicator after icon, in both trigger and items
// No injection slot exists inside DropdownMenuTrigger/Item children — wrapping
// would require duplicating the entire JSX tree, eliminating upstream-tracking
// benefit. Resolution deferred to chunk 11 (likely path-alias shadow with
// cerebro-access/project-picker.tsx as the canonical fork).
import { Check, FolderKanban, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
// CEREBRO-PATCH(project-picker-nesting-import): tree query instead of the flat
// list so the picker can show sub-projects (FIR-2487).
import { projectTreeOptions } from "@multica/core/projects/nesting";
import { getProjectColor } from "@multica/core/projects/config";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ProjectTreeItem, UpdateIssueRequest } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
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
  /** Open the dropdown on first mount. Used by progressive-disclosure
   *  sidebars so a newly-added field immediately enters edit state. */
  defaultOpen?: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
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

  return (
    <DropdownMenu defaultOpen={defaultOpen}>
      <DropdownMenuTrigger
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
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-52">
        {projects.map((proj) => (
          <DropdownMenuItem
            key={proj.id}
            onClick={() => onUpdate({ project_id: proj.id })}
            // CEREBRO-PATCH(project-picker-nesting-indent): indent sub-projects
            // 14px per level so the hierarchy reads at a glance (FIR-2487).
            style={proj.level > 0 ? { paddingLeft: 8 + proj.level * 14 } : undefined}
          >
            {proj.icon ? (
              <span className="mr-1">{proj.icon}</span>
            ) : (
              <span className={cn("size-2 rounded-full mr-1.5 shrink-0", getProjectColor(proj.color).dot)} />
            )}
            {/* CEREBRO-PATCH(SA3-project-picker-item-lock): restricted-access lock indicator */}
            {proj.access === "restricted" && <RestrictedLock className="mr-1" />}
            <span className="truncate">{proj.title}</span>
            {proj.id === projectId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        {projects.length > 0 && projectId && <DropdownMenuSeparator />}
        {projectId && (
          <DropdownMenuItem onClick={() => onUpdate({ project_id: null })}>
            <X className="h-3.5 w-3.5 text-muted-foreground" />
            {t(($) => $.picker.remove)}
          </DropdownMenuItem>
        )}
        {projects.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">{t(($) => $.picker.empty)}</div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
