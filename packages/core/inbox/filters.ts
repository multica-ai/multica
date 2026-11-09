import { toDateOnly } from "../issues/date";
import {
  DUE_DATE_FILTERS,
  FILTER_NONE_VALUE,
  dueDateMatches,
  type DueDateFilter,
} from "../issues/stores/filter-tree";
import type { InboxItem, InboxItemType, IssuePriority, IssueStatus } from "../types";
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
  status_changed: "change",
  priority_changed: "change",
  start_date_changed: "change",
  due_date_changed: "change",
  children_done: "change",
  task_completed: "run",
  task_failed: "run",
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

/** Whether an agent is running or queued on the issue. Active inbox only. */
export type InboxAgentFilter = "running" | "idle";
export const INBOX_AGENT_FILTERS: readonly InboxAgentFilter[] = ["running", "idle"];

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
  readonly agent: InboxAgentFilter | null;
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
export type InboxFilterKey = InboxListDimension | "received" | "unread" | "agent";

export function inboxFilterValues(filters: InboxFilters, dimension: InboxListDimension): readonly string[] {
  return filters[INBOX_DIMENSION_FIELD[dimension]];
}

/**
 * The server-filtered archive has no agent dimension, so `agent` never
 * reaches its endpoints or query keys.
 */
export type ArchivedInboxFilters = Omit<InboxFilters, "agent">;

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
  agent: null,
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
  if (filters.agent) keys.push("agent");
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
    case "agent":
      return { ...filters, agent: null };
    default:
      return {
        ...filters,
        [INBOX_DIMENSION_FIELD[key]]: [],
        negated: filters.negated.filter((dimension) => dimension !== key),
      };
  }
}

const RECEIVED_SET = new Set<string>(INBOX_RECEIVED_FILTERS);
const AGENT_SET = new Set<string>(INBOX_AGENT_FILTERS);
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
    agent: typeof value.agent === "string" && AGENT_SET.has(value.agent)
      ? (value.agent as InboxAgentFilter)
      : value.idleOnly === true
        ? "idle"
        : null,
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
 * would ignore them and answer a different question. The archive has no
 * agent dimension at all.
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
  if (archived && next.agent) next = withoutInboxFilter(next, "agent");
  return next;
}

