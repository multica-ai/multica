/**
 * The themes folder in Desktop's user data: one theme per subfolder (or
 * .zip), read by main and checked in the renderer by the same code as an
 * import. Main watches it and tells every window when something changes.
 */

export const THEME_FOLDER_READ_CHANNEL = "theme-folder:read";
export const THEME_FOLDER_CHANGED_CHANNEL = "theme-folder:changed";
export const THEME_FOLDER_OPEN_CHANNEL = "theme-folder:open";
export const THEME_FOLDER_WRITE_CHANNEL = "theme-folder:write";

export interface ThemeFolderFile {
  /** Relative to the theme's folder, with `/`. */
  path: string;
  data: Uint8Array;
}

export interface ThemeFolderEntrySnapshot {
  /** The subfolder or .zip name. */
  folder: string;
  files?: ThemeFolderFile[];
  /** Why the entry could not be read; the renderer shows it. */
  error?: string;
}

export interface ThemeFolderSnapshot {
  /** Absolute path of the themes folder, for display. */
  path: string;
  entries: ThemeFolderEntrySnapshot[];
}

export type ThemeFolderWriteResult =
  | { ok: true }
  | { ok: false; reason: "exists" | "invalid" | "error"; error?: string };

/** A folder name main will create: the theme id rules. */
export const THEME_FOLDER_NAME = /^[a-z0-9][a-z0-9-]{0,39}$/;
