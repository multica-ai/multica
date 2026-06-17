"use client";

import { useState } from "react";
import { CalendarRange, Check } from "lucide-react";
import { useQuery } from "@tanstack/react-query";

import { projectSprintsOptions } from "../core/queries";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

interface Props {
  workspaceId: string;
  projectId: string | null | undefined;
  /** Selected sprint id; "" means "No sprint". */
  value: string;
  onChange: (sprintId: string) => void;
  className?: string;
  /** Render-prop trigger so the field matches the surrounding picker chips —
   *  the create-issue modal passes <PillButton/> so Sprint looks like the
   *  Project / Priority / Assignee pills, not a boxed <Select>. */
  triggerRender?: React.ReactElement;
}

/**
 * SprintSelectField is the controlled sprint chooser for the Create issue
 * modal — TECH-3684. It lets the user drop a new task straight into a sprint at
 * creation time. Renders nothing until a project with real sprints is picked,
 * so it never adds noise to issues outside a sprint-enabled project.
 *
 * TECH-3684 follow-up: built on the same Popover pattern as ProjectPicker so it
 * accepts a render-prop trigger and matches the existing pill design instead of
 * the oversized bordered <Select>.
 */
export function SprintSelectField({ workspaceId, projectId, value, onChange, className, triggerRender }: Props) {
  const [open, setOpen] = useState(false);
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId ?? ""));
  const sprints = sprintsQuery.data?.sprints ?? [];
  const currentSprint = sprints.find((s) => s.id === value);

  if (!projectId) return null;
  if (sprintsQuery.isLoading || sprints.length === 0) return null;

  const select = (sprintId: string) => {
    onChange(sprintId);
    setOpen(false);
  };

  return (
    <div className={className}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          className={triggerRender ? undefined : "flex cursor-pointer items-center gap-1.5 overflow-hidden rounded -mx-1 px-1 transition-colors hover:bg-accent/30"}
          render={triggerRender}
        >
          <CalendarRange className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{currentSprint ? currentSprint.name : "No sprint"}</span>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-56 gap-0 p-1">
          <div className="max-h-72 overflow-y-auto">
            <button
              type="button"
              onClick={() => select("")}
              className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
            >
              <span className="flex-1 truncate text-muted-foreground">No sprint</span>
              {!value && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
            </button>
            {sprints.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => select(s.id)}
                className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
              >
                <CalendarRange className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate">{s.name}</span>
                {s.id === value && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
              </button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
