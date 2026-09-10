/**
 * Issue status resolution for mobile (MUL-6243).
 *
 * A workspace always has seven built-in status keys and may define custom
 * ones. Every status belongs to one of five lifecycle categories. Concrete
 * built-ins keep distinct behavior and glyphs inside those groups.
 *
 * Mirrored from `packages/core/issues/status-category.ts` and
 * `packages/core/issue-statuses/queries.ts` rather than imported: those modules
 * reach `../issue-statuses/index`, which pulls web's API client and React Query
 * key factories into the graph — not on mobile's import whitelist
 * (apps/mobile/CLAUDE.md). The TYPES are shared, so the two sides cannot drift
 * on shape; the fallback rules below are restated verbatim so they cannot drift
 * on behavior either.
 */
import type {
  BuiltInIssueStatus,
  Issue,
  IssuePriority,
  IssueStatus,
  IssueStatusCategory,
  IssueStatusEntry,
} from "@multica/core/types";

/**
 * The 5 categories in canonical display order. Mirrors `ALL_STATUSES` in
 * packages/core/issues/config/status.ts.
 */
export const STATUS_CATEGORIES: IssueStatusCategory[] = [
  "backlog",
  "unstarted",
  "started",
  "completed",
  "canceled",
];

export const BUILT_IN_STATUS_ORDER: BuiltInIssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
];

export const BUILT_IN_STATUS_CATEGORY: Record<BuiltInIssueStatus, IssueStatusCategory> = {
  backlog: "backlog",
  todo: "unstarted",
  in_progress: "started",
  in_review: "started",
  blocked: "started",
  done: "completed",
  cancelled: "canceled",
};

/**
 * The categories that get a section in mobile's grouped issue lists —
 * `canceled` excluded, which is a documented mobile divergence (see
 * `components/project/project-related-issues.tsx`).
 *
 * These are CATEGORIES, not status keys: a workspace's custom statuses live
 * inside their category's section rather than adding one of their own. Grouping
 * by key is what made custom-status issues vanish from these lists (MUL-6457) —
 * the bucket existed but no section ever read it.
 */
export const BOARD_CATEGORIES: IssueStatusCategory[] = STATUS_CATEGORIES.filter(
  (category) => category !== "canceled",
);

/**
 * Mobile's own copy for the 7 built-ins. Deliberately NOT the catalog's `name`
 * for those rows: the server seeds built-in names in English and mobile owns
 * its labels, exactly as web resolves built-ins through i18n and only custom
 * statuses through the catalog (`useStatusLabel`).
 */
export const STATUS_LABEL: Record<BuiltInIssueStatus, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
  blocked: "Blocked",
  cancelled: "Cancelled",
};

export const CATEGORY_LABEL: Record<IssueStatusCategory, string> = {
  backlog: "Backlog",
  unstarted: "Unstarted",
  started: "Started",
  completed: "Completed",
  canceled: "Canceled",
};

export const PRIORITY_LABEL: Record<IssuePriority, string> = {
  none: "No priority",
  low: "Low",
  medium: "Medium",
  high: "High",
  urgent: "Urgent",
};

const CATEGORY_SET = new Set<string>(STATUS_CATEGORIES);
const BUILT_IN_SET = new Set<string>(BUILT_IN_STATUS_ORDER);

export function isIssueStatusCategory(value: string): value is IssueStatusCategory {
  return CATEGORY_SET.has(value);
}

export function isBuiltInIssueStatus(value: string): value is BuiltInIssueStatus {
  return BUILT_IN_SET.has(value);
}

/** Accepts both the five-category API and the legacy seven-value response. */
export function normalizeIssueStatusCategory(value: string): IssueStatusCategory | null {
  if (isIssueStatusCategory(value)) return value;
  return isBuiltInIssueStatus(value) ? BUILT_IN_STATUS_CATEGORY[value] : null;
}

/**
 * Category for a bare status KEY, for render paths that hold only the string
 * and no catalog. Exact for the 7 built-ins — which is every status that exists
 * until an admin defines a custom one. A custom key answers `unstarted` so a lookup
 * always resolves to something renderable; surfaces that must show the real
 * status resolve through the catalog instead.
 */
