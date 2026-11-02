import {
  isKnownPropertyFilterOp,
  isPropertyOperatorFilter,
  propertyFilterValueKey,
  type IssuePriority,
  type IssueStatus,
  type IssueTableFilterGroup,
  type IssueTableFilterNode,
  type IssueTableNegatableFilter,
  type ProjectStatus,
  type PropertyFilterValue,
} from "../../types";
import { PRIORITY_DISPLAY_ORDER } from "../config/priority";
import { PROJECT_STATUS_ORDER } from "../../projects/config";
import { dateOnlyToUTCDate } from "../date";
import { createSafeId } from "../../utils";

/** How the viewer relates to an issue. Unlike the "my issues" scope axis this
 *  is an ordinary filter dimension: OR within, AND with everything else. */
export type IssueRelationFilter = "assigned" | "created" | "my_agents";

export const ISSUE_RELATION_FILTERS: readonly IssueRelationFilter[] = [
  "assigned",
  "created",
  "my_agents",
];

/** Relative due-date buckets. A view stores the bucket, never a date, so it
 *  keeps meaning "overdue" tomorrow. Weeks run Monday to Sunday. */
export type DueDateFilter = "overdue" | "today" | "this_week" | "next_7_days" | "none";

export const DUE_DATE_FILTERS: readonly DueDateFilter[] = [
  "overdue",
  "today",
  "this_week",
  "next_7_days",
  "none",
];

/** The built-in value dimensions: what the simple menu lists one row each
 *  for and what can say "is not" there. Custom properties carry their own
 *  operators and are not part of this. */
export type NegatableFilterDimension =
  | "status"
  | "priority"
  | "assignee"
  | "creator"
  | "project"
  | "projectStatus"
  | "label"
  | "relation"
  | "dueDate";

export const NEGATABLE_FILTER_DIMENSIONS: readonly NegatableFilterDimension[] = [
  "status",
  "priority",
  "assignee",
  "creator",
  "project",
  "projectStatus",
  "label",
  "relation",
  "dueDate",
];

/** Each dimension's field name in the issue table query. */
export const FILTER_DIMENSION_TABLE_FIELD: Record<NegatableFilterDimension, IssueTableNegatableFilter> = {
  status: "statuses",
  priority: "priorities",
  assignee: "assignees",
  creator: "creators",
  project: "project_ids",
  projectStatus: "project_statuses",
  label: "label_ids",
  relation: "relations",
  dueDate: "due_dates",
};

/** Condition value for "no assignee" / "no project". */
export const FILTER_NONE_VALUE = "none";

/** A custom property as a condition field. */
export type PropertyFilterField = `property:${string}`;

/** What an advanced condition can test: a built-in dimension or a property. */
export type AdvancedFilterField = NegatableFilterDimension | PropertyFilterField;

const PROPERTY_FIELD_PREFIX = "property:";

export function propertyFilterField(propertyId: string): PropertyFilterField {
  return `${PROPERTY_FIELD_PREFIX}${propertyId}`;
}

/** The property a field tests, or null for a built-in dimension. */
export function propertyIdOfField(field: AdvancedFilterField): string | null {
  return field.startsWith(PROPERTY_FIELD_PREFIX) ? field.slice(PROPERTY_FIELD_PREFIX.length) : null;
}

export interface DimensionFilterCondition {
  type: "condition";
  id: string;
  field: NegatableFilterDimension;
  negated: boolean;
  /**
   * OR within the condition. Actors are `${type}:${id}`; assignee and project
   * also take {@link FILTER_NONE_VALUE}; everything else is the value the
   * simple filter stores.
   */
  values: string[];
}

export interface PropertyFilterCondition {
  type: "condition";
  id: string;
  field: PropertyFilterField;
  negated: boolean;
  /** OR within the condition, in the simple property filter's shape: option
   *  ids, the "no value" sentinel, or operator objects. */
  values: PropertyFilterValue[];
}

export type AdvancedFilterCondition = DimensionFilterCondition | PropertyFilterCondition;

export function isPropertyCondition(condition: AdvancedFilterCondition): condition is PropertyFilterCondition {
  return propertyIdOfField(condition.field) !== null;
}

export interface AdvancedFilterGroup {
  type: "group";
  id: string;
  match: "all" | "any";
  /** The root may hold groups; a group under the root holds conditions only. */
  children: AdvancedFilterNode[];
}

export type AdvancedFilterNode = AdvancedFilterCondition | AdvancedFilterGroup;

