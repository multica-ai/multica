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
import { useT } from "@multica/i18n/react";
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
  const t = useT("agents");
  const c = useT("common");
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
      toast.success(t("toast_archived"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_archive"));
    }
  };

  const handleRestore = async () => {
    try {
      await api.restoreAgent(agent.id);
      invalidateAgents();
      toast.success(t("toast_restored"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_restore"));
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
          ? t("toast_no_active_tasks")
          : t("toast_cancelled_tasks", { count: cancelled }),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_cancel_tasks"));
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
              aria-label={t("aria_row_actions")}
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
              {t("row_cancel_tasks")}
            </DropdownMenuItem>
          )}
          {showDuplicate && (
            <DropdownMenuItem onClick={() => onDuplicate(agent)}>
              <Copy className="h-3.5 w-3.5" />
              {t("row_duplicate")}
            </DropdownMenuItem>
          )}
          {showRestore && (
            <DropdownMenuItem onClick={handleRestore}>
              <RotateCcw className="h-3.5 w-3.5" />
              {t("row_restore")}
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
                {t("row_archive")}
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
                {t("row_cancel_confirm_title", { name: agent.name })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {describeCancelImpact(runningCount, queuedCount, t)}
                {runningCount > 0 && (
                  <>
                    {" "}{t("row_cancel_confirm_halt")}
                  </>
                )}{" "}
                {t("row_cancel_confirm_resume")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("row_keep_them")}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmCancel(false);
                  void handleCancelTasks();
                }}
              >
                {t("row_cancel_all_tasks")}
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
                    {t("row_archive_confirm_title", { name: agent.name })}
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    {t("row_archive_confirm_desc")}
                  </AlertDialogDescription>
                </div>
              </div>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{c("cancel")}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  void handleArchive();
                }}
              >
                {t("row_archive")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}

function describeCancelImpact(running: number, queued: number, t: ReturnType<typeof useT>): string {
  // Both zero shouldn't happen — the menu item is gated on hasActiveWork —
  // but guarding anyway so the copy never reads "stop 0 tasks and 0 tasks".
  if (running === 0 && queued === 0) {
    return t("toast_no_active_tasks");
  }
  const parts: string[] = [];
  if (running > 0) parts.push(t("cancel_impact_running", { count: running }));
  if (queued > 0) parts.push(t("cancel_impact_queued", { count: queued }));
  const joined = parts.join(t("cancel_impact_and"));
  const taskWord = running + queued === 1
    ? t("cancel_impact_task_singular")
    : t("cancel_impact_task_plural");
  return t("cancel_impact_sentence", { parts: joined, taskWord });
}
