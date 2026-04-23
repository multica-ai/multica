/**
 * Client-side OS + architecture detection for the /download page.
 *
 * Prefers the modern `navigator.userAgentData.getHighEntropyValues`
 * API (Chromium), falling back to the UA string.
 *
 * Known limitation: Safari on macOS always reports `Intel Mac OS X`
 * in the UA string even on Apple Silicon, and Safari does not
 * implement userAgentData. This function therefore returns `arm64`
 * as the best default for any Mac — UI surfaces a small "On Intel
 * Mac? Use CLI." hint to cover the Intel minority.
 */

export type OSName = "mac" | "windows" | "linux" | "unknown";
export type Arch = "arm64" | "x64" | "unknown";

export interface DetectResult {
  os: OSName;
  arch: Arch;
  /** True when arch came from userAgentData high-entropy values
   *  (i.e. we can trust the Intel vs arm distinction). False when
   *  we defaulted — UI should show the Intel Mac disclaimer. */
  archConfident: boolean;
}

interface UADataRecord {
  platform: string;
  architecture: string;
}

interface UserAgentDataLike {
  getHighEntropyValues?: (hints: string[]) => Promise<UADataRecord>;
}

function normalizePlatform(raw: string): OSName {
  const p = raw.toLowerCase();
  if (p.includes("mac") || p === "darwin") return "mac";
  if (p.includes("win")) return "windows";
  if (p.includes("linux")) return "linux";
  return "unknown";
}

function normalizeArch(raw: string): Arch {
  const a = raw.toLowerCase();
  if (a === "arm" || a === "arm64" || a === "aarch64") return "arm64";
  if (a === "x86" || a === "x86_64" || a === "amd64" || a === "x64") return "x64";
  return "unknown";
}

export async function detectOS(): Promise<DetectResult> {
  if (typeof navigator === "undefined") {
    return { os: "unknown", arch: "unknown", archConfident: false };
  }

  // Modern Chromium: userAgentData with high-entropy values gives
  // both the platform name and CPU architecture unambiguously.
  const uaData = (navigator as unknown as { userAgentData?: UserAgentDataLike })
    .userAgentData;
  if (uaData?.getHighEntropyValues) {
    try {
      const data = await uaData.getHighEntropyValues([
        "platform",
        "architecture",
      ]);
      const os = normalizePlatform(data.platform);
      const arch = normalizeArch(data.architecture);
      return { os, arch, archConfident: arch !== "unknown" };
    } catch {
      // Some browsers expose the API but reject high-entropy requests.
    }
  }

  // Fallback: UA + navigator.platform. Safari on Mac lands here and
  // cannot distinguish Apple Silicon from Intel.
  const ua = navigator.userAgent;
  const platform = navigator.platform || "";

  const os: OSName = /Mac|iPhone|iPad|iPod/i.test(platform) || /Mac OS X/i.test(ua)
    ? "mac"
    : /Win/i.test(platform) || /Windows/i.test(ua)
      ? "windows"
      : /Linux/i.test(platform) || /Linux/i.test(ua)
        ? "linux"
        : "unknown";

  let arch: Arch = "unknown";
  if (os === "mac") {
    // Best default. Real Intel Mac users will see the disclaimer.
    arch = "arm64";
  } else if (/arm|aarch/i.test(ua)) {
    arch = "arm64";
  } else if (os !== "unknown") {
    arch = "x64";
  }

  return { os, arch, archConfident: false };
}
