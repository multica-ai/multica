"use client";

// CEREBRO-PATCH(sprint-sidebar): FIR-2828 the sprint's own page previously had
// no way to end it at all — no sidebar, no settings, nothing. This mirrors
// the project detail sidebar's "status lives in a sidebar on its own page"
// pattern, but gates the terminal transition (done) behind
// CompleteSprintDialog so the operator always states what happens to any
// issue still assigned to the sprint (leave / backlog / move to another
// sprint) — a project's status field has no such side effect, so it stays a
// plain dropdown; a sprint's does, so it is a dedicated "…" action instead.

import { useState } from "react";
import { toast } from "sonner";
import { MoreHorizontal } from "lucide-react";

import { useWorkspacePaths } from "@multica/core/paths";
import {
  useDeleteSprint,
  useUpdateSprint,
} from "@multica/cerebro-sprints/core/queries";
import type { Sprint } from "@multica/cerebro-sprints/core";
import { CompleteSprintDialog } from "@multica/cerebro-sprints/views";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

import { useNavigation } from "../../navigation";

const SPRINT_STATUS_CONFIG: Record<Sprint["status"], { label: string; className: string }> = {
  planned: { label: "Planned", className: "bg-muted text-muted-foreground" },
  active: { label: "Active", className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  done: { label: "Done", className: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  cancelled: { label: "Cancelled", className: "bg-destructive/15 text-destructive" },
};

interface Props {
  workspaceId: string;
  projectId: string;
  sprint: Sprint;
}

export function SprintSidebar({ workspaceId, projectId, sprint }: Props) {
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const updateSprint = useUpdateSprint(workspaceId, projectId);
  const deleteSprint = useDeleteSprint(workspaceId, projectId);
  const [completing, setCompleting] = useState(false);

  const statusConfig = SPRINT_STATUS_CONFIG[sprint.status] ?? SPRINT_STATUS_CONFIG.planned;
  const isOpen = sprint.status !== "done" && sprint.status !== "cancelled";

  return (
    <div className="flex w-72 shrink-0 flex-col gap-5 border-l p-4">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold leading-snug">{sprint.name}</h2>
          <Badge variant="secondary" className={cn("mt-1.5", statusConfig.className)}>
            {statusConfig.label}
          </Badge>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button size="sm" variant="ghost" title="Sprint actions">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            {isOpen && (
              <>
                <DropdownMenuItem onClick={() => setCompleting(true)}>
                  Complete sprint…
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() =>
                    updateSprint.mutate(
                      { id: sprint.id, payload: { status: "cancelled" } },
                      {
                        onSuccess: () => toast.success("Sprint cancelled"),
                        onError: () => toast.error("Failed to cancel sprint"),
                      },
                    )
                  }
                >
                  Cancel sprint
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            <DropdownMenuItem
              variant="destructive"
              onClick={() => {
                if (!window.confirm(`Delete sprint "${sprint.name}"?`)) return;
                deleteSprint.mutate(sprint.id, {
                  onSuccess: () => {
                    toast.success("Sprint deleted");
                    router.push(wsPaths.projectDetail(projectId));
                  },
                  onError: () => toast.error("Failed to delete sprint"),
                });
              }}
            >
              Delete sprint
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <div className="flex flex-col gap-1 text-xs text-muted-foreground">
        <span>
          {sprint.start_date} → {sprint.end_date}
        </span>
        {sprint.goal && <span>{sprint.goal}</span>}
      </div>

      <CompleteSprintDialog
        workspaceId={workspaceId}
        projectId={projectId}
        sprint={sprint}
        open={completing}
        onOpenChange={setCompleting}
      />
    </div>
  );
}
