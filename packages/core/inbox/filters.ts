import { toDateOnly } from "../issues/date";
import {
  DUE_DATE_FILTERS,
  FILTER_NONE_VALUE,
  dueDateMatches,
  type DueDateFilter,
} from "../issues/stores/filter-tree";
import type { InboxItem, InboxItemType, Issue, IssuePriority, IssueStatus } from "../types";
import { groupInboxRows, sortInboxNewestFirst, withCommentAnchor } from "./group";

/**
 * Notification kinds, grouped the way a reader thinks about them. Mirrored by
 * the CASE in `server/pkg/db/queries/inbox_archive.sql`; any kind not listed
 * (quick create, autopilot, kinds added later) is "system".
 */
export type InboxTypeGroup =
  | "mention"
  | "comment"
  | "assign"
  | "review"
  | "change"
  | "run"
  | "system";

export const INBOX_TYPE_GROUPS: readonly InboxTypeGroup[] = [
  "mention",
  "comment",
  "assign",
  "review",
  "change",
  "run",
  "system",
];

const TYPE_GROUP: Partial<Record<InboxItemType, InboxTypeGroup>> = {
  mentioned: "mention",
  new_comment: "comment",
  reaction_added: "comment",
  issue_assigned: "assign",
  unassigned: "assign",
  assignee_changed: "assign",
  issue_subscribed: "assign",
  review_requested: "review",
  issue_created: "change",
  status_changed: "change",
  priority_changed: "change",
  start_date_changed: "change",
  due_date_changed: "change",
  children_done: "change",
  task_completed: "run",
  task_failed: "run",
  task_cancelled: "run",
  agent_blocked: "run",
  agent_completed: "run",
};

export function inboxTypeGroup(type: string): InboxTypeGroup {
  return TYPE_GROUP[type as InboxItemType] ?? "system";
}

/** When the notification arrived, counted in whole local calendar days. */
export type InboxReceivedFilter = "today" | "last_7_days" | "last_30_days";
export const INBOX_RECEIVED_FILTERS: readonly InboxReceivedFilter[] = [
  "today",
  "last_7_days",
  "last_30_days",
];
const RECEIVED_DAYS: Record<InboxReceivedFilter, number> = {
  today: 1,
  last_7_days: 7,
  last_30_days: 30,
};

/** Multi-value dimensions; each one can be flipped to "is not". */
export type InboxListDimension =
  | "type"
  | "actor"
  | "status"
  | "priority"
  | "project"
  | "assignee"
  | "label"
  | "dueDate";

export const INBOX_LIST_DIMENSIONS: readonly InboxListDimension[] = [
  "type",
  "actor",
  "status",
  "priority",
  "project",
  "assignee",
  "label",
  "dueDate",
];

/** Dimensions that ask about the issue rather than the notification. */
export const INBOX_ISSUE_FIELD_DIMENSIONS: readonly InboxListDimension[] = [
  "project",
  "assignee",
  "label",
  "dueDate",
];

export interface InboxFilters {
  // About the notification.
  readonly types: readonly InboxTypeGroup[];
  /** Actor keys — see `inboxActorKey`. */
  readonly actors: readonly string[];
  readonly received: InboxReceivedFilter | null;
  readonly unreadOnly: boolean;
  // About its issue. Values use the issue filter's condition form: project
  // and label ids, `member:<id>` style assignees, and "none" for unset.
  readonly statuses: readonly IssueStatus[];
  readonly priorities: readonly IssuePriority[];
  readonly projects: readonly string[];
  readonly assignees: readonly string[];
  readonly labels: readonly string[];
  readonly dueDates: readonly DueDateFilter[];
  /** Dimensions whose values are excluded rather than required. */
  readonly negated: readonly InboxListDimension[];
}

type ListField = "types" | "actors" | "statuses" | "priorities" | "projects" | "assignees" | "labels" | "dueDates";

export const INBOX_DIMENSION_FIELD: Record<InboxListDimension, ListField> = {
  type: "types",
  actor: "actors",
  status: "statuses",
  priority: "priorities",
  project: "projects",
  assignee: "assignees",
  label: "labels",
  dueDate: "dueDates",
};

