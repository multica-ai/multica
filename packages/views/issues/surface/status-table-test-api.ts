import { ALL_STATUSES } from "@multica/core/issues/config";
import type {
  Issue,
  IssueTableGroupDescriptor,
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

async function allRows(
  listIssues: LegacyListIssues,
  query: IssueTableQuerySpec,
) {
  const rows = await Promise.all(
    ALL_STATUSES.map((status) =>
      rowsForStatus(
        listIssues,
        { ...query, filters: { ...query.filters, statuses: undefined } },
        status,
      ),
    ),
  );
  return rows.flat();
}

function primaryDescriptor(
  issue: Issue,
  primary: "assignee" | "project" | "parent",
  issueById: ReadonlyMap<string, Issue>,
): Omit<IssueTableGroupDescriptor, "count" | "secondary_groups"> {
  if (primary === "assignee") {
    const actor =
      issue.assignee_type && issue.assignee_id
        ? { type: issue.assignee_type, id: issue.assignee_id }
        : null;
    return {
      key: actor
        ? `assignee:${actor.type}:${actor.id}`
        : "assignee:unassigned",
      value: { kind: "assignee", actor },
    };
  }
  if (primary === "project") {
    return {
      key: issue.project_id ? `project:${issue.project_id}` : "project:none",
      value: { kind: "project", project_id: issue.project_id },
    };
  }
  const parent = issue.parent_issue_id
    ? issueById.get(issue.parent_issue_id) ?? null
    : null;
  return {
    key: issue.parent_issue_id
      ? `parent:${issue.parent_issue_id}`
      : "parent:none",
    value: {
      kind: "parent",
      parent_id: issue.parent_issue_id,
      parent: parent
        ? {
            id: parent.id,
            number: parent.number,
            identifier: parent.identifier,
            title: parent.title,
            status: parent.status,
          }
        : null,
      value_state: issue.parent_issue_id
        ? parent
          ? "value"
          : "unavailable"
        : "unset",
    },
  };
}

/**
 * Transitional adapter for pre-Table test fixtures. Production code never
 * imports this module; it lets existing surface tests keep their small
 * per-status in-memory data source while asserting the new request contract.
 */
export function statusTableMethodsFromLegacy(listIssues: LegacyListIssues) {
  return {
    listIssueTableGroups: async (request: IssueTableGroupsRequest) => {
      if (request.group.kind === "compound") {
        const issues = await allRows(listIssues, request.query);
        const issueById = new Map(issues.map((issue) => [issue.id, issue]));
        const grouped = new Map<
          string,
          {
            descriptor: ReturnType<typeof primaryDescriptor>;
            issues: Issue[];
          }
        >();
        for (const issue of issues) {
          const descriptor = primaryDescriptor(
            issue,
            request.group.primary,
            issueById,
          );
          const current = grouped.get(descriptor.key) ?? {
            descriptor,
            issues: [],
          };
          current.issues.push(issue);
          grouped.set(descriptor.key, current);
        }
        return {
          query_fingerprint: "test",
          total: issues.length,
          groups: Array.from(grouped.values(), ({ descriptor, issues }) => ({
            ...descriptor,
            count: issues.length,
            secondary_groups: ALL_STATUSES.flatMap((status) => {
              const count = issues.filter((issue) => issue.status === status).length;
              return count
                ? [{
                    key: `compound:${descriptor.key}:status:${status}`,
                    value: { kind: "status" as const, status },
                    count,
                  }]
                : [];
            }),
          })),
          next_cursor: null,
        };
      }
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
      if (request.group.kind === "compound") {
        const primary = request.group.primary;
        const marker = request.group_key?.lastIndexOf(":status:") ?? -1;
        const status =
          marker >= 0
            ? request.group_key?.slice(marker + ":status:".length)
            : undefined;
        const primaryKey =
          marker >= 0
            ? request.group_key?.slice("compound:".length, marker)
            : undefined;
        const all = await allRows(listIssues, request.query);
        const issueById = new Map(all.map((issue) => [issue.id, issue]));
        const issues = all.filter((issue) => {
          const descriptor = primaryDescriptor(
            issue,
            primary,
            issueById,
          );
          return descriptor.key === primaryKey && issue.status === status;
        });
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
      }
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
