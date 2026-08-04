// FIR-4350 — the Chat page's own display options: how many rows per box, how
// they group, how they sort, and whether search shows by default. These are the
// same controls the Inbox's Chat box has; the Chat page persists its own copy so
// the two surfaces don't fight over one layout. Stored per user in the
// server-synced user.preferences blob (like cerebro_chat_placement) — no table,
// no endpoint.
//
// Pure module: read/parse here, wired to preferences by use-chat-page-settings.

/** Row sort across all kinds. Mirrors SlackBlock's TeamSort. */
export type ChatPageSort = "name" | "recent";
/** Group by kind, or one flat list. Mirrors SlackBlock's TeamGroupBy. */
export type ChatPageGroupBy = "type" | "none";

export interface ChatPageSettings {
  /** Rows per box (per kind when grouped, shared total when flat). 0 = all. */
  limit: number;
  sort: ChatPageSort;
  unreadFirst: boolean;
  groupBy: ChatPageGroupBy;
  searchDefaultOpen: boolean;
}

export const CHAT_PAGE_SETTINGS_KEY = "cerebro_chat_page_settings";

export const DEFAULT_CHAT_PAGE_SETTINGS: ChatPageSettings = {
  limit: 10,
  sort: "recent",
  unreadFirst: true,
  groupBy: "type",
  searchDefaultOpen: false,
};

/**
 * Read settings from an untrusted preferences blob. Every missing or malformed
 * field falls back to {@link DEFAULT_CHAT_PAGE_SETTINGS}, per the API Response
 * Compatibility rule in CLAUDE.md.
 */
export function readChatPageSettings(value: unknown): ChatPageSettings {
  if (!value || typeof value !== "object") return DEFAULT_CHAT_PAGE_SETTINGS;
  const raw = value as Record<string, unknown>;
  const d = DEFAULT_CHAT_PAGE_SETTINGS;
  return {
    limit:
      typeof raw.limit === "number" && raw.limit >= 0 ? raw.limit : d.limit,
    sort: raw.sort === "name" || raw.sort === "recent" ? raw.sort : d.sort,
    unreadFirst:
      typeof raw.unreadFirst === "boolean" ? raw.unreadFirst : d.unreadFirst,
    groupBy:
      raw.groupBy === "type" || raw.groupBy === "none" ? raw.groupBy : d.groupBy,
    searchDefaultOpen:
      typeof raw.searchDefaultOpen === "boolean"
        ? raw.searchDefaultOpen
        : d.searchDefaultOpen,
  };
}
