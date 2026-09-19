"use client";

import { useQuery } from "@tanstack/react-query";
import { issueWorkflowOptions } from "@multica/core/issue-workflows";
import { statusCategoryOfKey } from "@multica/core/issues";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { IssueStatusCatalog } from "@multica/core/issue-statuses";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueStatus, IssueStatusEntry } from "@multica/core/types";
import { StatusIcon } from "./status-icon";

/**
 * Whether {@link CustomStatusChip} would render something for this status.
 *
 * Layouts that wrap the chip in a container need this: without it a card whose
 * only chip is the status chip would render an empty flex row with its own
 * margin whenever the chip decides to stay silent.
 */
type WorkflowIdentity = { workflowId?: string | null; workflowStatusId?: string | null };

function useCustomStatusEntry(status: IssueStatus, { workflowId, workflowStatusId }: WorkflowIdentity = {}): IssueStatusEntry | undefined {
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const definition = useQuery(issueWorkflowOptions(wsId, workflowId ?? ""));
  if (workflowId && workflowStatusId) {
    const node = definition.data?.statuses.find((node) => node.id === workflowStatusId);
    if (!node || (definition.data?.workflow.scope_type === "workspace" && node.legacy_status_key && isBuiltInIssueStatus(node.legacy_status_key))) return undefined;
    return { ...node, key: node.id, workspace_id: wsId, category: statusCategoryOfKey(node.phase), is_system: false };
  }
  return isCustomStatus(catalog, status) ? catalog.entryOf(status) : undefined;
}

export function useIsCustomStatus(status: IssueStatus, workflow?: WorkflowIdentity): boolean {
  return !!useCustomStatusEntry(status, workflow);
}

/**
 * Pure predicate behind {@link useIsCustomStatus}, so a component that already
 * holds the catalog does not open a second observer for the same question.
 */
function isCustomStatus(catalog: IssueStatusCatalog, status: IssueStatus): boolean {
  const entry = catalog.entryOf(status);
  if (!entry) return false;
  return entry.is_system !== true && !isBuiltInIssueStatus(status);
}

/** Names custom statuses on cards, including when grouped by project/assignee.
 * Built-ins remain silent to preserve the compact default card layout. */
export function CustomStatusChip({
  status,
  className = "",
  workflowId,
  workflowStatusId,
}: WorkflowIdentity & {
  status: IssueStatus;
  className?: string;
}) {
  const entry = useCustomStatusEntry(status, { workflowId, workflowStatusId });
  if (!entry) return null;

  return (
    <span
      className={`inline-flex max-w-[160px] items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground ${className}`}
    >
      <StatusIcon
        status={status}
        category={entry.category}
        color={entry.color}
        icon={entry.icon}
        className="size-3"
      />
      <span className="truncate">{entry.name}</span>
    </span>
  );
}
