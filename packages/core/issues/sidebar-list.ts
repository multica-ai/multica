import type { IssueView } from "../api/schemas";
import { baselineFromQuery } from "../issue-views/baseline";
import type { BuiltinViewKey } from "../issue-views/sidebar-views";
import type {
  IssueStatusCategory,
  IssueTableFacetsRequest,
  IssueTableQuerySpec,
  IssueTableRelation,
  IssueTableScope,
  IssueTableSortField,
  Project,
} from "../types";
import { projectLastActivityAt } from "../projects/activity";
import { assigneeTypesForActorKind } from "./surface/scope";
import { effectiveNegatedFilters } from "./stores/view-store";
import { FILTER_DIMENSION_TABLE_FIELD, advancedFilterToTableQuery, advancedFilterUsesField } from "./stores/filter-tree";
import type { SidebarIssueAssignee, SidebarIssueSettings, SidebarProjectSettings } from "./stores/sidebar-tree-store";

/** Rows an expanded sidebar list shows before "Load more". */
export const SIDEBAR_FIRST_PAGE = 5;
/** Rows each "Load more" adds. */
export const SIDEBAR_PAGE_STEP = 10;

export const projectSidebarSource = (projectId: string) => `project:${projectId}`;
export const viewSidebarSource = (viewId: string) => `view:${viewId}`;
export const builtinSidebarSource = (builtin: BuiltinViewKey) => `builtin:${builtin}`;

interface StatusRef {
  key: string;
  category: IssueStatusCategory;
}

const FINISHED: ReadonlySet<IssueStatusCategory> = new Set(["done", "closed"]);

/** Status keys an expanded project lists, in catalog order. */
export function shownSidebarStatuses(
  settings: Pick<SidebarIssueSettings, "hiddenStatuses">,
  statuses: readonly StatusRef[],
): string[] {
  const hidden = new Set(
    settings.hiddenStatuses ?? statuses.filter((s) => FINISHED.has(s.category)).map((s) => s.key),
  );
  return statuses.filter((s) => !hidden.has(s.key)).map((s) => s.key);
}

/** The viewer relations each assignee setting lists through the table query, OR'd together. */
const ASSIGNEE_RELATIONS: Record<SidebarIssueAssignee, IssueTableRelation[]> = {
  anyone: [],
  me: ["assigned"],
  my_agents: ["my_agents"],
  me_and_my_agents: ["assigned", "my_agents"],
};

function projectRowRequest(
  scope: IssueTableScope,
  filters: IssueTableQuerySpec["filters"],
  settings: SidebarIssueSettings,
  statuses: readonly StatusRef[],
  userId: string | undefined,
): SidebarIssuesRequest | null {
  const shown = shownSidebarStatuses(settings, statuses);
  if (shown.length === 0) return null;
  if (settings.assignee !== "anyone" && !userId) return null;
  const relations = ASSIGNEE_RELATIONS[settings.assignee] ?? [];
  return {
    table: {
      scope,
      filters: {
        ...filters,
        ...(shown.length < statuses.length ? { statuses: shown } : {}),
        ...(relations.length > 0 ? { relations } : {}),
        include_sub_issues: settings.showSubIssues,
      },
      sort: { field: settings.sortBy, direction: settings.sortDirection },
    },
  };
}

/** The request for an expanded project, or `null` when the settings leave nothing to list. */
export function projectSidebarRequest(
  projectId: string,
  settings: SidebarIssueSettings,
  statuses: readonly StatusRef[],
  userId: string | undefined,
): SidebarIssuesRequest | null {
  return projectRowRequest({ kind: "project", project_id: projectId }, {}, settings, statuses, userId);
}

/** The Projects section's row for issues outside every project. */
export const NO_PROJECT_SIDEBAR_SOURCE = "project:none";

/** The request for the expanded "No project" row, listed like an expanded project. */
export function noProjectSidebarRequest(
  settings: SidebarIssueSettings,
  statuses: readonly StatusRef[],
  userId: string | undefined,
): SidebarIssuesRequest | null {
  return projectRowRequest({ kind: "workspace" }, { include_no_project: true }, settings, statuses, userId);
}

/** The facet key the table query gives issues outside every project. */
export const NO_PROJECT_FACET_KEY = "__none__";

/**
 * Counts open issues per project, the "No project" row's under
 * `NO_PROJECT_FACET_KEY`. Open is what a project's count means: any status
 * outside the done and closed categories, sub-issues included.
 */
export function noProjectCountRequest(statuses: readonly StatusRef[]): IssueTableFacetsRequest {
  return {
    query: {
      scope: { kind: "workspace" },
      filters: {
        statuses: statuses.filter((s) => !FINISHED.has(s.category)).map((s) => s.key),
        include_sub_issues: true,
      },
      sort: { field: "position", direction: "asc" },
    },
    facets: [{ kind: "project" }],
    include_total: false,
  };
}

