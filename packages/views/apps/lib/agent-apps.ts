import type {
  AppAccountScope,
  AppCatalogEntry,
  AppsOverview,
  UpdateAgentAppsRequest,
} from "@multica/core/types";
import { appAccountStatus } from "../../common/app-status";

/**
 * An agent's full app list with one app set to `scope`, or removed when
 * `scope` is null. Saving replaces the agent's whole list, so its other apps
 * and every level ride along unchanged.
 */
export function agentAppsWith(
  overview: AppsOverview,
  agentId: string,
  slug: string,
  scope: AppAccountScope | null,
): UpdateAgentAppsRequest {
  const current = overview.agent_apps.filter((a) => a.agent_id === agentId);
  const existing = current.find((a) => a.toolkit_slug === slug);
  const others = current.filter((a) => a.toolkit_slug !== slug);
  const apps = others.map((a) => ({
    toolkit_slug: a.toolkit_slug,
    account_scope: a.account_scope,
    level: a.level,
  }));
  if (scope) {
    apps.push({ toolkit_slug: slug, account_scope: scope, level: existing?.level ?? "write" });
  }
  return { apps };
}

/**
 * The account an agent newly given this app uses. Only an admin can give an
 * agent the team account, and only one the workspace has connected; it wins
 * when the app is usually a team app or the admin has no account of their own.
 */
export function defaultAgentScope(
  overview: AppsOverview,
  slug: string,
  entry: AppCatalogEntry | undefined,
): AppAccountScope {
  if (!overview.can_manage) return "personal";
  if (appAccountStatus(overview, "team", slug) !== "connected") return "personal";
  if (entry?.recommended_scope === "team") return "team";
  return appAccountStatus(overview, "personal", slug) === "connected" ? "personal" : "team";
}
