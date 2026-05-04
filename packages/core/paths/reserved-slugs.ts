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

  // Next.js / web standards (framework-mandated)
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
