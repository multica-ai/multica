"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Clock3, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useCancelIssueRun } from "@multica/core/issues/mutations";
import type { AgentTask } from "@multica/core/types/agent";
import { useT } from "../../i18n";
import type { TimelineItem } from "./build-timeline";
import type { TraceStep } from "./build-steps";
import { redactSecrets } from "./redact";
import { QUIET_RUN_NOTICE_MS, runActivity } from "./run-activity";

export function RunActivityNotice({ task, items, steps, className, showRecovery = true }: {
  task: AgentTask;
  items: readonly TimelineItem[];
  steps: readonly TraceStep[];
  className?: string;
  showRecovery?: boolean;
}) {
  const { t } = useT("agents");
  const activity = useMemo(() => runActivity(items, steps), [items, steps]);
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (task.status !== "running" || !activity) return;
    const timer = setInterval(() => setNow(Date.now()), 15_000);
    return () => clearInterval(timer);
  }, [task.status, activity]);

  if (task.status !== "running" || !activity || now - activity.lastEventAt < QUIET_RUN_NOTICE_MS) return null;

  return (
    <div className={cn("flex flex-wrap items-center gap-3 rounded-md bg-muted/50 px-3 py-2", className)} data-run-activity-notice>
      <Clock3 aria-hidden className="size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1 basis-40 text-caption">
        <p>{t(($) => $.transcript.quiet_minutes, { count: Math.floor((now - activity.lastEventAt) / 60_000) })}</p>
        <p className="break-words text-muted-foreground">
          {activity.pendingTool
            ? t(($) => $.transcript.quiet_tool, { tool: redactSecrets(activity.pendingTool).slice(0, 120) })
            : t(($) => $.transcript.quiet_running)}
        </p>
      </div>
      {showRecovery && task.issue_id && <StopQuietIssueRun key={task.id} task={task} />}
    </div>
  );
}

function StopQuietIssueRun({ task }: { task: AgentTask }) {
  const { t } = useT("issues");
  const { t: tAgents } = useT("agents");
  const cancel = useCancelIssueRun(task.issue_id);
  const [open, setOpen] = useState(false);
  const [failed, setFailed] = useState(false);
  const requested = useRef(false);
  const stopping = cancel.isPending || cancel.isSuccess;

  return <>
    <Button type="button" variant="outline" size="xs" disabled={stopping} aria-busy={cancel.isPending}
      onClick={() => { setFailed(false); setOpen(true); }}>
      {stopping ? t(($) => $.inline_run.stopping) : t(($) => $.terminate_dialog.confirm)}
    </Button>
    <AlertDialog open={open} onOpenChange={(next) => { if (!cancel.isPending) setOpen(next); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.terminate_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>{tAgents(($) => $.transcript.quiet_stop_description)}</AlertDialogDescription>
        </AlertDialogHeader>
        {failed && <p role="alert" className="text-caption text-destructive">{t(($) => $.execution_log.cancel_failed)}</p>}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>{t(($) => $.terminate_dialog.keep)}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={stopping} aria-busy={cancel.isPending}
            onClick={async (event) => {
              event.preventDefault();
              if (requested.current) return;
              requested.current = true;
              setFailed(false);
              try {
                // Cancel only this run. The existing scheduler owns queue delivery.
                await cancel.mutateAsync(task.id);
                setOpen(false);
              } catch {
                requested.current = false;
                setFailed(true);
              }
            }}>
            {cancel.isPending && <Loader2 aria-hidden className="size-3.5 animate-spin motion-reduce:animate-none" />}
            {stopping ? t(($) => $.inline_run.stopping) : t(($) => $.terminate_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}
