/**
 * Custom issue properties — workspace-defined, typed fields on issues
 * (MUL-4463). Definitions live in a workspace catalog (managed by owner/admin
 * only); values live on each issue in a bag keyed by definition id, so
 * renames never touch issue rows.
 *
 * Values are typed per definition: select stores an option id, multi_select
 * an array of option ids (config order), date a "YYYY-MM-DD" string, checkbox
 * a boolean, number a number, text/url strings, actor a "member:<user_id>"
 * reference string, multi_actor an array of them (insertion order).
 */
export type IssuePropertyType =
  | "text"
  | "number"
  | "select"
  | "multi_select"
  | "date"
  | "checkbox"
  | "url"
  | "actor"
  | "multi_actor";

export const ISSUE_PROPERTY_TYPES: IssuePropertyType[] = [
  "text",
  "number",
  "select",
  "multi_select",
  "date",
  "checkbox",
  "url",
  "actor",
  "multi_actor",
];

export function isKnownPropertyType(type: string): type is IssuePropertyType {
  return (ISSUE_PROPERTY_TYPES as string[]).includes(type);
}

/**
 * Actor properties (MUL-6286) reference a workspace member. The assignee field
 * also accepts agents and squads; actor properties deliberately do not — an
 * agent reference would drag in agent-visibility rules, and a squad is a
 * routing target rather than a person.
 *
 * The stored form is "<kind>:<uuid>", so widening this union later needs no
 * migration and no new property type.
 */
export type IssuePropertyActorKind = "member";

export const ISSUE_PROPERTY_ACTOR_KINDS: IssuePropertyActorKind[] = ["member"];

/** Upper bound on a single multi_actor value; mirrors the server cap. */
export const MAX_ISSUE_PROPERTY_ACTOR_VALUES = 20;

export interface IssuePropertyActorRef {
  kind: IssuePropertyActorKind;
  /** Members are referenced by `user_id`, matching the issue assignee pair. */
  id: string;
}

export function isActorPropertyType(type: string): boolean {
  return type === "actor" || type === "multi_actor";
}

/**
 * Types the issue filter menu exposes for value + "No value" filtering.
 * Kept as an explicit enumeration (rather than delegating to
 * isKnownPropertyType) so a future property type must opt into filtering —
 * it should never become filterable by default.
 */
export function isFilterablePropertyType(type: string): boolean {
  return (
    type === "select" ||
    type === "multi_select" ||
    type === "checkbox" ||
    isScalarPropertyType(type) ||
    isActorPropertyType(type)
  );
}

/** Single-valued scalar properties: text / number / date / url. */
export type ScalarIssuePropertyType = Extract<IssuePropertyType, "text" | "number" | "date" | "url">;

export function isScalarPropertyType(type: string): type is ScalarIssuePropertyType {
  return type === "text" || type === "url" || type === "number" || type === "date";
}

export function formatActorRef(kind: IssuePropertyActorKind, id: string): string {
  return `${kind}:${id}`;
}

/** Returns null for anything that isn't a well-formed reference. */
export function parseActorRef(raw: unknown): IssuePropertyActorRef | null {
  if (typeof raw !== "string") return null;
  const separator = raw.indexOf(":");
  if (separator <= 0) return null;
  const kind = raw.slice(0, separator);
  const id = raw.slice(separator + 1);
  if (!id) return null;
  if (!(ISSUE_PROPERTY_ACTOR_KINDS as string[]).includes(kind)) return null;
  return { kind: kind as IssuePropertyActorKind, id };
}

/**
 * Raw reference strings in stored order, INCLUDING kinds this build does not
 * know about.
 *
 * Editors must round-trip through this rather than through
 * `actorRefsFromValue`: an installed desktop client talking to a newer backend
 * would otherwise drop every unknown-kind entry the moment the user toggles
 * one it does understand — a silent data loss the user never sees.
 */
export function actorRefValuesFromValue(value: IssuePropertyValue | undefined): string[] {
  if (typeof value === "string") return value ? [value] : [];
  if (Array.isArray(value)) {
    return value.filter((entry): entry is string => typeof entry === "string");
  }
  return [];
}

/**
 * Reads an actor property value as a list of refs this build can render.
 * Unknown kinds are dropped rather than thrown on — see
 * `actorRefValuesFromValue` for the edit path, which must keep them.
 */
export function actorRefsFromValue(value: IssuePropertyValue | undefined): IssuePropertyActorRef[] {
  if (typeof value === "string") {
    const ref = parseActorRef(value);
    return ref ? [ref] : [];
  }
  if (Array.isArray(value)) {
    return value.map(parseActorRef).filter((ref): ref is IssuePropertyActorRef => ref !== null);
  }
  return [];
}

