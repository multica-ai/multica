/**
 * Parses the GitHub Releases API asset array into a structured
 * download asset map. Skips auxiliary files (blockmaps, update
 * manifests, checksums) and the CLI tarballs — only desktop
 * installer artifacts are relevant on the /download page.
 *
 * Desktop artifact naming (see apps/desktop/electron-builder.yml):
 *   multica-desktop-{version}-mac-{arch}.{dmg|zip}
 *   multica-desktop-{version}-windows-{arch}.exe
 *   multica-desktop-{version}-linux-{arch}.{AppImage|deb|rpm}
 *
 * Linux arch appears as amd64 / x86_64 / arm64 / aarch64 depending
 * on the format; we normalize to amd64 and arm64.
 */

export interface GitHubAsset {
  name: string;
  browser_download_url: string;
  size?: number;
}

export interface DownloadAssets {
  macArm64Dmg?: string;
  macArm64Zip?: string;
  macX64Dmg?: string;
  macX64Zip?: string;
  winX64Exe?: string;
  winArm64Exe?: string;
  linuxAmd64AppImage?: string;
  linuxAmd64Deb?: string;
  linuxAmd64Rpm?: string;
  linuxArm64AppImage?: string;
  linuxArm64Deb?: string;
  linuxArm64Rpm?: string;
}

const DESKTOP_ARTIFACT_RE =
  /^multica-desktop-[^-]+-(mac|windows|linux)-([a-z0-9_]+)\.(dmg|zip|exe|AppImage|deb|rpm)$/i;

function normalizeLinuxArch(arch: string): "amd64" | "arm64" | null {
  const a = arch.toLowerCase();
  if (a === "amd64" || a === "x86_64") return "amd64";
  if (a === "arm64" || a === "aarch64") return "arm64";
  return null;
}

export type DownloadKey = keyof DownloadAssets;

/** Byte size per desktop asset, for the download page's file details. */
export type DownloadSizes = Partial<Record<DownloadKey, number>>;

const MAC_KEYS: Record<string, Partial<Record<string, DownloadKey>>> = {
  arm64: { dmg: "macArm64Dmg", zip: "macArm64Zip" },
  x64: { dmg: "macX64Dmg", zip: "macX64Zip" },
};
const WINDOWS_KEYS: Partial<Record<string, DownloadKey>> = { x64: "winX64Exe", arm64: "winArm64Exe" };
const LINUX_KEYS: Record<"amd64" | "arm64", Partial<Record<string, DownloadKey>>> = {
  amd64: { appimage: "linuxAmd64AppImage", deb: "linuxAmd64Deb", rpm: "linuxAmd64Rpm" },
  arm64: { appimage: "linuxArm64AppImage", deb: "linuxArm64Deb", rpm: "linuxArm64Rpm" },
};

/** Which desktop artifact a release file is, if any. */
function assetKey(name: string): DownloadKey | undefined {
  // Skip auxiliary files that share the release (update manifests,
  // blockmaps, checksums). CLI tarballs and other non-desktop
  // artifacts are excluded automatically because they don't match
  // DESKTOP_ARTIFACT_RE below.
  if (name.endsWith(".blockmap") || name.endsWith(".yml")) return undefined;
  if (name.startsWith("checksums")) return undefined;

  const match = DESKTOP_ARTIFACT_RE.exec(name);
  if (!match) return undefined;
  const [, platform, arch, ext] = match;
  if (!platform || !arch || !ext) return undefined;
  const archLower = arch.toLowerCase();
  const extLower = ext.toLowerCase();

  if (platform === "mac") return MAC_KEYS[archLower]?.[extLower];
  if (platform === "windows") return extLower === "exe" ? WINDOWS_KEYS[archLower] : undefined;
  const normalized = normalizeLinuxArch(arch);
  return normalized ? LINUX_KEYS[normalized][extLower] : undefined;
}

export function parseReleaseAssets(raw: GitHubAsset[]): DownloadAssets {
  const out: DownloadAssets = {};
  for (const asset of raw) {
    const key = assetKey(asset.name);
    if (key) out[key] = asset.browser_download_url;
  }
  return out;
}

export function parseReleaseAssetSizes(raw: GitHubAsset[]): DownloadSizes {
  const out: DownloadSizes = {};
  for (const asset of raw) {
    const key = assetKey(asset.name);
    if (key && typeof asset.size === "number") out[key] = asset.size;
  }
  return out;
}

/** Whether any desktop asset was parsed out. Used for UI degradation. */
export function hasAnyAsset(assets: DownloadAssets): boolean {
  return Object.values(assets).some((v) => typeof v === "string");
}

/**
 * The twelve desktop artifacts a finished release carries: four Mac,
 * two Windows, six Linux. Listed explicitly rather than counted, so
 * adding an optional key to `DownloadAssets` later (a universal Mac
 * build, say) can't silently redefine what "complete" means.
 */
const REQUIRED_ASSET_KEYS: (keyof DownloadAssets)[] = [
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
];

/**
 * Whether every platform/arch/format the /download page offers resolved
 * to a URL. A release that fails this is either mid-flight (CI has
 * uploaded Linux + Windows but the manually notarized Mac builds haven't
 * landed) or permanently broken (a packaging job failed and was never
 * re-run) — both render dead buttons, so both are worth stepping back
 * from. See `pickRelease` in `github-release.ts`.
 */
export function hasCompleteAssetSet(assets: DownloadAssets): boolean {
  return REQUIRED_ASSET_KEYS.every((key) => typeof assets[key] === "string");
}
