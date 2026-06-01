"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

import {
  issueSprintAssignmentOptions,
  projectSprintsOptions,
  useAssignIssueToSprint,
} from "../core/queries";

interface Props {
  workspaceId: string;
  projectId: string;
  issueId: string;
  /** Optional className for layout wrapping. */
  className?: string;
}

// SprintPicker drops into the issue side panel next to status / assignee /
// priority pickers. It only renders when the issue's project actually has
// sprints — the caller is expected to gate this on the cerebro_sprints flag
// and on whether sprint settings exist for the project.
export function SprintPicker({ workspaceId, projectId, issueId, className }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId));
  const assignmentQuery = useQuery(issueSprintAssignmentOptions(workspaceId, issueId));
  const assign = useAssignIssueToSprint(workspaceId);

  const sprints = sprintsQuery.data?.sprints ?? [];
  const currentSprintId = assignmentQuery.data?.sprint_id ?? "";

  if (sprintsQuery.isLoading || assignmentQuery.isLoading) {
    return <div className={className}>Loading sprint…</div>;
  }
  if (sprints.length === 0) {
    return null;
  }

  return (
    <div className={className}>
      <Select
        value={currentSprintId || "__none__"}
        onValueChange={(value) =>
          assign.mutate(
            {
              issueId,
              sprintId: value === "__none__" || value == null ? "" : value,
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
              {s.name} ({s.status})
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
