import type { DetectResult } from "./os-detect";
import type { DownloadAssets, DownloadKey } from "./parse-release-assets";

/**
 * The desktop builds the download buttons can offer, keyed like
 * `DownloadAssets`. The key also travels in `/download?started=<key>`,
 * so the page that follows a homepage click knows which file it is
 * about.
 */

export type DesktopOS = "mac" | "windows" | "linux";

/** Rows of the platform list, one per OS and architecture. */
export type PlatformKey = "macArm64" | "macX64" | "winX64" | "winArm64" | "linuxX64" | "linuxArm64";

/** The installers a platform gets by default. */
export type PrimaryDownloadKey = Extract<
  DownloadKey,
  "macArm64Dmg" | "macX64Dmg" | "winX64Exe" | "winArm64Exe" | "linuxAmd64AppImage" | "linuxArm64AppImage"
>;

export const DOWNLOAD_KEYS = [
  "macArm64Dmg",
  "macArm64Zip",
  "macX64Dmg",
  "macX64Zip",
  "winX64Exe",
  "winArm64Exe",
  "linuxAmd64AppImage",
  "linuxAmd64Deb",
  "linuxAmd64Rpm",
  "linuxArm64AppImage",
  "linuxArm64Deb",
  "linuxArm64Rpm",
] as const satisfies readonly DownloadKey[];

export function isDownloadKey(value: unknown): value is DownloadKey {
  return typeof value === "string" && (DOWNLOAD_KEYS as readonly string[]).includes(value);
}

export function osOf(key: DownloadKey): DesktopOS {
  return key.startsWith("mac") ? "mac" : key.startsWith("win") ? "windows" : "linux";
}

export function platformOf(key: DownloadKey): PlatformKey {
  if (key.startsWith("macArm64")) return "macArm64";
  if (key.startsWith("macX64")) return "macX64";
  if (key === "winArm64Exe") return "winArm64";
  if (key === "winX64Exe") return "winX64";
  return key.startsWith("linuxArm64") ? "linuxArm64" : "linuxX64";
}

/** The installer a detected platform gets by default, or null for phones and unknown systems. */
export function primaryKey(detected: DetectResult): PrimaryDownloadKey | null {
  const arm = detected.arch === "arm64";
  switch (detected.os) {
    case "mac":
      return detected.arch === "x64" ? "macX64Dmg" : "macArm64Dmg";
    case "windows":
      return arm ? "winArm64Exe" : "winX64Exe";
    case "linux":
      return arm ? "linuxArm64AppImage" : "linuxAmd64AppImage";
    default:
      return null;
  }
}

/** A Mac's other-architecture build, offered when detection had to guess. */
export function alternateKey(key: DownloadKey): "macArm64Dmg" | "macX64Dmg" | null {
  if (key === "macArm64Dmg") return "macX64Dmg";
  if (key === "macX64Dmg") return "macArm64Dmg";
  return null;
}

/** The primary download for a platform, when the release has it. */
export function resolvePrimary(detected: DetectResult, assets: DownloadAssets) {
  const key = primaryKey(detected);
  const href = key ? assets[key] : undefined;
  return key && href ? { key, href } : null;
}

export function fileName(href: string): string {
  return decodeURIComponent(href.split("/").pop() ?? href);
}

/** Decimal megabytes, the way download sizes usually read ("248 MB"). */
export function formatSize(bytes: number): string {
  return `${Math.round(bytes / 1_000_000)} MB`;
}

// A homepage click navigates to `/download?started=<key>` and leaves this
// flag so that page starts the file. Reloading or sharing the link shows the
// same page without downloading again.
const PENDING_KEY = "multica-download-pending";

export function markPendingDownload(key: DownloadKey) {
  try {
    sessionStorage.setItem(PENDING_KEY, key);
  } catch {
    // Storage can be unavailable (private modes); the page then offers its retry link.
  }
}

export function takePendingDownload(key: DownloadKey): boolean {
  try {
    const pending = sessionStorage.getItem(PENDING_KEY);
    sessionStorage.removeItem(PENDING_KEY);
    return pending === key;
  } catch {
    return false;
  }
}
