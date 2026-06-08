"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useUpdateIssue } from "@multica/core/issues/mutations";
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
  projectId: string | null | undefined;
  issueId: string;
  /** Optional className for layout wrapping. */
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

// SprintPicker drops into the issue side panel next to status / assignee /
// priority pickers. The active product model after FIR-2718 is "sprints as
// sub-projects", so selecting a sprint moves the issue's Project to that
// sprint project. Selecting "No sprint" moves it back to the sprint container.
export function SprintPicker({ workspaceId, projectId, issueId, className }: Props) {
  const treeQuery = useQuery(projectTreeOptions(workspaceId));
  const updateIssue = useUpdateIssue();
  const projects = flatten(treeQuery.data ?? []);
  const currentProject = projects.find((project) => project.id === projectId);
  const sprintContainerId = currentProject?.parent_project_id ?? currentProject?.id ?? "";
  const sprints = projects.filter((project) => project.parent_project_id === sprintContainerId);
  const currentSprintId = currentProject?.parent_project_id === sprintContainerId ? currentProject.id : "";

  if (!projectId) return null;
  if (treeQuery.isLoading) {
    return <div className={className}>Loading sprint…</div>;
  }
  if (!sprintContainerId || sprints.length === 0) {
    return null;
  }

  return (
    <div className={className}>
      <Select
        value={currentSprintId || "__none__"}
        onValueChange={(value) =>
          updateIssue.mutate(
            {
              id: issueId,
              project_id: value === "__none__" || value == null ? sprintContainerId : value,
            },
            {
              onError: () => toast.error("Failed to update sprint"),
            },
          )
        }
      >
        <SelectTrigger>
          <SelectValue placeholder="No sprint" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="__none__">No sprint</SelectItem>
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
