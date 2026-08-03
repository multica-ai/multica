// Where the app thinks it is — the one field a freeze report is useless
// without.
//
// Desktop runs a memory router, so `location.pathname` in the renderer is the
// packaged `index.html` file path and can never identify the visible page. The
// desktop shell publishes its bucketed route here; the freeze watchdog reads it
// so an in-thread freeze event carries a real route. Web sets nothing and keeps
// using `location.pathname`.

let route: string | null = null;

/** Longest route we keep; these are our own short templates. */
const MAX_ROUTE_LENGTH = 256;

/**
 * Publish the current app route, already bucketed to a template — see
 * `bucketDiagnosticPath`. Pass null when leaving a known route.
 */
export function setDiagnosticRoute(next: string | null): void {
  if (typeof next !== "string") {
    route = null;
    return;
  }
  const trimmed = next.trim();
  route = trimmed ? trimmed.slice(0, MAX_ROUTE_LENGTH) : null;
}

export function getDiagnosticRoute(): string | null {
  return route;
}

/** Test seam: drop state so cases don't leak into each other. */
export function resetDiagnosticContext(): void {
  route = null;
}

// The known route shapes, mirroring `packages/core/paths/paths.ts` (the single
// path builder every navigation goes through) and the routers that consume it.
// Bucketing matches against these STRUCTURALLY: a `:param` slot is whatever
// occupies that position, whether it is a UUID, an issue key, `p1`, `skl_123`
// or a percent-encoded string.
//
// Guessing from the shape of a segment instead would leak every id that does
// not look like an id — project, skill, agent, runtime and attachment ids are
// all arbitrary strings. `paths.ts` also URL-encodes them, so an id containing
// a slash arrives as one already-escaped segment and must not be mistaken for
// two path segments.
//
// A route added to `paths.ts` without being added here is not a leak: an
// unmatched path is masked below rather than passed through. The parity test in
// diagnostic-context.test.ts walks the real builders and fails when one is
// missing, so this list cannot silently fall behind.
type RoutePattern = readonly string[];

const WORKSPACE_ROUTES: readonly RoutePattern[] = [
  ["issues"],
  ["issues", ":id"],
  ["projects"],
  ["projects", ":id"],
  ["autopilots"],
  ["autopilots", ":id"],
  ["agents"],
  ["agents", "new"],
  ["agents", "new", "manual"],
  ["agents", "new", "ai"],
  ["agents", ":id"],
  ["members", ":id"],
  ["squads"],
  ["squads", ":id"],
  ["inbox"],
  ["chat"],
  ["my-issues"],
  ["usage"],
  ["billing"],
  ["runtimes"],
  ["runtimes", ":id"],
  ["runtimes", ":id", "runtime", ":runtimeId"],
  ["skills"],
  ["skills", ":id"],
  ["settings"],
  ["attachments", ":id", "preview"],
];

const GLOBAL_ROUTES: readonly RoutePattern[] = [
  ["login"],
  ["signup"],
  ["logout"],
  ["workspaces", "new"],
  ["invite", ":id"],
  ["invitations"],
  ["onboarding"],
  ["auth", "callback"],
];

const WORKSPACE_SECTIONS = new Set(WORKSPACE_ROUTES.map((route) => route[0]!));
const GLOBAL_SECTIONS = new Set(GLOBAL_ROUTES.map((route) => route[0]!));

/**
 * Collapse a concrete path to its route template: `/acme/issues/MUL-12` becomes
 * `/:slug/issues/:id`. Diagnostics only need to know which screen the user was
 * on, and a template keeps workspace slugs and resource ids out of telemetry
 * while making the field groupable in one query.
 *
 * A path that matches no known route is masked with `*` rather than reported —
 * an unrecognized segment is exactly the case where we cannot tell an id from a
 * page name, so it never travels.
 */
export function bucketDiagnosticPath(path: string): string {
  const [pathname = ""] = path.split(/[?#]/);
  const segments = pathname.split("/").filter(Boolean);
  if (segments.length === 0) return "/";

  const globalRoute = matchRoute(segments, GLOBAL_ROUTES);
  if (globalRoute) return `/${globalRoute.join("/")}`;

  // Reserved slugs mean a known global section can never also be a workspace,
  // so an unmatched path under one keeps its section and masks the rest.
  const first = segments[0]!;
  if (GLOBAL_SECTIONS.has(first)) return `/${first}/*`;

  // Everything else is workspace-scoped; the leading segment is the slug.
  const rest = segments.slice(1);
  if (rest.length === 0) return "/:slug";

  const scoped = matchRoute(rest, WORKSPACE_ROUTES);
  if (scoped) return `/:slug/${scoped.join("/")}`;

  const section = rest[0]!;
  return WORKSPACE_SECTIONS.has(section)
    ? `/:slug/${section}/*`
    : "/:slug/*";
}

/**
 * Find the route this path follows. A `:param` slot accepts any single segment;
 * every other slot must match literally. When several patterns fit, the most
 * literal one wins, so `/agents/new` is the create page rather than an agent
 * whose id happens to be "new".
 */
function matchRoute(
  segments: readonly string[],
  patterns: readonly RoutePattern[],
): RoutePattern | null {
  let best: RoutePattern | null = null;
  let bestLiterals = -1;

  for (const pattern of patterns) {
    if (pattern.length !== segments.length) continue;

    let literals = 0;
    let matches = true;
    for (let i = 0; i < pattern.length; i += 1) {
      const slot = pattern[i]!;
      if (slot.startsWith(":")) continue;
      if (slot !== segments[i]) {
        matches = false;
        break;
      }
      literals += 1;
    }

    if (matches && literals > bestLiterals) {
      best = pattern;
      bestLiterals = literals;
    }
  }

  return best;
}
