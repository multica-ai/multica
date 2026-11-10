// Desktop's themes folder, through the preload `desktopAPI`. Undefined on
// web and on Desktop builds that predate the folder.

export interface ThemeFolderFile {
  path: string;
  data: Uint8Array;
}

export interface ThemeFolderSnapshot {
  path: string;
  entries: { folder: string; files?: ThemeFolderFile[]; error?: string }[];
}

export type ThemeFolderWriteResult =
  | { ok: true }
  | { ok: false; reason: "exists" | "invalid" | "error"; error?: string };

export interface ThemeFolderAPI {
  readThemeFolder: () => Promise<ThemeFolderSnapshot>;
  onThemeFolderChange: (callback: () => void) => () => void;
  openThemeFolder: (name?: string) => Promise<{ ok: boolean; error?: string }>;
  writeThemeFolder: (name: string, files: ThemeFolderFile[]) => Promise<ThemeFolderWriteResult>;
}

export function themeFolderAPI(): ThemeFolderAPI | undefined {
  if (typeof window === "undefined") return undefined;
  const api = (window as unknown as { desktopAPI?: Partial<ThemeFolderAPI> }).desktopAPI;
  return api?.readThemeFolder && api.onThemeFolderChange && api.openThemeFolder && api.writeThemeFolder
    ? (api as ThemeFolderAPI)
    : undefined;
}
