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
  page: "The rounded canvas that holds the current page.",
  "window-toolbar": "Desktop only: the toolbar above the rail with history buttons.",
  "tab-bar": "Desktop only: the tab strip above the page.",
  "issue-group-header": "A status group header in issue lists; `data-status` is the status key.",
  "issue-row": "One issue in a list.",
  "board-column": "One column on a board.",
  "board-card": "One issue card on a board.",
  "status-icon": "The status glyph (an svg).",
  "priority-icon": "The priority glyph (an svg).",
  "label-chip": "A label.",
  content:
    "Rich text: issue descriptions, comments, chat messages and documents, in both reading and editing; `data-density` is document or compact when reading.",
  comment: "A top-level comment card in an issue timeline.",
  "comment-reply": "A reply inside a comment card.",
  "comment-composer": "The box for writing a new comment.",
  "chat-window": "The chat panel.",
  "chat-input": "The message box in chat.",
  "inbox-item": "One notification in the inbox list.",
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
