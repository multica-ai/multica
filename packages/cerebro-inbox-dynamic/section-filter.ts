// TECH-3413 — pure filtering/grouping for a dynamic-inbox section.
//
// Kept free of React and data-fetching so it can be unit-tested in isolation.
// Operates on the same merged-entry shape the classic inbox builds (notif /
// chat / channel) and reuses the cerebro action classifier for the
// action-based section kinds.
import { classifyInboxAction, inboxActionOrderIndex } from "@multica/cerebro-inbox";
import type { InboxActionContext, InboxActionCategory } from "@multica/cerebro-inbox";
import type { InboxItem, ChatSession, Channel, Project } from "@multica/core/types";
import type {
  InboxSectionConfig,
  FilterCondition,
  SectionGroupBy,
  SectionViewFilter,
} from "./layout";

export type DynInboxEntry =
  | { kind: "notif"; id: string; time: number; item: InboxItem }
  | { kind: "chat"; id: string; time: number; session: ChatSession }
  | { kind: "channel"; id: string; time: number; channel: Channel };

export interface SectionFilterContext {
  action: InboxActionContext;
  /** Pinned-view predicate (from useInboxPinnedMatcher). */
  matchesPins: (entry: DynInboxEntry) => boolean;
  /**
   * TECH-3502 #3 — descendant project ids per project (NOT including the
   * project itself), used by a "project" filter condition with
   * includeSubprojects. Optional: absent in unit tests and when the project
   * tree is unavailable, in which case sub-projects simply don't match.
   */
  subProjectIds?: Map<string, Set<string>>;
}

const ACTION_KIND: Partial<Record<InboxSectionConfig["kind"], InboxActionCategory>> = {
  act_now: "act_now",
  running: "watching",
  reminders: "reminders",
  waiting: "waiting",
  calm: "calm",
};

export function entryIsUnread(entry: DynInboxEntry): boolean {
  switch (entry.kind) {
    case "notif":
      return !entry.item.read;
    case "chat":
      return entry.session.has_unread === true;
    case "channel":
      return (entry.channel.unread_count ?? 0) > 0;
    default:
      return false;
  }
}

/** TECH-3413 #4: does this entry @-mention the user right now? */
export function entryIsMentioned(entry: DynInboxEntry, ctx: SectionFilterContext): boolean {
  switch (entry.kind) {
    case "notif":
      return entry.item.type === "mentioned";
    case "channel":
      return ctx.action.mentionedChannels.has(entry.channel.id);
    default:
      return false;
  }
}

/** TECH-3502 #3: does an agent currently have a run going on this row? */
export function entryIsRunning(entry: DynInboxEntry, ctx: SectionFilterContext): boolean {
  if (entry.kind === "notif" && entry.item.issue_id) {
    return (
      ctx.action.issueRunStates.has(entry.item.issue_id) ||
      ctx.action.subIssueRunStates.has(entry.item.issue_id)
    );
  }
  if (entry.kind === "chat") return ctx.action.chatRunStates.has(entry.session.id);
  return false;
}

export function entryProjectId(entry: DynInboxEntry): string | null {
  switch (entry.kind) {
    case "notif":
      return entry.item.project_id ?? null;
    case "channel":
      return entry.channel.project_id ?? null;
    default:
      return null;
  }
}

/** Does a single entry belong in this section? */
export function entryMatchesSection(
  entry: DynInboxEntry,
  section: InboxSectionConfig,
  ctx: SectionFilterContext,
): boolean {
  switch (section.kind) {
    case "all":
      return true;
    case "unread":
      return entryIsUnread(entry);
    case "pinned":
      return ctx.matchesPins(entry);
    case "project":
      return !!section.projectId && entryProjectId(entry) === section.projectId;
    case "filter":
      // TECH-3413 #5 — AND of every condition in the filter builder. An empty
      // filter shows everything (the user stacks conditions to narrow it).
      return sectionFilters(section).every((c) => conditionMatches(c, entry, ctx));
    case "act_now":
    case "running":
    case "reminders":
    case "waiting":
    case "calm": {
      const want = ACTION_KIND[section.kind];
      return classifyInboxAction(entry, ctx.action) === want;
    }
    default:
      return false;
  }
}

/**
 * TECH-3413 #5 — the effective filter conditions for a "filter" section.
 * Returns `section.filters` when set; otherwise seeds the list from the legacy
 * toggle booleans so filter sections saved before the builder keep working.
 */
