import { captureEvent } from "@multica/core/analytics";

export const QUALIFIED_LANDING_VIEW = "qualified_landing_view";
export const SIGNUP_OR_DOWNLOAD_START = "signup_or_download_start";
export const QUALIFIED_VIEW_DELAY_MS = 3_000;

const QUALIFIED_VIEW_SESSION_KEY = "multica_qualified_landing_view_v1";
const DIMENSION_MAX_LENGTH = 96;
const BOT_USER_AGENT = /bot|crawler|spider|headless|preview|prerender/i;

export type ConversionIntent = "signup" | "download";

interface BrowserContext {
  visibilityState: DocumentVisibilityState;
  webdriver: boolean;
  userAgent: string;
}

interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export function isQualifiedLandingContext(context: BrowserContext): boolean {
  return (
    context.visibilityState === "visible" &&
    !context.webdriver &&
    !BOT_USER_AGENT.test(context.userAgent)
  );
}

export function claimQualifiedLandingView(storage: StorageLike | null): boolean {
  if (!storage) return true;
  try {
    if (storage.getItem(QUALIFIED_VIEW_SESSION_KEY)) return false;
    storage.setItem(QUALIFIED_VIEW_SESSION_KEY, "1");
    return true;
  } catch {
    // Privacy modes may deny sessionStorage. The component-level ref still
    // prevents duplicate capture during this mount.
    return true;
  }
}

export function acquisitionDimensions(
  search: string,
  referrer: string,
  currentOrigin: string,
): Record<string, string> {
  const dimensions: Record<string, string> = {};

  try {
    const params = new URLSearchParams(search);
    addDimension(dimensions, "source", params.get("utm_source"));
    addDimension(dimensions, "medium", params.get("utm_medium"));
    addDimension(dimensions, "campaign", params.get("utm_campaign"));
  } catch {
    // Malformed browser URLs should not break navigation.
  }

  try {
    const referrerUrl = new URL(referrer);
    if (referrerUrl.origin !== currentOrigin) {
      addDimension(dimensions, "referrer_host", referrerUrl.hostname);
    }
  } catch {
    // Empty and malformed referrers are treated as direct traffic.
  }

  dimensions.source ??= dimensions.referrer_host ?? "direct";
  dimensions.medium ??= "none";
  dimensions.campaign ??= "none";

  return dimensions;
}

export function captureQualifiedLandingView(): void {
  captureEvent(QUALIFIED_LANDING_VIEW, {
    ...browserAcquisitionDimensions(),
    platform: "web",
    landing_path: "/",
    qualified_after_ms: QUALIFIED_VIEW_DELAY_MS,
  });
}

export function captureSignupOrDownloadStart(
  intent: ConversionIntent,
  placement: string,
): void {
  captureEvent(SIGNUP_OR_DOWNLOAD_START, {
    ...browserAcquisitionDimensions(),
    platform: "web",
    intent,
    placement: sanitizeDimension(placement),
  });
}

function browserAcquisitionDimensions(): Record<string, string> {
  if (typeof window === "undefined" || typeof document === "undefined") return {};
  return acquisitionDimensions(
    window.location.search,
    document.referrer,
    window.location.origin,
  );
}

function addDimension(
  target: Record<string, string>,
  key: string,
  value: string | null,
): void {
  const sanitized = sanitizeDimension(value ?? "");
  if (sanitized) target[key] = sanitized;
}

function sanitizeDimension(value: string): string {
  if (/@|:\/\/|[/?#]/.test(value) || isIpAddress(value)) return "";
  return value
    .trim()
    .slice(0, DIMENSION_MAX_LENGTH)
    .replace(/\s+/g, "_")
    .replace(/[^a-zA-Z0-9._-]/g, "")
    .toLowerCase();
}

function isIpAddress(value: string): boolean {
  const candidate = value.trim().replace(/^\[|\]$/g, "");
  return (
    /^\d{1,3}(?:\.\d{1,3}){3}$/.test(candidate) ||
    (/^[0-9a-f:]+$/i.test(candidate) && candidate.includes(":"))
  );
}