/**
 * True when an actor value holds at least one reference this build cannot
 * parse — i.e. a newer backend has widened the accepted kinds.
 *
 * Editors must consult this before offering a normal edit. For `multi_actor`
 * the toggle already round-trips unknown entries, but for single `actor` an
 * unresolvable value renders as empty, and letting the user "fill in the empty
 * field" would overwrite a value they were never shown (MUL-6286 review).
 */
export function hasUnknownActorRef(value: IssuePropertyValue | undefined): boolean {
  return actorRefValuesFromValue(value).length !== actorRefsFromValue(value).length;
}

export interface IssuePropertyOption {
  id: string;
  name: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
}

export interface IssuePropertyConfig {
  options?: IssuePropertyOption[];
}

export interface IssueProperty {
  id: string;
  workspace_id: string;
  name: string;
  /** Lenient string: newer servers may ship types this client doesn't know. */
  type: string;
  description?: string;
  /** Optional catalog icon key; absent on backends predating icon support. */
  icon?: string;
  config: IssuePropertyConfig;
  position: number;
  archived: boolean;
  archived_at?: string | null;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export type IssuePropertyValue = string | number | boolean | string[];
export type IssuePropertyValues = Record<string, IssuePropertyValue>;

/**
 * Structured operators for scalar property filters (#7692). A plain string
 * member still means exact equality — every pre-operator saved view, URL, and
 * API call keeps its meaning. Operators ride alongside as objects:
 *
 * - `contains` — case-insensitive substring match (text / url)
 * - `gt` / `gte` / `lt` / `lte` — numeric comparison (number)
 * - `before` / `after` — lexicographic date comparison (date, "YYYY-MM-DD")
 *
 * Within one definition all members still OR, so an operator member composes
 * with observed values and "No value" like any other alternative.
 */
export type PropertyFilterOp =
  | "contains"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "before"
  | "after";

export const PROPERTY_FILTER_OPS: readonly PropertyFilterOp[] = [
  "contains",
  "gt",
  "gte",
  "lt",
  "lte",
  "before",
  "after",
];

/** Mathematical symbols shared by the scalar filter picker and active chips. */
export const PROPERTY_FILTER_OP_SYMBOLS: Partial<Record<PropertyFilterOp, string>> = {
  gt: ">",
  gte: "≥",
  lt: "<",
  lte: "≤",
};

export function isKnownPropertyFilterOp(op: string): op is PropertyFilterOp {
  return (PROPERTY_FILTER_OPS as readonly string[]).includes(op);
}

export interface PropertyOperatorFilter {
  op: PropertyFilterOp;
  /** Raw user input; the server interprets it per op. */
  value: string;
}

/** One OR-alternative of a property filter: equality (bare string), the
 *  "No value" sentinel, or a structured operator. */
export type PropertyFilterValue = string | PropertyOperatorFilter;

/**
 * Canonical membership key for one filter member, so equality strings and
 * operator objects can live in `Set<string>`-shaped lookups (saved-view
 * baselines, chip deltas) without stringifying objects ad hoc.
 */
export function propertyFilterValueKey(value: PropertyFilterValue): string {
  return typeof value === "string" ? value : `${value.op}\u0000${value.value}`;
}

export function isPropertyOperatorFilter(value: unknown): value is PropertyOperatorFilter {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as PropertyOperatorFilter).op === "string" &&
    typeof (value as PropertyOperatorFilter).value === "string"
  );
}

/**
 * Ops the filter menu offers per scalar property type, in display order.
 * "is" (equality) is not in the list — it is the default and commits a bare
 * string, so every pre-operator flow stays untouched. Non-scalar entries are
 * intentionally empty: keeping the record exhaustive forces each future
 * property type to make an explicit operator decision.
 */
export const PROPERTY_FILTER_OPS_BY_TYPE: Record<IssuePropertyType, readonly PropertyFilterOp[]> = {
  text: ["contains"],
  number: ["gt", "gte", "lt", "lte"],
  select: [],
  multi_select: [],
  date: ["before", "after"],
  checkbox: [],
  url: ["contains"],
  actor: [],
  multi_actor: [],
};

export interface CreatePropertyRequest {
  name: string;
  type: IssuePropertyType;
  description?: string;
  icon?: string;
  config?: IssuePropertyConfig;
}

export interface UpdatePropertyRequest {
  name?: string;
  description?: string;
  /** Empty string clears the icon. */
  icon?: string;
  config?: IssuePropertyConfig;
  archived?: boolean;
}

export interface ListPropertiesResponse {
  properties: IssueProperty[];
  total: number;
}

/** Response of PUT/DELETE /api/issues/{id}/properties/{propertyId}: the full post-mutation bag. */
export interface IssuePropertiesResponse {
  properties: IssuePropertyValues;
  issue_revision?: number;
}
