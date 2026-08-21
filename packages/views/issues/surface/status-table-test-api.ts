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

/** One agent holding running issue tasks, as the working-agents projection
 *  reports it. `issue_ids` may name issues outside the queried surface — the
 *  facet is expected to drop those, which is the whole point of MUL-5525. */
export interface WorkingTaskFixture {
  id: string;
  issue_ids: readonly string[];
}

/**
 * Data for the `working_agents` facet. `rows` is read directly rather than
 * through the legacy list endpoint: several tests assert that a Table surface
 * never touches that endpoint, and a facet stand-in that called it would make
 * them pass or fail for the wrong reason.
 */
export interface WorkingAgentsFixture {
  rows: () => readonly Issue[];
  agents: () => readonly WorkingTaskFixture[];
}

/**
 * Group-key axis for a status grouping. Board / list / swimlane surfaces page by
 * CATEGORY (`status_category:<category>`); the table still groups by concrete
 * status key. This fixture holds only built-in statuses, where a key IS its own
 * category, so the two axes select the same rows — only the key shape differs.
 * (MUL-6243)
 */
function statusAxis(group: { kind: string }): string {
  return group.kind === "status_category" ? "status_category" : "status";
}

function secondaryAxis(group: { kind: string; secondary?: string }): string {
  return group.secondary === "status_category" ? "status_category" : "status";
}

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

// The working-agents facet drops only ITS dimension, so every other filter in
// the spec still applies. Mirrors the two predicates `rowsForStatus` enforces.
function workingAgentFacetValues(
  query: IssueTableQuerySpec,
  fixture: WorkingAgentsFixture,
) {
  const statuses = query.filters.statuses;
  const visible = new Set(
    fixture
      .rows()
      .filter((issue) => !statuses || statuses.includes(issue.status))
      .filter(
        (issue) =>
          query.filters.include_sub_issues !== false ||
          issue.parent_issue_id === null,
      )
      .map((issue) => issue.id),
  );
  return fixture.agents().flatMap((agent) => {
    const count = agent.issue_ids.filter((id) => visible.has(id)).length;
    return count > 0 ? [{ key: agent.id, count }] : [];
  });
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
export function statusTableMethodsFromLegacy(
  listIssues: LegacyListIssues,
  // Stand-in for the server's issue↔running-task join. Supply it to exercise
  // the `working_agents` facet; without it the facet answers empty, matching a
  // workspace where nothing is running.
  workingAgents: WorkingAgentsFixture = { rows: () => [], agents: () => [] },
) {
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
                    key: `compound:${descriptor.key}:${secondaryAxis(request.group)}:${status}`,
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
          key: `${statusAxis(request.group)}:${status}`,
          value: { kind: "status" as const, status },
          count: issues.length,
        })),
        next_cursor: null,
      };
    },
    listIssueTableRows: async (request: IssueTableRowsRequest) => {
      if (request.group.kind === "compound") {
        const primary = request.group.primary;
        const axis = `:${secondaryAxis(request.group)}:`;
        const marker = request.group_key?.lastIndexOf(axis) ?? -1;
        const status =
          marker >= 0 ? request.group_key?.slice(marker + axis.length) : undefined;
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
      const rawStatus = request.group_key?.replace(/^status(_category)?:/, "");
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
      // Only touch the legacy list endpoint when a status facet is actually
      // asked for: Table surfaces request other facets while asserting that
      // endpoint is never called.
      const groups = request.facets.some((facet) => facet.kind === "status")
        ? await Promise.all(
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
          )
        : [];
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
              : facet.kind === "working_agents"
                ? workingAgentFacetValues(request.query, workingAgents)
                : [],
        })),
      };
    },
  };
}
