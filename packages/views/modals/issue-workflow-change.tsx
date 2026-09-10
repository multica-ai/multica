"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import {
  useUpdateIssue,
  useTransitionIssueStatusNode,
  type UpdateIssueMutationInput,
} from "@multica/core/issues/mutations";
import {
  activeWorkflowStatuses,
  issueAutomationExecutionsOptions,
  workflowHandoff,
  effectiveIssueWorkflowOptions,
  issueWorkflowOptions,
  workflowPhaseCategory,
} from "@multica/core/issue-workflows";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { WorkflowNodePicker } from "../issues/components/pickers/workflow-node-picker";
import { WorkflowEntryEffects } from "../issues/components/workflow-transition-dialog";
import type { IssueSurfaceMutationOptions } from "../issues/surface/actions-context";
import { useT } from "../i18n";

export function IssueWorkflowChangeModal({
  data,
  onClose,
}: {
  data: Record<string, unknown> | null;
  onClose: () => void;
}) {
  const wsId = useWorkspaceId();
  const { t } = useT("issues");
  const issueId = typeof data?.issueId === "string" ? data.issueId : "";
  const issueQuery = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const options = data?.options as IssueSurfaceMutationOptions | undefined;
  const close = () => {
    options?.onSettled?.();
    onClose();
  };
  if (!issueQuery.data)
    return (
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) close();
        }}
      >
        <DialogContent>
          <DialogTitle>{t(($) => $.workflow_selection.choose)}</DialogTitle>
          <p role={issueQuery.isError ? "alert" : "status"}>
            {issueQuery.isError
              ? t(($) => $.workflow_selection.load_error)
              : t(($) => $.workflow_selection.loading)}
          </p>
          {issueQuery.isError && (
            <Button onClick={() => void issueQuery.refetch()}>
              {t(($) => $.workflow_selection.retry)}
            </Button>
          )}
        </DialogContent>
      </Dialog>
    );
  return (
    <ChangeForm
      issue={issueQuery.data}
      updates={(data?.updates ?? {}) as Omit<UpdateIssueMutationInput, "id">}
      options={options}
      onClose={close}
    />
  );
}

function ChangeForm({
  issue: loadedIssue,
  updates,
  options,
  onClose,
}: {
  issue: Issue;
  updates: Omit<UpdateIssueMutationInput, "id">;
  options?: IssueSurfaceMutationOptions;
  onClose: () => void;
}) {
  // Keep the cursor the user reviewed. A WS update must not silently rebase a confirmation.
  const [issue] = useState(loadedIssue);
  const wsId = useWorkspaceId();
  const { t } = useT("issues");
  const moving =
    updates.project_id !== undefined && updates.project_id !== issue.project_id;
  const usesEffective = moving || !issue.workflow_id;
  const effectiveQuery = useQuery({
    ...effectiveIssueWorkflowOptions(
      wsId,
      moving ? (updates.project_id ?? null) : issue.project_id,
    ),
    enabled: usesEffective,
  });
  const pinnedQuery = useQuery(
    issueWorkflowOptions(
      wsId,
      usesEffective ? "" : (issue.workflow_id ?? ""),
    ),
  );
  const workflowQuery = usesEffective ? effectiveQuery : pinnedQuery;
  const executions = useQuery({
    ...issueAutomationExecutionsOptions(wsId, issue.id),
    enabled: !moving && !!issue.workflow_id,
  });
  const { active } = workflowHandoff(issue, [], executions.data ?? []);
  const executionReady = moving || !issue.workflow_id || executions.isSuccess;
  const nodes = activeWorkflowStatuses(workflowQuery.data);
  const [selectedId, setSelectedId] = useState<string>();
  const matches = !moving
    ? nodes.filter((node) =>
        updates.workflow_status_id
          ? node.id === updates.workflow_status_id
          : node.legacy_status_key === updates.status,
      )
    : [];
  const categoryMatches =
    !moving && !matches.length && updates.status
      ? nodes.filter(
          (node) => workflowPhaseCategory(node.phase) === updates.status,
        )
      : [];
  const inferred =
    matches.length === 1
      ? matches[0]
      : categoryMatches.length === 1
        ? categoryMatches[0]
        : undefined;
  const target =
    nodes.find((node) => node.id === selectedId) ??
    (selectedId ? undefined : inferred);
  const update = useUpdateIssue();
  const transition = useTransitionIssueStatusNode();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const save = async () => {
    if (!target || !workflowQuery.data || !executionReady || pending) return;
    setPending(true);
    try {
      let result: Issue;
      if (moving || !issue.workflow_id) {
        const { status: _status, ...rest } = updates;
        result = await update.mutateAsync({
          id: issue.id,
          ...rest,
          workflow_status_id: target.id,
          expected_revision: issue.revision,
          expected_transition_id: issue.transition_id ?? undefined,
        });
      } else {
        const response = await transition.mutateAsync({
          id: issue.id,
          workflow_status_id: target.id,
          expected_revision: issue.revision,
          expected_transition_id: issue.transition_id ?? undefined,
          expected_workflow_revision: workflowQuery.data.workflow.revision,
        });
        result = response.issue;
        const {
          status: _status,
          workflow_status_id: _node,
          expected_revision: _revision,
          expected_transition_id: _cursor,
          ...remaining
        } = updates;
        if (Object.values(remaining).some((value) => value !== undefined)) {
          result = await update.mutateAsync({
            id: issue.id,
            ...remaining,
            expected_revision: result.revision,
            expected_transition_id: result.transition_id ?? undefined,
          });
        }
      }
      options?.onSuccess?.(result);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.handoff.error));
      options?.onError?.(err);
    } finally {
      setPending(false);
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {moving
              ? t(($) => $.workflow_selection.move_title)
              : t(($) => $.workflow_selection.choose)}
          </DialogTitle>
          <DialogDescription>
            {moving
              ? t(($) => $.workflow_selection.move_description)
              : t(($) => $.handoff.effects_intro)}
          </DialogDescription>
        </DialogHeader>
        {workflowQuery.isSuccess ? (
          <WorkflowNodePicker
            nodes={nodes}
            value={target?.id}
            onChange={(node) => setSelectedId(node.id)}
            disabled={pending}
          />
        ) : (
          <p role={workflowQuery.isError ? "alert" : "status"}>
            {workflowQuery.isError
              ? t(($) => $.workflow_selection.load_error)
              : t(($) => $.workflow_selection.loading)}
          </p>
        )}
        {workflowQuery.isError && (
          <Button onClick={() => void workflowQuery.refetch()}>
            {t(($) => $.workflow_selection.retry)}
          </Button>
        )}
        {!moving && (active || !executionReady) && (
          <p className="text-caption text-muted-foreground">
            {active
              ? t(($) => $.handoff.stops)
              : executions.isError
                ? t(($) => $.handoff.error)
                : t(($) => $.handoff.loading)}
          </p>
        )}
        {target && !moving && <WorkflowEntryEffects node={target} />}
        {error && (
          <p role="alert" className="text-caption text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={onClose}>
            {t(($) => $.handoff.cancel)}
          </Button>
          <Button
            disabled={!target || !executionReady || pending || !!error}
            onClick={() => void save()}
          >
            {pending
              ? t(($) => $.handoff.pending)
              : moving
                ? t(($) => $.workflow_selection.move)
                : target
                  ? t(($) => $.handoff.enter, { name: target.name })
                  : t(($) => $.workflow_selection.choose)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
