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
}

export interface DownloadAssets {
  macArm64Dmg?: string;
  macArm64Zip?: string;
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

export function parseReleaseAssets(raw: GitHubAsset[]): DownloadAssets {
  const out: DownloadAssets = {};
  for (const asset of raw) {
    const name = asset.name;
    // Skip auxiliary files that share the release (update manifests,
    // blockmaps, checksums). CLI tarballs and other non-desktop
    // artifacts are excluded automatically because they don't match
    // DESKTOP_ARTIFACT_RE below.
    if (name.endsWith(".blockmap") || name.endsWith(".yml")) continue;
    if (name.startsWith("checksums")) continue;

    const match = DESKTOP_ARTIFACT_RE.exec(name);
    if (!match) continue;
    const platform = match[1];
    const arch = match[2];
    const ext = match[3];
    if (!platform || !arch || !ext) continue;
    const archLower = arch.toLowerCase();
    const extLower = ext.toLowerCase();
    const url = asset.browser_download_url;

    if (platform === "mac") {
      if (archLower !== "arm64") continue; // we only ship arm64 today
      if (extLower === "dmg") out.macArm64Dmg = url;
      else if (extLower === "zip") out.macArm64Zip = url;
    } else if (platform === "windows") {
      if (extLower !== "exe") continue;
      if (archLower === "x64") out.winX64Exe = url;
      else if (archLower === "arm64") out.winArm64Exe = url;
    } else if (platform === "linux") {
      const normalized = normalizeLinuxArch(arch);
      if (!normalized) continue;
      const e = extLower;
      if (normalized === "amd64") {
        if (e === "appimage") out.linuxAmd64AppImage = url;
        else if (e === "deb") out.linuxAmd64Deb = url;
        else if (e === "rpm") out.linuxAmd64Rpm = url;
      } else {
        if (e === "appimage") out.linuxArm64AppImage = url;
        else if (e === "deb") out.linuxArm64Deb = url;
        else if (e === "rpm") out.linuxArm64Rpm = url;
      }
    }
  }
  return out;
}

/** Whether any desktop asset was parsed out. Used for UI degradation. */
export function hasAnyAsset(assets: DownloadAssets): boolean {
  return Object.values(assets).some((v) => typeof v === "string");
}
