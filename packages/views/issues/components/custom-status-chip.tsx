"use client";

import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { IssueStatusCatalog } from "@multica/core/issue-statuses";
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
  // `is_system` is the authority; the key comparison covers the window before
  // the catalog lands, where a built-in must still stay silent.
  return entry.is_system !== true && status !== catalog.categoryOf(status);
}

/**
 * Names an issue's status when the surface around it only shows the CATEGORY
 * (MUL-6243).
 *
 * Board columns and list sections are categories, so two issues sitting in the
 * same "In Review" column can be on different statuses — "Code Review" and "QA"
 * — with nothing on the card to tell them apart. This chip is that missing
 * signal.
 *
 * It renders NOTHING for a status that already is its category's built-in: the
 * column header says "In Review" and a chip repeating it is pure noise. So a
 * workspace that never defined a custom status sees no visual change at all.
 */
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
        category={entry.category}
        color={entry.color}
        className="size-3"
      />
      <span className="truncate">{entry.name}</span>
    </span>
  );
}