/** One chip / one "active" row: a list dimension or a single-choice one. */
export type InboxFilterKey = InboxListDimension | "received" | "unread";

/**
 * What the main Inbox can be narrowed by: the issue. Its sections already say
 * what kind of notification a row is and whether it is read, so the dimensions
 * about the notification belong to the archive, which is one flat list.
 */
export const INBOX_NOTIFICATION_FILTER_KEYS: readonly InboxFilterKey[] = ["unread", "type", "actor", "received"];

export function inboxFilterValues(filters: InboxFilters, dimension: InboxListDimension): readonly string[] {
  return filters[INBOX_DIMENSION_FIELD[dimension]];
}

/** The filters the server-filtered archive is asked with. */
export type ArchivedInboxFilters = InboxFilters;

/**
 * Stable identity for "who this notification came from", used as the value of
 * the `actors` dimension.
 *
 * Members and agents key on their id. `system` has no id — the backend writes
 * an invalid UUID that serializes to null — so it keys on the type alone and
 * every system notification collapses into one bucket. Returns null when the
 * row carries no usable attribution; such a row never matches an actor
 * selection, and always matches an excluded one.
 */
export function inboxActorKey(item: InboxItem): string | null {
  const type = item.actor_type;
  if (!type) return null;
  if (type === "system") return "system";
  return item.actor_id ? `${type}:${item.actor_id}` : null;
}

/** Inverse of `inboxActorKey`, for resolving a selection back to a directory. */
export function inboxActorKeyParts(key: string): { type: string; id: string } {
  const separator = key.indexOf(":");
  if (separator === -1) return { type: key, id: "" };
  return { type: key.slice(0, separator), id: key.slice(separator + 1) };
}

export type InboxPriorityFilterSupport =
  | "unknown"
  | "supported"
  | "unsupported";

/** Same tri-state, for the project / assignee / label / due-date projection. */
export type InboxFilterSupport = InboxPriorityFilterSupport;

export const EMPTY_INBOX_FILTERS: InboxFilters = Object.freeze({
  types: Object.freeze([]),
  actors: Object.freeze([]),
  received: null,
  unreadOnly: false,
  statuses: Object.freeze([]),
  priorities: Object.freeze([]),
  projects: Object.freeze([]),
  assignees: Object.freeze([]),
  labels: Object.freeze([]),
  dueDates: Object.freeze([]),
  negated: Object.freeze([]),
});

/** The dimensions that currently narrow the list, in menu order. */
export function activeInboxFilterKeys(filters: InboxFilters): InboxFilterKey[] {
  const keys: InboxFilterKey[] = [];
  if (filters.unreadOnly) keys.push("unread");
  if (filters.types.length > 0) keys.push("type");
  if (filters.actors.length > 0) keys.push("actor");
  if (filters.received) keys.push("received");
  if (filters.statuses.length > 0) keys.push("status");
  if (filters.priorities.length > 0) keys.push("priority");
  if (filters.projects.length > 0) keys.push("project");
  if (filters.assignees.length > 0) keys.push("assignee");
  if (filters.labels.length > 0) keys.push("label");
  if (filters.dueDates.length > 0) keys.push("dueDate");
  return keys;
}

/** Number of active dimensions — one per chip. */
export function inboxFilterCount(filters: InboxFilters): number {
  return activeInboxFilterKeys(filters).length;
}

/** True when nothing is selected in any dimension. */
export function isEmptyInboxFilters(filters: InboxFilters): boolean {
  return inboxFilterCount(filters) === 0;
}

export function toggleInboxFilterValue<T extends string>(values: readonly T[], value: T): T[] {
  return values.includes(value)
    ? values.filter((candidate) => candidate !== value)
    : [...values, value];
}

/** Clear one dimension, dropping its "is not" with its values. */
export function withoutInboxFilter(filters: InboxFilters, key: InboxFilterKey): InboxFilters {
  switch (key) {
    case "unread":
      return { ...filters, unreadOnly: false };
    case "received":
      return { ...filters, received: null };
    default:
      return {
        ...filters,
        [INBOX_DIMENSION_FIELD[key]]: [],
        negated: filters.negated.filter((dimension) => dimension !== key),
      };
  }
}

