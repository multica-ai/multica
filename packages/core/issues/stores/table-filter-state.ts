import type { IssueTableFilters, IssueTableNegatableFilter } from "../../types";
import { dateOnlyToLocalDate, toDateOnly, todayDateOnly } from "../date";
import {
  advancedFilterFromTableQuery,
  advancedFilterToTableQuery,
  advancedFilterUsesField,
  FILTER_DIMENSION_TABLE_FIELD,
  NEGATABLE_FILTER_DIMENSIONS,
  type NegatableFilterDimension,
} from "./filter-tree";
import type { FilterSnapshot, IssueDateFilter } from "./view-store";

/** The view-store fields a table query's filters map onto. */
export interface TableFilterState extends FilterSnapshot {
  dateFilter: IssueDateFilter | null;
  showSubIssues: boolean;
}

const TABLE_FIELD_DIMENSION = Object.fromEntries(
  NEGATABLE_FILTER_DIMENSIONS.map((dimension) => [FILTER_DIMENSION_TABLE_FIELD[dimension], dimension]),
) as Record<IssueTableNegatableFilter, NegatableFilterDimension>;

/**
 * Table-query filters as view-store state, so the issues page filter menu can
 * edit them. Running work maps to `agentRunningFilter` whether the query holds
 * the flag or a resolved ID list; unread maps to `unreadFilter` when the query
 * holds a membership list.
 */
export function tableFiltersToState(filters: IssueTableFilters): TableFilterState {
  const advanced = filters.advanced ? advancedFilterFromTableQuery(filters.advanced) : null;
  const end = filters.date ? new Date(filters.date.end) : null;
  end?.setDate(end.getDate() - 1);
  return {
    statusFilters: [...(filters.statuses ?? [])],
    priorityFilters: [...(filters.priorities ?? [])],
    assigneeFilters: (filters.assignees ?? []).map(({ type, id }) => ({ type, id })),
    includeNoAssignee: filters.include_no_assignee === true,
    creatorFilters: (filters.creators ?? []).map(({ type, id }) => ({ type, id })),
    projectFilters: [...(filters.project_ids ?? [])],
    includeNoProject: filters.include_no_project === true,
    projectStatusFilters: [...(filters.project_statuses ?? [])],
    labelFilters: [...(filters.label_ids ?? [])],
    relationFilters: [...(filters.relations ?? [])],
    dueDateFilters: [...(filters.due_dates ?? [])],
    negatedFilters: (filters.negate ?? []).flatMap((field) => TABLE_FIELD_DIMENSION[field] ?? []),
    propertyFilters: JSON.parse(JSON.stringify(filters.properties ?? {})),
    advancedFilter: advanced && advanced.children.length > 0 ? advanced : null,
    dateFilter: filters.date && end
      ? { field: filters.date.field, from: toDateOnly(new Date(filters.date.start)), to: toDateOnly(end) }
      : null,
    agentRunningFilter: filters.working_only === true || filters.working_issue_ids !== undefined,
    agentIdleFilter: filters.idle_only === true,
    unreadFilter: filters.unread_issue_ids !== undefined,
    showSubIssues: filters.include_sub_issues !== false,
  };
}

/**
 * View-store state back as table-query filters: the inverse of
 * {@link tableFiltersToState}. Running work is sent as `working_only` for the
 * server to resolve; unread needs the viewer's membership, passed as
 * `unreadIssueIds` (left out while unknown).
 */
export function stateToTableFilters(state: TableFilterState, unreadIssueIds?: readonly string[]): IssueTableFilters {
  const advanced = advancedFilterToTableQuery(state.advancedFilter);
  const properties = Object.fromEntries(Object.entries(state.propertyFilters).filter(([, values]) => values.length > 0));
  const negate = state.negatedFilters.map((dimension) => FILTER_DIMENSION_TABLE_FIELD[dimension]);
  const from = dateOnlyToLocalDate(state.dateFilter?.from), to = dateOnlyToLocalDate(state.dateFilter?.to);
  let date: IssueTableFilters["date"];
  if (state.dateFilter && from && to) {
    const end = new Date(from <= to ? to : from);
    end.setDate(end.getDate() + 1);
    date = { field: state.dateFilter.field, start: (from <= to ? from : to).toISOString(), end: end.toISOString() };
  }
  const usesDueDates = state.dueDateFilters.length > 0 || advancedFilterUsesField(state.advancedFilter, "dueDate");
  return {
    ...(state.statusFilters.length ? { statuses: [...state.statusFilters] } : {}),
    ...(state.priorityFilters.length ? { priorities: [...state.priorityFilters] } : {}),
    ...(state.assigneeFilters.length ? { assignees: state.assigneeFilters.map(({ type, id }) => ({ type, id })) } : {}),
    ...(state.includeNoAssignee ? { include_no_assignee: true } : {}),
    ...(state.creatorFilters.length ? { creators: state.creatorFilters.map(({ type, id }) => ({ type, id })) } : {}),
    ...(state.projectFilters.length ? { project_ids: [...state.projectFilters] } : {}),
    ...(state.includeNoProject ? { include_no_project: true } : {}),
    ...(state.projectStatusFilters.length ? { project_statuses: [...state.projectStatusFilters] } : {}),
    ...(state.labelFilters.length ? { label_ids: [...state.labelFilters] } : {}),
    ...(state.relationFilters.length ? { relations: [...state.relationFilters] } : {}),
    ...(state.dueDateFilters.length ? { due_dates: [...state.dueDateFilters] } : {}),
    ...(usesDueDates ? { today: todayDateOnly() } : {}),
    ...(negate.length ? { negate } : {}),
    ...(advanced ? { advanced } : {}),
    ...(Object.keys(properties).length ? { properties } : {}),
    ...(date ? { date } : {}),
    ...(state.agentRunningFilter ? { working_only: true } : {}),
    ...(state.agentIdleFilter ? { idle_only: true } : {}),
    ...(state.unreadFilter && unreadIssueIds ? { unread_issue_ids: [...new Set(unreadIssueIds)] } : {}),
    ...(state.showSubIssues ? {} : { include_sub_issues: false }),
  };
}
