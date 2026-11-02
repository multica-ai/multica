import type { Issue, IssueStatus, IssuePriority, IssueAssigneeGroup, ProjectStatus, PropertyFilterValue, PropertyOperatorFilter } from "@multica/core/types";
import type {
  ActorFilterValue,
  IssueRelationFilter,
  NegatableFilterDimension,
} from "@multica/core/issues/stores/view-store";
import {
  dueDateMatches,
  FILTER_NONE_VALUE,
  isPropertyCondition,
  propertyIdOfField,
  type AdvancedFilterCondition,
  type AdvancedFilterGroup,
  type DueDateFilter,
} from "@multica/core/issues/stores/filter-tree";
import { PROPERTY_FILTER_NONE_VALUE } from "@multica/core/issues/stores/filter-operators";
import { todayDateOnly } from "@multica/core/issues/date";
import type { IssueActivityState } from "../surface/activity";

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  /** Keeps an explicitly active assignee predicate distinct from the normal
   *  empty-array = no-filter state. When true with no selected assignees and
   *  includeNoAssignee=false, the predicate intentionally matches nothing. */
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  /** Lifecycle status of the parent project. Needs
   *  `IssueFilterContext.projectStatusById` to be evaluated — an Issue only
   *  carries `project_id`. */
  projectStatusFilters?: ProjectStatus[];
  /** See IssueFilterContext.projectStatusById. Carried on the filter object
   *  so the context-free `filterIssues` entry point can evaluate the
   *  project-status predicate, like `runningIssueIds` does for working-only. */
  projectStatusById?: ReadonlyMap<string, ProjectStatus>;
  labelFilters: string[];
  relationFilters?: IssueRelationFilter[];
  /** See IssueFilterContext.relation. */
  relationContext?: IssueRelationContext;
  dueDateFilters?: DueDateFilter[];
  /** See IssueFilterContext.today. */
  today?: string;
  advancedFilter?: AdvancedFilterGroup | null;
  negatedFilters?: readonly NegatableFilterDimension[];
  /** Custom-property filters: definition id → selected values (OR within
   *  a definition, AND across definitions; checkbox uses "true"/"false"). */
  propertyFilters?: Record<string, PropertyFilterValue[]>;
  // When `agentRunningFilter` is true, only keep issues whose id is in
  // `runningIssueIds`. The surface derives this set from the independent
  // `/api/working-agents` projection so filter.ts stays free of fetching.
  agentRunningFilter?: boolean;
  runningIssueIds?: ReadonlySet<string>;
  // "Show sub-issues" display toggle. When explicitly `false`, hide issues
  // that have a parent so only top-level issues remain. Undefined / true keeps
  // the default behaviour of showing everything, so existing callers that omit
  // it (and mobile's positional variant) are unaffected.
  showSubIssues?: boolean;
}

export interface IssueFilterState {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  /** See IssueFilters.assigneeFilterActive. */
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  /** See IssueFilters.projectStatusFilters. */
  projectStatusFilters?: ProjectStatus[];
  labelFilters: string[];
  relationFilters?: IssueRelationFilter[];
  dueDateFilters?: DueDateFilter[];
  /** A rule tree ANDed with everything else. */
  advancedFilter?: AdvancedFilterGroup | null;
  /** Dimensions evaluated as "is not". */
  negatedFilters?: readonly NegatableFilterDimension[];
  propertyFilters?: Record<string, PropertyFilterValue[]>;
  workingOnly: boolean;
  /** See IssueFilters.showSubIssues — only an explicit `false` hides. */
  showSubIssues?: boolean;
}

export interface IssueFilterContext {
  activityByIssueId?: ReadonlyMap<string, IssueActivityState>;
  runningIssueIds?: ReadonlySet<string>;
  /** Project id → its lifecycle status, from the workspace project list.
   *  Absent on surfaces that never load it, which makes the project-status
   *  filter a no-op there rather than blanking the surface. */
  projectStatusById?: ReadonlyMap<string, ProjectStatus>;
  /** Who "me" is for the relation filter. Absent until the viewer and their
   *  agents are known, which makes that filter a no-op. */
  relation?: IssueRelationContext;
  /** The viewer's calendar day ("YYYY-MM-DD") due-date buckets resolve
   *  against; defaults to the local today. */
  today?: string;
}

