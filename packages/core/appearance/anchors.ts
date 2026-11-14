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
  "panel-section":
    "A labelled group of rows, headed by an `h3`, in the sidebar or a detail page's property panel.",
  page: "The rounded canvas that holds the current page.",
  "page-header": "The bar at the top of a page, with its title and actions.",
  "window-toolbar": "Desktop only: the toolbar above the rail with history buttons.",
  "tab-bar": "Desktop only: the tab strip above the page.",
  "bottom-bar": "The status bar under the page, on windows wide enough to show it.",
  "issue-group-header": "A status group header in issue lists; `data-status` is the status key.",
  "issue-row":
    "One issue in a list; `data-status`, `data-priority`, `data-assignee-type` (member, agent, squad or none), `data-parity` (even or odd within its status group) and `data-selected` (true or false).",
  "board-column": "One column on a board.",
  "board-card":
    "One issue card on a board; `data-status`, `data-priority` and `data-assignee-type` as on `issue-row`.",
  "status-icon":
    "The status glyph (an svg). A custom status adds `data-color`, its hex colour, and `data-palette`, its colour slot.",
  "priority-icon": "The priority glyph (an svg).",
  "label-chip":
    "A label; `data-color` is its hex colour and `data-palette` its colour slot. The chip paints with `--label-color` (the slot's colour when the theme declares one) and `--label-foreground`.",
  content:
    "Rich text: issue descriptions, comments, chat messages and documents, in both reading and editing; `data-density` is document or compact when reading.",
  comment: "A top-level comment card in an issue timeline; `data-author-type` is member, agent or system.",
  "comment-reply": "A reply inside a comment card; `data-author-type` as on `comment`.",
  "comment-composer": "The box for writing a new comment.",
  "chat-window": "The chat panel.",
  "chat-input": "The message box in chat.",
  "chat-message": "One message in a chat; `data-role` is user or assistant.",
  "chat-bubble": "The bubble around a user message. Assistant replies have none.",
  "inbox-item":
    "One notification in the inbox list; `data-read` is true or false, `data-selected` is true on the one open in the detail pane.",
  "unread-dot": "The unread marker on an inbox item.",
  toaster:
    "The toast region. Each toast is an `li`; `data-type` is success, error, info, warning, loading or default, and a plain toast may have no `data-type` at all.",
  "settings-page": "The settings page.",
  "issue-detail":
    "An open issue, on its page or in the peek; `data-status`, `data-priority`, and `data-view` (page or peek).",
  "issue-title": "The issue's title above its description, reading and editing.",
  "issue-properties": "The properties sidebar next to an issue.",
  "issue-activity": "The activity and comments under an issue.",
  "object-page":
    "The page of one project, agent, squad, runtime, member, autopilot or skill; `data-type` names which.",
  "command-palette": "The search and command palette.",
  "palette-item":
    "A row in the palette; `data-type` is command, page, workspace, issue, project, member, agent, squad, object or option (a choice inside a palette page). The highlighted row has `data-selected`.",
  avatar: "An avatar; `data-type` is member, agent, squad or system.",
  "empty-state": "A placeholder for something with nothing in it yet; `data-variant` is page or section.",
  "editor-toolbar": "The formatting toolbar over selected text; `data-mode` is format or link.",
  "editor-menu": "A menu floating over the editor while typing; `data-kind` is slash or mention.",
  tab: "Desktop only: one tab in `tab-bar`; `data-active` and `data-pinned` are true or false.",
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
