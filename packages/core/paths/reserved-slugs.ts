/**
 * Slugs reserved because they collide with frontend top-level routes.
 * Keep in sync with server/internal/handler/workspace_reserved_slugs.go.
 */
export const RESERVED_SLUGS = new Set([
  // Auth + onboarding
  "login",
  "logout",
  "signup",
  "onboarding",
  "invite",
  "auth",

  // Reserved for future platform routes
  "api",
  "admin",
  "help",
  "about",
  "pricing",
  "changelog",

  // Dashboard route segments. Even though Next.js's route specificity
  // would technically resolve /{slug}/{view} correctly, having a workspace
  // slug equal to a route name (e.g. slug="issues") makes URLs visually
  // ambiguous — /issues/abc reads as either "issue abc in workspace
  // 'issues'" or "issue abc in some workspace". Reserve to avoid the
  // ambiguity entirely.
  "issues",
  "projects",
  "autopilots",
  "agents",
  "inbox",
  "my-issues",
  "runtimes",
  "skills",
  "settings",
  "workflows",
  "schedules",
  "command",

  // Next.js / hosting internals
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
