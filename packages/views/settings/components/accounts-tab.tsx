"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  appCatalogOptions,
  appsOverviewOptions,
  myPendingAppRequestsOptions,
} from "@multica/core/connected-apps";
import type { AppAccountScope, AppCatalogEntry, AppConnection, AppRequest, AppsOverview } from "@multica/core/types";
import { appAccountStatus, teamRequestFor, type AppAccountStatus } from "../../common/app-status";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { AccountOwner, AccountRow, type AccountRowHandlers } from "../../apps/components/account-row";
import { AppAccountDialog } from "../../apps/components/app-account-dialog";
import { DisconnectAccountDialog } from "../../apps/components/disconnect-account-dialog";
import { useAppConnectReturn } from "../../apps/hooks/use-app-connect-return";
import { useT } from "../../i18n";
import { AddAppDialog } from "./add-app-dialog";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

type AppRef = { slug: string; name: string; logo?: string };

// Rows that need someone to act come first.
const ORDER: Record<AppAccountStatus, number> = {
  needs_reconnecting: 0,
  requested: 1,
  not_connected: 2,
  connected: 3,
};

/**
 * The apps involved in one owner's accounts: those connected, those agents
 * use through that owner, and, for the team, those someone is waiting for;
 * for the viewer, those an agent is waiting on them to connect.
 */
function accountSlugs(overview: AppsOverview, scope: AppAccountScope, waiting: AppRequest[]): string[] {
  const slugs = new Set<string>();
  for (const c of overview[scope]) if (c.status !== "revoked") slugs.add(c.toolkit_slug);
  for (const a of overview.agent_apps) if (a.account_scope === scope) slugs.add(a.toolkit_slug);
  if (scope === "team") for (const r of overview.team_requests ?? []) slugs.add(r.toolkit_slug);
  else for (const r of waiting) slugs.add(r.toolkit_slug);
  return [...slugs];
}

