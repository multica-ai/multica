"use client";

import { useMemo } from "react";
import {
  ALL_STATUSES,
  BUILT_IN_STATUS_CATEGORY,
  BUILT_IN_STATUS_ORDER,
} from "@multica/core/issues/config";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  issueStatusColor,
  normalizeIssueStatusCategory,
} from "@multica/core/issue-statuses/queries";
import type { IssueStatus, IssueStatusCategory } from "@multica/core/types";
import { useStatusLabel } from "./status-label";

export interface StatusOption {
  key: IssueStatus;
  /** Lifecycle category; not the custom status's icon or automation behavior. */
  category: IssueStatusCategory;
  label: string;
  /** `#rrggbb` for a custom status; null for a built-in, which keeps its token color. */
  color: string | null;
  icon?: string | null;
}

/**
 * The statuses a user can pick or filter by, as one flat list in canonical
 * category order (MUL-6243, MUL-6399).
 *
 * Category is carried per option rather than expressed as a heading. Shape
 * and color belong to the concrete status and convey no automation behavior.
 *
 * Shared by the status picker and the status filter so the two can never drift
 * — a status offered in one and missing from the other is exactly how an issue
 * becomes unfindable.
 *
 * Archived statuses are excluded by default: archiving retires a status from
 * future assignment. A read-only filter can opt specific current keys back in
 * through `includeArchivedKeys`, so existing issues remain findable without a
 * picker offering a retired value for assignment.
 */
const NO_ARCHIVED_STATUS_KEYS: readonly IssueStatus[] = [];

export function useStatusOptions(
  wsId: string,
  includeArchivedKeys: readonly IssueStatus[] = NO_ARCHIVED_STATUS_KEYS,
): StatusOption[] {
  const { statuses } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);

  return useMemo(
    () => {
      const includedArchived = new Set(includeArchivedKeys);
      return ALL_STATUSES.flatMap<StatusOption>((category) => {
        const entries = statuses.filter(
          (entry) =>
            normalizeIssueStatusCategory(entry.category) === category &&
            (!entry.archived_at || includedArchived.has(entry.key)),
        );
        // No catalog row for this category: the fetch is still in flight, or
        // this workspace predates the seed. Offer every built-in in the
        // category so the seven concrete status choices remain available.
        if (entries.length === 0) {
          return BUILT_IN_STATUS_ORDER.filter(
            (key) => BUILT_IN_STATUS_CATEGORY[key] === category,
          ).map((key) => ({
            key,
            category,
            label: labelOf(key),
            color: null,
          }));
        }
        return entries.map((e) => ({
          key: e.key as IssueStatus,
          category,
          label: labelOf(e.key),
          color: issueStatusColor(e),
          icon: e.icon,
        }));
      });
    },
    [includeArchivedKeys, labelOf, statuses],
  );
}
