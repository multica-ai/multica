"use client";

import { useQuery } from "@tanstack/react-query";

import { projectSprintsOptions } from "../core/queries";
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
  /** Selected sprint id; "" means "No sprint". */
  value: string;
  onChange: (sprintId: string) => void;
  className?: string;
}

const NONE = "__none__";

/**
 * SprintSelectField is the controlled sprint chooser for the Create issue
 * modal — TECH-3684. It lets the user drop a new task straight into a sprint at
 * creation time. Renders nothing until a project with real sprints is picked,
 * so it never adds noise to issues outside a sprint-enabled project.
 */
export function SprintSelectField({ workspaceId, projectId, value, onChange, className }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId ?? ""));
  const sprints = sprintsQuery.data?.sprints ?? [];
  // TECH-3684: map the selected id to its sprint name for the trigger label —
  // Base UI's <SelectValue> otherwise renders the raw value (the sprint UUID).
  const currentSprint = sprints.find((s) => s.id === value);

  if (!projectId) return null;
  if (sprintsQuery.isLoading || sprints.length === 0) return null;

  return (
    <div className={className}>
      <Select
        value={value || NONE}
        onValueChange={(v) => onChange(v === NONE || v == null ? "" : v)}
      >
        <SelectTrigger>
          <SelectValue placeholder="No sprint">
            {() => currentSprint?.name ?? "No sprint"}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>No sprint</SelectItem>
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