/** The accounts agents in this workspace act as: the team's and the viewer's. */
export function AccountsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace()?.name ?? t(($) => $.page.workspace_fallback);
  const overview = useQuery(appsOverviewOptions(wsId));
  const catalog = useQuery(appCatalogOptions(wsId));
  const requests = useQuery(myPendingAppRequestsOptions(wsId));
  const [adding, setAdding] = useState<AppAccountScope | null>(null);
  const [connecting, setConnecting] = useState<{ scope: AppAccountScope; app: AppRef } | null>(null);
  const [activity, setActivity] = useState<{ scope: AppAccountScope; app: AppRef } | null>(null);
  const [disconnecting, setDisconnecting] = useState<{ scope: AppAccountScope; connection: AppConnection; app: AppRef } | null>(null);
  useAppConnectReturn({ tab: "accounts" });

  const entries = new Map<string, AppCatalogEntry>((catalog.data?.apps ?? []).map((a) => [a.slug, a]));
  const appRef = (slug: string): AppRef => {
    const entry = entries.get(slug);
    return { slug, name: entry?.name || slug, logo: entry?.logo };
  };
  const waiting = (requests.data ?? []).filter(
    (r) => r.kind === "connect" && r.status === "pending" && r.account_scope === "personal",
  );
  const data = overview.data;
  const usage = useUsage(data, waiting);

  const handlersFor = (app: AppRef): AccountRowHandlers => ({
    connect: (scope) => setConnecting({ scope, app }),
    activity: (scope) => setActivity({ scope, app }),
    disconnect: (scope, connection) => setDisconnecting({ scope, connection, app }),
  });

  const rows = (scope: AppAccountScope) => {
    if (!data) return [];
    return accountSlugs(data, scope, waiting)
      .map((slug) => ({ app: appRef(slug), status: appAccountStatus(data, scope, slug) }))
      .sort((a, b) => ORDER[a.status] - ORDER[b.status] || a.app.name.localeCompare(b.app.name));
  };

  const group = (scope: AppAccountScope) => {
    const list = rows(scope);
    if (overview.isLoading) {
      return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
    }
    if (overview.isError || !data) {
      return <p className="text-caption text-destructive">{t(($) => $.connected_apps.load_failed)}</p>;
    }
    if (list.length === 0) {
      return (
        <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
          {scope === "team"
            ? t(($) => $.connected_apps.accounts_page.team_empty)
            : t(($) => $.connected_apps.accounts_page.empty)}
        </p>
      );
    }
    return (
      <SettingsCard>
        {list.map(({ app }) => (
          <AccountRow
            key={app.slug}
            scope={scope}
            slug={app.slug}
            name={app.name}
            logo={app.logo}
            showLogo
            overview={data}
            title={app.name}
            subtitle={<AccountOwner scope={scope} detail={usage(scope, app.slug)} />}
            on={handlersFor(app)}
          />
        ))}
      </SettingsCard>
    );
  };

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.accounts)}
      description={t(($) => $.connected_apps.accounts_page.description, { workspace })}
      scope="workspace"
    >
      <SettingsSection
        title={t(($) => $.connected_apps.accounts_page.team_title)}
        description={
          data?.can_manage
            ? t(($) => $.connected_apps.accounts_page.team_description_admin)
            : t(($) => $.connected_apps.accounts_page.team_description_member)
        }
        action={
          data?.can_manage ? (
            <Button size="sm" variant="outline" onClick={() => setAdding("team")}>
              <Plus />
              {t(($) => $.connected_apps.accounts_page.connect_team_account)}
            </Button>
          ) : null
        }
      >
        {group("team")}
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.connected_apps.accounts_page.yours_title)}
        description={t(($) => $.connected_apps.accounts_page.yours_description)}
        action={
          <Button size="sm" variant="outline" disabled={!data} onClick={() => setAdding("personal")}>
            <Plus />
            {t(($) => $.connected_apps.accounts_page.connect_account)}
          </Button>
        }
      >
        {group("personal")}
      </SettingsSection>

      {adding && data && (
        <AddAppDialog
          open
          onOpenChange={(open) => { if (!open) setAdding(null); }}
          canManage={data.can_manage}
          scope={adding}
          overview={data}
          catalog={catalog.data?.apps ?? []}
          catalogLoading={catalog.isLoading}
          catalogError={catalog.isError}
        />
      )}
      {connecting && (
        <ConnectAppDialog
          open
          onOpenChange={(open) => { if (!open) setConnecting(null); }}
          scope={connecting.scope}
          slug={connecting.app.slug}
          name={connecting.app.name}
          logo={connecting.app.logo}
        />
      )}
      {activity && (
        <AppAccountDialog
          scope={activity.scope}
          slug={activity.app.slug}
          name={activity.app.name}
          onClose={() => setActivity(null)}
        />
      )}
      {disconnecting && (
        <DisconnectAccountDialog
          scope={disconnecting.scope}
          connectionId={disconnecting.connection.id}
          slug={disconnecting.app.slug}
          name={disconnecting.app.name}
          logo={disconnecting.app.logo}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </SettingsTab>
  );
}

/** Who relies on the account: the agents acting as it, or who waits for it. */
function useUsage(overview: AppsOverview | undefined, waiting: AppRequest[]) {
  const { t } = useT("settings");
  const { getActorName } = useActorName();
  return (scope: AppAccountScope, slug: string): string | undefined => {
    if (!overview) return undefined;
    const agentIds = [...new Set(overview.agent_apps.filter((a) => a.toolkit_slug === slug && a.account_scope === scope).map((a) => a.agent_id))];
    const waitingAgents = scope === "team"
      ? (teamRequestFor(overview, slug)?.agent_ids ?? [])
      : waiting.filter((r) => r.toolkit_slug === slug).map((r) => r.agent_id);
    const needing = [...new Set(waitingAgents)].filter(Boolean);
    if (needing.length > 0 && appAccountStatus(overview, scope, slug) !== "connected") {
      return t(($) => $.connected_apps.accounts_page.needed_by, { agents: names(needing, getActorName) });
    }
    if (agentIds.length === 0) return undefined;
    return t(($) => $.connected_apps.accounts_page.used_by, { agents: names(agentIds, getActorName) });
  };
}

function names(ids: string[], getActorName: (type: "agent", id: string) => string): string {
  return ids.map((id) => getActorName("agent", id)).join(", ");
}
