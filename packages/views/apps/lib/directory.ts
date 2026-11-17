import type {
  AppAccountScope,
  AppCatalogEntry,
  AppTeamRequest,
  AppsOverview,
} from "@multica/core/types";
import { accountFor, appAccountStatus, type AppAccountStatus } from "../../common/app-status";

/** The one thing a card offers. Connecting always goes through the connect
 * dialog; "set up" opens the app's page. */
export type AppCardAction =
  | { kind: "connect"; scope: AppAccountScope; reconnect: boolean }
  | { kind: "ask_admin" }
  | { kind: "requested" }
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
  expired: 3,
  needs_your_account: 2,
  no_team_account: 1,
  connected: 0,
};

const POPULAR_LIMIT = 6;

/**
 * Groups the workspace's apps for the Apps page. `agentIds` limits agent
 * usage to the agents the viewer can see (archived ones excluded).
 */
export function buildAppDirectory({
  overview,
  catalog,
  viewerId,
  agentIds,
}: {
  overview: AppsOverview;
  catalog: AppCatalogEntry[];
  viewerId: string | null;
  agentIds?: ReadonlySet<string>;
}): AppDirectory {
  const entries = new Map(catalog.map((entry) => [entry.slug, entry]));
  const slugs = new Set<string>(catalog.map((entry) => entry.slug));
  for (const c of [...overview.team, ...overview.personal]) {
    if (c.status !== "revoked") slugs.add(c.toolkit_slug);
  }
  const agentApps = overview.agent_apps.filter((a) => !agentIds || agentIds.has(a.agent_id));
  for (const a of agentApps) slugs.add(a.toolkit_slug);

  const apps = new Map(
    [...slugs].map((slug) => [
      slug,
      describeApp(slug, entries.get(slug), overview, agentApps, viewerId),
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
  agentApps: AppsOverview["agent_apps"],
  viewerId: string | null,
): DirectoryApp {
  const uses = agentApps.filter((a) => a.toolkit_slug === slug);
  const usedScopes = new Set(uses.map((a) => a.account_scope));
  const request = overview.team_requests?.find(
    (r) => r.toolkit_slug === slug && r.requested_by.length > 0,
  );
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
    appAccountStatus(overview, "team", slug) === "expired" ||
    appAccountStatus(overview, "personal", slug) === "expired" ||
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
    action: cardAction(status, statusScope, overview.can_manage, request, viewerId),
  };
}

function cardAction(
  status: AppAccountStatus | null,
  scope: AppAccountScope | null,
  canManage: boolean,
  request: AppTeamRequest | undefined,
  viewerId: string | null,
): AppCardAction | null {
  if (status === null || scope === null) return { kind: "set_up" };
  switch (status) {
    case "connected":
      return null;
    case "expired":
      return scope === "personal" || canManage
        ? { kind: "connect", scope, reconnect: true }
        : null;
    case "needs_your_account":
      return { kind: "connect", scope: "personal", reconnect: false };
    case "no_team_account":
      if (canManage) return { kind: "connect", scope: "team", reconnect: false };
      return viewerId && request?.requested_by.includes(viewerId)
        ? { kind: "requested" }
        : { kind: "ask_admin" };
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