export function sectionFilters(section: InboxSectionConfig): FilterCondition[] {
  if (section.filters) return section.filters;
  const seeded: FilterCondition[] = [];
  if (section.filterUnread) seeded.push({ field: "unread" });
  if (section.filterMentioned) seeded.push({ field: "mentioned" });
  if (section.filterPinned) seeded.push({ field: "pinned" });
  if (section.projectId) seeded.push({ field: "project", projectId: section.projectId });
  return seeded;
}

function conditionMatches(
  cond: FilterCondition,
  entry: DynInboxEntry,
  ctx: SectionFilterContext,
): boolean {
  switch (cond.field) {
    case "unread":
      return entryIsUnread(entry);
    case "read":
      return !entryIsUnread(entry);
    case "mentioned":
      return entryIsMentioned(entry, ctx);
    case "pinned":
      return ctx.matchesPins(entry);
    case "muted":
      return entryIsMuted(entry);
    case "running":
      return entryIsRunning(entry, ctx);
    case "kind": {
      if (!cond.entryKind) return true;
      // "issue" rows are stored as notif entries; chat/channel match directly.
      const want = cond.entryKind === "issue" ? "notif" : cond.entryKind;
      return entry.kind === want;
    }
    case "project": {
      if (!cond.projectId) return false;
      const pid = entryProjectId(entry);
      if (pid === cond.projectId) return true;
      if (cond.includeSubprojects && pid && ctx.subProjectIds) {
        return ctx.subProjectIds.get(cond.projectId)?.has(pid) ?? false;
      }
      return false;
    }
    default:
      return true;
  }
}

// TECH-3541 #2 — the three time-signals per entry used by the user-selectable
// sort, mirroring the classic inbox's inboxSortSignals. -Infinity means the
// signal is unknown for that entry so it sinks to the bottom regardless of
// direction.
function inboxSortSignals(
  entry: DynInboxEntry,
  userId: string,
): { lastMessage: number; lastFromMe: number; lastFromOther: number } {
  const lastMessage = entry.time;
  if (entry.kind === "notif") {
    const item = entry.item;
    const authoredByMe = item.actor_type === "member" && item.actor_id === userId;
    return {
      lastMessage,
      lastFromMe: authoredByMe ? lastMessage : -Infinity,
      lastFromOther: authoredByMe ? -Infinity : lastMessage,
    };
  }
  if (entry.kind === "channel") {
    const lastMsg = entry.channel.last_message;
    if (!lastMsg) return { lastMessage, lastFromMe: -Infinity, lastFromOther: -Infinity };
    const authoredByMe = lastMsg.author_type === "member" && lastMsg.author_id === userId;
    return {
      lastMessage,
      lastFromMe: authoredByMe ? lastMessage : -Infinity,
      lastFromOther: authoredByMe ? -Infinity : lastMessage,
    };
  }
  // Chat sessions: has_unread === true means the agent replied last (= not from
  // me), false means the user spoke last.
  const fromAgent = !!entry.session.has_unread;
  return {
    lastMessage,
    lastFromMe: fromAgent ? -Infinity : lastMessage,
    lastFromOther: fromAgent ? lastMessage : -Infinity,
  };
}

export function sortEntries(
  entries: DynInboxEntry[],
  section: InboxSectionConfig,
  userId = "",
): DynInboxEntry[] {
  const dir = section.sort === "oldest" ? 1 : -1;
  const method = section.sortMethod ?? "last_message";
  // "last_message" is the plain activity-time sort the box has always used.
  if (method === "last_message") {
    return [...entries].sort((a, b) => (a.time - b.time) * dir);
  }
  const pick = (e: DynInboxEntry): number => {
    const s = inboxSortSignals(e, userId);
    return method === "last_me" ? s.lastFromMe : s.lastFromOther;
  };
  return [...entries].sort((a, b) => {
    const sa = pick(a);
    const sb = pick(b);
    const aMissing = !Number.isFinite(sa);
    const bMissing = !Number.isFinite(sb);
    if (aMissing && !bMissing) return 1;
    if (!aMissing && bMissing) return -1;
    if (aMissing && bMissing) return 0;
    return (sa - sb) * dir;
  });
}

/** TECH-3413 #4 / TECH-3541 #2: is this entry muted right now? Notif, channel
 *  and chat rows can all carry a muted_until (matches the classic inbox). */
