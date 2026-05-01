// Frontend analytics glue. Thin wrapper over Amplitude Browser SDK.
//
// The source-of-truth event catalog is `docs/analytics.md`. This module only
// handles the two things the backend can't do itself: attribution capture on
// first anonymous pageview, and person-identity merge on login. Every funnel
// event (signup, workspace_created, runtime_registered, issue_executed,
// invite_sent, invite_accepted, story_created, pr_opened) is emitted
// server-side — see `server/internal/analytics`.
//
// Configuration comes from the backend's `/api/config` response (populated
// from AMPLITUDE_API_KEY on the server), NOT from NEXT_PUBLIC_* envs. That
// keeps self-hosted Docker images from leaking our project key — their
// backend returns an empty key and this module stays inert.

import * as amplitude from "@amplitude/analytics-browser";

const SIGNUP_SOURCE_COOKIE = "multica_signup_source";
// Per-value cap keeps a long utm_content from blowing the budget. We drop
// the entire cookie if the JSON still exceeds the overall limit — partial
// JSON is worse than no attribution because Amplitude can't parse it.
const SIGNUP_SOURCE_VALUE_MAX_LEN = 96;
const SIGNUP_SOURCE_MAX_LEN = 512;
const UTM_KEYS = [
  "utm_source",
  "utm_medium",
  "utm_campaign",
  "utm_content",
  "utm_term",
] as const;

let initialized = false;
// auth-initializer fetches /api/config and /api/me in parallel — on a
// slow-config path, identify() can fire before initAnalytics(). Buffer the
// most recent pending identify (only one matters, since it's per-session)
// and flush it inside initAnalytics.
let pendingIdentify: { userId: string; props?: Record<string, unknown> } | null = null;
// Likewise pageviews: the initial "/" pageview is the anchor of the
// acquisition funnel, and the Next.js router fires it on mount before the
// config fetch resolves. We keep the first pending pageview so that step
// doesn't silently drop.
let pendingPageview: string | undefined | null = null;
// Frontend-emitted events (captureEvent) and person-property updates
// (setPersonProperties) can also arrive before init — same config-race as
// identify/pageview. We replay them in order once init succeeds. These
// only ever carry user-triggered signals on identified users, so the
// buffer stays small (~one step-transition worth).
type PendingOp =
  | { kind: "event"; name: string; props?: Record<string, unknown> }
  | { kind: "set"; props: Record<string, unknown> };
const pendingOps: PendingOp[] = [];

export {
  captureDownloadIntent,
  captureDownloadPageViewed,
  captureDownloadInitiated,
  type DownloadIntentSource,
  type DownloadDetectPayload,
  type DownloadInitiatedPayload,
} from "./download";

export {
  captureFeedbackOpened,
  type FeedbackOpenedSource,
} from "./feedback";

export interface AnalyticsConfig {
  key: string;
  /**
   * Amplitude doesn't need a host for the standard SDK — it sends to
   * api2.amplitude.com by default. We keep this field for parity with the
   * config endpoint but it's unused by the browser SDK (the Go backend uses
   * it for its own HTTP client if you ever need to point at an EU endpoint).
   */
  host: string;
  /**
   * Client app version — attached to every event via Amplitude's
   * `defaultTracking.appVersion`. Web injects the build-time tag / sha;
   * desktop reads from the Electron API. Optional because local dev may not
   * have a version available.
   */
  appVersion?: string;
}

export type ClientType = "desktop" | "web";

/**
 * Classify the current runtime as desktop (Electron renderer) or web. Used as
 * an event property so every event can be split by client without relying on
 * Amplitude's platform detection (which reports "Web" for both the Next.js app
 * and the Electron renderer since both are Chromium).
 */
export function detectClientType(): ClientType {
  if (typeof window === "undefined") return "web";
  const w = window as unknown as { electron?: unknown; desktopAPI?: unknown };
  if (w.electron || w.desktopAPI) return "desktop";
  if (typeof navigator !== "undefined" && /Electron/i.test(navigator.userAgent)) {
    return "desktop";
  }
  return "web";
}

/**
 * Initialize the Amplitude Browser SDK if a key is present. Safe to call
 * multiple times; subsequent calls with the same config are no-ops.
 *
 * Returns `true` when analytics is actually running; `false` when disabled
 * (no key, SSR, or already initialized).
 */
export function initAnalytics(config: AnalyticsConfig | null | undefined): boolean {
  if (typeof window === "undefined") return false;
  if (!config?.key) return false;
  if (initialized) return true;

  amplitude.init(config.key, {
    // Don't auto-track anything — our funnel is narrow and explicit.
    autocapture: false,
    defaultTracking: false,
    appVersion: config.appVersion || undefined,
  });


  initialized = true;

  // Flush any identify() that arrived before init resolved.
  if (pendingIdentify) {
    const identifyEvent = new amplitude.Identify();
    if (pendingIdentify.props) {
      for (const [k, v] of Object.entries(pendingIdentify.props)) {
        identifyEvent.set(k, v as amplitude.Types.ValidPropertyType);
      }
    }
    amplitude.identify(identifyEvent, { user_id: pendingIdentify.userId });
    amplitude.setUserId(pendingIdentify.userId);
    pendingIdentify = null;
  }
  // And any first pageview we captured while config was loading.
  if (pendingPageview !== null) {
    amplitude.track("[Amplitude] Page Viewed", pendingPageview ? { page_url: pendingPageview } : undefined);
    pendingPageview = null;
  }
  // Replay buffered events / person-property updates in their original
  // order — funnel correctness depends on sequence.
  while (pendingOps.length > 0) {
    const op = pendingOps.shift()!;
    if (op.kind === "event") {
      amplitude.track(op.name, op.props);
    } else {
      const identifyEvent = new amplitude.Identify();
      for (const [k, v] of Object.entries(op.props)) {
        identifyEvent.set(k, v as amplitude.Types.ValidPropertyType);
      }
      amplitude.identify(identifyEvent);
    }
  }
  return true;
}