export interface IssueRelationContext {
  userId: string;
  /** Agents the viewer owns. */
  myAgentIds: ReadonlySet<string>;
}

/**
 * Filter value that selects issues where a custom property is UNSET ("No
 * value"). Mirrors the backend sentinel in `parsePropertiesFilterParam`
 * (`server/internal/handler/property.go`); cannot collide with a real option id
 * (select options are UUIDs, checkbox uses "true"/"false").
 */
export const NO_PROPERTY_VALUE = PROPERTY_FILTER_NONE_VALUE;

/**
 * Match one stored value against one operator filter member. Mirrors the
 * server-side operator predicates in `parsePropertiesFilterParam`
 * (`server/internal/handler/property.go`):
 *
 * - `contains` is a case-insensitive substring test over stored strings only
 *   (the server's guarded `ILIKE '%…%'`), so "Foo" and "foo" agree.
 * - `gt`/`gte`/`lt`/`lte` only match numeric stored values — the server
 *   guards with `jsonb_typeof(...) = 'number'`, so a text value is a miss
 *   here too.
 * - `before`/`after` only match string values and compare lexicographically,
 *   which is chronological for the "YYYY-MM-DD" date-only strings the server
 *   stores and filters on.
 *
 * A missing key never matches an operator (the server's `->>` yields NULL,
 * which no operator predicate satisfies) — handled by the caller before this
 * runs.
 */
export function issueValueMatchesOperator(
  value: NonNullable<Issue["properties"]>[string],
  filter: PropertyOperatorFilter,
): boolean {
  switch (filter.op) {
    case "contains": {
      const needle = filter.value.toLowerCase();
      // An empty needle would substring-match every value ("".includes("") is
      // true); the server rejects an empty operator value outright, so the
      // matcher must refuse it too rather than match-all on hand-edited
      // saved-view blobs.
      if (needle === "") return false;
      if (typeof value === "string") return value.toLowerCase().includes(needle);
      // Numbers, booleans, and arrays never match. Although jsonb ->> can
      // serialize them, contains is deliberately a text/url operator on both
      // the server and the client.
      return false;
    }
    case "gt":
    case "gte":
    case "lt":
    case "lte": {
      if (typeof value !== "number") return false;
      const bound = Number(filter.value);
      if (!Number.isFinite(bound)) return false;
      if (filter.op === "gt") return value > bound;
      if (filter.op === "gte") return value >= bound;
      if (filter.op === "lt") return value < bound;
      return value <= bound;
    }
    case "before":
    case "after": {
      if (typeof value !== "string") return false;
      return filter.op === "before" ? value < filter.value : value > filter.value;
    }
    default:
      return false;
  }
}

/**
 * Match one issue against the property filters. Select values are single
 * option-id strings, multi_select values are option-id arrays, checkbox
 * values are booleans compared against the "true"/"false" pseudo-options,
 * and scalar definitions accept operator members (`PropertyOperatorFilter`)
 * alongside plain equality strings. Within one definition any member
 * matching is enough (OR); every definition must match (AND). An issue with
 * no value for a filtered definition matches only when the "No value"
 * (NO_PROPERTY_VALUE) pseudo-option is selected for it.
 */