export function entryIsMuted(entry: DynInboxEntry): boolean {
  const until =
    entry.kind === "notif"
      ? entry.item.muted_until
      : entry.kind === "channel"
        ? entry.channel.muted_until
        : entry.session.muted_until;
  if (!until) return false;
  const ts = Date.parse(until);
  return Number.isFinite(ts) && ts > Date.now();
}

/**
 * TECH-3541 #2 — the classic inbox "All ▾" view switch, as a per-box filter.
 * Mirrors `matchesView` in the classic inbox: muted rows are hidden from every
 * view except "muted", and "pinned" uses the pin matcher.
 */
export function matchesViewFilter(
  entry: DynInboxEntry,
  view: SectionViewFilter,
  ctx: SectionFilterContext,
): boolean {
  if (view !== "muted" && entryIsMuted(entry)) return false;
  switch (view) {
    case "all":
      return true;
    case "unread":
      return entryIsUnread(entry);
    case "mentioned":
      return entry.kind === "notif" && entry.item.type === "mentioned";
    case "issues":
      return entry.kind === "notif";
    case "channels":
      return entry.kind === "channel" && entry.channel.kind === "channel";
    case "dms":
      // 1:1 agent chats group with DMs — the user's "DM" view is every 1:1
      // thread they have.
      return entry.kind === "chat" || (entry.kind === "channel" && entry.channel.kind === "dm");
    case "muted":
      return entryIsMuted(entry);
    case "pinned":
      return ctx.matchesPins(entry);
    default:
      return true;
  }
}

/** TECH-3413 #9: free-text search across a row's visible text. */
export function entryText(entry: DynInboxEntry): string {
  switch (entry.kind) {
    case "notif":
      return `${entry.item.title ?? ""} ${entry.item.body ?? ""}`;
    case "chat":
      return entry.session.title ?? "";
    case "channel":
      return `${entry.channel.title ?? ""} ${entry.channel.last_message?.content ?? ""}`;
    default:
      return "";
  }
}

export function entryMatchesQuery(entry: DynInboxEntry, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return entryText(entry).toLowerCase().includes(q);
}

export interface SelectOptions {
  /** Free-text query applied across all sections (from the inbox search bar). */
  query?: string;
}

/** Filter + sort + cap a section's rows. */
export function selectSectionEntries(
  entries: DynInboxEntry[],
  section: InboxSectionConfig,
  ctx: SectionFilterContext,
  opts: SelectOptions = {},
): DynInboxEntry[] {
  const filtered = entries.filter(
    (e) =>
      entryMatchesSection(e, section, ctx) &&
      // TECH-3541 #2 — the classic view switch, applied on top of the box kind.
      (!section.viewFilter || matchesViewFilter(e, section.viewFilter, ctx)) &&
      (!section.excludeMuted || !entryIsMuted(e)) &&
      entryMatchesQuery(e, opts.query ?? ""),
  );
  const sorted = sortEntries(filtered, section, ctx.action.userId);
  if (section.maxRows && section.maxRows > 0) return sorted.slice(0, section.maxRows);
  return sorted;
}

export interface SectionGroup {
  key: string;
  label: string;
  order: number;
  entries: DynInboxEntry[];
  /** A "No project" / "No agent" style catch-all bucket — sorts to the bottom. */
  isFallback?: boolean;
  /** TECH-3541 #2 — second-level ("Then by") groups. When present the rows
   *  live in the children, not in `entries`. */
  children?: SectionGroup[];
}

/**
 * TECH-3541 #2 — lookup tables the project/agent/type grouping needs to render
 * human labels. All optional so the pure grouping degrades to ids/fallbacks
 * when a map is missing (e.g. in unit tests).
 */
export interface SectionGroupContext {
  projectMap?: Map<string, Project>;
  agentMap?: Map<string, { name?: string }>;
  /** InboxItem.type → label (mirrors the classic inbox's useTypeLabels). */
  typeLabels?: Record<string, string>;
}

interface Bucket {
  key: string;
  label: string;
  /** Fixed bucket order (action mode); undefined falls back to label sort. */
  order?: number;
  isFallback: boolean;
}

