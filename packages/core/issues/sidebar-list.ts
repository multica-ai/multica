import type { IssueView } from "../api/schemas";
import { baselineFromQuery } from "../issue-views/baseline";
import type { BuiltinViewKey } from "../issue-views/sidebar-views";
import type {
  IssueStatusCategory,
  IssueTableQuerySpec,
  IssueTableRelation,
  IssueTableScope,
  IssueTableSortField,
  ListIssuesParams,
  Project,
} from "../types";
import { assigneeTypesForActorKind } from "./surface/scope";
import { effectiveNegatedFilters } from "./stores/view-store";
import { FILTER_DIMENSION_TABLE_FIELD, advancedFilterToTableQuery, advancedFilterUsesField } from "./stores/filter-tree";
import type { MyIssuesRelation } from "./stores/my-issues-view-store";
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

/** The request for an expanded project, or `null` when the settings leave nothing to list. */
export function projectSidebarRequest(
  projectId: string,
  settings: SidebarIssueSettings,
  statuses: readonly StatusRef[],
  userId: string | undefined,
): SidebarIssuesRequest | null {
  const shown = shownSidebarStatuses(settings, statuses);
  if (shown.length === 0) return null;
  if (settings.assignee !== "anyone" && !userId) return null;
  const statusFilter = shown.length < statuses.length ? { statuses: shown } : {};
  // The list endpoint cannot OR the user with their agents: `involves_user_id`
  // never matches issues assigned to the user directly.
  if (settings.assignee === "me_and_my_agents") {
    return {
      table: {
        scope: { kind: "project", project_id: projectId },
        filters: {
          ...statusFilter,
          relations: ASSIGNEE_RELATIONS.me_and_my_agents,
          include_sub_issues: settings.showSubIssues,
        },
        sort: { field: settings.sortBy, direction: settings.sortDirection },
      },
    };
  }
  return {
    project_id: projectId,
    ...statusFilter,
    ...(settings.assignee === "me" ? { assignee_id: userId } : {}),
    ...(settings.assignee === "my_agents" ? { involves_user_id: userId } : {}),
    ...(settings.showSubIssues ? {} : { top_level_only: true }),
    sort_by: settings.sortBy,
    sort_direction: settings.sortDirection,
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

/** A sidebar list read through the table query, which can express every view filter. */
export interface SidebarTableRequest {
  table: IssueTableQuerySpec;
}

export type SidebarIssuesRequest = ListIssuesParams | SidebarTableRequest;

export const isSidebarTableRequest = (request: SidebarIssuesRequest): request is SidebarTableRequest =>
  "table" in request;

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

/**
 * The request for an expanded saved view: its scope, filters and ordering,
 * so the sidebar lists what opening the view shows. `null` when the view's
 * scope cannot be listed.
 */
export function viewSidebarRequest(view: SidebarViewFields, today: string): SidebarTableRequest | null {
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
        include_sub_issues: display.showSubIssues !== false,
      },
      sort: viewTableSort(display),
    },
  };
}

/**
 * The request for an expanded built-in view: the page's scope, listed with
 * the same sidebar settings as an expanded project. `null` when the settings
 * leave nothing to list.
 */
export function builtinSidebarRequest(
  builtin: BuiltinViewKey,
  myRelation: MyIssuesRelation,
  settings: SidebarIssueSettings,
  statuses: readonly StatusRef[],
): SidebarTableRequest | null {
  const shown = shownSidebarStatuses(settings, statuses);
  if (shown.length === 0) return null;
  const relation = builtin === "my_agents" ? "involved" : myRelation === "all" ? "any" : myRelation;
  const relations = ASSIGNEE_RELATIONS[settings.assignee] ?? [];
  return {
    table: {
      scope: builtin === "all_issues" ? { kind: "workspace" } : { kind: "my", relation },
      filters: {
        ...(shown.length < statuses.length ? { statuses: shown } : {}),
        ...(relations.length > 0 ? { relations } : {}),
        include_sub_issues: settings.showSubIssues,
      },
      sort: { field: settings.sortBy, direction: settings.sortDirection },
    },
  };
}

/** The projects the sidebar lists, filtered and ordered by its settings. */
export function sidebarProjects<P extends Pick<Project, "id" | "title" | "status" | "lead_type" | "lead_id" | "created_at" | "updated_at">>(
  projects: readonly P[],
  settings: SidebarProjectSettings,
  manualOrder: readonly string[],
  userId: string | undefined,
): P[] {
  const statuses = new Set<string>(settings.statuses);
  const shown = projects.filter(
    (p) =>
      statuses.has(p.status) &&
      (!settings.onlyMine || (p.lead_type === "member" && !!userId && p.lead_id === userId)),
  );
  switch (settings.ordering) {
    case "name":
      return shown.sort((a, b) => a.title.localeCompare(b.title));
    case "updated":
      return shown.sort((a, b) => b.updated_at.localeCompare(a.updated_at));
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
