"use client";

import { useMemo } from "react";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { IssueStatus, IssueStatusCategory } from "@multica/core/types";
import { useStatusLabel } from "./status-label";

export interface StatusOption {
  key: IssueStatus;
  label: string;
  /** `#rrggbb` for a custom status; null for a built-in, which keeps its token color. */
  color: string | null;
}

export interface StatusOptionGroup {
  category: IssueStatusCategory;
  options: StatusOption[];
}

/**
 * The statuses a user can pick or filter by, grouped by category in canonical
 * category order (MUL-6243).
 *
 * Shared by the status picker and the status filter so the two can never drift
 * — a status offered in one and missing from the other is exactly how an issue
 * becomes unfindable.
 *
 * Archived statuses are excluded: archiving retires a status from future
 * assignment. Issues already on one keep it, and `useStatusLabel` still names
 * it, because the catalog query keeps archived rows.
 */
export function useStatusOptions(wsId: string): {
  groups: StatusOptionGroup[];
  /** Flat list in the same order, for surfaces without grouping. */
  options: StatusOption[];
  /** True once any category holds more than one status — i.e. group headings earn their space. */
  hasCustom: boolean;
} {
  const { activeStatuses } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);

  return useMemo(() => {
    const groups = ALL_STATUSES.map((category) => {
      const entries = activeStatuses.filter((e) => e.category === category);
      const options: StatusOption[] =
        entries.length > 0
          ? entries.map((e) => ({
              key: e.key as IssueStatus,
              label: labelOf(e.key),
              color: e.is_system ? null : e.color,
            }))
          : // No catalog row for this category: the fetch is still in flight,
            // or this workspace predates the seed. Offer the built-in, whose
            // key IS the category, so a lifecycle step is never missing.
            [{ key: category as IssueStatus, label: labelOf(category), color: null }];
      return { category, options };
    });

    return {
      groups,
      options: groups.flatMap((g) => g.options),
      hasCustom: groups.some((g) => g.options.length > 1),
    };
  }, [activeStatuses, labelOf]);
}
