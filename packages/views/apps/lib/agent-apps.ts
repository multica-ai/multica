import type { AppAccountScope, AppsOverview, UpdateAgentAppsRequest } from "@multica/core/types";

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