export function issueMatchesPropertyFilters(
  issue: Issue,
  propertyFilters: Record<string, PropertyFilterValue[]> | undefined,
): boolean {
  if (!propertyFilters) return true;
  for (const [propertyId, selected] of Object.entries(propertyFilters)) {
    if (selected.length === 0) continue;
    const value = issue.properties?.[propertyId];
    if (value === undefined) {
      if (selected.includes(NO_PROPERTY_VALUE)) continue;
      return false;
    }
    const matched = selected.some((member) => {
      if (member === NO_PROPERTY_VALUE) {
        // The sentinel never matches a SET value: a literal "__none__" text
        // value is a real value (the server's key-absence predicate excludes
        // it from a No-value filter), and this path must agree with the
        // server.
        return false;
      }
      if (typeof member === "string") {
        if (typeof value === "string") return member === value;
        // Compare numerically so "3.50" and 3.5 agree with the server's
        // jsonb number containment.
        if (typeof value === "number") return Number(member) === value;
        if (Array.isArray(value)) return value.includes(member);
        if (typeof value === "boolean") return member === String(value);
        return false;
      }
      return issueValueMatchesOperator(value, member);
    });
    if (!matched) return false;
  }
  return true;
}

function issueIsWorking(issueId: string, context: IssueFilterContext) {
  if (context.activityByIssueId) {
    return context.activityByIssueId.get(issueId)?.isWorking === true;
  }
  return context.runningIssueIds?.has(issueId) === true;
}

/**
 * Filter issues using positive selection model.
 * Empty arrays = no filter (show all). Non-empty = show only matching.
 *
 * Assignee has a special "No assignee" toggle (includeNoAssignee):
 * - When only includeNoAssignee is true → show only unassigned issues
 * - When assigneeFilters has items → show only those assignees' issues
 * - When both → show matching assignees + unassigned
 */
export function applyIssueFilters(
  issues: Issue[],
  filters: IssueFilterState,
  context: IssueFilterContext = {},
): Issue[] {
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, workingOnly } = filters;
  const hasAssigneeFilter =
    filters.assigneeFilterActive === true ||
    assigneeFilters.length > 0 ||
    includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;
  const projectStatusFilters = filters.projectStatusFilters ?? [];
  // Without the catalog the predicate cannot be answered. Treat that as
  // "no filter" — the server-driven surfaces enforce it for real, and a
  // silent match-none here would empty a list with no visible cause.
  const projectStatusCatalog = context.projectStatusById;
  const hasProjectStatusFilter =
    projectStatusFilters.length > 0 && projectStatusCatalog !== undefined;
  // Empty set passed without `agentRunningFilter` is a no-op. When the
  // filter is on but the set is missing/empty, hide everything — the
  // user opted into "only running" and there is nothing running.
  const applyWorkingOnly = workingOnly === true;
  const hideSubIssues = filters.showSubIssues === false;

  const relationFilters = filters.relationFilters ?? [];
  // Same rule as the project-status catalog: no viewer, no answer, no filter.
  const relationContext = context.relation;
  const hasRelationFilter = relationFilters.length > 0 && relationContext !== undefined;
  const dueDateFilters = filters.dueDateFilters ?? [];
  const today = context.today ?? todayDateOnly();
  const advancedFilter = filters.advancedFilter ?? null;
  const negated = new Set(filters.negatedFilters ?? []);
  // "Is not" keeps exactly what "is" drops, so an issue with no value for the
  // dimension (unassigned, no project, no labels) survives a negated filter.
  const passes = (dimension: NegatableFilterDimension, matched: boolean) =>
    negated.has(dimension) ? !matched : matched;

  return issues.filter((issue) => {
    if (applyWorkingOnly && !issueIsWorking(issue.id, context))
      return false;

    if (hideSubIssues && issue.parent_issue_id) return false;

    if (statusFilters.length > 0 && !passes("status", statusFilters.includes(issue.status)))
      return false;

    if (
      priorityFilters.length > 0 &&
      !passes("priority", priorityFilters.includes(issue.priority))
    )
      return false;

    if (hasAssigneeFilter) {
      // Unassigned matches only "No assignee"; assigned matches only a listed
      // actor (an active filter with neither matches nothing).
      const matched = !issue.assignee_id
        ? includeNoAssignee
        : assigneeFilters.some(
            (f) => f.type === issue.assignee_type && f.id === issue.assignee_id,
          );
      if (!passes("assignee", matched)) return false;
    }

    if (
      creatorFilters.length > 0 &&
      !passes(
        "creator",
        creatorFilters.some(
          (f) => f.type === issue.creator_type && f.id === issue.creator_id,
        ),
      )
    ) {
      return false;
    }

    if (hasProjectFilter) {
      const matched = !issue.project_id
        ? includeNoProject
        : projectFilters.includes(issue.project_id);
      if (!passes("project", matched)) return false;
    }

    if (hasProjectStatusFilter) {
      // An issue with no project has no status to match — same as the
      // server's EXISTS predicate. `includeNoProject` widens the project-id
      // dimension only.
      const projectStatus = issue.project_id
        ? projectStatusCatalog.get(issue.project_id)
        : undefined;
      const matched = !!projectStatus && projectStatusFilters.includes(projectStatus);
      if (!passes("projectStatus", matched)) return false;
    }

    if (labelFilters.length > 0) {
      // OR semantics within the filter: keep issues that carry any of the
      // selected labels. Matches existing priority / project multi-select.
      const matched = (issue.labels ?? []).some((l) => labelFilters.includes(l.id));
      if (!passes("label", matched)) return false;
    }

    if (hasRelationFilter) {
      const matched = relationFilters.some((relation) =>
        issueMatchesRelation(issue, relation, relationContext),
      );
      if (!passes("relation", matched)) return false;
    }

    if (dueDateFilters.length > 0) {
      const matched = dueDateFilters.some((bucket) => dueDateMatches(issue.due_date, bucket, today));
      if (!passes("dueDate", matched)) return false;
    }

    if (advancedFilter && issueMatchesFilterGroup(issue, advancedFilter, context, today) === false)
      return false;

    if (!issueMatchesPropertyFilters(issue, filters.propertyFilters)) return false;

    return true;
  });
}

