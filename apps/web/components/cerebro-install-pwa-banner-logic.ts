// FIR-2042: pure logic for the mobile-web "Install Multica" banner. Kept
// separate from the React component so the platform detection and visibility
// rules can be unit-tested without jsdom navigator stubbing.

export type MobilePlatform = "ios" | "android" | "other";

const IOS_UA_RE = /iPhone|iPod|iPad/i;
const ANDROID_UA_RE = /Android/i;

const DAY_MS = 24 * 60 * 60 * 1000;

export interface DetectPlatformInput {
  ua: string;
  /** navigator.maxTouchPoints — used to spot iPadOS, which reports a Mac UA. */
  maxTouchPoints?: number;
  /** navigator.platform — "MacIntel" on iPadOS 13+. */
  platform?: string;
}

/**
 * Classify the device as ios, android, or other. iPadOS 13+ masquerades as a
 * desktop Mac (UA says "Macintosh"), so a touch-capable Mac is treated as iOS —
 * that is the only environment where a "Mac" reports multiple touch points.
 */
export function detectMobilePlatform({
  ua,
  maxTouchPoints = 0,
  platform = "",
}: DetectPlatformInput): MobilePlatform {
  if (!ua && !platform) return "other";
  if (IOS_UA_RE.test(ua)) return "ios";
  if (ANDROID_UA_RE.test(ua)) return "android";
  // iPadOS 13+ desktop-class Safari: Mac UA but a real touchscreen.
  const looksLikeMac = /Macintosh/i.test(ua) || platform === "MacIntel";
  if (looksLikeMac && maxTouchPoints > 1) return "ios";
  return "other";
}

export interface ShouldShowInstallBannerInput {
  platform: MobilePlatform;
  isStandalone: boolean;
  /** Epoch ms of the last dismiss, or null if never dismissed. */
  dismissedAt?: number | null;
  /** How many days to suppress after a dismiss. 0 = next visit shows again. */
  dismissForDays?: number;
  /** Override "now" — defaults to Date.now(). */
  now?: number;
}

export function shouldShowInstallBanner({
  platform,
  isStandalone,
  dismissedAt,
  dismissForDays = 7,
  now = Date.now(),
}: ShouldShowInstallBannerInput): boolean {
  // Already installed (launched from home screen) — nothing to install.
  if (isStandalone) return false;
  // Install guidance only makes sense on iOS + Android. Desktop browsers have
  // their own omnibox install affordance and don't need this banner.
  if (platform === "other") return false;
  if (dismissedAt && dismissForDays > 0) {
    const expiresAt = dismissedAt + dismissForDays * DAY_MS;
    if (now < expiresAt) return false;
  }
  return true;
}

export function readDismissedAt(
  storage: Pick<Storage, "getItem"> | null | undefined,
  key: string,
): number | null {
  if (!storage) return null;
  try {
    const raw = storage.getItem(key);
    if (!raw) return null;
    const n = Number(raw);
    return Number.isFinite(n) ? n : null;
  } catch {
    return null;
  }
}

export function writeDismissedAt(
  storage: Pick<Storage, "setItem"> | null | undefined,
  key: string,
  ts: number,
): void {
  if (!storage) return;
  try {
    storage.setItem(key, String(ts));
  } catch {
    // localStorage unavailable (private mode, quota): banner just shows again
    // next visit. No fallback needed.
  }
}
