import type { AppAccountScope, AppConnection, AppsOverview } from "@multica/core/types";

/**
 * Where an app stands for the person looking at it. Every Apps surface (the
 * agent page, the Apps directory, settings) labels apps with these values so
 * one state never reads two ways.
 */
export type AppAccountStatus =
  | "connected"
  | "expired"
  | "needs_your_account"
  | "no_team_account";

/** The account an app on an agent would use for the viewer, if any. */
export function accountFor(
  overview: AppsOverview | undefined,
  scope: AppAccountScope,
  slug: string,
): AppConnection | undefined {
  const rows = scope === "team" ? overview?.team : overview?.personal;
  return rows?.find((c) => c.toolkit_slug === slug && c.status !== "revoked");
}

export function appAccountStatus(
  overview: AppsOverview | undefined,
  scope: AppAccountScope,
  slug: string,
): AppAccountStatus {
  const account = accountFor(overview, scope, slug);
  if (account?.status === "active") return "connected";
  if (account?.status === "expired") return "expired";
  return scope === "team" ? "no_team_account" : "needs_your_account";
}

/** Whether the status needs someone to act before agents can use the app. */
export function needsAttention(status: AppAccountStatus): boolean {
  return status !== "connected";
}
