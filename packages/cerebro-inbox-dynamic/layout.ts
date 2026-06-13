// TECH-3413 — Dynamic inbox layout model.
//
// A user's inbox is a list of TABS; each tab is one outer box holding an ordered
// list of SECTIONS. Each section is a filtered/grouped slice of the same inbox
// data the classic inbox uses. The whole InboxLayout is persisted to the
// server-synced user.preferences blob (see use-inbox-layout.ts), with an
// optional separate layout for the mobile/PWA view.

/** What a section pulls from the merged inbox feed. */
export type SectionKind =
  | "act_now"
  | "running"
  | "reminders"
  | "waiting"
  | "calm"
  | "unread"
  | "pinned"
  | "project"
  | "all"
  // TECH-3413 #4 — a fully dynamic box: the user composes the filter
  // (unread / pinned / mentioned / project) instead of picking a fixed kind.
  | "filter"
  // TECH-3422 — the Slack-block: a people/DM/channels rail with live presence
  // and typing. Rendered by a dedicated component, not the entry-list filter.
  | "team";

/** How rows inside a section are grouped under sub-headers. */
export type SectionGroupBy = "none" | "action" | "project";

/** How rows inside a section are ordered. */
export type SectionSort = "newest" | "oldest";

export interface InboxSectionConfig {
  /** Stable id for React keys + reorder/remove. */
  id: string;
  kind: SectionKind;
  /** Optional custom header; falls back to the catalog label. */
  title?: string;
  /** Required when kind === "project". */
  projectId?: string;
  groupBy?: SectionGroupBy;
  sort?: SectionSort;
  /** Show the priority chip on rows. */
  showPriority?: boolean;
  /** Compact (denser) rows. */
  compact?: boolean;
  /** Max rows before a "show more" affordance; 0 / undefined = no cap. */
  maxRows?: number;
  // --- TECH-3413 (Jesper feedback) ---
  /** #2: can the box be folded open/closed? Default true. */
  collapsible?: boolean;
  /** #2: does the box start folded closed? Default false (open). */
  defaultCollapsed?: boolean;
  /** #3: how the count renders — plain number or a coloured circle badge. */
  countStyle?: "plain" | "circle";
  /** #4 (partial): hide muted rows in this box (e.g. reminders without muted). */
  excludeMuted?: boolean;
  // --- TECH-3413 #4: composable filter for kind === "filter" (AND of the
  // enabled predicates; projectId is reused as the optional project narrow). ---
  /** Only unread rows. */
  filterUnread?: boolean;
  /** Only pinned rows (same predicate as the "pinned" kind). */
  filterPinned?: boolean;
  /** Only rows that @-mention you. */
  filterMentioned?: boolean;
  /** TECH-3422 — for the "team" (Slack) section: how many people to show.
   *  0 / undefined = show all. Starred people always count first. */
  maxPeople?: number;
  /** TECH-3422 — sort order for the "team" (Chat) section's people + channels.
   *  Starred always float to the top regardless. Default "name". */
  teamSort?: "name" | "recent" | "unread";
}

export interface InboxTabConfig {
  id: string;
  title: string;
  sections: InboxSectionConfig[];
}

export interface InboxLayout {
  /** Bumped when the shape changes so stale blobs can be reset. */
  version: 1;
  tabs: InboxTabConfig[];
  /** Id of the tab to open by default. */
  activeTabId?: string;
}

export const INBOX_LAYOUT_VERSION = 1 as const;

/** Catalog entry shown in the "+ Add section" menu. */
export interface SectionCatalogEntry {
  kind: SectionKind;
  label: string;
  /** Whether picking it needs extra input (e.g. a project). */
  needsProject?: boolean;
}

export const SECTION_CATALOG: SectionCatalogEntry[] = [
  { kind: "act_now", label: "Act now" },
  { kind: "unread", label: "Unread" },
  { kind: "running", label: "Agents working" },
  { kind: "reminders", label: "Reminders" },
  { kind: "pinned", label: "Pinned issues" },
  { kind: "project", label: "Project…", needsProject: true },
  { kind: "waiting", label: "Waiting" },
  { kind: "calm", label: "Done / calm" },
  { kind: "all", label: "All messages" },
  { kind: "filter", label: "Custom filter…" },
  // TECH-3422 — only surfaced in the Add-section menu when the
  // cerebro_inbox_slack_block flag is on (filtered in DynamicInbox).
  { kind: "team", label: "Chat" },
];

export function sectionLabel(section: InboxSectionConfig): string {
  if (section.title && section.title.trim()) return section.title.trim();
  return SECTION_CATALOG.find((c) => c.kind === section.kind)?.label ?? section.kind;
}

let counter = 0;
/** Deterministic-enough id without Date.now()/Math.random() (banned in some envs). */
export function makeId(prefix: string): string {
  counter += 1;
  return `${prefix}_${counter}`;
}

function section(kind: SectionKind, extra: Partial<InboxSectionConfig> = {}): InboxSectionConfig {
  return { id: makeId(kind), kind, groupBy: "none", sort: "newest", showPriority: true, ...extra };
}

/** Built-in presets the user can start from (mirrors the brainstorm). */
export function operatorPreset(): InboxLayout {
  const tabId = makeId("tab");
  return {
    version: INBOX_LAYOUT_VERSION,
    activeTabId: tabId,
    tabs: [
      {
        id: tabId,
        title: "Inbox",
        sections: [
          section("act_now", { groupBy: "action" }),
          section("running"),
          section("reminders"),
        ],
      },
    ],
  };
}

export function managerPreset(): InboxLayout {
  const tabId = makeId("tab");
  return {
    version: INBOX_LAYOUT_VERSION,
    activeTabId: tabId,
    tabs: [
      {
        id: tabId,
        title: "Inbox",
        sections: [section("unread"), section("running"), section("pinned")],
      },
    ],
  };
}

export const DEFAULT_INBOX_LAYOUT = operatorPreset;

export interface InboxPreset {
  key: string;
  label: string;
  build: () => InboxLayout;
}

export const INBOX_PRESETS: InboxPreset[] = [
  { key: "operator", label: "Operator", build: operatorPreset },
  { key: "manager", label: "Manager", build: managerPreset },
];

/** Defensive: validate an untrusted layout blob from preferences. */
export function isValidLayout(value: unknown): value is InboxLayout {
  if (!value || typeof value !== "object") return false;
  const v = value as Partial<InboxLayout>;
  if (v.version !== INBOX_LAYOUT_VERSION) return false;
  if (!Array.isArray(v.tabs) || v.tabs.length === 0) return false;
  return v.tabs.every(
    (t) =>
      t &&
      typeof t.id === "string" &&
      typeof t.title === "string" &&
      Array.isArray(t.sections) &&
      t.sections.every((s) => typeof s?.id === "string" && typeof s?.kind === "string"),
  );
}