// Mirror of the classic inbox `bucketize`: map one entry to its group bucket
// for a given grouping mode.
function bucketize(
  mode: SectionGroupBy,
  entry: DynInboxEntry,
  ctx: SectionFilterContext,
  labels: Record<InboxActionCategory, string>,
  gctx: SectionGroupContext,
): Bucket {
  if (mode === "action") {
    const cat = classifyInboxAction(entry, ctx.action);
    return { key: cat, label: labels[cat] ?? cat, order: inboxActionOrderIndex(cat), isFallback: false };
  }
  if (mode === "project") {
    const pid = entryProjectId(entry);
    if (pid) {
      return { key: pid, label: gctx.projectMap?.get(pid)?.title ?? "Unknown project", isFallback: false };
    }
    return { key: "__no_project__", label: "No project", isFallback: true };
  }
  if (mode === "agent") {
    if (entry.kind === "chat") {
      const id = entry.session.agent_id;
      return { key: id, label: gctx.agentMap?.get(id)?.name ?? "Unknown agent", isFallback: false };
    }
    if (entry.kind === "notif") {
      if (entry.item.actor_type === "agent" && entry.item.actor_id) {
        const id = entry.item.actor_id;
        return { key: id, label: gctx.agentMap?.get(id)?.name ?? "Unknown agent", isFallback: false };
      }
      return { key: "__no_agent__", label: "No agent", isFallback: true };
    }
    const agentParticipant = entry.channel.participants?.find((p) => p.user_type === "agent");
    if (agentParticipant) {
      const id = agentParticipant.user_id;
      return { key: id, label: gctx.agentMap?.get(id)?.name ?? "Unknown agent", isFallback: false };
    }
    return { key: "__no_agent__", label: "No agent", isFallback: true };
  }
  if (mode === "type") {
    if (entry.kind === "chat") return { key: "__chat__", label: "Chats", isFallback: true };
    if (entry.kind === "channel") {
      return entry.channel.kind === "dm"
        ? { key: "__dm__", label: "Direct messages", isFallback: true }
        : { key: "__channel__", label: "Channels", isFallback: true };
    }
    const t = entry.item.type;
    return { key: t, label: gctx.typeLabels?.[t] ?? t, isFallback: false };
  }
  return { key: "__none__", label: "", isFallback: true };
}

function buildGroups(
  entries: DynInboxEntry[],
  mode: SectionGroupBy,
  ctx: SectionFilterContext,
  labels: Record<InboxActionCategory, string>,
  gctx: SectionGroupContext,
): SectionGroup[] {
  // `hasOrder` tracks whether a bucket carries an explicit order (action mode).
  const map = new Map<string, SectionGroup & { hasOrder: boolean }>();
  for (const entry of entries) {
    const b = bucketize(mode, entry, ctx, labels, gctx);
    let g = map.get(b.key);
    if (!g) {
      g = {
        key: b.key,
        label: b.label,
        order: b.order ?? 0,
        hasOrder: b.order !== undefined,
        isFallback: b.isFallback,
        entries: [],
      };
      map.set(b.key, g);
    }
    g.entries.push(entry);
  }
  return [...map.values()]
    .sort((a, b) => {
      // Mirror the classic inbox sortGroups: explicit order (action) wins, then
      // fallback buckets sink to the bottom, then alphabetical by label.
      if (a.hasOrder && b.hasOrder) return a.order - b.order;
      if (!!a.isFallback !== !!b.isFallback) return a.isFallback ? 1 : -1;
      return a.label.localeCompare(b.label);
    })
    .map(({ hasOrder: _hasOrder, ...g }) => g);
}

/** Group a section's already-selected rows under sub-headers (optionally two
 *  levels deep via groupBySecondary). */
export function groupSectionEntries(
  entries: DynInboxEntry[],
  section: InboxSectionConfig,
  ctx: SectionFilterContext,
  labels: Record<InboxActionCategory, string>,
  gctx: SectionGroupContext = {},
): SectionGroup[] {
  const primary = section.groupBy ?? "none";
  if (primary === "none") {
    return [{ key: "all", label: "", order: 0, entries }];
  }
  const top = buildGroups(entries, primary, ctx, labels, gctx);
  // "Then by" — only meaningful for the multi-level modes (not none/action).
  const secondary = section.groupBySecondary ?? "none";
  if (secondary === "none" || secondary === primary || primary === "action") {
    return top;
  }
  return top.map((g) => ({
    ...g,
    entries: [],
    children: buildGroups(g.entries, secondary, ctx, labels, gctx),
  }));
}
