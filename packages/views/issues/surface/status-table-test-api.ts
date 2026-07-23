import { ALL_STATUSES } from "@multica/core/issues/config";
import type {
  IssueStatus,
  IssueTableFacetsRequest,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
  ListIssuesParams,
  ListIssuesResponse,
} from "@multica/core/types";

type LegacyListIssues = (
  params?: ListIssuesParams,
) => Promise<ListIssuesResponse>;

function legacyParamsForStatus(
  query: IssueTableQuerySpec,
  status: IssueStatus,
): ListIssuesParams {
  const scope = query.scope;
  return {
    status,
    limit: 50,
    offset: 0,
    ...(scope.kind === "project" ? { project_id: scope.project_id } : {}),
    ...(scope.kind === "assignee" && scope.actor
      ? {
          assignee_type: scope.actor.type,
          assignee_id: scope.actor.id,
        }
      : {}),
    ...(scope.kind === "creator" && scope.actor
      ? {
          creator_type: scope.actor.type,
          creator_id: scope.actor.id,
        }
      : {}),
    ...(query.search ? { search: query.search } : {}),
  };
}

async function rowsForStatus(
  listIssues: LegacyListIssues,
  query: IssueTableQuerySpec,
  status: IssueStatus,
) {
  if (
    query.filters.statuses &&
    !query.filters.statuses.includes(status)
  ) {
    return [];
  }
  const response = await listIssues(legacyParamsForStatus(query, status));
  return response.issues.filter((issue) => {
    if (
      query.filters.include_sub_issues === false &&
      issue.parent_issue_id !== null
    ) {
      return false;
    }
    return issue.status === status;
  });
}

/**
 * Transitional adapter for pre-Table test fixtures. Production code never
 * imports this module; it lets existing surface tests keep their small
 * per-status in-memory data source while asserting the new request contract.
 */
export function statusTableMethodsFromLegacy(listIssues: LegacyListIssues) {
  return {
    listIssueTableGroups: async (request: IssueTableGroupsRequest) => {
      const groups = await Promise.all(
        ALL_STATUSES.map(async (status) => ({
          status,
          issues: await rowsForStatus(listIssues, request.query, status),
        })),
      );
      const nonEmpty = groups.filter(({ issues }) => issues.length > 0);
      return {
        query_fingerprint: "test",
        total: nonEmpty.reduce((sum, group) => sum + group.issues.length, 0),
        groups: nonEmpty.map(({ status, issues }) => ({
          key: `status:${status}`,
          value: { kind: "status" as const, status },
          count: issues.length,
        })),
        next_cursor: null,
      };
    },
    listIssueTableRows: async (request: IssueTableRowsRequest) => {
      const rawStatus = request.group_key?.replace(/^status:/, "");
      const status = ALL_STATUSES.find((value) => value === rawStatus);
      const issues = status
        ? await rowsForStatus(listIssues, request.query, status)
        : [];
      return {
        query_fingerprint: "test",
        group_key: request.group_key,
        parent_id: request.parent_id,
        total: 0,
        rows: issues.map((issue) => ({
          issue,
          direct_child_count: 0,
        })),
        branch_total: issues.length,
        next_cursor: null,
      };
    },
    listIssueTableFacets: async (request: IssueTableFacetsRequest) => {
      const groups = await Promise.all(
        ALL_STATUSES.map(async (status) => ({
          status,
          issues: await rowsForStatus(
            listIssues,
            {
              ...request.query,
              filters: {
                ...request.query.filters,
                statuses: undefined,
              },
            },
            status,
          ),
        })),
      );
      return {
        query_fingerprint: "test",
        total: groups.reduce((sum, group) => sum + group.issues.length, 0),
        facets: request.facets.map((facet) => ({
          ...facet,
          values:
            facet.kind === "status"
              ? groups
                  .filter(({ issues }) => issues.length > 0)
                  .map(({ status, issues }) => ({
                    key: status,
                    count: issues.length,
                  }))
              : [],
        })),
      };
    },
  };
}