/** The simple-mode value fields, in the shape the view store keeps them. */
export interface SimpleFilterFields {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: { type: "member" | "agent" | "squad"; id: string }[];
  includeNoAssignee: boolean;
  creatorFilters: { type: "member" | "agent" | "squad"; id: string }[];
  projectFilters: string[];
  includeNoProject: boolean;
  projectStatusFilters: ProjectStatus[];
  labelFilters: string[];
  relationFilters: IssueRelationFilter[];
  dueDateFilters: DueDateFilter[];
  negatedFilters: NegatableFilterDimension[];
  propertyFilters: Record<string, PropertyFilterValue[]>;
}

export const EMPTY_SIMPLE_FILTER_FIELDS: Readonly<SimpleFilterFields> = {
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  projectStatusFilters: [],
  labelFilters: [],
  relationFilters: [],
  dueDateFilters: [],
  negatedFilters: [],
  propertyFilters: {},
};

export function createFilterCondition(
  field: NegatableFilterDimension,
  values?: string[],
  negated?: boolean,
): DimensionFilterCondition;
export function createFilterCondition(
  field: PropertyFilterField,
  values?: PropertyFilterValue[],
  negated?: boolean,
): PropertyFilterCondition;
export function createFilterCondition(
  field: AdvancedFilterField,
  values?: PropertyFilterValue[],
  negated?: boolean,
): AdvancedFilterCondition;
export function createFilterCondition(
  field: AdvancedFilterField,
  values: PropertyFilterValue[] = [],
  negated = false,
): AdvancedFilterCondition {
  return { type: "condition", id: createSafeId(), field, negated, values } as AdvancedFilterCondition;
}

export function createFilterGroup(
  match: "all" | "any" = "all",
  children: AdvancedFilterNode[] = [],
): AdvancedFilterGroup {
  return { type: "group", id: createSafeId(), match, children };
}

// ---------------------------------------------------------------------------
// Due dates

const DAY_MS = 86_400_000;

function dayNumber(dateOnly: string | null | undefined): number | null {
  const date = dateOnlyToUTCDate(dateOnly);
  return date ? Math.floor(date.getTime() / DAY_MS) : null;
}

/** Whether a "YYYY-MM-DD" due date falls in a bucket, `today` being the
 *  viewer's calendar day. Mirrors `issueTableDueDatePredicate` on the server. */
export function dueDateMatches(
  dueDate: string | null | undefined,
  bucket: DueDateFilter,
  today: string,
): boolean {
  const due = dayNumber(dueDate);
  if (bucket === "none") return due === null;
  const now = dayNumber(today);
  if (due === null || now === null) return false;
  switch (bucket) {
    case "overdue":
      return due < now;
    case "today":
      return due === now;
    case "this_week": {
      const weekday = new Date(now * DAY_MS).getUTCDay();
      const monday = now - ((weekday + 6) % 7);
      return due >= monday && due < monday + 7;
    }
    case "next_7_days":
      return due >= now && due < now + 7;
  }
}

// ---------------------------------------------------------------------------
// Trees

/** Conditions that constrain something (have values). */
export function countAdvancedConditions(group: AdvancedFilterGroup | null | undefined): number {
  if (!group) return 0;
  let count = 0;
  for (const child of group.children) {
    if (child.type === "group") count += countAdvancedConditions(child);
    else if (child.values.length > 0) count++;
  }
  return count;
}

/**
 * The tree without empty conditions and groups, or null when nothing is
 * left. An empty member must drop out rather than read as "true": inside an
 * "any" group that would match everything.
 */
export function pruneAdvancedFilter(
  group: AdvancedFilterGroup | null | undefined,
): AdvancedFilterGroup | null {
  if (!group) return null;
  const children: AdvancedFilterNode[] = [];
  for (const child of group.children) {
    if (child.type === "group") {
      const pruned = pruneAdvancedFilter(child);
      if (pruned) children.push(pruned);
    } else if (child.values.length > 0) {
      children.push(child);
    }
  }
  return children.length > 0 ? { ...group, children } : null;
}

const ACTOR_TYPES = new Set(["member", "agent", "squad"]);

function knownValues(field: NegatableFilterDimension, values: string[]): string[] {
  const keep = (allowed: readonly string[]) => values.filter((v) => allowed.includes(v));
  switch (field) {
    case "priority":
      return keep(PRIORITY_DISPLAY_ORDER);
    case "projectStatus":
      return keep(PROJECT_STATUS_ORDER);
    case "relation":
      return keep(ISSUE_RELATION_FILTERS);
    case "dueDate":
      return keep(DUE_DATE_FILTERS);
    case "assignee":
    case "creator":
      return values.filter((v) => {
        if (v === FILTER_NONE_VALUE) return field === "assignee";
        const [type, id] = v.split(":");
        return !!type && !!id && ACTOR_TYPES.has(type);
      });
    default:
      return values.filter((v) => v.length > 0);
  }
}

