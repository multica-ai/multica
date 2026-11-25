"use client";
import { useState } from "react";
import { toast } from "sonner";
import { AlertCircle, Loader2, Square } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { useCancelDocTaskRun } from "@multica/core/docs";
import type { AgentTask, DocTaskRun } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { redactSecrets } from "../../common/task-transcript/redact";
import { runOutcomeLabel } from "../../agents/components/tabs/task-failure";
import { useStatusLabel } from "../../issues/components/task-run-labels";
import { TerminateTaskConfirmDialog } from "../../issues/components/terminate-task-confirm-dialog";
import { useT } from "../../i18n";

// Deferred is an automatic retry that has not started yet (waiting for the
// runtime to reconnect, or a short backoff after a provider network error):
// it can still be stopped.
const ACTIVE_STATUSES = new Set(["queued", "dispatched", "waiting_local_directory", "running", "deferred"]);

export function isActiveDocRun(run: DocTaskRun): boolean {
  return ACTIVE_STATUSES.has(run.status);
}

/**
 * The runs a discussion shows, oldest first: every run still queued or
 * working, each with its own way to stop it, and an agent's failed run while
 * it is that agent's latest on the discussion. A finished run shows as its
 * reply; a stopped one shows nothing. `runs` is newest first.
 */
export function docThreadRuns(runs: readonly DocTaskRun[], threadId: string): DocTaskRun[] {
  const seen = new Set<string>();
  const shown: DocTaskRun[] = [];
  for (const run of runs) {
    if (run.docThreadId !== threadId) continue;
    const latest = !seen.has(run.agentId);
    seen.add(run.agentId);
    if (isActiveDocRun(run) || (latest && run.status === "failed")) shown.push(run);
  }
  return shown.reverse();
}

export function DocThreadRun({ docId, run }: { docId: string; run: DocTaskRun }) {
  const { t } = useT("issues");
  const { t: tAgents } = useT("agents");
  const { t: tDocs } = useT("docs");
  const { getActorName } = useActorName();
  const cancel = useCancelDocTaskRun(docId);
  const [confirmStop, setConfirmStop] = useState(false);
  const name = getActorName("agent", run.agentId);
  const status = useStatusLabel(run.status as AgentTask["status"]);
  const active = isActiveDocRun(run);
  const outcome = { status: run.status, error: run.error, failure_reason: run.failureReason, cancelled_by: run.cancelledBy };
  const label = run.status === "deferred"
    ? tDocs(($) => $.comments.run_waiting_retry)
    : active ? status : (runOutcomeLabel(outcome, tAgents) ?? status);
  const error = !active && run.error?.trim() ? redactSecrets(run.error.trim()) : "";
  const stopping = cancel.isPending || cancel.isSuccess;
  return (
    <div className="mt-2 flex items-center gap-2 text-caption" data-run-id={run.id}>
      <ActorAvatar actorType="agent" actorId={run.agentId} size="sm" />
      <span className="truncate font-medium text-foreground">{name}</span>
      <span role="status" className="flex min-w-0 items-center gap-1 text-muted-foreground">
        {active ? (
          <Loader2 className="size-3.5 shrink-0 animate-spin motion-reduce:animate-none" />
        ) : (
          <AlertCircle className="size-3.5 shrink-0 text-destructive" />
        )}
        {error && error !== label ? (
          <Tooltip>
            <TooltipTrigger render={<span className="truncate">{label}</span>} />
            <TooltipContent side="top" className="flex-col items-start">
              <span>{label}</span>
              <span className="break-all font-mono text-muted-foreground">{error}</span>
            </TooltipContent>
          </Tooltip>
        ) : (
          <span className="truncate">{label}</span>
        )}
      </span>
      {active && run.canCancel && (
        <IconButton
          size="icon-xs"
          variant="ghost"
          className="ml-auto text-muted-foreground"
          label={stopping ? t(($) => $.inline_run.stopping) : t(($) => $.inline_run.stop)}
          disabled={stopping}
          onClick={() => setConfirmStop(true)}
        >
          {stopping ? (
            <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" />
          ) : (
            <Square className="size-3.5" />
          )}
        </IconButton>
      )}
      <TerminateTaskConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        showRunningNote={run.status !== "queued"}
        onConfirm={() => {
          cancel.mutateAsync(run.id).catch(() => toast.error(t(($) => $.execution_log.cancel_failed)));
        }}
      />
    </div>
  );
}
