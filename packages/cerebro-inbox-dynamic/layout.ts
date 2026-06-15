// TECH-3413 — Dynamic inbox layout model.
//
// A user's inbox is a list of TABS; each tab is one outer box holding an ordered
// list of SECTIONS. Each section is a filtered/grouped slice of the same inbox
// data the classic inbox uses. The whole InboxLayout is persisted to the
// server-synced user.preferences blob (see use-inbox-layout.ts), with an
// optional separate layout for the mobile/PWA view.

// TECH-3413 #5 / TECH-3502 #3 — one condition in the filter builder. Most
// predicates are pure booleans; `project` needs a projectId (and may include
// its sub-projects); `kind` needs an entryKind.
export type FilterField =
  | "unread"
  | "read"
  | "mentioned"
  | "pinned"
  | "muted"
  | "running"
  | "kind"
  | "project";

/** TECH-3502 #3 — narrow a "kind" condition to one row type. */
export type EntryKindFilter = "issue" | "chat" | "channel";

export interface FilterCondition {
  field: FilterField;
  /** Required when field === "project". */
  projectId?: string;
  /** field === "project": also match rows in this project's sub-projects. */
  includeSubprojects?: boolean;
  /** Required when field === "kind". */
  entryKind?: EntryKindFilter;
}

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
  // TECH-3421 — a self-contained box of the user's recent notes (custom
  // renderer, not a slice of the inbox feed). See NotesInboxBox.
  | "notes"
  | "all"
  // TECH-3413 #4 — a fully dynamic box: the user composes the filter
  // (unread / pinned / mentioned / project) instead of picking a fixed kind.
  | "filter"
  // TECH-3422 — the Slack-block: a people/DM/channels rail with live presence
  // and typing. Rendered by a dedicated component, not the entry-list filter.
  | "team"
  // TECH-3557 — Secretary: a focused local batch over existing inbox rows.
  // Rendered by a dedicated component so it can manage its start/selection flow.
  | "secretary";

/** How rows inside a section are grouped under sub-headers. TECH-3541 #2 —
 *  widened to the full classic-inbox set (project / agent / type) so the "All
 *  messages" box can group exactly like the classic inbox. */
export type SectionGroupBy = "none" | "action" | "project" | "agent" | "type";

/** How rows inside a section are ordered (direction). */
export type SectionSort = "newest" | "oldest";

/** TECH-3541 #2 — sort method, mirroring the classic inbox's sort picker.
 *  "last_message" is the default (= plain newest/oldest by activity time). */
export type SectionSortMethod = "last_message" | "last_other" | "last_me";

/** TECH-3541 #2 — quick view filter on a box, mirroring the classic inbox's
 *  "All ▾" view switch. "all" shows everything (the box's own kind still
 *  applies first). */
export type SectionViewFilter =
  | "all"
  | "unread"
  | "mentioned"
  | "issues"
  | "channels"
  | "dms"
  | "muted"
  | "pinned";

/** Semantic colour token for a circle count badge. */
export type SectionBadgeColor = "brand" | "warning" | "success" | "destructive" | "muted";