const RECEIVED_SET = new Set<string>(INBOX_RECEIVED_FILTERS);
const TYPE_SET = new Set<string>(INBOX_TYPE_GROUPS);
const DUE_SET = new Set<string>(DUE_DATE_FILTERS);
const DIMENSION_SET = new Set<string>(INBOX_LIST_DIMENSIONS);

function stringList(value: unknown, allowed?: ReadonlySet<string>): string[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter((v): v is string => typeof v === "string" && v !== "" && (!allowed || allowed.has(v))))];
}

/**
 * Rebuild a stored entry field by field. Persisted state outlives the code
 * that wrote it, so an unknown value or a missing field (an entry written
 * before a dimension existed) falls back to "not filtering" instead of
 * reaching the filters as `undefined`.
 */
export function sanitizeInboxFilters(raw: unknown): InboxFilters {
  const value = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  const filters: InboxFilters = {
    types: stringList(value.types, TYPE_SET) as InboxTypeGroup[],
    actors: stringList(value.actors),
    received: typeof value.received === "string" && RECEIVED_SET.has(value.received)
      ? (value.received as InboxReceivedFilter)
      : null,
    unreadOnly: value.unreadOnly === true,
    statuses: stringList(value.statuses) as IssueStatus[],
    priorities: stringList(value.priorities) as IssuePriority[],
    projects: stringList(value.projects),
    assignees: stringList(value.assignees),
    labels: stringList(value.labels),
    dueDates: stringList(value.dueDates, DUE_SET) as DueDateFilter[],
    negated: stringList(value.negated, DIMENSION_SET) as InboxListDimension[],
  };
  return {
    ...filters,
    negated: filters.negated.filter((dimension) => inboxFilterValues(filters, dimension).length > 0),
  };
}

/**
 * Capability inferred from the parsed response, not from a version string.
 *
 * The new backend always serializes `issue_priority` for every Inbox row
 * (including `null` for notifications without an issue). An older backend
 * omits it. Requiring every returned row to carry a defined value also keeps a
 * priority cache patch from making a mixed rolling-deploy response look fully
 * supported.
 */
export function inboxPriorityFilterSupport(
  items: readonly InboxItem[],
): InboxPriorityFilterSupport {
  if (items.length === 0) return "unknown";
  return items.every((item) => item.issue_priority !== undefined)
    ? "supported"
    : "unsupported";
}

/**
 * Same rule for the project / assignee / label / due-date projection. Its
 * label list is always an array on a server that has it, so that field is
 * the probe.
 */
export function inboxIssueFieldsSupport(
  items: readonly InboxItem[],
): InboxFilterSupport {
  if (items.length === 0) return "unknown";
  return items.every((item) => Array.isArray(item.issue_label_ids))
    ? "supported"
    : "unsupported";
}

/**
 * Capability of the main Inbox list, for the page, its preview and the filter
 * menu alike. With no notification there is nothing to probe, but nothing for
 * the server to answer either: every row left is a run in "Working", matched
 * against its issue on the client. The selection holds for those rows, and an
 * older server is still caught by the first notification that arrives.
 */
export function activeInboxFilterSupport(
  items: readonly InboxItem[],
): { priority: InboxPriorityFilterSupport; issueFields: InboxFilterSupport } {
  if (items.length === 0) return { priority: "supported", issueFields: "supported" };
  return { priority: inboxPriorityFilterSupport(items), issueFields: inboxIssueFieldsSupport(items) };
}

/** Ignore a stale priority selection until the backend capability is proven. */
export function inboxFiltersForPrioritySupport(
  filters: InboxFilters,
  support: InboxPriorityFilterSupport,
): InboxFilters {
  if (support === "supported" || filters.priorities.length === 0) {
    return filters;
  }
  return withoutInboxFilter(filters, "priority");
}

/**
 * The selection a list can honour. Unproven dimensions stay dormant rather
 * than hiding every row: priority and the issue fields until the server is
 * known to send them, and on the archive also everything only that newer
 * server understands — type, received, and "is not" — since an older one
 * would ignore them and answer a different question.
 *
 * The two views share one stored selection. The main Inbox only reads its
 * issue dimensions (INBOX_NOTIFICATION_FILTER_KEYS), so a notification
 * dimension picked in the archive is dormant there rather than lost.
 */