function issueMatchesRelation(
  issue: Issue,
  relation: IssueRelationFilter,
  context: IssueRelationContext,
): boolean {
  switch (relation) {
    case "assigned":
      return issue.assignee_type === "member" && issue.assignee_id === context.userId;
    case "created":
      return issue.creator_type === "member" && issue.creator_id === context.userId;
    case "my_agents":
      return (
        issue.assignee_type === "agent" &&
        !!issue.assignee_id &&
        context.myAgentIds.has(issue.assignee_id)
      );
  }
}

/**
 * One rule-tree node against an issue, or undefined when the node cannot be
 * answered: it is empty, or it needs a catalog the surface never loaded
 * (project statuses, the viewer). An unanswerable member drops out of its
 * group, mirroring the server dropping empty members.
 */
function issueMatchesFilterGroup(
  issue: Issue,
  group: AdvancedFilterGroup,
  context: IssueFilterContext,
  today: string,
): boolean | undefined {
  let answered = false;
  for (const child of group.children) {
    const matched =
      child.type === "group"
        ? issueMatchesFilterGroup(issue, child, context, today)
        : issueMatchesFilterCondition(issue, child, context, today);
    if (matched === undefined) continue;
    answered = true;
    if (group.match === "any" && matched) return true;
    if (group.match === "all" && !matched) return false;
  }
  if (!answered) return undefined;
  return group.match === "all";
}

