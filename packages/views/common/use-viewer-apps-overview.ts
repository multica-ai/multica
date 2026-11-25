"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentMember } from "@multica/core/permissions";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { AppsOverview } from "@multica/core/types";
import { agentAppsForViewer } from "./app-status";

/**
 * The overview with its agent apps narrowed by `agentAppsForViewer`. Until the
 * agents and the viewer's membership load it lists no agent apps, rather than
 * naming agents that may not be the viewer's.
 */
export function useViewerAppsOverview(wsId: string, overview: AppsOverview | undefined): AppsOverview | undefined {
  const agents = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const { userId, role, isLoading } = useCurrentMember(wsId);
  return useMemo(() => {
    if (!overview) return undefined;
    const agentApps = agents.data && !isLoading
      ? agentAppsForViewer(overview.agent_apps, agents.data, { userId, role })
      : [];
    return { ...overview, agent_apps: agentApps };
  }, [overview, agents.data, isLoading, userId, role]);
}