export interface InboxSectionConfig {
  /** Stable id for React keys + reorder/remove. */
  id: string;
  kind: SectionKind;
  /** Optional custom header; falls back to the catalog label. */
  title?: string;
  /** Required when kind === "project". */
  projectId?: string;
  groupBy?: SectionGroupBy;
  /** TECH-3541 #2 — second-level grouping ("Then by"), like the classic inbox.
   *  Only applies when groupBy is a multi-level mode (not none/action). */
  groupBySecondary?: SectionGroupBy;
  sort?: SectionSort;
  /** TECH-3541 #2 — sort method (last message / last from another / last from
   *  me). Combined with `sort` (direction). Defaults to "last_message". */
  sortMethod?: SectionSortMethod;
  /** TECH-3541 #2 — quick view filter applied on top of the box's kind.
   *  Defaults to "all". Mirrors the classic inbox's view switch. */
  viewFilter?: SectionViewFilter;
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
  /** #6: semantic colour for the circle count badge. Default "brand". */
  badgeColor?: SectionBadgeColor;
  /** #4 (partial): hide muted rows in this box. TECH-3541 (Jesper) — muted /
   *  snoozed rows are now hidden from every view globally, so this per-box flag
   *  is a no-op; kept only so older persisted layouts still parse. */
  excludeMuted?: boolean;
  // --- TECH-3413 #4: composable filter for kind === "filter" (AND of the
  // enabled predicates; projectId is reused as the optional project narrow). ---
  /** Only unread rows. */
  filterUnread?: boolean;
  /** Only pinned rows (same predicate as the "pinned" kind). */
  filterPinned?: boolean;
  /** Only rows that @-mention you. */
  filterMentioned?: boolean;
  // TECH-3413 #5 — filter BUILDER: an explicit, stackable list of conditions
  // (AND of all). Replaces the fixed toggles above; when present it is the
  // source of truth. Empty array = show everything. The legacy booleans are
  // kept only as a migration seed (see `sectionFilters` in section-filter.ts).
  filters?: FilterCondition[];
  /** TECH-3494 — for the "team" (Chat) section: total rows to show across
   *  channels + people + agents. 0 = all. undefined defaults to 10. */
  teamLimit?: number;
  /** TECH-3494 — sort order across all kinds in the "team" (Chat) section.
   *  Starred float to the top regardless. Default "recent". */
  teamSort?: "name" | "recent";
  /** TECH-3494 — keep unread "team" (Chat) rows first. Default true.
   *  Backcompat: persisted `teamGroupBy: "unread"` means true. */
  teamUnreadFirst?: boolean;
  /** TECH-3494 — group the "team" (Chat) section by kind or one flat list.
   *  Default "type". */
  teamGroupBy?: "unread" | "type" | "none";
  /** TECH-3494 — for the "team" (Chat) section: also list workspace agents.
   *  undefined / false = only people + channels. Opt-in via section settings. */
  showAgents?: boolean;
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
  { kind: "notes", label: "Notes" },
  { kind: "waiting", label: "Waiting" },
  { kind: "calm", label: "Done / calm" },
  { kind: "all", label: "All messages" },
  { kind: "filter", label: "Custom filter…" },
  // TECH-3422 — only surfaced in the Add-section menu when the
  // cerebro_inbox_slack_block flag is on (filtered in DynamicInbox).
  { kind: "team", label: "Chat" },
  // TECH-3557 — only surfaced when the cerebro_inbox_secretary flag is on.
  { kind: "secretary", label: "Secretary" },
];

export function sectionLabel(section: InboxSectionConfig): string {
  if (section.title && section.title.trim()) return section.title.trim();
  return SECTION_CATALOG.find((c) => c.kind === section.kind)?.label ?? section.kind;
}

let counter = 0;
/**
 * Globally-unique id. TECH-3502 #1: the old counter-only scheme restarted at
 * `_1` on every page load, so a tab/section minted in a new session collided
 * with a tab/section of the same id already persisted from an earlier session.
 * Two tabs with the same id made `tabs.find(id)` always resolve to the first —
 * tab switching silently broke. The random component (crypto.randomUUID, which
 * is available in the browser and Node ≥18, and unlike Date.now/Math.random is
 * not banned in our test/SSR envs) makes collisions across sessions impossible.
 */
export function makeId(prefix: string): string {
  counter += 1;
  const uuid =
    typeof globalThis !== "undefined" && typeof globalThis.crypto?.randomUUID === "function"
      ? globalThis.crypto.randomUUID().slice(0, 8)
      : "";
  return uuid ? `${prefix}_${uuid}` : `${prefix}_${counter}`;
}

/**
 * TECH-3502 #1 — defensively de-duplicate tab/section ids in a persisted
 * layout. Blobs written before the unique-id fix can carry colliding ids that
 * break tab switching and React keys. Collisions are renamed deterministically
 * (`<id>__2`, `__3`, …) so the same input always yields the same output (no
 * remount churn across renders). Returns the same reference when already clean.
 */
