"use client";

// FIR-2283 v2 point 8 — "per-issue workflow activation", the picker that was
// missing from the issue side panel (Tine's review: the API/mutation existed,
// but there was no control on the issue itself — only a paste-a-uuid field
// inside the workflow editor). Built on the same Popover + inline-trigger
// pattern as ProjectPicker/SprintPicker so it matches the existing property
// row design instead of introducing a new look.
import { useState } from "react";
import { Check, Workflow as WorkflowIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  cerebroActiveWorkflowForIssueOptions,
  cerebroWorkflowsListOptions,
  useActivateWorkflowMutation,
} from "../core";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

interface Props {
  workspaceId: string;
  issueId: string;
  className?: string;
}

// WorkflowPicker drops into the issue side panel next to Status / Assignee /
// Project. Only "Issue workflow" recipes (workflow_type === "issue_loop")
// are selectable — a "Standard workflow" rule reacts to events across the
// whole project and isn't something you "activate on one issue". Activating
// a recipe here never touches the recipe itself (it stays reusable) — it
// compiles an independent set of rules scoped to THIS issue (see
// loops.IssueLoopBridge.ActivateOnIssue). There is no "deactivate" affordance
// yet: re-activating a different recipe replaces the compiled rules, but
// removing a recipe entirely from an issue isn't wired up server-side.
export function WorkflowPicker({ workspaceId, issueId, className }: Props) {
  const [open, setOpen] = useState(false);
  const workflowsQuery = useQuery(cerebroWorkflowsListOptions(workspaceId));
  const activeQuery = useQuery(cerebroActiveWorkflowForIssueOptions(workspaceId, issueId));
  const activate = useActivateWorkflowMutation(workspaceId, issueId);

  const recipes = (workflowsQuery.data?.workflows ?? []).filter(
    (w) => w.workflow_type === "issue_loop",
  );
  const activeWorkflowId = activeQuery.data?.active ? activeQuery.data.workflow_id : undefined;
  const activeRecipe = recipes.find((w) => w.id === activeWorkflowId);

  if (workflowsQuery.isLoading || activeQuery.isLoading) {
    return (
      <div className={className}>
        <span className="flex items-center gap-1.5 -mx-1 px-1 text-xs text-muted-foreground">
          <WorkflowIcon className="h-3.5 w-3.5 shrink-0" />
          Loading workflow…
        </span>
      </div>
    );
  }

  // Stay visible even with no Issue workflow recipes yet, so the field never
  // disappears from the side panel — show a muted "No recipes yet".
  if (recipes.length === 0) {
    return (
      <div className={className}>
        <span className="flex items-center gap-1.5 -mx-1 px-1 text-xs text-muted-foreground">
          <WorkflowIcon className="h-3.5 w-3.5 shrink-0" />
          No Issue workflow recipes yet
        </span>
      </div>
    );
  }

  const select = (workflowId: string) => {
    activate.mutate(workflowId, {
      onError: () => toast.error("Failed to activate workflow on this issue"),
    });
    setOpen(false);
  };

  return (
    <div className={className}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger className="flex cursor-pointer items-center gap-1.5 overflow-hidden rounded -mx-1 px-1 transition-colors hover:bg-accent/30">
          <WorkflowIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{activeRecipe ? activeRecipe.name : "No workflow"}</span>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-56 gap-0 p-1">
          <div className="max-h-72 overflow-y-auto">
            {recipes.map((w) => (
              <button
                key={w.id}
                type="button"
                onClick={() => select(w.id)}
                className="flex w-full cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
              >
                <WorkflowIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{w.name}</span>
                {w.id === activeWorkflowId && (
                  <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                )}
              </button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
