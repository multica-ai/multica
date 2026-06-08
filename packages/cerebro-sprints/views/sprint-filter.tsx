"use client";

import { useQuery } from "@tanstack/react-query";
import { projectTreeOptions } from "@multica/core/projects/nesting";
import type { ProjectTreeItem } from "@multica/core/types";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

interface Props {
  workspaceId: string;
  projectId: string;
  /** Currently selected sprint id; "" means "all sprints". */
  value: string;
  onChange: (sprintId: string) => void;
  className?: string;
}

function flatten(nodes: ProjectTreeItem[]): ProjectTreeItem[] {
  const out: ProjectTreeItem[] = [];
  for (const node of nodes) {
    out.push(node);
    if (node.children?.length) out.push(...flatten(node.children));
  }
  return out;
}

/**
 * SprintFilter is a board/list filter widget for picking a sprint sub-project.
 * The host surface owns the actual filtering — this just selects the project id.
 * Renders nothing when the project has no sprint children.
 */
export function SprintFilter({ workspaceId, projectId, value, onChange, className }: Props) {
  const treeQuery = useQuery(projectTreeOptions(workspaceId));
  const sprints = flatten(treeQuery.data ?? []).filter((project) => project.parent_project_id === projectId);
  if (treeQuery.isLoading || sprints.length === 0) return null;

  return (
    <div className={className}>
      <Select
        value={value || "__all__"}
        onValueChange={(v) => onChange(v === "__all__" || v == null ? "" : v)}
      >
        <SelectTrigger>
          <SelectValue placeholder="All sprints" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="__all__">All sprints</SelectItem>
          {sprints.map((s) => (
            <SelectItem key={s.id} value={s.id}>
              {s.title}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
