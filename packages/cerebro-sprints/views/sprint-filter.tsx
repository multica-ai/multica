"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

import { projectSprintsOptions } from "../core/queries";

interface Props {
  workspaceId: string;
  projectId: string;
  /** Currently selected sprint id; "" means "all sprints". */
  value: string;
  onChange: (sprintId: string) => void;
  className?: string;
}

/**
 * SprintFilter is a board/list filter widget for picking a sprint. The host
 * surface owns the actual filtering — this just selects the id. Renders
 * nothing when the project has no sprints.
 */
export function SprintFilter({ workspaceId, projectId, value, onChange, className }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId));
  const sprints = sprintsQuery.data?.sprints ?? [];
  if (sprintsQuery.isLoading || sprints.length === 0) return null;

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
              {s.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
