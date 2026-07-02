import type { UseQueryOptions } from "@tanstack/react-query";
import {
  issueAssigneeGroupsOptions,
  issueListOptions,
  myIssueAssigneeGroupsOptions,
  myIssueListOptions,
  projectGanttIssuesOptions,
  type AssigneeGroupedIssuesFilter,
  type IssueSortParam,
} from "../queries";
import type {
  GroupedIssuesResponse,
  Issue,
  ListIssuesCache,
} from "../../types";
import type { IssueSurfaceQueryPlan } from "./query-plan";

/**
 * Issue surface repository — resolves a {@link IssueSurfaceQueryPlan} to the
 * concrete query options backing each render mode. Views declare WHICH
 * window they show (scope → plan) and WHICH mode is active; which endpoint,
 * cache key, and pagination style serve it is decided here in core, so the
 * view layer never branches on workspace-vs-scoped plumbing.
 */

/** Status-bucketed list — feeds the board / list / swimlane modes.
 *
 * Both branches share the exact cache shape (`ListIssuesCache` selected to
 * `Issue[]`); the cast below only erases the branch-specific query-key
 * literal so one `useQuery` call site can consume either plan kind. */
export function issueSurfaceListOptions(
  wsId: string,
  plan: IssueSurfaceQueryPlan,
  sort?: IssueSortParam,
): UseQueryOptions<ListIssuesCache, Error, Issue[]> {
  return (
    plan.kind === "workspace"
      ? issueListOptions(wsId, sort)
      : myIssueListOptions(wsId, plan.queryScope, plan.queryFilter, plan.userId, sort)
  ) as UseQueryOptions<ListIssuesCache, Error, Issue[]>;
}

/** Assignee-grouped list — feeds the board's group-by-assignee mode. */
export function issueSurfaceAssigneeGroupsOptions(
  wsId: string,
  plan: IssueSurfaceQueryPlan,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
): UseQueryOptions<GroupedIssuesResponse> {
  return (
    plan.kind === "workspace"
      ? issueAssigneeGroupsOptions(wsId, filter, sort)
      : myIssueAssigneeGroupsOptions(wsId, plan.queryScope, filter, plan.userId, sort)
  ) as UseQueryOptions<GroupedIssuesResponse>;
}

/** Scheduled-only issue set — feeds a project surface's Gantt mode. */
export function issueSurfaceGanttOptions(wsId: string, projectId: string) {
  return projectGanttIssuesOptions(wsId, projectId);
}
