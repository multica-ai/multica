/**
 * Hotkey scope constants.
 *
 * A scope limits which hotkeys are active. Components activate/deactivate
 * scopes via the scope store; the `useHotkey` wrapper only fires when the
 * command's scope is currently active.
 */

export const SCOPES = {
  /** Always active — top-level navigation, command palette, etc. */
  global: "global",
  /** Issue list / board view. */
  issueList: "issue-list",
  /** Single-issue detail pane. */
  issueDetail: "issue-detail",
  /** Command-palette / quick-switcher overlay. */
  picker: "picker",
  /** Any modal dialog. */
  modal: "modal",
  /** Rich-text / markdown editor has focus. */
  editor: "editor",
  /** AI chat panel. */
  chat: "chat",
  /** Generic form with text inputs. */
  form: "form",
} as const;

export type HotkeyScope = (typeof SCOPES)[keyof typeof SCOPES];
