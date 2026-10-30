"use client";

import { useMemo } from "react";
import {
  ALL_STATUSES,
  BUILT_IN_STATUS_CATEGORY,
  BUILT_IN_STATUS_ORDER,
} from "@multica/core/issues/config";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useProjectWithWorkflow } from "@multica/core/issue-workflows";
import {
  issueStatusColor,
  normalizeIssueStatusCategory,
} from "@multica/core/issue-statuses/queries";
import type { IssueStatus, IssueStatusCategory, IssueWorkflow, Project } from "@multica/core/types";
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

const CATEGORY_RANK = new Map<IssueStatusCategory, number>(ALL_STATUSES.map((c, i) => [c, i]));

/**
 * The statuses an issue in `projectId` may take: its project workflow's steps
 * in workflow order, or the whole catalog when the project has none. Every
 * single-issue status control offers this set, so a status the workflow
 * refuses is never on offer. (MUL-7420)
 *
 * `projectId` is three-valued:
 * - a project id: that project's workflow;
 * - `null`: an issue outside any project. It offers the workspace Default
 *   workflow's statuses, as a project on Default does, so statuses another
 *   project's workflow added stay out of its list. Default has no handoffs
 *   here — the server applies none outside a project — so `workflow` stays
 *   null and `defaultScope` names the Default instead. The server still
 *   accepts any status, so `catalog` keeps the full set reachable by search,
 *   and `currentStatus` stays listed even when Default leaves it out.
 *   (MUL-8121)
 * - `undefined`: a surface spanning projects; the whole catalog.
 */
export function useIssueStatusOptions(
  wsId: string,
  projectId: string | null | undefined,
  currentStatus?: IssueStatus | null,
): {
  options: StatusOption[];
  workflow: IssueWorkflow | null;
  project: Project | null;
  defaultScope: IssueWorkflow | null;
  catalog: StatusOption[];
} {
  const catalogOptions = useStatusOptions(wsId);
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const { workflow, project, defaultWorkflow } = useProjectWithWorkflow(wsId, projectId);
  const defaultScope = projectId === null && !workflow ? defaultWorkflow ?? null : null;
  const options = useMemo<StatusOption[]>(() => {
    const listed = workflow ?? defaultScope;
    if (!listed) return catalogOptions;
    const byKey = new Map(catalogOptions.map((o) => [o.key, o]));
    const optionOf = (key: IssueStatus): StatusOption =>
      byKey.get(key) ?? {
        key,
        category: categoryOf(key),
        label: labelOf(key),
        color: colorOf(key),
        icon: iconOf(key),
      };
    const steps = listed.steps.map((step) => optionOf(step.status_key));
    if (!defaultScope || !currentStatus || steps.some((o) => o.key === currentStatus)) return steps;
    // The current status is outside Default (e.g. the issue left a project
    // whose workflow added it): keep it visible and checked, after the last
    // step of its category, so it can still be seen and moved off.
    const current = optionOf(currentStatus);
    const rank = CATEGORY_RANK.get(current.category) ?? ALL_STATUSES.length;
    let at = steps.length;
    while (at > 0 && (CATEGORY_RANK.get(steps[at - 1]!.category) ?? 0) > rank) at -= 1;
    return [...steps.slice(0, at), current, ...steps.slice(at)];
  }, [catalogOptions, categoryOf, colorOf, currentStatus, defaultScope, iconOf, labelOf, workflow]);
  return { options, workflow, project, defaultScope, catalog: catalogOptions };
}
