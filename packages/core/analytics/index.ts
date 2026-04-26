// Frontend analytics glue. Thin wrapper over posthog-js.
//
// The source-of-truth event catalog is `docs/analytics.md`. This module only
// handles the two things the backend can't do itself: attribution capture on
// first anonymous pageview, and person-identity merge on login. Every funnel
// event (signup, workspace_created, runtime_registered, issue_executed,
// invite_sent, invite_accepted) is emitted server-side — see
// `server/internal/analytics`.
//
// Configuration comes from the backend's `/api/config` response (populated
// from POSTHOG_API_KEY on the server), NOT from NEXT_PUBLIC_* envs. That
// keeps self-hosted Docker images from leaking our project key — their
// backend returns an empty key and this module stays inert.

import posthog from "posthog-js";

const SIGNUP_SOURCE_COOKIE = "multica_signup_source";
// Per-value cap keeps a long utm_content from blowing the budget. We drop
// the entire cookie if the JSON still exceeds the overall limit — partial
// JSON is worse than no attribution because PostHog can't parse it.
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
// Cached super-properties so resetAnalytics() can re-register them after
// posthog.reset() wipes the persisted set. Without this, logout / account
// switch silently drops client_type + app_version from every subsequent
// event until a full reload.
let superProperties: Record<string, unknown> = {};

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
  host: string;
  /**
   * Client app version — attached to every event as an `app_version`
   * super-property. Web injects the build-time tag / sha; desktop reads from
   * the Electron API. Optional because local dev may not have a version
   * available.
   */
  appVersion?: string;
}

export type ClientType = "desktop" | "web";

/**
 * Classify the current runtime as desktop (Electron renderer) or web. Used as
 * a super-property so every event can be split by client without relying on
 * PostHog's `$lib`, which reports "web" in both the Next.js app and the
 * Electron renderer (both Chromium).
 *
 * Signals we trust:
 *   - `window.electron` is exposed by the preload script in every renderer.
 *   - `navigator.userAgent` contains "Electron" as a fallback.
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
 * Initialize posthog-js if a key is present. Safe to call multiple times;
 * subsequent calls with the same config are no-ops.
 *
 * Returns `true` when analytics is actually running; `false` when disabled
 * (no key, SSR, or already initialized with a conflicting key — which we
 * treat as "use the existing instance").
 */
export function initAnalytics(config: AnalyticsConfig | null | undefined): boolean {
  if (typeof window === "undefined") return false;
  if (!config?.key) return false;
  if (initialized) return true;

  posthog.init(config.key, {
    api_host: config.host || "https://us.i.posthog.com",
    // person_profiles=identified_only keeps anonymous drive-by traffic off
    // the billed events until they actually identify, which aligns with how
    // our funnel is set up: signup is the first real funnel step.
    person_profiles: "identified_only",
    // Turn off every on-by-default auto-capture surface. Our funnel is
    // narrow and explicit (the events in docs/analytics.md + a manual
    // $pageview). Autocapture floods the Activity view with anonymous
    // "clicked button" / "clicked link" noise, burns the billed event
    // budget, and risks capturing user-typed content in input values.
    // Turn things back on deliberately if we ever want them.
    capture_pageview: false,
    autocapture: false,
    capture_heatmaps: false,
    capture_dead_clicks: false,
    capture_exceptions: false,
    disable_session_recording: true,
    disable_surveys: true,
  });
  // Register super-properties — attached to every event emitted from this
  // client. `client_type` is the canonical split between desktop and web
  // (PostHog's own `$lib` reports "web" for both because Electron renderers
  // are Chromium). `app_version` is optional so self-hosted or local dev
  // builds without a version don't pollute the property.
  // We cache the set so resetAnalytics() can re-apply it after
  // posthog.reset() — reset() clears persisted super-properties otherwise.
  superProperties = { client_type: detectClientType() };
  if (config.appVersion) superProperties.app_version = config.appVersion;
  posthog.register(superProperties);
  initialized = true;

  // Flush any identify() that arrived before init resolved.
  if (pendingIdentify) {
    posthog.identify(pendingIdentify.userId, pendingIdentify.props);
    pendingIdentify = null;
  }
  // And any first pageview we captured while config was loading.
  if (pendingPageview !== null) {
    posthog.capture("$pageview", pendingPageview ? { $current_url: pendingPageview } : undefined);
    pendingPageview = null;
  }
  // Replay buffered events / person-property updates in their original
  // order — funnel correctness depends on sequence (e.g. a user submits
  // the questionnaire and then finishes onboarding within the same
  // config-race window).
  while (pendingOps.length > 0) {
    const op = pendingOps.shift()!;
    if (op.kind === "event") {
      posthog.capture(op.name, op.props);
    } else {
      capturePersonSet(op.props);
    }
  }
  return true;
}