/**
 * Counts what an expanded row lists, as the facet total. The server folds the
 * total into a facet's own scan only while the query does not filter that
 * facet's dimension, so the facet is one the request leaves unfiltered.
 */
export function sidebarCountRequest(request: SidebarIssuesRequest): IssueTableFacetsRequest {
  const { table } = request;
  return {
    query: { ...table, sort: { field: "position", direction: "asc" } },
    facets: [{ kind: table.filters.priorities?.length ? "creator" : "priority" }],
    include_total: true,
  };
}

const VIEW_SORTS = new Set<string>([
  "position",
  "status",
  "priority",
  "title",
  "created_at",
  "updated_at",
  "last_activity",
  "start_date",
  "due_date",
]);

type SidebarViewFields = Pick<IssueView, "query" | "display" | "scope_variant"> &
  Partial<Pick<IssueView, "scope_type" | "scope_id">>;

/** A sidebar list, read through the table query, which can express every view filter. */
export interface SidebarIssuesRequest {
  table: IssueTableQuerySpec;
}

function viewTableScope(view: SidebarViewFields): IssueTableScope | null {
  const assigneeTypes = assigneeTypesForActorKind(
    view.scope_variant === "members" || view.scope_variant === "agents" ? view.scope_variant : undefined,
  );
  const typed = assigneeTypes ? { assignee_types: assigneeTypes } : {};
  switch (view.scope_type ?? "workspace") {
    case "workspace":
      return { kind: "workspace", ...typed };
    case "project":
      return view.scope_id ? { kind: "project", project_id: view.scope_id, ...typed } : null;
    case "my": {
      const variant = view.scope_variant ?? "any";
      return variant === "assigned" || variant === "created" || variant === "involved" || variant === "any"
        ? { kind: "my", relation: variant }
        : null;
    }
    default:
      return null;
  }
}

function viewTableSort(display: Record<string, unknown>): IssueTableQuerySpec["sort"] {
  const field =
    typeof display.sortBy === "string" && (VIEW_SORTS.has(display.sortBy) || display.sortBy.startsWith("property:"))
      ? (display.sortBy as IssueTableSortField)
      : "position";
  return { field, direction: display.sortDirection === "desc" ? "desc" : "asc" };
}

/** Whether listing the view needs the viewer's unread issues. */
export function viewSidebarNeedsUnread(view: SidebarViewFields): boolean {
  return baselineFromQuery(view.query).raw.unreadFilter;
}

/**
 * The request for an expanded saved view: its scope, filters and ordering,
 * so the sidebar lists what opening the view shows. `null` when the view's
 * scope cannot be listed. A view filtered to unread needs `unreadIssueIds`
 * (see `viewSidebarNeedsUnread`); without them it lists nothing.
 */
export function viewSidebarRequest(
  view: SidebarViewFields,
  today: string,
  unreadIssueIds: readonly string[] = [],
): SidebarIssuesRequest | null {
  const scope = viewTableScope(view);
  if (!scope) return null;
  const { raw } = baselineFromQuery(view.query);
  // Same as the surface: a project view drops the project dimension, a "my"
  // view the viewer-relation one.
  const fields = {
    ...raw,
    ...(scope.kind === "project" ? { projectFilters: [], includeNoProject: false } : {}),
    ...(scope.kind === "my" ? { relationFilters: [] } : {}),
  };
  const negate = effectiveNegatedFilters(fields).map((dimension) => FILTER_DIMENSION_TABLE_FIELD[dimension]);
  const advanced = advancedFilterToTableQuery(fields.advancedFilter);
  const usesDueDates = fields.dueDateFilters.length > 0 || advancedFilterUsesField(fields.advancedFilter, "dueDate");
  const display = view.display ?? {};

  return {
    table: {
      scope,
      filters: {
        ...(fields.statusFilters.length > 0 ? { statuses: fields.statusFilters } : {}),
        ...(fields.priorityFilters.length > 0 ? { priorities: fields.priorityFilters } : {}),
        ...(fields.assigneeFilters.length > 0 ? { assignees: fields.assigneeFilters } : {}),
        ...(fields.includeNoAssignee ? { include_no_assignee: true } : {}),
        ...(fields.creatorFilters.length > 0 ? { creators: fields.creatorFilters } : {}),
        ...(fields.projectFilters.length > 0 ? { project_ids: fields.projectFilters } : {}),
        ...(fields.includeNoProject ? { include_no_project: true } : {}),
        ...(fields.projectStatusFilters.length > 0 ? { project_statuses: fields.projectStatusFilters } : {}),
        ...(fields.labelFilters.length > 0 ? { label_ids: fields.labelFilters } : {}),
        ...(fields.relationFilters.length > 0 ? { relations: fields.relationFilters } : {}),
        ...(fields.dueDateFilters.length > 0 ? { due_dates: fields.dueDateFilters } : {}),
        ...(usesDueDates ? { today } : {}),
        ...(negate.length > 0 ? { negate } : {}),
        ...(advanced ? { advanced } : {}),
        ...(Object.keys(fields.propertyFilters).length > 0 ? { properties: fields.propertyFilters } : {}),
        ...(fields.agentRunningFilter ? { working_only: true } : {}),
        ...(fields.agentIdleFilter ? { idle_only: true } : {}),
        ...(fields.unreadFilter ? { unread_issue_ids: [...unreadIssueIds].sort() } : {}),
        include_sub_issues: display.showSubIssues !== false,
      },
      sort: viewTableSort(display),
    },
  };
}