function sanitizeNode(raw: unknown, depth: number): AdvancedFilterNode | null {
  if (!raw || typeof raw !== "object") return null;
  const node = raw as Record<string, unknown>;
  const id = typeof node.id === "string" && node.id ? node.id : createSafeId();
  if (node.type === "group") {
    if (depth > 1 || !Array.isArray(node.children)) return null;
    const children = node.children
      .map((child) => sanitizeNode(child, depth + 1))
      .filter((child): child is AdvancedFilterNode => child !== null);
    return { type: "group", id, match: node.match === "any" ? "any" : "all", children };
  }
  if (node.type !== "condition") return null;
  const negated = node.negated === true;
  if (typeof node.field === "string" && node.field.length > PROPERTY_FIELD_PREFIX.length) {
    const propertyField = node.field as PropertyFilterField;
    if (propertyIdOfField(propertyField) !== null) {
      const members = Array.isArray(node.values)
        ? node.values.filter(
            (v): v is PropertyFilterValue =>
              (typeof v === "string" && v.length > 0) ||
              (isPropertyOperatorFilter(v) && isKnownPropertyFilterOp(v.op)),
          )
        : [];
      const values = [...new Map(members.map((v) => [propertyFilterValueKey(v), v])).values()];
      return { type: "condition", id, field: propertyField, negated, values };
    }
  }
  const field = node.field as NegatableFilterDimension;
  if (!NEGATABLE_FILTER_DIMENSIONS.includes(field)) return null;
  const values = Array.isArray(node.values)
    ? [...new Set(node.values.filter((v): v is string => typeof v === "string"))]
    : [];
  return { type: "condition", id, field, negated, values: knownValues(field, values) };
}

/**
 * A stored tree (a persisted snapshot or a saved view, both hand-editable)
 * reduced to what the store can represent: known fields and values, at most
 * one level of groups under the root. Anything unrecognizable drops out.
 */
export function sanitizeAdvancedFilter(raw: unknown): AdvancedFilterGroup | null {
  const node = sanitizeNode(raw, 0);
  return node && node.type === "group" ? node : null;
}

function actorValue(actor: { type: string; id: string }): string {
  return `${actor.type}:${actor.id}`;
}

function parseActorValue(value: string): { type: "member" | "agent" | "squad"; id: string } | null {
  const separator = value.indexOf(":");
  if (separator <= 0) return null;
  const type = value.slice(0, separator);
  const id = value.slice(separator + 1);
  return ACTOR_TYPES.has(type) && id ? { type: type as "member" | "agent" | "squad", id } : null;
}

/** One simple dimension's selection in condition-value form. */
export function simpleFieldValues(
  fields: Omit<SimpleFilterFields, "negatedFilters" | "propertyFilters">,
  field: NegatableFilterDimension,
): string[] {
  switch (field) {
    case "status":
      return fields.statusFilters;
    case "priority":
      return fields.priorityFilters;
    case "assignee":
      return [
        ...fields.assigneeFilters.map(actorValue),
        ...(fields.includeNoAssignee ? [FILTER_NONE_VALUE] : []),
      ];
    case "creator":
      return fields.creatorFilters.map(actorValue);
    case "project":
      return [...fields.projectFilters, ...(fields.includeNoProject ? [FILTER_NONE_VALUE] : [])];
    case "projectStatus":
      return fields.projectStatusFilters;
    case "label":
      return fields.labelFilters;
    case "relation":
      return fields.relationFilters;
    case "dueDate":
      return fields.dueDateFilters;
  }
}

/** One condition per simple dimension and property that has values, ANDed:
 *  the same filter, as a rule list. `negated` must already be the effective
 *  set. */
export function advancedFilterFromSimple(
  fields: Omit<SimpleFilterFields, "negatedFilters">,
  negated: readonly NegatableFilterDimension[],
): AdvancedFilterGroup {
  const children: AdvancedFilterNode[] = NEGATABLE_FILTER_DIMENSIONS.flatMap((field) => {
    const values = simpleFieldValues(fields, field);
    return values.length > 0
      ? [createFilterCondition(field, [...values], negated.includes(field))]
      : [];
  });
  for (const [propertyId, values] of Object.entries(fields.propertyFilters)) {
    if (values.length > 0) children.push(createFilterCondition(propertyFilterField(propertyId), [...values]));
  }
  return createFilterGroup("all", children);
}

