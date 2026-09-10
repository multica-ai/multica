"use client";

import { useQuery } from "@tanstack/react-query";
import { Bot, UserRound, Loader2 } from "lucide-react";
import type { Issue, IssueWorkflowStatusNode } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { issueAutomationExecutionsOptions, workflowHandoff } from "@multica/core/issue-workflows";
import { useTransitionIssueStatusNode } from "@multica/core/issues/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function WorkflowEntryEffects({ node }: { node: IssueWorkflowStatusNode }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const { executor, assignee } = node.entry_policy;
  return <span className="block space-y-1.5 text-caption text-muted-foreground">
    <span className="flex items-start gap-2"><Bot className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" /><span className="min-w-0 break-words">{executor.type === "none" ? t(($) => $.handoff.manual) : t(($) => $.handoff.starts, { name: getActorName(executor.type, executor.id) })}</span></span>
    <span className="flex items-start gap-2"><UserRound className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" /><span className="min-w-0 break-words">{assignee.type === "keep" ? t(($) => $.handoff.keeps_assignee) : t(($) => $.handoff.assigns, { name: getActorName(assignee.type === "human" ? "member" : assignee.type, assignee.id) })}</span></span>
  </span>;
}

export function WorkflowTransitionDialog({ issue, target, workflowRevision, onClose }: {
  issue: Issue;
  target: IssueWorkflowStatusNode;
  workflowRevision: number;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const query = useQuery(issueAutomationExecutionsOptions(wsId, issue.id));
  const transition = useTransitionIssueStatusNode();
  const { active } = workflowHandoff(issue, [], query.data ?? []);
  const error = query.isError || transition.isError;
  return <Dialog open onOpenChange={(open) => { if (!open && !transition.isPending) onClose(); }}>
    <DialogContent className="sm:max-w-md">
      <DialogHeader>
        <DialogTitle className="break-words">{t(($) => $.handoff.transition_title, { name: target.name })}</DialogTitle>
        <DialogDescription>{query.isPending ? t(($) => $.handoff.loading) : active ? t(($) => $.handoff.stops) : t(($) => $.handoff.effects_intro)}</DialogDescription>
      </DialogHeader>
      <WorkflowEntryEffects node={target} />
      {error && <p role="alert" className="text-caption text-destructive">{t(($) => $.handoff.error)}</p>}
      <DialogFooter>
        <Button variant="ghost" disabled={transition.isPending} onClick={onClose}>{t(($) => $.handoff.cancel)}</Button>
        <Button className="h-auto min-h-8 max-w-full whitespace-normal break-words py-1.5" disabled={!query.isSuccess || transition.isPending || transition.isError} onClick={() => transition.mutate({
          id: issue.id, workflow_status_id: target.id, expected_revision: issue.revision,
          expected_transition_id: issue.transition_id ?? undefined, expected_workflow_revision: workflowRevision,
        }, { onSuccess: onClose })}>
          {transition.isPending && <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />}
          {transition.isPending ? t(($) => $.handoff.pending) : t(($) => $.handoff.enter, { name: target.name })}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
