"use client";

import { useState } from "react";
import { Loader2, MessageSquarePlus } from "@multica/ui/icons";
import { toast } from "sonner";
import { api, dispatchReasonCode } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

/** A run can start over once it has ended, however it ended. */
export function canRerunInFreshSession(task: AgentTask): boolean {
  return task.status === "completed" || task.status === "failed" || task.status === "cancelled";
}

// Repeats one past run in a new agent session and working directory, for when
// the old context has drifted. Shared by the execution log's rows and the Runs
// dialog. A plain retry keeps continuing the source run's session.
export function FreshSessionRunButton({ task }: { task: AgentTask }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const [starting, setStarting] = useState(false);

  const handleConfirm = async () => {
    if (starting) return;
    setStarting(true);
    try {
      await api.rerunTask(task.id, { fresh: true });
      setOpen(false);
      toast.success(t(($) => $.fresh_session_dialog.started));
    } catch (e) {
      toast.error(
        dispatchReasonCode(e) === "invocation_not_allowed"
          ? t(($) => $.execution_log.retry_blocked)
          : e instanceof Error
            ? e.message
            : t(($) => $.fresh_session_dialog.failed),
      );
    } finally {
      setStarting(false);
    }
  };

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setOpen(true);
              }}
              disabled={starting}
              aria-label={t(($) => $.execution_log.fresh_session_tooltip)}
            />
          }
          className="flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
        >
          {starting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <MessageSquarePlus className="h-3.5 w-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>{t(($) => $.execution_log.fresh_session_tooltip)}</TooltipContent>
      </Tooltip>
      {open && (
        <AlertDialog open onOpenChange={(next) => !starting && setOpen(next)}>
          <AlertDialogContent onClick={(e) => e.stopPropagation()}>
            <AlertDialogHeader>
              <AlertDialogTitle>{t(($) => $.fresh_session_dialog.title)}</AlertDialogTitle>
              <AlertDialogDescription>{t(($) => $.fresh_session_dialog.body)}</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={starting}>{t(($) => $.fresh_session_dialog.cancel)}</AlertDialogCancel>
              <AlertDialogAction
                disabled={starting}
                // The dialog closes only once the request succeeds.
                onClick={() => void handleConfirm()}
              >
                {t(($) => $.fresh_session_dialog.confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}
