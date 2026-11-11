/**
 * OS + architecture detection for the desktop download buttons.
 *
 * The server reads the request's User-Agent (and the low-entropy
 * `Sec-CH-UA-Platform` / `Sec-CH-UA-Mobile` hints Chromium always sends)
 * so the first paint already names the visitor's platform. In the
 * browser, `navigator.userAgentData.getHighEntropyValues` can then
 * confirm the CPU architecture on Chromium.
 *
 * Known limitation: Safari on macOS always reports `Intel Mac OS X`
 * in the UA string even on Apple Silicon, and Safari does not
 * implement userAgentData. Detection therefore returns `arm64` as the
 * best default for any Mac, with `archConfident: false` so the UI can
 * point Intel users to the Intel build.
 */

export type OSName = "mac" | "windows" | "linux" | "mobile" | "unknown";
export type Arch = "arm64" | "x64" | "unknown";

export interface DetectResult {
  os: OSName;
  arch: Arch;
  /** True when arch came from userAgentData high-entropy values
   *  (i.e. we can trust the Intel vs arm distinction). False when
   *  we defaulted — UI should offer the other architecture. */
  archConfident: boolean;
}

const UNKNOWN_PLATFORM: DetectResult = { os: "unknown", arch: "unknown", archConfident: false };

interface UADataRecord {
  platform: string;
  architecture: string;
  mobile?: boolean;
}

interface UserAgentDataLike {
  mobile?: boolean;
  getHighEntropyValues?: (hints: string[]) => Promise<UADataRecord>;
}

function normalizePlatform(raw: string): OSName {
  const p = raw.toLowerCase();
  if (p.includes("android") || p.includes("ios")) return "mobile";
  if (p.includes("mac") || p === "darwin") return "mac";
  if (p.includes("win")) return "windows";
  if (p.includes("linux") || p.includes("chrome os")) return "linux";
  return "unknown";
}

function normalizeArch(raw: string): Arch {
  const a = raw.toLowerCase();
  if (a === "arm" || a === "arm64" || a === "aarch64") return "arm64";
  if (a === "x86" || a === "x86_64" || a === "amd64" || a === "x64") return "x64";
  return "unknown";
}

/**
 * Platform from a User-Agent string, optionally sharpened by the
 * `Sec-CH-UA-Platform` and `Sec-CH-UA-Mobile` request hints.
 */
export function detectFromUserAgent(
  ua: string,
  hints: { platform?: string | null; mobile?: string | null } = {},
): DetectResult {
  const hinted = hints.platform ? normalizePlatform(hints.platform.replace(/"/g, "")) : "unknown";
  const mobile =
    hints.mobile === "?1" || hinted === "mobile" || /Android|iPhone|iPad|iPod|Mobile/i.test(ua);
  if (mobile) return { os: "mobile", arch: "unknown", archConfident: false };

  const os: OSName =
    hinted !== "unknown"
      ? hinted
      : /Mac OS X|Macintosh/i.test(ua)
        ? "mac"
        : /Windows/i.test(ua)
          ? "windows"
          : /Linux|X11|CrOS/i.test(ua)
            ? "linux"
            : "unknown";

  let arch: Arch = "unknown";
  if (os === "mac") {
    // Best default. Intel Mac users get the Intel build offered beside it.
    arch = "arm64";
  } else if (/arm|aarch/i.test(ua)) {
    arch = "arm64";
  } else if (os !== "unknown") {
    arch = "x64";
  }
  return { os, arch, archConfident: false };
}

export async function detectOS(): Promise<DetectResult> {
  if (typeof navigator === "undefined") return UNKNOWN_PLATFORM;

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
      if (data.mobile ?? uaData.mobile) return { os: "mobile", arch: "unknown", archConfident: false };
      const os = normalizePlatform(data.platform);
      const arch = normalizeArch(data.architecture);
      return { os, arch, archConfident: arch !== "unknown" };
    } catch {
      // Some browsers expose the API but reject high-entropy requests.
    }
  }

  // iPadOS Safari presents itself as a Mac; touch support gives it away.
  if (/Macintosh/i.test(navigator.userAgent) && navigator.maxTouchPoints > 1) {
    return { os: "mobile", arch: "unknown", archConfident: false };
  }
  return detectFromUserAgent(navigator.userAgent, { platform: navigator.platform });
}
