"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  issueSprintAssignmentOptions,
  projectSprintsOptions,
  useAssignIssueToSprint,
} from "../core/queries";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

interface Props {
  workspaceId: string;
  /** The issue's home project — the sprint container that owns the sprints. */
  projectId: string | null | undefined;
  issueId: string;
  /** Optional className for layout wrapping. */
  className?: string;
}

const NONE = "__none__";

// SprintPicker drops into the issue side panel next to status / assignee /
// priority pickers. Sprints are real cerebro_sprint rows (not sub-projects):
// assigning an issue to a sprint records the membership in the
// cerebro_sprint_issue join and NEVER touches the issue's project_id, so the
// issue stays in its home project AND shows on the sprint board. Selecting
// "No sprint" clears the membership. Renders nothing when the project has no
// real sprints.
export function SprintPicker({ workspaceId, projectId, issueId, className }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId ?? ""));
  const assignmentQuery = useQuery(issueSprintAssignmentOptions(workspaceId, issueId));
  const assign = useAssignIssueToSprint(workspaceId);

  const sprints = sprintsQuery.data?.sprints ?? [];
  const currentSprintId = assignmentQuery.data?.sprint_id ?? "";

  if (!projectId) return null;
  if (sprintsQuery.isLoading) {
    return <div className={className}>Loading sprint…</div>;
  }
  if (sprints.length === 0) {
    return null;
  }

  return (
    <div className={className}>
      <Select
        value={currentSprintId || NONE}
        onValueChange={(value) =>
          assign.mutate(
            { issueId, sprintId: value === NONE || value == null ? "" : value },
            { onError: () => toast.error("Failed to update sprint") },
          )
        }
      >
        <SelectTrigger>
          <SelectValue placeholder="No sprint" />
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
