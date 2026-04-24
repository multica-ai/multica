/**
 * Slugs reserved because they collide with frontend top-level routes,
 * platform features, or web standards.
 *
 * Keep in sync with server/internal/handler/workspace_reserved_slugs.go.
 *
 * Convention for new global routes (CLAUDE.md): use a single word
 * (`/login`, `/inbox`) or `/{noun}/{verb}` (`/workspaces/new`). Hyphenated
 * root-level word groups (`/new-workspace`, `/create-team`) collide with
 * common user workspace names — see PR for full discussion.
 */
export const RESERVED_SLUGS = new Set([
  // Auth flow
  "login",
  "logout",
  "signin",
  "signout",
  "signup",
  "auth",
  "oauth",
  "callback",
  "invite",
  "verify",
  "reset",
  "password",
  "onboarding", // historical, kept reserved post-removal

  // Platform / marketing routes (current + likely-future)
  "api",
  "admin",
  "multica", // brand name — prevent impersonation workspaces
  "www",     // hostname confusable; never a legitimate workspace slug
  "new",     // ambiguous verb-as-slug; reserved for future global create routes
  "home",     // likely-future marketing/landing entry
  "homepage", // existing /homepage landing variant in apps/web
  "dashboard", // standard SaaS entry; likely-future global route
  "help",
  "about",
  "pricing",
  "changelog",
  "docs",
  "support",
  "status",
  "legal",
  "privacy",
  "terms",
  "security",
  "contact",
  "blog",
  "careers",
  "press",
  "download",

  // Account / billing (likely-future global routes in the avatar menu).
  "profile",
  "account",
  "billing",
  "notifications",
  "search",
  "members",

  // Dashboard / workspace route segments. Reserving the segment name
  // prevents `/{slug}/{view}` from being visually ambiguous (e.g. a
  // workspace named "issues" makes `/issues/abc` mean two things).
  "issues",
  "projects",
  "autopilots",
  "agents",
  "inbox",
  "my-issues",
  "runtimes",
  "skills",
  "settings",
  "workspaces", // global `/workspaces/new` workspace creation page
  "teams",      // reserved for future team management routes

  // API / integration prefixes. `api` above already covers /api/*; these
  // guard against future top-level API alias routes (e.g. /v1, /graphql)
  // and against accidental workspace slugs that read like API identifiers.
  "v1",
  "v2",
  "graphql",
  "webhooks",
  "sdk",
  "tokens",
  "cli",

  // Backend ops / observability. `/health`, `/readyz`, `/healthz`, and `/ws`
  // exist on the backend
  // host; reserving them on the workspace slug space prevents naming
  // confusion if/when these paths are ever proxied through the web origin.
  "health",
  "readyz",
  "healthz",
  "ws",
  "metrics",
  "ping",

  // RFC 2142 — privileged email mailboxes. Allowing user workspaces with
  // these slugs would let attackers spoof system messaging.
  "postmaster",
  "abuse",
  "noreply",
  "webmaster",
  "hostmaster",

  // Hostname / subdomain confusables. Even on path-based routing these
  // names attract phishing and subdomain-takeover attempts.
  "mail",
  "ftp",
  "static",
  "cdn",
  "assets",
  "public",
  "files",
  "uploads",

  // Next.js / web standards. These entries contain characters (dots,
  // underscores) that today's slug regex `^[a-z0-9]+(?:-[a-z0-9]+)*$`
  // already rejects at the format-validation step — so `isReservedSlug`
  // never actually matches them. They are kept as defense-in-depth so
  // that if the slug regex is ever relaxed (e.g. to support dotted
  // corporate slugs like `acme.io`), these system paths stay protected.
  "_next",
  "favicon.ico",
  "robots.txt",
  "sitemap.xml",
  "manifest.json",
  ".well-known",
]);

export function isReservedSlug(slug: string): boolean {
  return RESERVED_SLUGS.has(slug);
}
