"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Bot, Hand, Loader2 } from "lucide-react";
import type { Issue } from "@multica/core/types";
import { issueAutomationExecutionsOptions, issueWorkflowOptions, workflowHandoff, useTakeOverAutomationExecution } from "@multica/core/issue-workflows";
import { useTransitionIssueStatusNode } from "@multica/core/issues/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { useLocale, useT } from "../../i18n";
import { WorkflowEntryEffects } from "./workflow-transition-dialog";
import { ExecutionLogSection } from "./execution-log-section";

/** Ordinary manual issues keep the existing status selector without a second
 * action block. Workflow actions are mounted only for a concrete pinned issue. */
export function AutomationExecutionSection({ issue }: { issue: Issue }) {
  return issue.workflow_id ? <WorkflowActions key={issue.id} issue={issue} /> : null;
}

function WorkflowActions({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const locale = useLocale();
  const workflow = useQuery(issueWorkflowOptions(wsId, issue.workflow_id ?? ""));
  const executions = useQuery(issueAutomationExecutionsOptions(wsId, issue.id));
  const takeOver = useTakeOverAutomationExecution();
  const transition = useTransitionIssueStatusNode();
  const { getActorName } = useActorName();
  const [showExecutions, setShowExecutions] = useState(false);
  const state = workflowHandoff(issue, workflow.data?.statuses ?? [], executions.data ?? []);
  const { execution, next, active, awaitingConfirmation, showNext, unavailableNext } = state;
  const needsAttention = execution?.status === "failed" || execution?.status === "cancelled";
  const stopped = execution?.status === "superseded";
  const pending = takeOver.isPending || transition.isPending;
  const error = takeOver.isError || transition.isError;

  if (workflow.isError || executions.isError) return <div className="flex items-center gap-3 text-caption" role="alert">
    <span className="text-muted-foreground">{t(($) => $.handoff.error)}</span>
    <Button size="sm" variant="outline" onClick={() => { void workflow.refetch(); void executions.refetch(); }}>{t(($) => $.handoff.retry)}</Button>
  </div>;
  if (!workflow.isSuccess || !executions.isSuccess || (!active && !awaitingConfirmation && !showNext && !unavailableNext && !needsAttention && !stopped)) return null;

  const executionLabel = awaitingConfirmation ? t(($) => $.handoff.waiting)
    : active ? execution?.status === "running" ? t(($) => $.handoff.running) : t(($) => $.handoff.queued)
    : needsAttention ? t(($) => $.handoff.failed)
    : stopped ? t(($) => $.handoff.superseded) : t(($) => $.handoff.completed);

  return <section className="space-y-4 rounded-lg border bg-muted/20 p-4" aria-label={t(($) => $.handoff.title)}>
    <div className="space-y-2">
      <h2 className="text-body font-medium">{active || awaitingConfirmation || needsAttention || stopped ? executionLabel : t(($) => $.handoff.title)}</h2>
      {execution?.executor_type && execution.executor_id && <p className="flex items-center gap-2 text-caption text-muted-foreground"><Bot className="size-3.5 shrink-0" aria-hidden="true" /><span className="break-words">{getActorName(execution.executor_type, execution.executor_id)}</span></p>}
      {showNext && next && <><p className="break-words text-body font-medium">{next.name}</p><WorkflowEntryEffects node={next} /></>}
      {unavailableNext && <p className="text-caption text-muted-foreground">{t(($) => $.handoff.unavailable)}</p>}
      {awaitingConfirmation && !next && !unavailableNext && <p className="text-caption text-muted-foreground">{t(($) => $.handoff.no_next)}</p>}
      {needsAttention && <p className="text-caption text-muted-foreground">{t(($) => $.handoff.failed_hint)}</p>}
      {stopped && <p className="text-caption text-muted-foreground">{t(($) => $.handoff.taken_over_hint)}</p>}
    </div>
    {error && <p role="alert" className="text-caption text-destructive">{t(($) => $.handoff.error)}</p>}
    <div className="flex flex-wrap items-center gap-2">
      {showNext && next && <Button size="sm" className="h-auto min-h-8 max-w-full whitespace-normal py-1.5" disabled={pending || error} onClick={() => transition.mutate({
        id: issue.id, workflow_status_id: next.id, expected_revision: issue.revision,
        expected_transition_id: issue.transition_id ?? undefined,
        expected_workflow_revision: workflow.data.workflow.revision,
      })}>
        {pending ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : <ArrowRight className="size-3.5" aria-hidden="true" />}
        <span className="whitespace-normal break-words">{pending ? t(($) => $.handoff.pending) : awaitingConfirmation ? t(($) => $.handoff.confirm, { name: next.name }) : t(($) => $.handoff.enter, { name: next.name })}</span>
      </Button>}
      {execution && execution.executor_type && <Button size="sm" variant="outline" aria-expanded={showExecutions} onClick={() => setShowExecutions((open) => !open)}>{t(($) => $.handoff.view_execution)}</Button>}
      {active && execution && <Button size="sm" variant="outline" disabled={pending} aria-describedby={`takeover-hint-${issue.id}`} onClick={() => takeOver.mutate({ issueId: issue.id, executionId: execution.id, expectedRevision: issue.revision })}>
        {takeOver.isPending ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : <Hand className="size-3.5" aria-hidden="true" />}{t(($) => $.handoff.takeover)}
      </Button>}
      {error && <Button size="sm" variant="ghost" onClick={() => { transition.reset(); takeOver.reset(); void workflow.refetch(); void executions.refetch(); }}>{t(($) => $.handoff.retry)}</Button>}
    </div>
    {active && <p id={`takeover-hint-${issue.id}`} className="text-caption text-muted-foreground">{t(($) => $.handoff.takeover_hint)}</p>}
    {showExecutions && <div className="space-y-4 border-t pt-4"><ExecutionLogSection issueId={issue.id} identifier={issue.identifier} />
      {executions.data.map((entry) => <details key={entry.id} className="text-caption">
        <summary className="cursor-pointer break-words text-muted-foreground">{t(($) => $.handoff.instructions)} · {new Date(entry.created_at).toLocaleString(locale)}</summary>
        <p className="mt-2 whitespace-pre-wrap break-words">{entry.policy_snapshot.instructions || t(($) => $.handoff.manual)}</p>
      </details>)}
    </div>}
  </section>;
}
