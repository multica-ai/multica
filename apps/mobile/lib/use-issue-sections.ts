import { useMemo, useState } from "react";
import type { Issue, IssueStatusEntry } from "@multica/core/types";
import { groupIssuesByStatus } from "./group-issues-by-status";

const EMPTY: string[] = [];

/** Fold state stays local to the mounted list and isolated by workspace/project. */
export function useIssueSections(
  issues: Issue[],
  statuses: IssueStatusEntry[],
  scopeKey: string,
) {
  const [collapsedByScope, setCollapsedByScope] = useState<Record<string, string[]>>({});
  const collapsed = collapsedByScope[scopeKey] ?? EMPTY;
  const sections = useMemo(
    () => groupIssuesByStatus(issues, statuses, "workflow").map((section) => ({
      ...section,
      count: section.data.length,
      expanded: !collapsed.includes(section.status),
      data: collapsed.includes(section.status) ? [] : section.data,
    })),
    [issues, statuses, collapsed],
  );
  const toggleSection = (status: string) => {
    setCollapsedByScope((current) => {
      const previous = current[scopeKey] ?? EMPTY;
      return {
        ...current,
        [scopeKey]: previous.includes(status)
          ? previous.filter((key) => key !== status)
          : [...previous, status],
      };
    });
  };
  return { sections, toggleSection };
}
