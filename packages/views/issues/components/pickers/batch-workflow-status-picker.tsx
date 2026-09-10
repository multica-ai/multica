"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Issue } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  activeWorkflowStatuses,
  issueWorkflowOptions,
} from "@multica/core/issue-workflows";
import { useTransitionIssueStatusNode } from "@multica/core/issues/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { WorkflowNodePicker } from "./workflow-node-picker";
import { PropertyPicker } from "./property-picker";
import { useT } from "../../../i18n";

export function BatchWorkflowStatusPicker({
  issues,
  disabled,
}: {
  issues: Issue[];
  disabled: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const flow = issues[0]?.workflow_id;
  const sameFlow =
    !!flow && issues.every((issue) => issue.workflow_id === flow);
  const query = useQuery(issueWorkflowOptions(wsId, sameFlow ? flow : ""));
  const transition = useTransitionIssueStatusNode();
  const label = t(($) => $.batch.status);
  const trigger = (
    <Button variant="ghost" size="sm" disabled={disabled || pending} />
  );
  if (!sameFlow || !query.isSuccess)
    return (
      <PropertyPicker
        open={open}
        onOpenChange={setOpen}
        trigger={label}
        triggerRender={trigger}
      >
        <p className="p-2 text-caption text-muted-foreground">
          {!sameFlow
            ? t(($) => $.workflow_selection.mixed)
            : query.isError
              ? t(($) => $.workflow_selection.load_error)
              : t(($) => $.workflow_selection.loading)}
        </p>
        {sameFlow && query.isError && (
          <Button onClick={() => void query.refetch()}>
            {t(($) => $.workflow_selection.retry)}
          </Button>
        )}
      </PropertyPicker>
    );
  const firstId = issues[0]?.workflow_status_id;
  return (
    <WorkflowNodePicker
      nodes={activeWorkflowStatuses(query.data)}
      value={
        issues.every((issue) => issue.workflow_status_id === firstId)
          ? firstId
          : null
      }
      trigger={label}
      triggerRender={trigger}
      disabled={disabled || pending}
      onChange={async (node) => {
        setPending(true);
        const targets = issues.filter(
          (issue) => issue.workflow_status_id !== node.id,
        );
        const results = await Promise.allSettled(
          targets.map((issue) =>
            transition.mutateAsync({
              id: issue.id,
              workflow_status_id: node.id,
              expected_revision: issue.revision,
              expected_transition_id: issue.transition_id ?? undefined,
              expected_workflow_revision: query.data.workflow.revision,
            }),
          ),
        );
        setPending(false);
        const failures = results.filter(
          (result) => result.status === "rejected",
        ).length;
        if (failures)
          toast.error(
            t(($) => $.workflow_selection.batch_failed, { count: failures }),
          );
        else
          toast.success(
            t(($) => $.batch.update_success, { count: targets.length }),
          );
      }}
    />
  );
}