/** What each built-in adds to All issues, as the issues page applies it. */
const BUILTIN_FILTERS: Record<Exclude<BuiltinViewKey, "working">, IssueTableQuerySpec["filters"]> = {
  all_issues: {},
  my_work: { relations: ASSIGNEE_RELATIONS.me_and_my_agents },
  subscribed: { relations: ["subscribed"] },
};

/**
 * The request for an expanded built-in view: its filter, listed with the same
 * status, sub-issue and ordering settings as an expanded project. The
 * assignee setting is for projects only: a built-in already says whose
 * issues it lists. Working lists `workingIssueIds`, the issues agents are
 * running on, as the page does. `null` when the settings leave nothing to list.
 */
export function builtinSidebarRequest(
  builtin: BuiltinViewKey,
  settings: SidebarIssueSettings,
  statuses: readonly StatusRef[],
  workingIssueIds: readonly string[] = [],
): SidebarIssuesRequest | null {
  const shown = shownSidebarStatuses(settings, statuses);
  if (shown.length === 0) return null;
  return {
    table: {
      scope: { kind: "workspace" },
      filters: {
        ...(builtin === "working"
          ? { working_issue_ids: [...workingIssueIds].sort() }
          : BUILTIN_FILTERS[builtin]),
        ...(shown.length < statuses.length ? { statuses: shown } : {}),
        include_sub_issues: settings.showSubIssues,
      },
      sort: { field: settings.sortBy, direction: settings.sortDirection },
    },
  };
}

/** The projects the sidebar lists, filtered and ordered by its settings. */
export function sidebarProjects<
  P extends Pick<Project, "id" | "title" | "status" | "lead_type" | "lead_id" | "created_at" | "updated_at" | "archived_at">,
>(
  projects: readonly P[],
  settings: SidebarProjectSettings,
  manualOrder: readonly string[],
  userId: string | undefined,
): P[] {
  const statuses = new Set<string>(settings.statuses);
  const shown = projects.filter(
    (p) =>
      !p.archived_at &&
      statuses.has(p.status) &&
      (!settings.onlyMine || (p.lead_type === "member" && !!userId && p.lead_id === userId)),
  );
  switch (settings.ordering) {
    case "name":
      return shown.sort((a, b) => a.title.localeCompare(b.title));
    case "updated":
      return shown.sort((a, b) => Date.parse(projectLastActivityAt(b)) - Date.parse(projectLastActivityAt(a)));
    case "created":
      return shown.sort((a, b) => b.created_at.localeCompare(a.created_at));
    case "manual": {
      const rank = new Map(manualOrder.map((id, i) => [id, i]));
      return shown
        .map((p, i) => ({ p, i }))
        .sort((a, b) => (rank.get(a.p.id) ?? manualOrder.length + a.i) - (rank.get(b.p.id) ?? manualOrder.length + b.i))
        .map(({ p }) => p);
    }
  }
}

/** The "No project" row's id in the manual project order, beside the project ids. */
export const NO_PROJECT_ORDER_ID = "no_project";

/**
 * Where the "No project" row sits among the shown projects: first, until it
 * is dragged elsewhere. A sort by name or date leaves it first, since it is
 * not a project.
 */
export function noProjectRowIndex(
  shown: readonly Pick<Project, "id">[],
  settings: Pick<SidebarProjectSettings, "ordering">,
  manualOrder: readonly string[],
): number {
  const at = settings.ordering === "manual" ? manualOrder.indexOf(NO_PROJECT_ORDER_ID) : -1;
  if (at === -1) return 0;
  const before = new Set(manualOrder.slice(0, at));
  return shown.filter((project) => before.has(project.id)).length;
}