/**
 * Merge the current anonymous session into the logged-in person. Must be
 * called exactly once per auth transition (login / session-resume). Pulling
 * attribution properties into person_properties on identify is how we keep
 * UTM / referrer on the user profile without re-emitting them per event.
 *
 * Calls before initAnalytics() are buffered — auth-initializer fetches
 * config and user in parallel, so identify can arrive first.
 */
export function identify(userId: string, userProperties?: Record<string, unknown>): void {
  if (!initialized) {
    pendingIdentify = { userId, props: userProperties };
    return;
  }
  posthog.identify(userId, userProperties);
}

/**
 * Clear the client-side identity on logout so the next login merges cleanly
 * and doesn't bleed the previous user's events into a new session.
 */
export function resetAnalytics(): void {
  pendingIdentify = null;
  pendingPageview = null;
  pendingOps.length = 0;
  if (!initialized) return;
  posthog.reset();
  // reset() wipes persisted super-properties too, so re-register the ones
  // set at init time. Otherwise every event after logout / account-switch
  // would be missing client_type + app_version until a full reload.
  if (Object.keys(superProperties).length > 0) {
    posthog.register(superProperties);
  }
}

/**
 * Capture a frontend-emitted event. Most funnel events fire server-side
 * (see `server/internal/analytics`); this wrapper is reserved for the
 * handful of signals the backend can't see — primarily the Step 3
 * platform-fork choice on web, where the user's click never round-trips
 * to a handler.
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
  posthog.capture(name, props);
}

/**
 * Set (overwrite) person properties on the currently identified user.
 * Mirrors the backend's `Event.Set` path — keep these aligned so the
 * same cohort signals (role, use_case, platform_preference) are
 * queryable regardless of which side emitted last. Use for mutable
 * signals; use `identify(userId, { $set_once: {...} })` style for
 * attribution fields that must never be overwritten.
 */
export function setPersonProperties(props: Record<string, unknown>): void {
  if (!initialized) {
    pendingOps.push({ kind: "set", props });
    return;
  }
  capturePersonSet(props);
}

// The public wire-level contract for `$set` is a no-op event carrying a
// `$set` property. Wrapping it here (rather than calling
// `posthog.setPersonProperties` directly) keeps us version-independent —
// older posthog-js builds expose the same protocol under `posthog.people.set`,
// and the capture form works uniformly.
function capturePersonSet(props: Record<string, unknown>): void {
  posthog.capture("$set", { $set: props });
}

/**
 * Capture a page view. Call once per client-side navigation. We disable
 * posthog's automatic pageview tracking in init() so this module owns the
 * event shape — that makes it trivial to add properties (e.g. workspace
 * slug) without fighting the SDK.
 *
 * Calls before initAnalytics() buffer the most-recent path so the first
 * pageview isn't dropped on slow /api/config fetches. Subsequent pre-init
 * pageviews overwrite the buffer; after init flushes, every navigation
 * captures synchronously as expected.
 */
export function capturePageview(path?: string): void {
  if (!initialized) {
    pendingPageview = path ?? "";
    return;
  }
  posthog.capture("$pageview", path ? { $current_url: path } : undefined);
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
  // Drop rather than mid-JSON truncate — a half-string would fail to parse
  // on the backend and the attribution would be worse than missing.
  if (payload.length > SIGNUP_SOURCE_MAX_LEN) return;

  // 30-day expiry covers the typical signup consideration window. Lax is
  // the right default — the cookie is only consumed by same-origin auth.
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
