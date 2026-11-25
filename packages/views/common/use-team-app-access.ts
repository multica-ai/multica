"use client";

import { useWorkspaceId } from "@multica/core/hooks";
import { usePaidFeatureAccess } from "@multica/core/billing";
import type { AppsOverview } from "@multica/core/types";
import { accountFor } from "./app-status";

/**
 * The plan's `team_app_accounts` access for the Apps surfaces. Without it,
 * existing team accounts keep working and can be reauthorized or
 * disconnected; only a team account for a new app, or moving an agent onto
 * the team account, is refused.
 */
export function useTeamAppAccess(overview: AppsOverview | undefined) {
  const wsId = useWorkspaceId();
  const access = usePaidFeatureAccess(wsId, "team_app_accounts", {
    hasExistingConfig: (overview?.team ?? []).some((c) => c.status !== "revoked"),
  });
  const locked = access.state === "read_only" || access.state === "unavailable";
  return {
    ...access,
    /**
     * Whether connecting the team account for `slug` adds a new one the plan
     * is known not to include. Unknown while the plan loads, so Connect never
     * flickers to an upgrade prompt; the server decides either way.
     */
    needsUpgrade: (slug: string) => locked && !accountFor(overview, "team", slug),
  };
}
