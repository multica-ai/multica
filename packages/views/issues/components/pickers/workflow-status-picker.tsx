"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Issue, IssueWorkflowStatusNode } from "@multica/core/types";
import { issueWorkflowOptions, issueAutomationExecutionsOptions, workflowHandoff, workflowPhaseCategory, activeWorkflowStatuses } from "@multica/core/issue-workflows";
import { useTransitionIssueStatusNode } from "@multica/core/issues/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { PropertyPicker, PickerItem } from "./property-picker";
import { StatusIcon } from "../status-icon";
import { WorkflowEntryEffects, WorkflowTransitionDialog } from "../workflow-transition-dialog";
import { useT } from "../../../i18n";

const SEARCH_THRESHOLD = 9;

/** Stable-node status picker for one concrete, workflow-pinned issue. */
export function WorkflowStatusPicker({ issue, align = "start", trigger, open: controlledOpen, onOpenChange }: {
  issue: Issue;
  trigger?: React.ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const wsId = useWorkspaceId();
  const workflowId = issue.workflow_id ?? "";
  const workflowQuery = useQuery(issueWorkflowOptions(wsId, workflowId));
  const data = workflowQuery.data;
  const transition = useTransitionIssueStatusNode();
  const executions = useQuery(issueAutomationExecutionsOptions(wsId, issue.id));
  const { active } = workflowHandoff(issue, [], executions.data ?? []);
  const [preview, setPreview] = useState<{ node: IssueWorkflowStatusNode; revision: number } | null>(null);
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = (value: boolean) => { setInternalOpen(value); onOpenChange?.(value); };
  const [query, setQuery] = useState("");
  const { t } = useT("issues");

  const activeStatuses = useMemo(
    () => activeWorkflowStatuses(data),
    [data],
  );
  const current = data?.statuses.find((status) => status.id === issue.workflow_status_id);
  const options = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return normalized
      ? activeStatuses.filter((status) => status.name.toLowerCase().includes(normalized))
      : activeStatuses;
  }, [activeStatuses, query]);

  if (!workflowId) return null;
  const currentCategory = workflowPhaseCategory(current?.phase ?? "started");

  return (
    <>
    <PropertyPicker
      open={open}
      onOpenChange={(next) => {
        if (!next) setQuery("");
        setOpen(next);
      }}
      width="w-72"
      footer={workflowQuery.isError || transition.isError ? <div role="alert" className="p-2 text-caption text-destructive">{t(($) => $.handoff.error)} <button type="button" className="underline" onClick={() => void workflowQuery.refetch()}>{t(($) => $.workflow_selection.retry)}</button></div> : workflowQuery.isPending ? <p role="status" className="p-2 text-caption text-muted-foreground">{t(($) => $.workflow_selection.loading)}</p> : undefined}
      align={align}
      searchable={activeStatuses.length > SEARCH_THRESHOLD}
      searchPlaceholder={t(($) => $.filters.search_status)}
      onSearchChange={setQuery}
      trigger={trigger ?? (current ? (
        <>
          <StatusIcon
            status={current.legacy_status_key ?? issue.status}
            category={currentCategory}
            color={current.color}
            className="h-3.5 w-3.5 shrink-0"
          />
          <span className="truncate">{current.name}</span>
        </>
      ) : <span className="truncate">{issue.status_name || t(($) => $.workflow_selection.choose)}</span>)}
    >
      {options.map((status) => {
        const category = workflowPhaseCategory(status.phase);
        return (
          <PickerItem
            key={status.id}
            selected={status.id === issue.workflow_status_id}
            hoverClassName={STATUS_CONFIG[category].hoverBg}
            disabled={transition.isPending}
            onClick={() => {
              if (!data?.workflow.id) return;
              if (status.id === issue.workflow_status_id) { setOpen(false); return; }
              if (!executions.isSuccess || active || status.entry_policy.executor.type !== "none" || status.entry_policy.assignee.type !== "keep") {
                setPreview({ node: status, revision: data.workflow.revision });
                setOpen(false);
                return;
              }
              transition.mutate({
                id: issue.id, workflow_status_id: status.id, expected_revision: issue.revision,
                expected_transition_id: issue.transition_id ?? undefined,
                expected_workflow_revision: data.workflow.revision,
              }, { onSuccess: () => { setOpen(false); setQuery(""); } });
            }}
          >
            <StatusIcon
              status={status.legacy_status_key ?? issue.status}
              category={category}
              color={status.color}
              className="h-3.5 w-3.5"
            />
            <span className="min-w-0 flex-1 text-left"><span className="block truncate">{status.name}</span>
              {(status.entry_policy.executor.type !== "none" || status.entry_policy.assignee.type !== "keep") && <WorkflowEntryEffects node={status} />}
            </span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
    {preview && <WorkflowTransitionDialog issue={issue} target={preview.node} workflowRevision={preview.revision} onClose={() => setPreview(null)} />}
    </>
  );
}