/**
 * Set the user identity for all subsequent events. Must be called exactly
 * once per auth transition (login / session-resume).
 *
 * Calls before initAnalytics() are buffered — auth-initializer fetches
 * config and user in parallel, so identify can arrive first.
 */
export function identify(userId: string, userProperties?: Record<string, unknown>): void {
  if (!initialized) {
    pendingIdentify = { userId, props: userProperties };
    return;
  }
  amplitude.setUserId(userId);
  if (userProperties) {
    const identifyEvent = new amplitude.Identify();
    for (const [k, v] of Object.entries(userProperties)) {
      identifyEvent.set(k, v as amplitude.Types.ValidPropertyType);
    }
    amplitude.identify(identifyEvent);
  }
}

/**
 * Clear the client-side identity on logout so the next login doesn't bleed
 * the previous user's events into a new session.
 */
export function resetAnalytics(): void {
  pendingIdentify = null;
  pendingPageview = null;
  pendingOps.length = 0;
  if (!initialized) return;
  amplitude.reset();
}

/**
 * Capture a frontend-emitted event. Most funnel events fire server-side
 * (see `server/internal/analytics`); this wrapper is reserved for the
 * handful of signals the backend can't see.
 *
 * Calls before initAnalytics() buffer in order so a late-arriving config
 * doesn't silently swallow a step transition.
 */
export function captureEvent(
  name: string,
  props?: Record<string, unknown>,
): void {
  if (!initialized) {
    pendingOps.push({ kind: "event", name, props });
    return;
  }
  amplitude.track(name, props);
}

/**
 * Set (overwrite) user properties on the currently identified user.
 * Mirrors the backend's `Event.UserProperties` path — keep these aligned
 * so the same cohort signals (role, use_case, platform_preference) are
 * queryable regardless of which side emitted last.
 */
export function setPersonProperties(props: Record<string, unknown>): void {
  if (!initialized) {
    pendingOps.push({ kind: "set", props });
    return;
  }
  const identifyEvent = new amplitude.Identify();
  for (const [k, v] of Object.entries(props)) {
    identifyEvent.set(k, v as amplitude.Types.ValidPropertyType);
  }
  amplitude.identify(identifyEvent);
}

/**
 * Set user properties that should only be written once (first write wins).
 * Use for acquisition attribution fields that must never be overwritten.
 */
export function setPersonPropertiesOnce(props: Record<string, unknown>): void {
  if (typeof window === "undefined") return;
  if (!initialized) return;
  const identifyEvent = new amplitude.Identify();
  for (const [k, v] of Object.entries(props)) {
    identifyEvent.setOnce(k, v as amplitude.Types.ValidPropertyType);
  }
  amplitude.identify(identifyEvent);
}

/**
 * Capture a page view. Call once per client-side navigation.
 *
 * Calls before initAnalytics() buffer the most-recent path so the first
 * pageview isn't dropped on slow /api/config fetches.
 */
export function capturePageview(path?: string): void {
  if (!initialized) {
    pendingPageview = path ?? "";
    return;
  }
  amplitude.track("[Amplitude] Page Viewed", path ? { page_url: path } : undefined);
}

/**
 * On the very first anonymous pageview in a browser session, read UTM +
 * referrer and stash them in a cookie that the backend reads during signup.
 *
 * Never use raw `document.referrer` as attribution — it can leak OAuth
 * callback URLs with `code` / `state` in the query string. We keep only the
 * referrer's origin (scheme + host), which is what a funnel actually needs.
 *
 * This cookie is what `signup_source` in the backend's signup event reads
 * from; both fields are intentionally opaque JSON so the schema can evolve
 * without a backend deploy.
 */
export function captureSignupSource(): void {
  if (typeof window === "undefined" || typeof document === "undefined") return;
  if (readCookie(SIGNUP_SOURCE_COOKIE)) return;

  const source: Record<string, string> = {};
  const cap = (v: string) =>
    v.length > SIGNUP_SOURCE_VALUE_MAX_LEN ? v.slice(0, SIGNUP_SOURCE_VALUE_MAX_LEN) : v;

  try {
    const params = new URLSearchParams(window.location.search);
    for (const key of UTM_KEYS) {
      const v = params.get(key);
      if (v) source[key] = cap(v);
    }
  } catch {
    // URL APIs unavailable — skip silently.
  }

  const refOrigin = safeReferrerOrigin(document.referrer);
  if (refOrigin) source.referrer_origin = cap(refOrigin);

  if (Object.keys(source).length === 0) return;

  const payload = JSON.stringify(source);
  if (payload.length > SIGNUP_SOURCE_MAX_LEN) return;

  const maxAge = 60 * 60 * 24 * 30;
  document.cookie = `${SIGNUP_SOURCE_COOKIE}=${encodeURIComponent(payload)}; path=/; max-age=${maxAge}; samesite=lax`;
}

function safeReferrerOrigin(referrer: string): string {
  if (!referrer) return "";
  try {
    const url = new URL(referrer);
    if (url.origin === window.location.origin) return "";
    return url.origin;
  } catch {
    return "";
  }
}

function readCookie(name: string): string {
  if (typeof document === "undefined") return "";
  const prefix = `${name}=`;
  const parts = document.cookie ? document.cookie.split("; ") : [];
  for (const part of parts) {
    if (part.startsWith(prefix)) return decodeURIComponent(part.slice(prefix.length));
  }
  return "";
}