export function effectiveInboxFilters(
  filters: InboxFilters,
  {
    priority,
    issueFields,
    archived = false,
  }: { priority: InboxFilterSupport; issueFields: InboxFilterSupport; archived?: boolean },
): InboxFilters {
  let next = inboxFiltersForPrioritySupport(filters, priority);
  if (issueFields !== "supported") {
    const dormant: InboxFilterKey[] = archived
      ? [...INBOX_ISSUE_FIELD_DIMENSIONS, "type", "received", ...next.negated]
      : [...INBOX_ISSUE_FIELD_DIMENSIONS];
    next = dormant.reduce(withoutInboxFilter, next);
  }
  if (!archived) next = INBOX_NOTIFICATION_FILTER_KEYS.reduce(withoutInboxFilter, next);
  return next;
}

/** Start of the received window, at a local midnight so it holds all day. */
export function inboxReceivedAfter(received: InboxReceivedFilter, now: Date = new Date()): Date {
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  start.setDate(start.getDate() - (RECEIVED_DAYS[received] - 1));
  return start;
}

/**
 * The archive's filter parameters, in a stable order. Also the archive's
 * query key, so the request and its cache entry can never disagree.
 */
export function archivedInboxQuery(
  filters: ArchivedInboxFilters,
  now: Date = new Date(),
): Record<string, string> {
  const params: Record<string, string> = {};
  const list = (name: string, values: readonly string[]) => {
    if (values.length > 0) params[name] = [...values].sort().join(",");
  };
  list("types", filters.types);
  list("actors", filters.actors);
  list("statuses", filters.statuses);
  list("priorities", filters.priorities);
  list("projects", filters.projects);
  list("assignees", filters.assignees);
  list("labels", filters.labels);
  list("due_dates", filters.dueDates);
  list(
    "negated",
    filters.negated.filter((dimension) => inboxFilterValues(filters as InboxFilters, dimension).length > 0),
  );
  if (filters.dueDates.length > 0) params.today = toDateOnly(now);
  if (filters.received) params.received_after = inboxReceivedAfter(filters.received, now).toISOString();
  if (filters.unreadOnly) params.unread_only = "true";
  return params;
}

/** Page state `filterInboxItems` needs beyond the selected filters. */
export interface InboxFilterContext {
  /**
   * Group key (`issue_id ?? id`) of the open notification, pinned against the
   * unread dimension only. Set by the page after opening marks the row read.
   */
  readonly pinnedUnreadKey?: string | null;
  /** Clock for the received and due-date dimensions. */
  readonly now?: Date;
}

/** The issue fields an inbox row carries, which the issue dimensions read. */
export type InboxIssueProjection = Pick<
  InboxItem,
  | "issue_id"
  | "issue_status"
  | "issue_priority"
  | "issue_project_id"
  | "issue_assignee_type"
  | "issue_assignee_id"
  | "issue_due_date"
  | "issue_label_ids"
>;

type RowTest = (item: InboxItem) => boolean;
type IssueTest = (issue: InboxIssueProjection) => boolean;

type NotificationDimension = "type" | "actor";
type IssueDimension = Exclude<InboxListDimension, NotificationDimension>;

const NOTIFICATION_DIMENSIONS: readonly NotificationDimension[] = ["type", "actor"];
const ISSUE_DIMENSIONS: readonly IssueDimension[] = [
  "status",
  "priority",
  "project",
  "assignee",
  "label",
  "dueDate",
];

function isNotificationDimension(dimension: InboxListDimension): dimension is NotificationDimension {
  return dimension === "type" || dimension === "actor";
}

/** Values a notification has in a dimension about itself. */
function notificationValues(item: InboxItem, dimension: NotificationDimension): readonly string[] {
  if (dimension === "type") return [inboxTypeGroup(item.type)];
  const key = inboxActorKey(item);
  return key ? [key] : [];
}

