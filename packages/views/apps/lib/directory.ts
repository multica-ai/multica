import type {
  AppAccountScope,
  AppCatalogEntry,
  AppTeamRequest,
  AppsOverview,
} from "@multica/core/types";
import {
  accountAction,
  accountFor,
  appAccountStatus,
  teamRequestFor,
  viewerTeamAsk,
  type AppAccountStatus,
} from "../../common/app-status";

/** The one thing a card offers. Connecting always goes through the connect
 * dialog; "set up" opens the app's page. */
export type AppCardAction =
  | { kind: "connect"; scope: AppAccountScope; reconnect: boolean }
  | { kind: "ask_admin" }
  | { kind: "withdraw" }
  | { kind: "set_up" };

export interface DirectoryApp {
  slug: string;
  name: string;
  entry?: AppCatalogEntry;
  /** Accounts the app involves: connected ones and the ones agents use. */
  scopes: AppAccountScope[];
  /** The status to show, the most pressing of `scopes`; null when unused. */
  status: AppAccountStatus | null;
  statusScope: AppAccountScope | null;
  agentIds: string[];
  /** Open team-account request, when the workspace has no team account. */
  request?: AppTeamRequest;
  inUse: boolean;
  needsAttention: boolean;
  action: AppCardAction | null;
}

export interface AppDirectory {
  inUse: DirectoryApp[];
  attention: DirectoryApp[];
  all: DirectoryApp[];
  /** Curated apps nobody uses yet. */
  popular: DirectoryApp[];
}

const SEVERITY: Record<AppAccountStatus, number> = {
  needs_reconnecting: 3,
  not_connected: 2,
  requested: 1,
  connected: 0,
};

const POPULAR_LIMIT = 6;

/**
 * Groups the workspace's apps for the Apps page. Agent usage comes from
 * `overview.agent_apps` as given; callers narrow it to the viewer's
 * (`agentAppsForViewer`).
 */
export function buildAppDirectory({
  overview,
  catalog,
  viewerId,
}: {
  overview: AppsOverview;
  catalog: AppCatalogEntry[];
  viewerId: string | null;
}): AppDirectory {
  const entries = new Map(catalog.map((entry) => [entry.slug, entry]));
  const slugs = new Set<string>(catalog.map((entry) => entry.slug));
  for (const c of [...overview.team, ...overview.personal]) {
    if (c.status !== "revoked") slugs.add(c.toolkit_slug);
  }
  for (const a of overview.agent_apps) slugs.add(a.toolkit_slug);

  const apps = new Map(
    [...slugs].map((slug) => [
      slug,
      describeApp(slug, entries.get(slug), overview, viewerId),
    ]),
  );
  const described = [...apps.values()];
  const byName = (a: DirectoryApp, b: DirectoryApp) => a.name.localeCompare(b.name);
  const inCatalog = catalog.map((entry) => apps.get(entry.slug)!);
  return {
    inUse: described
      .filter((app) => app.inUse)
      .sort((a, b) => Number(b.needsAttention) - Number(a.needsAttention) || byName(a, b)),
    attention: described.filter((app) => app.needsAttention).sort(byName),
    all: inCatalog.concat(described.filter((app) => !entries.has(app.slug)).sort(byName)),
    popular: inCatalog
      .filter((app) => app.entry?.curated && !app.inUse && !app.request)
      .slice(0, POPULAR_LIMIT),
  };
}

function describeApp(
  slug: string,
  entry: AppCatalogEntry | undefined,
  overview: AppsOverview,
  viewerId: string | null,
): DirectoryApp {
  const uses = overview.agent_apps.filter((a) => a.toolkit_slug === slug);
  const usedScopes = new Set(uses.map((a) => a.account_scope));
  const request = teamRequestFor(overview, slug);
  const hasTeam = !!accountFor(overview, "team", slug);
  const hasPersonal = !!accountFor(overview, "personal", slug);
  const scopes: AppAccountScope[] = [];
  if (hasTeam || usedScopes.has("team") || (request && !hasTeam)) scopes.push("team");
  if (hasPersonal || usedScopes.has("personal")) scopes.push("personal");

  let status: AppAccountStatus | null = null;
  let statusScope: AppAccountScope | null = null;
  for (const scope of scopes) {
    const next = appAccountStatus(overview, scope, slug);
    if (status === null || SEVERITY[next] > SEVERITY[status]) {
      status = next;
      statusScope = scope;
    }
  }

  const blocked = (scope: AppAccountScope) =>
    usedScopes.has(scope) && appAccountStatus(overview, scope, slug) !== "connected";
  const needsAttention =
    blocked("team") ||
    blocked("personal") ||
    appAccountStatus(overview, "team", slug) === "needs_reconnecting" ||
    appAccountStatus(overview, "personal", slug) === "needs_reconnecting" ||
    (overview.can_manage && !!request && !hasTeam);

  return {
    slug,
    name: entry?.name || slug,
    entry,
    scopes,
    status,
    statusScope,
    agentIds: [...new Set(uses.map((a) => a.agent_id))],
    request: hasTeam ? undefined : request,
    inUse: hasTeam || hasPersonal || uses.length > 0,
    needsAttention,
    action: cardAction(slug, status, statusScope, overview, viewerId),
  };
}

function cardAction(
  slug: string,
  status: AppAccountStatus | null,
  scope: AppAccountScope | null,
  overview: AppsOverview,
  viewerId: string | null,
): AppCardAction | null {
  if (status === null || scope === null) return { kind: "set_up" };
  const action = accountAction({
    scope,
    status,
    canManage: overview.can_manage,
    // The card swaps Connect for the upgrade link itself.
    needsUpgrade: false,
    viewerAsk: scope === "team" ? viewerTeamAsk(overview, slug, viewerId) : null,
  });
  switch (action) {
    case "connect":
    case "upgrade":
      return { kind: "connect", scope, reconnect: false };
    case "reconnect":
      return { kind: "connect", scope, reconnect: true };
    case "ask_admin":
      return { kind: "ask_admin" };
    case "withdraw":
      return { kind: "withdraw" };
    case null:
      return null;
  }
}

export function matchesAppSearch(app: DirectoryApp, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (
    app.name.toLowerCase().includes(q) ||
    app.slug.includes(q) ||
    (app.entry?.category ?? "").toLowerCase().includes(q)
  );
}
