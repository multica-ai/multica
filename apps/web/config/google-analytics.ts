import { RESERVED_SLUGS } from "@multica/core/paths";

type RuntimeEnv = Record<string, string | undefined>;

// GA4 Measurement IDs look like `G-RQVF209EHD`. The value is interpolated
// into the gtag.js URL and config call, so anything that doesn't match this
// shape is rejected outright rather than escaped.
const MEASUREMENT_ID_PATTERN = /^G-[A-Z0-9]{4,20}$/;

/**
 * Resolve the Google Analytics (GA4) Measurement ID from the runtime env.
 *
 * Opt-in: returns undefined unless GOOGLE_ANALYTICS_ID is set to a valid
 * GA4 ID, so local dev and self-hosted images never load gtag.js. It is read
 * by the Next.js server per request (not a NEXT_PUBLIC_* build-time var), so
 * the published Docker images carry no ID and operators can toggle it without
 * a rebuild.
 */
export function resolveGoogleAnalyticsId(env: RuntimeEnv): string | undefined {
  const value = env.GOOGLE_ANALYTICS_ID?.trim().toUpperCase();
  if (!value || !MEASUREMENT_ID_PATTERN.test(value)) return undefined;
  return value;
}

// Query params that are safe and useful to report. Everything else is
// dropped: the app carries live credentials in the query string — IM bind
// tokens (`/wecom/bind?token=…` and friends), OAuth `code`/`state` on
// `/auth/callback`, CLI login `cli_callback`/`cli_state`, `next` redirect
// targets — and GA would otherwise record the full URL (page_location /
// page_referrer). An allowlist keeps new secret-bearing params out by default.
const ALLOWED_QUERY_PARAMS = new Set([
  "utm_source",
  "utm_medium",
  "utm_campaign",
  "utm_term",
  "utm_content",
  "gclid",
]);

// Path segments that are capabilities rather than page names. Invitation ids
// are bearer-ish links, so report the route shape, not the value.
const REDACTED_PATH_PATTERNS: Array<[RegExp, string]> = [
  [/^\/invite\/[^/]+/, "/invite/[id]"],
];

/**
 * Reduce a URL to what is safe to send to Google Analytics: origin + path
 * (with capability segments masked) + allowlisted query params. The hash is
 * always dropped. Returns "" for empty or unparseable input, which GA treats
 * as "not set".
 */
export function sanitizeAnalyticsUrl(raw: string | undefined | null): string {
  const parts = analyticsUrlParts(raw);
  if (!parts) return "";
  return parts.sanitized;
}

/**
 * True when the raw URL carries something sanitizeAnalyticsUrl would strip
 * (a non-allowlisted query param or a capability path segment). GA's own
 * history-change page views read the live URL directly, so the component
 * stops GA for this document once the loaded tag may have observed one.
 */
export function isSensitiveAnalyticsUrl(raw: string | undefined | null): boolean {
  if (!raw) return false;
  const parts = analyticsUrlParts(raw);
  return !parts || parts.sensitive;
}

// Top-level routes that are not workspace slugs. RESERVED_SLUGS covers most of
// them; the IM bind and join routes predate it and are listed here directly.
const GLOBAL_ROUTES_OUTSIDE_RESERVED = new Set(["join", "lark", "slack", "telegram", "wecom"]);

// Sections under /{workspaceSlug}/. Anything else is reported as a generic
// workspace page rather than echoing an unknown segment.
const WORKSPACE_SECTIONS = new Set([
  "agents",
  "attachments",
  "autopilots",
  "billing",
  "chat",
  "inbox",
  "issues",
  "members",
  "my-issues",
  "projects",
  "runtimes",
  "settings",
  "skills",
  "squads",
  "usage",
  "views",
]);

/**
 * The page_title reported to Google Analytics for a URL.
 *
 * GA4 sends `document.title` as page_title on every event by default, and in
 * a workspace that title is user content (Issue titles, project names, agent
 * names). This returns a fixed label built only from route names Multica
 * defines — never from IDs, slugs, or the live title — so the reported title
 * carries nothing a workspace member wrote. The path itself is still reported
 * via page_location.
 */
export function analyticsPageTitle(raw: string | undefined | null): string {
  let pathname: string;
  try {
    pathname = new URL(raw ?? "").pathname;
  } catch {
    return "Multica";
  }
  const segments = pathname.split("/").filter(Boolean);
  const first = segments[0]?.toLowerCase();
  if (!first) return "Home";
  if (RESERVED_SLUGS.has(first) || GLOBAL_ROUTES_OUTSIDE_RESERVED.has(first)) {
    return humanizeRouteSegment(first);
  }
  const section = segments[1]?.toLowerCase();
  if (!section) return "Workspace";
  if (!WORKSPACE_SECTIONS.has(section)) return "Workspace · Page";
  const label = `Workspace · ${humanizeRouteSegment(section)}`;
  return segments.length > 2 ? `${label} · Detail` : label;
}

function humanizeRouteSegment(segment: string): string {
  const words = segment.replace(/-/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

function analyticsUrlParts(
  raw: string | undefined | null,
): { sanitized: string; sensitive: boolean } | null {
  if (!raw) return null;
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;

  let sensitive = false;
  let pathname = url.pathname;
  for (const [pattern, replacement] of REDACTED_PATH_PATTERNS) {
    if (pattern.test(pathname)) {
      sensitive = true;
      pathname = pathname.replace(pattern, replacement);
    }
  }

  const kept = new URLSearchParams();
  url.searchParams.forEach((value, key) => {
    if (ALLOWED_QUERY_PARAMS.has(key)) kept.append(key, value);
    else sensitive = true;
  });
  const search = kept.toString();

  return {
    sanitized: `${url.origin}${pathname}${search ? `?${search}` : ""}`,
    sensitive,
  };
}
