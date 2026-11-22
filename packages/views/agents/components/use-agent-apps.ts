"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentAppsOptions,
  appCatalogOptions,
  appsOverviewOptions,
} from "@multica/core/connected-apps";
import type { AgentApp, AppCatalogEntry } from "@multica/core/types";
import { appAccountStatus, type AppAccountStatus } from "../../common/app-status";
import { useT } from "../../i18n";

export interface AgentAppRow {
  app: AgentApp;
  name: string;
  logo?: string;
  status: AppAccountStatus;
}

/** The agent's apps with where each stands for the viewer. */
export function useAgentApps(agentId: string, { enabled = true }: { enabled?: boolean } = {}) {
  const wsId = useWorkspaceId();
  const appsQuery = useQuery({ ...agentAppsOptions(wsId, agentId), enabled });
  const overview = useQuery({ ...appsOverviewOptions(wsId), enabled });
  const catalog = useQuery({ ...appCatalogOptions(wsId), enabled });

  const rows = useMemo<AgentAppRow[]>(() => {
    const bySlug = new Map<string, AppCatalogEntry>();
    for (const entry of catalog.data?.apps ?? []) bySlug.set(entry.slug, entry);
    return (appsQuery.data ?? []).map((app) => {
      const entry = bySlug.get(app.toolkit_slug);
      return {
        app,
        name: entry?.name || app.toolkit_slug,
        logo: entry?.logo,
        status: appAccountStatus(overview.data, app.account_scope, app.toolkit_slug),
      };
    });
  }, [appsQuery.data, catalog.data, overview.data]);

  return {
    rows,
    apps: appsQuery.data,
    overview: overview.data,
    catalog: catalog.data?.apps,
    isLoading: appsQuery.isLoading || overview.isLoading,
    isError: appsQuery.isError || overview.isError,
  };
}

/** Where an app stands, in the status vocabulary every Apps surface shares. */
export function useAppStatusLabel() {
  const { t } = useT("settings");
  return (status: AppAccountStatus) => t(($) => $.connected_apps.status[status]);
}