/**
 * The simple fields a tree means, or null when simple mode cannot say it: a
 * group, an "any" across two or more conditions, one field tested twice, or
 * "is not" on a property. Empty conditions are ignored.
 */
export function simpleFromAdvancedFilter(
  group: AdvancedFilterGroup | null | undefined,
): SimpleFilterFields | null {
  const pruned = pruneAdvancedFilter(group);
  const fields: SimpleFilterFields = {
    ...EMPTY_SIMPLE_FILTER_FIELDS,
    assigneeFilters: [],
    creatorFilters: [],
    negatedFilters: [],
    propertyFilters: {},
  };
  if (!pruned) return fields;
  if (pruned.match === "any" && pruned.children.length > 1) return null;
  const seen = new Set<AdvancedFilterField>();
  for (const child of pruned.children) {
    if (child.type === "group" || seen.has(child.field)) return null;
    seen.add(child.field);
    if (isPropertyCondition(child)) {
      if (child.negated) return null;
      fields.propertyFilters[propertyIdOfField(child.field) ?? ""] = [...child.values];
      continue;
    }
    if (child.negated) fields.negatedFilters.push(child.field);
    const values = child.values;
    const none = values.includes(FILTER_NONE_VALUE);
    const actors = () =>
      values
        .map(parseActorValue)
        .filter((actor): actor is NonNullable<typeof actor> => actor !== null);
    switch (child.field) {
      case "status":
        fields.statusFilters = [...values];
        break;
      case "priority":
        fields.priorityFilters = values as IssuePriority[];
        break;
      case "assignee":
        fields.assigneeFilters = actors();
        fields.includeNoAssignee = none;
        break;
      case "creator":
        fields.creatorFilters = actors();
        break;
      case "project":
        fields.projectFilters = values.filter((v) => v !== FILTER_NONE_VALUE);
        fields.includeNoProject = none;
        break;
      case "projectStatus":
        fields.projectStatusFilters = values as ProjectStatus[];
        break;
      case "label":
        fields.labelFilters = [...values];
        break;
      case "relation":
        fields.relationFilters = values as IssueRelationFilter[];
        break;
      case "dueDate":
        fields.dueDateFilters = values as DueDateFilter[];
        break;
    }
  }
  fields.negatedFilters = NEGATABLE_FILTER_DIMENSIONS.filter((d) => fields.negatedFilters.includes(d));
  return fields;
}

/** The pruned tree without the conditions `drop` picks. */
export function withoutAdvancedFilterConditions(
  group: AdvancedFilterGroup | null | undefined,
  drop: (condition: AdvancedFilterCondition) => boolean,
): AdvancedFilterGroup | null {
  if (!group) return null;
  const strip = (node: AdvancedFilterGroup): AdvancedFilterGroup => ({
    ...node,
    children: node.children.flatMap((child): AdvancedFilterNode[] =>
      child.type === "group" ? [strip(child)] : drop(child) ? [] : [child],
    ),
  });
  return pruneAdvancedFilter(strip(group));
}

/** The pruned tree without conditions on these fields — the ones a surface's
 *  scope already fixes. */
export function withoutAdvancedFilterFields(
  group: AdvancedFilterGroup | null | undefined,
  fields: readonly NegatableFilterDimension[],
): AdvancedFilterGroup | null {
  if (fields.length === 0) return pruneAdvancedFilter(group);
  return withoutAdvancedFilterConditions(group, (condition) =>
    (fields as readonly string[]).includes(condition.field),
  );
}

/**
 * Flat property filters folded into a tree as conditions, ANDed like they
 * were. Returns null when the tree's shape can't take them: an "any" root
 * that already holds a group.
 */
export function foldPropertyFiltersIntoTree(
  tree: AdvancedFilterGroup,
  propertyFilters: Record<string, PropertyFilterValue[]>,
): AdvancedFilterGroup | null {
  const conditions = Object.entries(propertyFilters)
    .filter(([, values]) => values.length > 0)
    .map(([propertyId, values]) => createFilterCondition(propertyFilterField(propertyId), [...values]));
  if (conditions.length === 0) return tree;
  if (tree.match === "all") return { ...tree, children: [...tree.children, ...conditions] };
  if (tree.children.some((child) => child.type === "group")) return null;
  const inner = tree.children.length > 0 ? [{ ...tree, id: createSafeId() }] : [];
  return { ...tree, match: "all", children: [...inner, ...conditions] };
}

/**
 * The tree with the node `id` replaced by `edit(node)`: no nodes removes it,
 * one updates it, more inserts siblings after it. Editing the root keeps a
 * root: anything but a group comes back as an empty one.
 */