export function statusCategoryOfKey(statusKey: string): IssueStatusCategory {
  if (isBuiltInIssueStatus(statusKey)) return BUILT_IN_STATUS_CATEGORY[statusKey];
  return normalizeIssueStatusCategory(statusKey) ?? "unstarted";
}

/**
 * The category an issue's status belongs to, or null when this payload cannot
 * answer. Reads the server-resolved `status_category` first and otherwise falls
 * back to the fixed built-in key mapping.
 *
 * Pure: no catalog, so list grouping never has to wait on a fetch and never
 * guesses a category while one is in flight.
 */
export function issueStatusCategory(
  issue: Pick<Issue, "status" | "status_category">,
): IssueStatusCategory | null {
  const fromServer = issue.status_category;
  if (fromServer) {
    const normalized = normalizeIssueStatusCategory(fromServer);
    if (normalized) return normalized;
  }
  if (isBuiltInIssueStatus(issue.status)) return BUILT_IN_STATUS_CATEGORY[issue.status];
  return null;
}

/**
 * The section an issue renders in — always an answer, never null.
 *
 * The unresolved fallback lands in `unstarted` rather than nowhere: a row in a
 * possibly-wrong section is recoverable, a row in no section is invisible, and
 * invisible is the bug this exists to prevent ("counts and visibility must
 * agree", apps/mobile/CLAUDE.md). Unreachable in practice — the server sends a
 * category on every issue payload, and every built-in key has a fixed category.
 */
export function issueColumnCategory(
  issue: Pick<Issue, "status" | "status_category">,
): IssueStatusCategory {
  return issueStatusCategory(issue) ?? statusCategoryOfKey(issue.status);
}

/**
 * Whether an issue belongs to a given lifecycle category (MUL-6243).
 *
 * The one question every status-coupled product rule actually asks. Comparing
 * `issue.status` to a built-in key answers it only for a workspace with no
 * custom statuses: a custom status in the `completed` category is done, and code
 * that checks `status === "done"` silently disagrees.
 *
 * An unresolved custom key answers `false`, and that direction is deliberate —
 * callers fail safe that way. "Is it done/cancelled?" false keeps a row at full
 * opacity rather than dimming work that is still open.
 */
export function issueBehavesAs(
  issue: Pick<Issue, "status" | "status_category">,
  category: IssueStatusCategory,
): boolean {
  return issueStatusCategory(issue) === category;
}

/** True when the issue belongs to any of the given categories. */
export function issueBehavesAsAny(
  issue: Pick<Issue, "status" | "status_category">,
  categories: readonly IssueStatusCategory[],
): boolean {
  const resolved = issueStatusCategory(issue);
  return resolved !== null && categories.includes(resolved);
}

/** The categories that mean "this issue is closed" — done or cancelled. */
export const CLOSED_CATEGORIES: readonly IssueStatusCategory[] = ["completed", "canceled"];

/**
 * The `#rrggbb` a surface must paint one catalog entry with, or null when it
 * keeps its category's semantic token.
 *
 * ALWAYS null for a built-in: the 7 carry a seeded hex the server refuses to
 * let anyone edit, and every surface draws them from their category token so
 * they follow the theme into dark mode. Reading the seed instead paints the
 * same status in two different greens depending on which control you look at
 * (MUL-6440).
 */
export function issueStatusColor(entry: IssueStatusEntry | undefined): string | null {
  if (!entry || entry.is_system === true) return null;
  return entry.color || null;
}

/**
 * A resolved view over the workspace catalog. Every lookup falls back to
 * something renderable, because an issue can legitimately carry a status this
 * client has not heard of: one created moments ago in another session, or one
 * whose catalog fetch has not landed yet.
 */
export interface IssueStatusCatalog {
  /**
   * Every status in display order, ARCHIVED INCLUDED, so an issue left on an
   * archived status still resolves to its real name, colour and category. Use
   * `activeStatuses` for anything that OFFERS a status to pick.
   */
  statuses: IssueStatusEntry[];
  /** Assignable statuses — `statuses` minus archived ones. */
  activeStatuses: IssueStatusEntry[];
  /** Category for a status key; exact when built-in, else `unstarted`. */
  categoryOf: (statusKey: string) => IssueStatusCategory;
  /** Label for a status key: mobile's copy for built-ins, the catalog's `name`
   *  for custom statuses, the raw key when neither resolves. */
  labelOf: (statusKey: string) => string;
  /** Catalog entry for a status key, when the catalog knows it. */
  entryOf: (statusKey: string) => IssueStatusEntry | undefined;
  /** See {@link issueStatusColor} — null keeps the category's token colour. */
  colorOf: (statusKey: string) => string | null;
  /** ACTIVE statuses in one category, in display order. */
  inCategory: (category: IssueStatusCategory) => IssueStatusEntry[];
  /** True once the catalog has loaded; false while it is still in flight. */
  isLoaded: boolean;
}

