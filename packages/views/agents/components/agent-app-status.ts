import type { AppAccountScope, AppsOverview } from "@multica/core/types";
import { appAccountStatus, type AppAccountStatus } from "../../common/app-status";

/**
 * The account a newly added app uses. Only an admin can give an agent the
 * team account, only one the workspace has connected, and only while the plan
 * includes team accounts (`canAddTeam`); it wins when the app is usually a
 * team app or the admin has no account of their own.
 */
export function scopeForNewApp(
  overview: AppsOverview | undefined,
  slug: string,
  recommended: AppAccountScope | undefined,
  canAddTeam: boolean,
): AppAccountScope {
  if (!overview?.can_manage || !canAddTeam) return "personal";
  if (appAccountStatus(overview, "team", slug) !== "connected") return "personal";
  if (recommended === "team") return "team";
  return appAccountStatus(overview, "personal", slug) === "connected" ? "personal" : "team";
}

/** The Properties panel's Apps line scrolls to this element. */
export function agentAppsSectionId(agentId: string) {
  return `agent-apps-${agentId}`;
}

/** A personal-account app the viewer cannot run the agent with yet. */
export function needsViewerAccount(scope: AppAccountScope, status: AppAccountStatus): boolean {
  return scope === "personal" && status !== "connected";
}