function issueMatchesFilterCondition(
  issue: Issue,
  condition: AdvancedFilterCondition,
  context: IssueFilterContext,
  today: string,
): boolean | undefined {
  if (condition.values.length === 0) return undefined;
  if (isPropertyCondition(condition)) {
    const propertyId = propertyIdOfField(condition.field) ?? "";
    const matched = issueMatchesPropertyFilters(issue, { [propertyId]: condition.values });
    return condition.negated ? !matched : matched;
  }
  const { values } = condition;
  const actorMatches = (type: string | null, id: string | null) =>
    !!id && values.includes(`${type}:${id}`);
  let matched: boolean;
  switch (condition.field) {
    case "status":
      matched = values.includes(issue.status);
      break;
    case "priority":
      matched = values.includes(issue.priority);
      break;
    case "assignee":
      matched = issue.assignee_id
        ? actorMatches(issue.assignee_type, issue.assignee_id)
        : values.includes(FILTER_NONE_VALUE);
      break;
    case "creator":
      matched = actorMatches(issue.creator_type, issue.creator_id);
      break;
    case "project":
      matched = issue.project_id
        ? values.includes(issue.project_id)
        : values.includes(FILTER_NONE_VALUE);
      break;
    case "projectStatus": {
      if (!context.projectStatusById) return undefined;
      const status = issue.project_id ? context.projectStatusById.get(issue.project_id) : undefined;
      matched = !!status && values.includes(status);
      break;
    }
    case "label":
      matched = (issue.labels ?? []).some((label) => values.includes(label.id));
      break;
    case "relation": {
      const relation = context.relation;
      if (!relation) return undefined;
      matched = values.some((value) =>
        issueMatchesRelation(issue, value as IssueRelationFilter, relation),
      );
      break;
    }
    case "dueDate":
      matched = values.some((bucket) => dueDateMatches(issue.due_date, bucket as DueDateFilter, today));
      break;
  }
  return condition.negated ? !matched : matched;
}

export function filterIssues(issues: Issue[], filters: IssueFilters): Issue[] {
  return applyIssueFilters(
    issues,
    {
      statusFilters: filters.statusFilters,
      priorityFilters: filters.priorityFilters,
      assigneeFilters: filters.assigneeFilters,
      includeNoAssignee: filters.includeNoAssignee,
      assigneeFilterActive: filters.assigneeFilterActive,
      creatorFilters: filters.creatorFilters,
      projectFilters: filters.projectFilters,
      includeNoProject: filters.includeNoProject,
      projectStatusFilters: filters.projectStatusFilters,
      labelFilters: filters.labelFilters,
      relationFilters: filters.relationFilters,
      dueDateFilters: filters.dueDateFilters,
      advancedFilter: filters.advancedFilter,
      negatedFilters: filters.negatedFilters,
      propertyFilters: filters.propertyFilters,
      workingOnly: filters.agentRunningFilter === true,
      showSubIssues: filters.showSubIssues,
    },
    {
      runningIssueIds: filters.runningIssueIds,
      projectStatusById: filters.projectStatusById,
      relation: filters.relationContext,
      today: filters.today,
    },
  );
}

/**
 * Re-apply the client-only display filters to a server-grouped response.
 * The assignee-grouped board renders straight from `groups`, bypassing the
 * flat `applyIssueFilters` output, so the "Show sub-issues" toggle and the
 * agents-working quick filter must be applied per group here. Recomputes
 * each group's total and drops emptied groups. Returns the input by
 * reference when no client filter is active.
 */
export function filterAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  filters: {
    showSubIssues?: boolean;
    agentRunningFilter?: boolean;
    runningIssueIds?: ReadonlySet<string>;
    propertyFilters?: Record<string, PropertyFilterValue[]>;
  },
): IssueAssigneeGroup[] | undefined {
  const applyRunning = filters.agentRunningFilter === true;
  const hideSubIssues = filters.showSubIssues === false;
  const hasPropertyFilters = Object.values(filters.propertyFilters ?? {}).some(
    (selected) => selected.length > 0,
  );
  if (!groups || (!applyRunning && !hideSubIssues && !hasPropertyFilters)) return groups;

  const { runningIssueIds } = filters;
  return groups
    .map((group) => {
      const issues = group.issues.filter((issue) => {
        if (applyRunning && !(runningIssueIds?.has(issue.id) ?? false))
          return false;
        if (hideSubIssues && issue.parent_issue_id) return false;
        if (hasPropertyFilters && !issueMatchesPropertyFilters(issue, filters.propertyFilters))
          return false;
        return true;
      });
      return { ...group, issues, total: issues.length };
    })
    .filter((group) => group.issues.length > 0);
}
