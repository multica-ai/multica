"use client";

import { useState } from "react";
import {
  AlertCircle,
  Copy,
  MoreHorizontal,
  RotateCcw,
  Square,
  Trash2,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";

interface AgentRowActionsProps {
  agent: Agent;
  presence: AgentPresenceDetail | null | undefined;
  // True when the current user can manage this agent (owner of agent or
  // workspace admin/owner). Mirrors the back-end's canManageAgent check —
  // the server is still the source of truth, this only hides UI for ops
  // the user can't perform.
  canManage: boolean;
  // Called when the user picks "Duplicate" — the page opens a Create
  // dialog pre-populated with this agent's config as a template.
  onDuplicate: (agent: Agent) => void;
}

/**
 * Per-row dropdown menu for the agents list. The set of actions is derived
 * from (a) the agent's lifecycle state (active vs archived) and (b) the
 * caller's permission level. If no actions apply, the trigger is omitted so
 * the row renders an empty cell (column width still preserved by the parent
 * `<TableCell className="w-10" />`).
 *
 * All triggers stop event propagation so clicks don't bubble up to the
 * row's navigate-to-detail handler.
 */
export function AgentRowActions({
  agent,
  presence,
  canManage,
  onDuplicate,
}: AgentRowActionsProps) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const [confirmArchive, setConfirmArchive] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);

  const isArchived = !!agent.archived_at;
  const runningCount = presence?.runningCount ?? 0;
  const queuedCount = presence?.queuedCount ?? 0;
  const hasActiveWork = runningCount + queuedCount > 0;

  // Derive which menu items to render. Doing this once here keeps the JSX
  // below a flat list of conditionals rather than a tangle of role/state
  // branches.
  const showStop = canManage && !isArchived && hasActiveWork;
  const showDuplicate = !isArchived; // any workspace member can duplicate
  const showArchive = canManage && !isArchived;
  const showRestore = canManage && isArchived;

  const hasAnyAction = showStop || showDuplicate || showArchive || showRestore;

  const invalidateAgents = () => {
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

  const handleArchive = async () => {
    try {
      await api.archiveAgent(agent.id);
      invalidateAgents();
      toast.success("Agent archived");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to archive agent");
    }
  };

  const handleRestore = async () => {
    try {
      await api.restoreAgent(agent.id);
      invalidateAgents();
      toast.success("Agent restored");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to restore agent");
    }
  };

  const handleCancelTasks = async () => {
    try {
      const { cancelled } = await api.cancelAgentTasks(agent.id);
      // Server broadcasts task:cancelled per row; useRealtimeSync will
      // invalidate the agent-task-snapshot cache for us. We still kick
      // agents in case the back-end's ReconcileAgentStatus changed
      // agent.status.
      invalidateAgents();
      toast.success(
        cancelled === 0
          ? "No active tasks to cancel"
          : `Cancelled ${cancelled} task${cancelled === 1 ? "" : "s"}`,
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to cancel tasks");
    }
  };

  if (!hasAnyAction) {
    return null;
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Row actions"
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => e.stopPropagation()}
            />
          }
        >
          <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="end"
          className="w-auto"
          // Prevent the row's onClick from firing if a click on a menu item
          // somehow bubbles back through the portal.
          onClick={(e) => e.stopPropagation()}
        >
          {showStop && (
            <DropdownMenuItem
              onClick={() => setConfirmCancel(true)}
            >
              <Square className="h-3.5 w-3.5" />
              Cancel all tasks
            </DropdownMenuItem>
          )}
          {showDuplicate && (
            <DropdownMenuItem onClick={() => onDuplicate(agent)}>
              <Copy className="h-3.5 w-3.5" />
              Duplicate
            </DropdownMenuItem>
          )}
          {showRestore && (
            <DropdownMenuItem onClick={handleRestore}>
              <RotateCcw className="h-3.5 w-3.5" />
              Restore
            </DropdownMenuItem>
          )}
          {showArchive && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="text-destructive"
                onClick={() => setConfirmArchive(true)}
              >
                <Trash2 className="h-3.5 w-3.5" />
                Archive
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      {confirmCancel && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmCancel(false);
          }}
        >
          <AlertDialogContent
            // Keep clicks inside the dialog from bubbling to the row.
            onClick={(e) => e.stopPropagation()}
          >
            <AlertDialogHeader>
              <AlertDialogTitle>
                Cancel all tasks for &ldquo;{agent.name}&rdquo;?
              </AlertDialogTitle>
              <AlertDialogDescription>
                {describeCancelImpact(runningCount, queuedCount)}
                {runningCount > 0 && (
                  <>
                    {" "}Running tasks may take up to 5 seconds to fully halt.
                  </>
                )}{" "}
                Cancelled tasks cannot be resumed.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep them</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmCancel(false);
                  void handleCancelTasks();
                }}
              >
                Cancel all tasks
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      {confirmArchive && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmArchive(false);
          }}
        >
          <AlertDialogContent onClick={(e) => e.stopPropagation()}>
            <AlertDialogHeader>
              <div className="flex items-start gap-3">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
                  <AlertCircle className="h-5 w-5 text-destructive" />
                </div>
                <div className="flex-1">
                  <AlertDialogTitle>
                    Archive &ldquo;{agent.name}&rdquo;?
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    The agent won&apos;t be assignable or mentionable, and any
                    active tasks will be cancelled. All history is preserved
                    and you can restore it later.
                  </AlertDialogDescription>
                </div>
              </div>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  void handleArchive();
                }}
              >
                Archive
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}

function describeCancelImpact(running: number, queued: number): string {
  // Both zero shouldn't happen — the menu item is gated on hasActiveWork —
  // but guarding anyway so the copy never reads "stop 0 tasks and 0 tasks".
  if (running === 0 && queued === 0) {
    return "There are no active tasks to cancel.";
  }
  const parts: string[] = [];
  if (running > 0) parts.push(`${running} running`);
  if (queued > 0) parts.push(`${queued} queued`);
  return `This will cancel ${parts.join(" and ")} ${
    running + queued === 1 ? "task" : "tasks"
  }.`;
}
