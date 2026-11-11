/**
 * Selectors a theme's CSS may rely on. Class names and DOM structure are not
 * part of this list and change without notice.
 *
 * `data-mc` marks product surfaces. Removing or renaming one breaks themes:
 * keep it until the next THEME_API_VERSION. A test checks that every name
 * here is still rendered somewhere and that no unlisted `data-mc` appears.
 */
export const THEME_ANCHORS = {
  "app-rail": "The icon rail on the far left.",
  sidebar: "The navigation panel next to the rail; `data-state` is expanded or collapsed.",
  "nav-item": "A row in the sidebar; `data-active` is true on the current one.",
  page: "The rounded canvas that holds the current page.",
  "window-toolbar": "Desktop only: the toolbar above the rail with history buttons.",
  "tab-bar": "Desktop only: the tab strip above the page.",
  "bottom-bar": "The status bar under the page, on windows wide enough to show it.",
  "issue-group-header": "A status group header in issue lists; `data-status` is the status key.",
  "issue-row":
    "One issue in a list; `data-status`, `data-priority` and `data-assignee-type` (member, agent, squad or none).",
  "board-column": "One column on a board.",
  "board-card": "One issue card on a board; same attributes as `issue-row`.",
  "status-icon": "The status glyph (an svg).",
  "priority-icon": "The priority glyph (an svg).",
  "label-chip": "A label; `data-color` is its hex colour.",
  content:
    "Rich text: issue descriptions, comments, chat messages and documents, in both reading and editing; `data-density` is document or compact when reading.",
  comment: "A top-level comment card in an issue timeline; `data-author-type` is member, agent or system.",
  "comment-reply": "A reply inside a comment card; `data-author-type` as on `comment`.",
  "comment-composer": "The box for writing a new comment.",
  "chat-window": "The chat panel.",
  "chat-input": "The message box in chat.",
  "chat-message": "One message in a chat; `data-role` is user or assistant.",
  "inbox-item": "One notification in the inbox list; `data-read` is true or false.",
  "settings-page": "The settings page.",
} as const;

export type ThemeAnchor = keyof typeof THEME_ANCHORS;

/**
 * `data-slot` values of UI primitives that are public too. Buttons also carry
 * `data-variant` and `data-size`.
 */
export const THEME_PUBLIC_SLOTS = [
  "button",
  "input",
  "textarea",
  "switch",
  "checkbox",
  "select-trigger",
  "select-content",
  "select-item",
  "dropdown-menu-content",
  "dropdown-menu-item",
  "popover-content",
  "tooltip-content",
  "dialog-overlay",
  "dialog-content",
  "alert-dialog-content",
  "sheet-content",
  "tabs-list",
  "tabs-trigger",
  "card",
  "kbd",
  "separator",
] as const;
