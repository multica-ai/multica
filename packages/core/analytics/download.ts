/**
 * Download funnel instrumentation.
 *
 * Complements the onboarding events added in PR #1489 by covering
 * every surface that advertises the desktop app — landing hero,
 * landing footer, login, Welcome (web branch), Step 3 — and the
 * /download page itself. Without this layer we can see Step 3
 * path selection but not the touchpoint that got the user there,
 * nor the /download → installer conversion.
 *
 * Event names and property shapes are governed by docs/analytics.md;
 * keep the two in sync when adding a new source or field.
 */

import posthog from "posthog-js";

import { captureEvent, setPersonProperties } from "./index";

/**
 * Where the user clicked a CTA that points at `/download`. Typed union
 * prevents drift across the five touchpoints and lets PostHog funnels
 * split cleanly by top-of-funnel entry.
 */
export type DownloadIntentSource =
  | "landing_hero"
  | "landing_footer"
  | "login"
  | "welcome"
  | "step3";

/**
 * OS + arch detect result for the /download page. Mirrors the shape of
 * `@/features/landing/utils/os-detect.ts` without importing it (that
 * module lives in the web app; core packages can't depend on it). Keep
 * these enums in lockstep.
 */
export interface DownloadDetectPayload {
  detected_os: "mac" | "windows" | "linux" | "unknown";
  detected_arch: "arm64" | "x64" | "unknown";
  detect_confident: boolean;
  version_available: boolean;
}

/**
 * Specific installer the user chose on /download. Version is the GitHub
 * tag name (e.g. "v0.2.13") so we can correlate adoption-by-release.
 */
export interface DownloadInitiatedPayload {
  platform: "mac" | "windows" | "linux";
  arch: "arm64" | "x64";
  format: "dmg" | "zip" | "exe" | "appimage" | "deb" | "rpm";
  version: string;
  primary_cta: boolean;
  matched_detect: boolean;
}

/**
 * Fires when a user clicks any CTA that navigates to `/download`. We
 * also write `platform_preference` to person properties so the backend
 * can segment subsequent events — same convention the Step 3 handler
 * already uses (see `step-platform-fork.tsx`).
 */
export function captureDownloadIntent(source: DownloadIntentSource): void {
  captureEvent("download_intent_expressed", {
    source,
  });
  setPersonProperties({ platform_preference: "desktop" });
}

/**
 * Fires once on /download page mount, after OS detection resolves. The
 * first detection for a given person is mirrored into person properties
 * via `$set_once` so every downstream event gains a platform dimension
 * without re-emitting.
 */
export function captureDownloadPageViewed(
  payload: DownloadDetectPayload,
): void {
  captureEvent("download_page_viewed", {
    detected_os: payload.detected_os,
    detected_arch: payload.detected_arch,
    detect_confident: payload.detect_confident,
    version_available: payload.version_available,
  });
  setPersonPropertiesOnce({
    first_detected_os: payload.detected_os,
    first_detected_arch: payload.detected_arch,
  });
}

/**
 * Fires when the user clicks a concrete installer link on `/download`.
 * `primary_cta` marks the hero-level recommendation versus a manual
 * pick from the All Platforms matrix; `matched_detect` captures
 * whether the click matched what we detected (miss = detect got it
 * wrong / user overrode).
 */
export function captureDownloadInitiated(
  payload: DownloadInitiatedPayload,
): void {
  captureEvent("download_initiated", { ...payload });
}

/**
 * $set_once wire form. Mirrors the backend's `Event.SetOnce` path —
 * first write wins, subsequent ones are no-ops on PostHog's side.
 * Wrapping it here keeps call sites free of the no-op `$set_once`
 * envelope quirk.
 */
function setPersonPropertiesOnce(props: Record<string, unknown>): void {
  if (typeof window === "undefined") return;
  posthog.capture("$set", { $set_once: props });
}
