/** Known apps the desktop shell may open a local file with. v1 is an
 *  allowlist — the renderer cannot name an arbitrary executable. */
export type LocalFileOpenerId =
  | "cursor"
  | "vscode"
  | "vscodium"
  | "zed"
  | "sublime"
  | "jetbrains-idea"
  | "jetbrains-idea-ce"
  | "jetbrains-webstorm"
  | "jetbrains-pycharm"
  | "jetbrains-pycharm-ce"
  | "jetbrains-goland"
  | "jetbrains-phpstorm"
  | "jetbrains-clion"
  | "jetbrains-rider"
  | "jetbrains-rustrover"
  | "file-manager";

export type LocalFileOpenerKind = "editor" | "file_manager";

export type LocalFileHostPlatform = "darwin" | "win32" | "linux";

export interface LocalFileOpener {
  id: LocalFileOpenerId;
  kind: LocalFileOpenerKind;
  /** English app name for editors ("Cursor"). File manager is localized in the renderer. */
  name: string;
}

export interface ListLocalFileOpenersResult {
  openers: LocalFileOpener[];
  platform: LocalFileHostPlatform;
}

export type ResolveLocalFileReason =
  | "not_absolute"
  | "not_found"
  | "unsafe"
  | "error";

export type ResolveLocalFileResult =
  | { ok: true; path: string }
  | { ok: false; reason: ResolveLocalFileReason; error?: string };

export type OpenLocalFileReason =
  | ResolveLocalFileReason
  | "opener_unavailable"
  | "launch_failed";

export type OpenLocalFileResult =
  | { ok: true }
  | { ok: false; reason: OpenLocalFileReason; error?: string };

export interface OpenLocalFileRequest {
  path: string;
  openerId: string;
}

export const LOCAL_FILE_OPENER_IDS: readonly LocalFileOpenerId[] = [
  "cursor",
  "vscode",
  "vscodium",
  "zed",
  "sublime",
  "jetbrains-idea",
  "jetbrains-idea-ce",
  "jetbrains-webstorm",
  "jetbrains-pycharm",
  "jetbrains-pycharm-ce",
  "jetbrains-goland",
  "jetbrains-phpstorm",
  "jetbrains-clion",
  "jetbrains-rider",
  "jetbrains-rustrover",
  "file-manager",
] as const;

export const DEFAULT_OPENER_FALLBACK_IDS: readonly LocalFileOpenerId[] = [
  "cursor",
  "vscode",
  "file-manager",
];

export function isLocalFileOpenerId(value: string): value is LocalFileOpenerId {
  return (LOCAL_FILE_OPENER_IDS as readonly string[]).includes(value);
}

export function hostPlatformFromNode(
  platform: NodeJS.Platform,
): LocalFileHostPlatform {
  if (platform === "darwin" || platform === "win32") return platform;
  return "linux";
}
