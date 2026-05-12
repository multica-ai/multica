// Pure logic for the "Open in app" banner. Kept separate from the React
// component so the visibility rules can be unit-tested without jsdom navigator
// stubbing.

const MOBILE_UA_RE = /iPhone|iPod|iPad|Android.*Mobile/i;

const DAY_MS = 24 * 60 * 60 * 1000;

export function isMobileUserAgent(ua: string): boolean {
  if (!ua) return false;
  return MOBILE_UA_RE.test(ua);
}

export interface ShouldShowBannerInput {
  ua: string;
  isStandalone: boolean;
  /** Epoch ms of the last dismiss, or null if never dismissed. */
  dismissedAt?: number | null;
  /** How many days to suppress after a dismiss. 0 = next visit shows again. */
  dismissForDays?: number;
  /** Override "now" — defaults to Date.now(). */
  now?: number;
}

export function shouldShowOpenInAppBanner({
  ua,
  isStandalone,
  dismissedAt,
  dismissForDays = 7,
  now = Date.now(),
}: ShouldShowBannerInput): boolean {
  if (isStandalone) return false;
  if (!isMobileUserAgent(ua)) return false;
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
