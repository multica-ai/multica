"use client";

import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { IssueStatusCatalog } from "@multica/core/issue-statuses";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueStatus } from "@multica/core/types";
import { StatusIcon } from "./status-icon";

/**
 * Whether {@link CustomStatusChip} would render something for this status.
 *
 * Layouts that wrap the chip in a container need this: without it a card whose
 * only chip is the status chip would render an empty flex row with its own
 * margin whenever the chip decides to stay silent.
 */
export function useIsCustomStatus(status: IssueStatus): boolean {
  const wsId = useWorkspaceId();
  return isCustomStatus(useIssueStatuses(wsId), status);
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
}: {
  status: IssueStatus;
  className?: string;
}) {
  const wsId = useWorkspaceId();
  // ONE catalog observer for both the predicate and the entry — subscribing
  // twice per card is free on the network (React Query dedupes the request) but
  // not on re-renders.
  const catalog = useIssueStatuses(wsId);
  const entry = catalog.entryOf(status);

  if (!isCustomStatus(catalog, status) || !entry) return null;

  return (
    <span
      className={`inline-flex max-w-[160px] items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground ${className}`}
    >
      <StatusIcon
        status={status}
        category={catalog.categoryOf(status)}
        color={entry.color}
        icon={entry.icon}
        className="size-3"
      />
      <span className="truncate">{entry.name}</span>
    </span>
  );
}