/** @deprecated Use `effectiveInboxFilters` with `archived: true`. */
export function inboxFiltersForArchive(filters: InboxFilters): InboxFilters {
  return filters.agent ? withoutInboxFilter(filters, "agent") : filters;
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

const EMPTY_ISSUE_IDS: ReadonlySet<string> = new Set();

/** Page state `filterInboxItems` needs beyond the selected filters. */
export interface InboxFilterContext {
  /** Issues an agent is running or queued on. Read only by `agent`. */
  readonly activeIssueIds?: ReadonlySet<string>;
  /**
   * Group key (`issue_id ?? id`) of the open notification, pinned against the
   * unread dimension only. Set by the page after opening marks the row read.
   */
  readonly pinnedUnreadKey?: string | null;
  /**
   * Group key of the open notification, pinned against the agent dimension
   * only. Set by the page once the row was opened while it matched.
   */
  readonly pinnedAgentKey?: string | null;
  /** Clock for the received and due-date dimensions. */
  readonly now?: Date;
}

type RowTest = (item: InboxItem) => boolean;

/** Values a row has in a dimension; null when it has no issue to ask. */
function dimensionValues(item: InboxItem, dimension: InboxListDimension, today: string): readonly string[] | null {
  switch (dimension) {
    case "type":
      return [inboxTypeGroup(item.type)];
    case "actor": {
      const key = inboxActorKey(item);
      return key ? [key] : [];
    }
    default:
      break;
  }
  if (item.issue_id == null) return null;
  switch (dimension) {
    case "status":
      return item.issue_status == null ? null : [item.issue_status];
    case "priority":
      return item.issue_priority == null ? null : [item.issue_priority];
    case "project":
      return [item.issue_project_id ?? FILTER_NONE_VALUE];
    case "assignee":
      return [
        item.issue_assignee_type && item.issue_assignee_id
          ? `${item.issue_assignee_type}:${item.issue_assignee_id}`
          : FILTER_NONE_VALUE,
      ];
    case "label":
      return item.issue_label_ids ?? [];
    case "dueDate":
      return DUE_DATE_FILTERS.filter((bucket) => dueDateMatches(item.issue_due_date, bucket, today));
  }
}

function dimensionTest(filters: InboxFilters, dimension: InboxListDimension, today: string): RowTest | null {
  const selected = inboxFilterValues(filters, dimension);
  if (selected.length === 0) return null;
  const set = new Set(selected);
  const negated = filters.negated.includes(dimension);
  return (item) => {
    const values = dimensionValues(item, dimension, today);
    // A notification without an issue answers no issue question, "is not"
    // included.
    if (values === null) return false;
    return values.some((value) => set.has(value)) !== negated;
  };
}

const NOTIFICATION_DIMENSIONS: readonly InboxListDimension[] = ["type", "actor"];
const ISSUE_DIMENSIONS: readonly InboxListDimension[] = [
  "status",
  "priority",
  "project",
  "assignee",
  "label",
  "dueDate",
];

interface CompiledInboxFilters {
  /** Per-notification tests, keyed so a facet can leave its own out. */
  notification: Map<InboxListDimension | "received", RowTest>;
  issue: Map<InboxListDimension, RowTest>;
}

function compile(filters: InboxFilters, now: Date): CompiledInboxFilters {
  const today = toDateOnly(now);
  const notification = new Map<InboxListDimension | "received", RowTest>();
  for (const dimension of NOTIFICATION_DIMENSIONS) {
    const test = dimensionTest(filters, dimension, today);
    if (test) notification.set(dimension, test);
  }
  if (filters.received) {
    const after = inboxReceivedAfter(filters.received, now).getTime();
    notification.set("received", (item) => new Date(item.created_at).getTime() >= after);
  }
  const issue = new Map<InboxListDimension, RowTest>();
  for (const dimension of ISSUE_DIMENSIONS) {
    const test = dimensionTest(filters, dimension, today);
    if (test) issue.set(dimension, test);
  }
  return { notification, issue };
}

function passes(
  tests: Map<string, RowTest>,
  item: InboxItem,
  skip?: string,
): boolean {
  for (const [key, test] of tests) {
    if (key !== skip && !test(item)) return false;
  }
  return true;
}

function agentMatches(
  item: InboxItem,
  agent: InboxAgentFilter | null,
  activeIssueIds: ReadonlySet<string>,
): boolean {
  if (!agent) return true;
  if (item.issue_id == null) return false;
  return activeIssueIds.has(item.issue_id) === (agent === "running");
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
 * date, agent — ask about the issue, so a notification without one never
 * matches them, "is not" included. `unreadOnly` reads the rendered row. The
 * unread badge (`useInboxUnreadCount`) counts the same newest-per-issue rows
 * from the server's summary endpoint, so "only unread" and the number next to
 * Inbox still cannot disagree when no other filter is set.
 *
 * `pinnedUnreadKey` / `pinnedAgentKey` name the notification whose detail is
 * open. Opening an unread notification marks it read, and replying from the
 * detail can start or finish an agent on its issue; either would otherwise
 * remove the row the user is working in from under the detail pane. The
 * pinned row therefore survives the unread and agent dimensions only, and
 * each pin only crosses its own dimension. Every other dimension always
 * applies, because changing one is an explicit request to filter the
 * notification itself out.
 */
export function filterInboxItems(
  items: readonly InboxItem[],
  filters: InboxFilters,
  {
    activeIssueIds = EMPTY_ISSUE_IDS,
    pinnedUnreadKey = null,
    pinnedAgentKey = null,
    now = new Date(),
  }: InboxFilterContext = {},
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
    const matchesAgent =
      agentMatches(row, filters.agent, activeIssueIds) ||
      (pinnedAgentKey != null && key === pinnedAgentKey);
    if (matchesUnread && matchesAgent) out.push(withCommentAnchor(row, group));
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
  {
    activeIssueIds = EMPTY_ISSUE_IDS,
    now = new Date(),
  }: Pick<InboxFilterContext, "activeIssueIds" | "now"> = {},
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
    if (!head || !agentMatches(head, filters.agent, activeIssueIds)) continue;
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
