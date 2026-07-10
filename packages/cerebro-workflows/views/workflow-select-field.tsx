"use client";

// FIR-2283 v2 point 8 — pick an Issue workflow straight from the Create issue
// modal, for both humans and agents. Mirrors SprintSelectField (TECH-3684):
// a controlled chip that renders nothing until there is something to pick, so
// it never adds noise when the workspace has no Issue workflows or the feature
// flag is off. The modal activates the chosen recipe on the new issue right
// after creation (see the create-issue CEREBRO-PATCH), reusing the same
// activate path the issue-side WorkflowPicker uses.

import { useState } from "react";
import { Check, Workflow as WorkflowIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

import { cerebroWorkflowsListOptions } from "../core";

interface Props {
  workspaceId: string;
  /** Selected workflow id; "" means "No workflow". */
  value: string;
  onChange: (workflowId: string) => void;
  className?: string;
  /** Render-prop trigger so the chip matches the sibling pills in the modal. */
  triggerRender?: React.ReactElement;
}

export function WorkflowSelectField({ workspaceId, value, onChange, className, triggerRender }: Props) {
  const enabled = useFeatureFlag("cerebro_workflows");
  const [open, setOpen] = useState(false);
  const workflowsQuery = useQuery(cerebroWorkflowsListOptions(workspaceId));
  // Only enabled "Issue workflow" recipes can run a loop on an issue.
  const recipes = (workflowsQuery.data?.workflows ?? []).filter(
    (w) => w.workflow_type === "issue_loop" && w.enabled,
  );
  const current = recipes.find((w) => w.id === value);

  if (!enabled) return null;
  if (workflowsQuery.isLoading || recipes.length === 0) return null;

  const select = (workflowId: string) => {
    onChange(workflowId);
    setOpen(false);
  };

  return (
    <div className={className}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          className={triggerRender ? undefined : "flex cursor-pointer items-center gap-1.5 overflow-hidden rounded -mx-1 px-1 transition-colors hover:bg-accent/30"}
          render={triggerRender}
        >
          <WorkflowIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{current ? current.name : "No workflow"}</span>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-56 gap-0 p-1">
          <div className="max-h-72 overflow-y-auto">
            <button
              type="button"
              onClick={() => select("")}
              className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
            >
              <span className="flex-1 truncate text-muted-foreground">No workflow</span>
              {!value && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
            </button>
            {recipes.map((w) => (
              <button
                key={w.id}
                type="button"
                onClick={() => select(w.id)}
                className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
              >
                <WorkflowIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate">{w.name}</span>
                {w.id === value && <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
              </button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
