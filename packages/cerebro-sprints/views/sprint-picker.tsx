"use client";

// TECH-3684 follow-up. The sprint field previously used the generic bordered
// <Select> primitive, which renders a boxed, 32px-tall control that stood out
// next to the borderless Status / Assignee / Project pickers in the issue side
// panel. Rebuilt on the same Popover + inline-trigger pattern as ProjectPicker
// so the Sprint row matches the existing property-row design instead of looking
// oversized.
import { useState } from "react";
import { CalendarRange, Check } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  issueSprintAssignmentOptions,
  projectSprintsOptions,
  useAssignIssueToSprint,
} from "../core/queries";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

interface Props {
  workspaceId: string;
  /** The issue's home project — the sprint container that owns the sprints. */
  projectId: string | null | undefined;
  issueId: string;
  /** Optional className for layout wrapping. */
  className?: string;
}

// SprintPicker drops into the issue side panel next to status / assignee /
// priority pickers. Sprints are real cerebro_sprint rows (not sub-projects):
// assigning an issue to a sprint records the membership in the
// cerebro_sprint_issue join and NEVER touches the issue's project_id, so the
// issue stays in its home project AND shows on the sprint board. Selecting
// "No sprint" clears the membership. Stays visible (muted) when the project has
// no real sprints yet, so the row never disappears.
export function SprintPicker({ workspaceId, projectId, issueId, className }: Props) {
  const [open, setOpen] = useState(false);
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId ?? ""));
  const assignmentQuery = useQuery(issueSprintAssignmentOptions(workspaceId, issueId));
  const assign = useAssignIssueToSprint(workspaceId);

  const sprints = sprintsQuery.data?.sprints ?? [];
  const currentSprintId = assignmentQuery.data?.sprint_id ?? "";
  const currentSprint = sprints.find((s) => s.id === currentSprintId);

  if (!projectId) return null;

  if (sprintsQuery.isLoading) {
    return (
      <div className={className}>
        <span className="flex items-center gap-1.5 -mx-1 px-1 text-sm text-muted-foreground">
          <CalendarRange className="h-3.5 w-3.5 shrink-0" />
          Loading sprint…
        </span>
      </div>
    );
  }

  // Stay visible even with no sprints yet, so the field never disappears from
  // the issue side panel — show a muted "No sprints yet".
  if (sprints.length === 0) {
    return (
      <div className={className}>
        <span className="flex items-center gap-1.5 -mx-1 px-1 text-sm text-muted-foreground">
          <CalendarRange className="h-3.5 w-3.5 shrink-0" />
          No sprints yet
        </span>
      </div>
    );
  }

  const select = (sprintId: string) => {
    assign.mutate(
      { issueId, sprintId },
      { onError: () => toast.error("Failed to update sprint") },
    );
    setOpen(false);
  };

  return (
    <div className={className}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger className="flex cursor-pointer items-center gap-1.5 overflow-hidden rounded -mx-1 px-1 transition-colors hover:bg-accent/30">
          <CalendarRange className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{currentSprint ? currentSprint.name : "No sprint"}</span>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-56 gap-0 p-1">
          <div className="max-h-72 overflow-y-auto">
            {sprints.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => select(s.id)}
                className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
              >
                <CalendarRange className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate">{s.name}</span>
                {s.id === currentSprintId && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
              </button>
            ))}
          </div>
          {currentSprintId && (
            <>
              <div className="my-1 border-t" />
              <button
                type="button"
                onClick={() => select("")}
                className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
              >
                <span className="flex-1 truncate text-muted-foreground">No sprint</span>
              </button>
            </>
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