/**
 * Builds the resolved catalog from a raw entry list. Pure, so a non-React
 * caller can use a list it already holds.
 */
export function buildIssueStatusCatalog(
  entries: IssueStatusEntry[] | undefined,
): IssueStatusCatalog {
  const list = entries ?? [];
  const byKey = new Map(list.map((entry) => [entry.key, entry]));

  return {
    statuses: list,
    activeStatuses: list.filter((entry) => !entry.archived_at),
    categoryOf: (statusKey) => {
      const category = byKey.get(statusKey)?.category;
      if (category) {
        const normalized = normalizeIssueStatusCategory(category);
        if (normalized) return normalized;
      }
      return statusCategoryOfKey(statusKey);
    },
    entryOf: (statusKey) => byKey.get(statusKey),
    colorOf: (statusKey) => issueStatusColor(byKey.get(statusKey)),
    labelOf: (statusKey) => {
      // Built-in first, so a workspace that never opened status settings reads
      // exactly as it did before the catalog existed.
      if (isBuiltInIssueStatus(statusKey)) return STATUS_LABEL[statusKey];
      return byKey.get(statusKey)?.name ?? statusKey;
    },
    inCategory: (category) =>
      list.filter(
        (entry) =>
          normalizeIssueStatusCategory(entry.category) === category && !entry.archived_at,
      ),
    isLoaded: entries !== undefined,
  };
}

/**
 * Whether a status key names a CUSTOM status — i.e. whether a surface that
 * already shows the CATEGORY still has something left to say (MUL-6243).
 *
 * Pure, and takes the catalog the caller already holds, so a row does not open
 * a second observer to answer the same question it just asked for a colour.
 */
export function isCustomStatus(
  catalog: IssueStatusCatalog,
  statusKey: string,
): boolean {
  const entry = catalog.entryOf(statusKey);
  if (!entry) return false;
  // `is_system` is the authority. The key comparison is the backstop for a
  // server that does not send it — the schema defaults it to false, and a
  // built-in must stay silent either way.
  return entry.is_system !== true && !isBuiltInIssueStatus(statusKey);
}

/** One row in the status picker / status filter. */
export interface StatusOption {
  key: IssueStatus;
  /** The category this status behaves as — drives its glyph. */
  category: IssueStatusCategory;
  label: string;
  /** `#rrggbb` for a custom status; null for a built-in, which keeps its token. */
  color: string | null;
}

/**
 * The statuses a user can pick or filter by, as one flat list in canonical
 * category order. Mirrors web's `useStatusOptions`
 * (packages/views/issues/utils/status-options.ts).
 *
 * Shared by the picker and the filter so the two can never drift — a status
 * offered in one and missing from the other is exactly how an issue becomes
 * unfindable. Archived statuses are excluded: archiving retires a status from
 * future assignment, and issues already on one keep it and keep their label.
 *
 * A category with no catalog row falls back to its built-in, so a cold render
 * (or a backend that predates the endpoint) offers the same 7 rows it always
 * did instead of an empty sheet.
 */
export function statusOptions(catalog: IssueStatusCatalog): StatusOption[] {
  return STATUS_CATEGORIES.flatMap<StatusOption>((category) => {
    const entries = catalog.inCategory(category);
    if (entries.length === 0) {
      return BUILT_IN_STATUS_ORDER.filter(
        (status) => BUILT_IN_STATUS_CATEGORY[status] === category,
      ).map((status) => ({
        key: status,
        category,
        label: STATUS_LABEL[status],
        color: null,
      }));
    }
    return entries.map((entry) => ({
      key: entry.key,
      category,
      label: catalog.labelOf(entry.key),
      color: issueStatusColor(entry),
    }));
  });
}