/** Values an issue has in a dimension; null when there is no issue to ask. */
function issueValues(issue: InboxIssueProjection, dimension: IssueDimension, today: string): readonly string[] | null {
  if (issue.issue_id == null) return null;
  switch (dimension) {
    case "status":
      return issue.issue_status == null ? null : [issue.issue_status];
    case "priority":
      return issue.issue_priority == null ? null : [issue.issue_priority];
    case "project":
      return [issue.issue_project_id ?? FILTER_NONE_VALUE];
    case "assignee":
      return [
        issue.issue_assignee_type && issue.issue_assignee_id
          ? `${issue.issue_assignee_type}:${issue.issue_assignee_id}`
          : FILTER_NONE_VALUE,
      ];
    case "label":
      return issue.issue_label_ids ?? [];
    case "dueDate":
      return DUE_DATE_FILTERS.filter((bucket) => dueDateMatches(issue.issue_due_date, bucket, today));
  }
}

function dimensionValues(item: InboxItem, dimension: InboxListDimension, today: string): readonly string[] | null {
  return isNotificationDimension(dimension) ? notificationValues(item, dimension) : issueValues(item, dimension, today);
}

function dimensionTest<Row>(
  filters: InboxFilters,
  dimension: InboxListDimension,
  valuesOf: (row: Row) => readonly string[] | null,
): ((row: Row) => boolean) | null {
  const selected = inboxFilterValues(filters, dimension);
  if (selected.length === 0) return null;
  const set = new Set(selected);
  const negated = filters.negated.includes(dimension);
  return (row) => {
    const values = valuesOf(row);
    // A notification without an issue answers no issue question, "is not"
    // included.
    if (values === null) return false;
    return values.some((value) => set.has(value)) !== negated;
  };
}

interface CompiledInboxFilters {
  /** Per-notification tests, keyed so a facet can leave its own out. */
  notification: Map<InboxListDimension | "received", RowTest>;
  issue: Map<InboxListDimension, IssueTest>;
}

function compile(filters: InboxFilters, now: Date): CompiledInboxFilters {
  const today = toDateOnly(now);
  const notification = new Map<InboxListDimension | "received", RowTest>();
  for (const dimension of NOTIFICATION_DIMENSIONS) {
    const test = dimensionTest(filters, dimension, (item: InboxItem) => notificationValues(item, dimension));
    if (test) notification.set(dimension, test);
  }
  if (filters.received) {
    const after = inboxReceivedAfter(filters.received, now).getTime();
    notification.set("received", (item) => new Date(item.created_at).getTime() >= after);
  }
  const issue = new Map<InboxListDimension, IssueTest>();
  for (const dimension of ISSUE_DIMENSIONS) {
    const test = dimensionTest(filters, dimension, (row: InboxIssueProjection) => issueValues(row, dimension, today));
    if (test) issue.set(dimension, test);
  }
  return { notification, issue };
}

function passes<Row>(
  tests: Map<string, (row: Row) => boolean>,
  item: Row,
  skip?: string,
): boolean {
  for (const [key, test] of tests) {
    if (key !== skip && !test(item)) return false;
  }
  return true;
}

/** The issue fields of an issue, shaped like an inbox row carries them. */
export function inboxIssueProjection(
  issue: Pick<Issue, "id" | "status" | "priority" | "project_id" | "assignee_type" | "assignee_id" | "due_date" | "labels">,
): InboxIssueProjection {
  return {
    issue_id: issue.id,
    issue_status: issue.status,
    issue_priority: issue.priority,
    issue_project_id: issue.project_id,
    issue_assignee_type: issue.assignee_type,
    issue_assignee_id: issue.assignee_id,
    issue_due_date: issue.due_date,
    issue_label_ids: (issue.labels ?? []).map((label) => label.id),
  };
}

/**
 * Whether an issue passes the issue dimensions: the test for a row that has
 * no notification to ask, such as an issue an agent is working on.
 */
export function inboxIssueMatches(
  issue: InboxIssueProjection,
  filters: InboxFilters,
  now: Date = new Date(),
): boolean {
  return passes(compile(filters, now).issue, issue);
}

