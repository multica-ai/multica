// TECH-3413 — pure filtering/grouping for a dynamic-inbox section.
//
// Kept free of React and data-fetching so it can be unit-tested in isolation.
// Operates on the same merged-entry shape the classic inbox builds (notif /
// chat / channel) and reuses the cerebro action classifier for the
// action-based section kinds.
import { classifyInboxAction, inboxActionOrderIndex } from "@multica/cerebro-inbox";
import type { InboxActionContext, InboxActionCategory } from "@multica/cerebro-inbox";
import type { InboxItem, ChatSession, Channel } from "@multica/core/types";
import type { InboxSectionConfig } from "./layout";

export type DynInboxEntry =
  | { kind: "notif"; id: string; time: number; item: InboxItem }
  | { kind: "chat"; id: string; time: number; session: ChatSession }
  | { kind: "channel"; id: string; time: number; channel: Channel };

export interface SectionFilterContext {
  action: InboxActionContext;
  /** Pinned-view predicate (from useInboxPinnedMatcher). */
  matchesPins: (entry: DynInboxEntry) => boolean;
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

export function sortEntries(entries: DynInboxEntry[], section: InboxSectionConfig): DynInboxEntry[] {
  const dir = section.sort === "oldest" ? 1 : -1;
  return [...entries].sort((a, b) => (a.time - b.time) * dir);
}

/** TECH-3413 #4: is this entry muted right now? (notif rows carry muted_until). */
export function entryIsMuted(entry: DynInboxEntry): boolean {
  if (entry.kind !== "notif") return false;
  const until = entry.item.muted_until;
  if (!until) return false;
  const ts = Date.parse(until);
  return Number.isFinite(ts) && ts > Date.now();
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
      (!section.excludeMuted || !entryIsMuted(e)) &&
      entryMatchesQuery(e, opts.query ?? ""),
  );
  const sorted = sortEntries(filtered, section);
  if (section.maxRows && section.maxRows > 0) return sorted.slice(0, section.maxRows);
  return sorted;
}

export interface SectionGroup {
  key: string;
  label: string;
  order: number;
  entries: DynInboxEntry[];
}

/** Group a section's already-selected rows under sub-headers. */
export function groupSectionEntries(
  entries: DynInboxEntry[],
  section: InboxSectionConfig,
  ctx: SectionFilterContext,
  labels: Record<InboxActionCategory, string>,
): SectionGroup[] {
  if (section.groupBy !== "action") {
    return [{ key: "all", label: "", order: 0, entries }];
  }
  const byCat = new Map<InboxActionCategory, DynInboxEntry[]>();
  for (const entry of entries) {
    const cat = classifyInboxAction(entry, ctx.action);
    const list = byCat.get(cat) ?? [];
    list.push(entry);
    byCat.set(cat, list);
  }
  return [...byCat.entries()]
    .map(([cat, list]) => ({
      key: cat,
      label: labels[cat] ?? cat,
      order: inboxActionOrderIndex(cat),
      entries: list,
    }))
    .sort((a, b) => a.order - b.order);
}