export function editAdvancedFilter(
  root: AdvancedFilterGroup,
  id: string,
  edit: (node: AdvancedFilterNode) => AdvancedFilterNode[],
): AdvancedFilterGroup {
  if (root.id === id) {
    const [next] = edit(root);
    return next?.type === "group" ? next : { ...root, children: [] };
  }
  const visit = (group: AdvancedFilterGroup): AdvancedFilterGroup => ({
    ...group,
    children: group.children.flatMap((child) =>
      child.id === id ? edit(child) : child.type === "group" ? [visit(child)] : [child],
    ),
  });
  return visit(root);
}

/** Conditions in the tree, empty ones included — what the server's
 *  per-request limit counts once they have values. */
export function advancedFilterConditionSlots(group: AdvancedFilterGroup | null | undefined): number {
  if (!group) return 0;
  return group.children.reduce(
    (sum, child) => sum + (child.type === "group" ? advancedFilterConditionSlots(child) : 1),
    0,
  );
}

/** The server rejects more conditions than this in one tree. */
export const ADVANCED_FILTER_MAX_CONDITIONS = 50;

/** Whether a (pruned) tree tests this field anywhere. */
export function advancedFilterUsesField(
  group: AdvancedFilterGroup | null | undefined,
  field: AdvancedFilterField,
): boolean {
  if (!group) return false;
  return group.children.some((child) =>
    child.type === "group" ? advancedFilterUsesField(child, field) : child.field === field && child.values.length > 0,
  );
}

/** The table-query form of a tree, or undefined when it constrains nothing. */
export function advancedFilterToTableQuery(
  group: AdvancedFilterGroup | null | undefined,
): IssueTableFilterGroup | undefined {
  const pruned = pruneAdvancedFilter(group);
  if (!pruned) return undefined;
  const convert = (node: AdvancedFilterNode): IssueTableFilterNode =>
    node.type === "group"
      ? { match: node.match, children: node.children.map(convert) }
      : {
          field: isPropertyCondition(node) ? node.field : FILTER_DIMENSION_TABLE_FIELD[node.field],
          ...(node.negated ? { negate: true } : {}),
          values: node.values,
        };
  return { match: pruned.match, children: pruned.children.map(convert) };
}

export interface StatusColumnFilter<S extends string = string> {
  statuses: readonly string[];
  negated: boolean;
  hiddenStatuses: readonly S[];
}

/**
 * The status selection that decides which columns a surface draws, given the
 * simple status filter and the rule tree it is ANDed with.
 *
 * A status condition every row must meet (the root matches all, or it is the
 * only rule) picks columns exactly like the simple filter. A looser tree can't
 * narrow the columns, but a status it names positively still un-hides that
 * column, so its rows have somewhere to land.
 */
export function statusColumnFilter<S extends string>(
  statusFilters: readonly string[],
  negated: boolean,
  tree: AdvancedFilterGroup | null | undefined,
  hiddenStatuses: readonly S[],
): StatusColumnFilter<S> {
  const pruned = statusFilters.length > 0 ? null : pruneAdvancedFilter(tree);
  if (!pruned) return { statuses: statusFilters, negated, hiddenStatuses };
  const required =
    pruned.match === "all" || pruned.children.length === 1
      ? pruned.children.filter(
          (child): child is DimensionFilterCondition => child.type === "condition" && child.field === "status",
        )
      : [];
  const [only] = required;
  if (only && required.length === 1) {
    return { statuses: only.values, negated: only.negated, hiddenStatuses };
  }
  const named = new Set<string>();
  const visit = (node: AdvancedFilterNode) => {
    if (node.type === "group") node.children.forEach(visit);
    else if (node.field === "status" && !node.negated) {
      for (const value of node.values) if (typeof value === "string") named.add(value);
    }
  };
  visit(pruned);
  return {
    statuses: [],
    negated: false,
    hiddenStatuses: named.size > 0 ? hiddenStatuses.filter((key) => !named.has(key)) : hiddenStatuses,
  };
}

/** A tree's meaning without its ids, for comparing two trees. */
export function advancedFilterSignature(group: AdvancedFilterGroup | null | undefined): string {
  const pruned = pruneAdvancedFilter(group);
  if (!pruned) return "";
  const sign = (node: AdvancedFilterNode): unknown =>
    node.type === "group"
      ? [node.match, node.children.map(sign)]
      : [node.field, node.negated, node.values.map(propertyFilterValueKey).sort()];
  return JSON.stringify(sign(pruned));
}
