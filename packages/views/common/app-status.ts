import { canAssignAgentToIssue } from "@multica/core/permissions/rules";
import type { PermissionContext } from "@multica/core/permissions";
import type { Agent, AgentApp, AppAccountScope, AppConnection, AppTeamRequest, AppsOverview } from "@multica/core/types";

/**
 * Where an account stands. Every Apps surface (the agent page, the app page,
 * the Apps directory, settings) labels accounts with these values, next to
 * whose account it is, so one state never reads two ways.
 */
export type AppAccountStatus =
  | "connected"
  | "not_connected"
  | "needs_reconnecting"
  | "requested";

/** The one action an account offers the viewer. */
export type AppAccountAction = "connect" | "reconnect" | "upgrade" | "ask_admin" | "withdraw";

/** The account an app on an agent would use for the viewer, if any. */
export function accountFor(
  overview: AppsOverview | undefined,
  scope: AppAccountScope,
  slug: string,
): AppConnection | undefined {
  const rows = scope === "team" ? overview?.team : overview?.personal;
  return rows?.find((c) => c.toolkit_slug === slug && c.status !== "revoked");
}

/** What is waiting for the team account of an app the workspace lacks. */
export function teamRequestFor(overview: AppsOverview | undefined, slug: string): AppTeamRequest | undefined {
  if (accountFor(overview, "team", slug)?.status === "active") return undefined;
  return overview?.team_requests?.find((r) => r.toolkit_slug === slug && r.requested_by.length > 0);
}

export function appAccountStatus(
  overview: AppsOverview | undefined,
  scope: AppAccountScope,
  slug: string,
): AppAccountStatus {
  const account = accountFor(overview, scope, slug);
  if (account?.status === "active") return "connected";
  if (account?.status === "expired") return "needs_reconnecting";
  if (scope === "team" && teamRequestFor(overview, slug)) return "requested";
  return "not_connected";
}

/** Whether the status needs someone to act before agents can use the app. */
export function needsAttention(status: AppAccountStatus): boolean {
  return status !== "connected";
}

/**
 * The action for an account, from whose it is, where it stands, and who is
 * looking: anyone connects their own; admins connect the team's (or upgrade
 * when the plan leaves team accounts out); members ask an admin, then can
 * withdraw the ask.
 */
export function accountAction({
  scope,
  status,
  canManage,
  needsUpgrade,
  viewerAsk,
}: {
  scope: AppAccountScope;
  status: AppAccountStatus;
  canManage: boolean;
  needsUpgrade: boolean;
  viewerAsk: ViewerTeamAsk;
}): AppAccountAction | null {
  if (status === "connected") return null;
  if (scope === "personal") return status === "needs_reconnecting" ? "reconnect" : "connect";
  if (canManage) {
    if (status === "needs_reconnecting") return "reconnect";
    return needsUpgrade ? "upgrade" : "connect";
  }
  if (viewerAsk === "asked") return "withdraw";
  // A run the viewer started already waits on it; the admins know.
  if (viewerAsk === "waiting") return null;
  return "ask_admin";
}

/**
 * How the viewer is waiting for an app's team account: they asked an admin
 * (and can withdraw it), or only a run they started waits on it.
 */
export type ViewerTeamAsk = "asked" | "waiting" | null;

export function viewerTeamAsk(
  overview: AppsOverview | undefined,
  slug: string,
  viewerId: string | null | undefined,
): ViewerTeamAsk {
  const request = teamRequestFor(overview, slug);
  if (!viewerId || !request) return null;
  if ((request.asked_by ?? request.requested_by).includes(viewerId)) return "asked";
  return request.requested_by.includes(viewerId) ? "waiting" : null;
}

/**
 * The agent apps that involve the viewer's accounts: those of live agents,
 * and for personal accounts only agents the viewer may run. An agent acts
 * through the personal account of whoever starts its run, so another
 * member's private agent never uses the viewer's account. Mirrors the
 * server's personal disconnect impact.
 */
export function agentAppsForViewer(
  agentApps: AgentApp[],
  agents: Agent[],
  viewer: PermissionContext,
): AgentApp[] {
  const byId = new Map(agents.map((agent) => [agent.id, agent]));
  return agentApps.filter((app) => {
    const agent = byId.get(app.agent_id);
    if (!agent || agent.archived_at) return false;
    return app.account_scope === "team" || canAssignAgentToIssue(agent, viewer).allowed;
  });
}