/**
 * One row per issue: OR within a dimension, AND between dimensions, "is not"
 * per dimension.
 *
 * Takes every notification, not the deduplicated list, because the
 * notification dimensions — type, sender, received — match ANY notification
 * of an issue, and the issue then renders as its newest matching one. Filter
 * by "mentions" and an issue mentioned yesterday and updated since shows the
 * mention, not the update: what you filter by is what you see. With no
 * notification filter that is simply the newest row, as deduplication shows.
 *
 * The issue dimensions — status, priority, project, assignee, label, due
 * date — ask about the issue, so a notification without one never matches
 * them, "is not" included. `unreadOnly` reads the rendered row. The
 * unread badge (`useInboxUnreadCount`) counts the same newest-per-issue rows
 * from the server's summary endpoint, so "only unread" and the number next to
 * Inbox still cannot disagree when no other filter is set.
 *
 * `pinnedUnreadKey` names the notification whose detail is open. Opening an
 * unread notification marks it read, which would otherwise remove the row the
 * user is reading from under the detail pane. The pinned row therefore
 * survives the unread dimension only. Every other dimension always applies,
 * because changing one is an explicit request to filter the notification
 * itself out.
 */
export function filterInboxItems(
  items: readonly InboxItem[],
  filters: InboxFilters,
  { pinnedUnreadKey = null, now = new Date() }: InboxFilterContext = {},
): InboxItem[] {
  const tests = compile(filters, now);
  const out: InboxItem[] = [];
  for (const group of groupInboxRows(items)) {
    const row = group.find((item) => passes(tests.notification, item));
    if (!row || !passes(tests.issue, row)) continue;
    const key = row.issue_id ?? row.id;
    const matchesUnread =
      !filters.unreadOnly ||
      row.read !== true ||
      (pinnedUnreadKey != null && key === pinnedUnreadKey);
    if (matchesUnread) out.push(withCommentAnchor(row, group));
  }
  return sortInboxNewestFirst(out);
}

export interface InboxFacetCounts {
  types: Map<string, number>;
  actors: Map<string, number>;
  statuses: Map<string, number>;
  priorities: Map<string, number>;
  projects: Map<string, number>;
  assignees: Map<string, number>;
  labels: Map<string, number>;
  /** Rows "only unread" would leave. */
  unread: number;
}

function bump(counts: Map<string, number>, key: string) {
  counts.set(key, (counts.get(key) ?? 0) + 1);
}

/**
 * Faceted counts: each value's count is the number of rows selecting that
 * value alone would show, every other dimension applied. For type and sender
 * that is the issue's newest notification of that value, the row selecting it
 * renders.
 */
export function inboxFacetCounts(
  items: readonly InboxItem[],
  filters: InboxFilters,
  { now = new Date() }: Pick<InboxFilterContext, "now"> = {},
): InboxFacetCounts {
  const tests = compile(filters, now);
  const today = toDateOnly(now);
  const counts: InboxFacetCounts = {
    types: new Map(),
    actors: new Map(),
    statuses: new Map(),
    priorities: new Map(),
    projects: new Map(),
    assignees: new Map(),
    labels: new Map(),
    unread: 0,
  };
  const issueFacets: [InboxListDimension, Map<string, number>][] = [
    ["status", counts.statuses],
    ["priority", counts.priorities],
    ["project", counts.projects],
    ["assignee", counts.assignees],
    ["label", counts.labels],
  ];
  const readMatches = (item: InboxItem) => !filters.unreadOnly || item.read !== true;
  for (const group of groupInboxRows(items)) {
    const head = group[0];
    if (!head) continue;
    const issueOk = passes(tests.issue, head);

    const row = group.find((item) => passes(tests.notification, item));
    if (row) {
      for (const [dimension, target] of issueFacets) {
        if (!readMatches(row) || !passes(tests.issue, row, dimension)) continue;
        for (const value of dimensionValues(row, dimension, today) ?? []) bump(target, value);
      }
      if (issueOk && row.read !== true) counts.unread++;
    }
    if (!issueOk) continue;
    for (const [dimension, target] of [["type", counts.types], ["actor", counts.actors]] as const) {
      const seen = new Set<string>();
      for (const item of group) {
        if (!passes(tests.notification, item, dimension)) continue;
        for (const value of dimensionValues(item, dimension, today) ?? []) {
          if (seen.has(value)) continue;
          seen.add(value);
          if (readMatches(item)) bump(target, value);
        }
      }
    }
  }
  return counts;
}