export function dedupeLayoutIds(layout: InboxLayout): InboxLayout {
  const seen = new Set<string>();
  let changed = false;
  const unique = (id: string): string => {
    if (!seen.has(id)) {
      seen.add(id);
      return id;
    }
    changed = true;
    let n = 2;
    let next = `${id}__${n}`;
    while (seen.has(next)) {
      n += 1;
      next = `${id}__${n}`;
    }
    seen.add(next);
    return next;
  };

  const tabs = layout.tabs.map((tab) => {
    const id = unique(tab.id);
    const sections = tab.sections.map((s) => {
      const sid = unique(s.id);
      return sid === s.id ? s : { ...s, id: sid };
    });
    const tabChanged = id !== tab.id || sections.some((s, i) => s !== tab.sections[i]);
    return tabChanged ? { ...tab, id, sections } : tab;
  });

  if (!changed) return layout;
  const activeTabId = tabs.some((t) => t.id === layout.activeTabId)
    ? layout.activeTabId
    : tabs[0]?.id;
  return { ...layout, tabs, activeTabId };
}

function section(kind: SectionKind, extra: Partial<InboxSectionConfig> = {}): InboxSectionConfig {
  return { id: makeId(kind), kind, groupBy: "none", sort: "newest", showPriority: true, ...extra };
}

/**
 * Initial dynamic inbox layout for users without a saved layout.
 * TECH-3541 #2 — the default is a single "All messages" box grouped by action,
 * so switching to the dynamic inbox is effectively the same as the classic
 * inbox (which also defaults to action grouping). Users then add boxes / tabs
 * on top of this baseline.
 */
export function operatorPreset(): InboxLayout {
  const tabId = makeId("tab");
  return {
    version: INBOX_LAYOUT_VERSION,
    activeTabId: tabId,
    tabs: [
      {
        id: tabId,
        title: "Inbox",
        sections: [section("secretary"), section("all", { groupBy: "action" })],
      },
    ],
  };
}

export const DEFAULT_INBOX_LAYOUT = operatorPreset;

export interface UserInboxPreset {
  id: string;
  label: string;
  layout: InboxLayout;
}

export const USER_INBOX_PRESETS_VERSION = 1 as const;

export interface UserInboxPresetsBlob {
  version: typeof USER_INBOX_PRESETS_VERSION;
  presets: UserInboxPreset[];
}

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

export function cloneInboxLayout(layout: InboxLayout): InboxLayout {
  return {
    ...layout,
    tabs: layout.tabs.map((tab) => ({
      ...tab,
      sections: tab.sections.map((section) => ({ ...section })),
    })),
  };
}

function isValidUserPreset(value: unknown): value is UserInboxPreset {
  if (!value || typeof value !== "object") return false;
  const v = value as Partial<UserInboxPreset>;
  return typeof v.id === "string" && typeof v.label === "string" && isValidLayout(v.layout);
}

export function readUserInboxPresets(value: unknown): UserInboxPreset[] {
  if (!value || typeof value !== "object") return [];
  const v = value as Partial<UserInboxPresetsBlob>;
  if (v.version !== USER_INBOX_PRESETS_VERSION || !Array.isArray(v.presets)) return [];
  return v.presets.filter(isValidUserPreset).map((preset) => ({
    id: preset.id,
    label: preset.label.trim(),
    layout: cloneInboxLayout(preset.layout),
  }));
}

export function makeUserInboxPresetsBlob(presets: UserInboxPreset[]): UserInboxPresetsBlob {
  return {
    version: USER_INBOX_PRESETS_VERSION,
    presets: presets
      .filter((preset) => preset.label.trim() && isValidLayout(preset.layout))
      .map((preset) => ({
        id: preset.id,
        label: preset.label.trim(),
        layout: cloneInboxLayout(preset.layout),
      })),
  };
}

export function upsertUserInboxPreset(
  existing: UserInboxPreset[],
  preset: UserInboxPreset,
): UserInboxPreset[] {
  const clean: UserInboxPreset = {
    id: preset.id,
    label: preset.label.trim(),
    layout: cloneInboxLayout(preset.layout),
  };
  const next = existing.filter((p) => p.id !== clean.id && p.label.trim() !== clean.label);
  return [...next, clean];
}

export function deleteUserInboxPreset(existing: UserInboxPreset[], presetId: string): UserInboxPreset[] {
  return existing.filter((preset) => preset.id !== presetId);
}
