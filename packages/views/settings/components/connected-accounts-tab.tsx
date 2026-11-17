"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import {
  appCatalogOptions,
  appsOverviewOptions,
  myPendingAppRequestsOptions,
  personalAppImpactOptions,
  useRestoreAppAccess,
} from "@multica/core/connected-apps";
import type { AppCatalogEntry, AppConnection, AppRequest } from "@multica/core/types";
import { appAccountStatus } from "../../common/app-status";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { SavedAppRestriction } from "../../common/saved-app-restriction";
import { AccountMenu } from "../../apps/components/account-menu";
import { AppAccountDialog } from "../../apps/components/app-account-dialog";
import { AppStatusLabel } from "../../apps/components/app-status-label";
import { DisconnectPersonalAppDialog } from "../../apps/components/disconnect-personal-dialog";
import { useAppConnectReturn } from "../../apps/hooks/use-app-connect-return";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { AddAppDialog } from "./add-app-dialog";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

type AppRef = { slug: string; name: string; logo?: string };

/** The viewer's own app accounts, which follow them into every workspace. */
export function ConnectedAccountsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const overview = useQuery(appsOverviewOptions(wsId));
  const catalog = useQuery(appCatalogOptions(wsId));
  const requests = useQuery(myPendingAppRequestsOptions(wsId));
  const [adding, setAdding] = useState(false);
  const [connecting, setConnecting] = useState<AppRef | null>(null);
  const [activity, setActivity] = useState<AppRef | null>(null);
  const [disconnecting, setDisconnecting] = useState<{ connection: AppConnection; name: string } | null>(null);
  useAppConnectReturn({ tab: "connected-accounts" });

  const entries = new Map<string, AppCatalogEntry>((catalog.data?.apps ?? []).map((a) => [a.slug, a]));
  const appRef = (slug: string): AppRef => {
    const entry = entries.get(slug);
    return { slug, name: entry?.name || slug, logo: entry?.logo };
  };
  const accounts = (overview.data?.personal ?? []).filter((c) => c.status !== "revoked");
  const waiting = (requests.data ?? []).filter(
    (r) => r.kind === "connect" && r.status === "pending" && r.account_scope === "personal",
  );

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.connected_accounts)}
      description={t(($) => $.connected_apps.accounts_page.description)}
      scope="all-workspaces"
    >
      {waiting.length > 0 && (
        <SettingsSection
          title={t(($) => $.connected_apps.accounts_page.waiting_title)}
          description={t(($) => $.connected_apps.accounts_page.waiting_description)}
        >
          <SettingsCard>
            {waiting.map((request) => (
              <WaitingRow
                key={request.id}
                request={request}
                app={appRef(request.toolkit_slug)}
                expired={appAccountStatus(overview.data, "personal", request.toolkit_slug) === "expired"}
                onConnect={setConnecting}
              />
            ))}
          </SettingsCard>
        </SettingsSection>
      )}

      <SettingsSection
        title={t(($) => $.connected_apps.accounts_page.accounts_title)}
        description={t(($) => $.connected_apps.accounts_page.accounts_description)}
        action={
          <Button size="sm" variant="outline" disabled={!overview.data} onClick={() => setAdding(true)}>
            <Plus />
            {t(($) => $.connected_apps.accounts_page.connect_account)}
          </Button>
        }
      >
        {overview.isLoading ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>
        ) : overview.isError ? (
          <p className="text-caption text-destructive">{t(($) => $.connected_apps.load_failed)}</p>
        ) : accounts.length === 0 ? (
          <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
            {t(($) => $.connected_apps.accounts_page.empty)}
          </p>
        ) : (
          <SettingsCard>
            {accounts.map((connection) => {
              const app = appRef(connection.toolkit_slug);
              return (
                <AccountRow
                  key={connection.id}
                  connection={connection}
                  app={app}
                  onReconnect={() => setConnecting(app)}
                  onActivity={() => setActivity(app)}
                  onDisconnect={() => setDisconnecting({ connection, name: app.name })}
                />
              );
            })}
          </SettingsCard>
        )}
        <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.accounts_page.footer)}</p>
      </SettingsSection>

      {adding && overview.data && (
        <AddAppDialog
          open
          onOpenChange={setAdding}
          canManage={overview.data.can_manage}
          scope="personal"
          overview={overview.data}
          catalog={catalog.data?.apps ?? []}
          catalogLoading={catalog.isLoading}
          catalogError={catalog.isError}
        />
      )}
      {connecting && (
        <ConnectAppDialog
          open
          onOpenChange={(open) => { if (!open) setConnecting(null); }}
          scope="personal"
          slug={connecting.slug}
          name={connecting.name}
          logo={connecting.logo}
        />
      )}
      {activity && (
        <AppAccountDialog scope="personal" slug={activity.slug} name={activity.name} onClose={() => setActivity(null)} />
      )}
      {disconnecting && (
        <DisconnectPersonalAppDialog
          connectionId={disconnecting.connection.id}
          name={disconnecting.name}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </SettingsTab>
  );
}

function WaitingRow({
  request,
  app,
  expired,
  onConnect,
}: {
  request: AppRequest;
  app: AppRef;
  expired: boolean;
  onConnect: (app: AppRef) => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { getActorName } = useActorName();
  const issue = useQuery({ ...issueDetailOptions(wsId, request.issue_id ?? ""), enabled: !!request.issue_id });
  const agent = getActorName("agent", request.agent_id);

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ComposioToolkitLogo slug={app.slug} name={app.name} fallbackLogo={app.logo} className="size-8" />
      <p className="min-w-0 flex-1 text-body">
        {request.issue_id
          ? t(($) => $.connected_apps.accounts_page.needs_on_issue, {
              agent,
              app: app.name,
              issue: issue.data?.identifier ?? "…",
            })
          : t(($) => $.connected_apps.accounts_page.needs_in_chat, { agent, app: app.name })}
      </p>
      <div className="flex shrink-0 items-center gap-1.5">
        {request.issue_id ? (
          <Button
            size="sm"
            variant="ghost"
            nativeButton={false}
            render={<AppLink href={paths.issueDetail(request.issue_id)} />}
          >
            {t(($) => $.connected_apps.accounts_page.open_issue)}
          </Button>
        ) : request.chat_session_id ? (
          <Button
            size="sm"
            variant="ghost"
            nativeButton={false}
            render={<AppLink href={paths.chatSession(request.chat_session_id)} />}
          >
            {t(($) => $.connected_apps.accounts_page.open_chat)}
          </Button>
        ) : null}
        <Button size="sm" onClick={() => onConnect(app)}>
          {expired
            ? t(($) => $.connected_apps.accounts_page.reconnect_app, { app: app.name })
            : t(($) => $.connected_apps.accounts_page.connect_app, { app: app.name })}
        </Button>
      </div>
    </div>
  );
}

function AccountRow({
  connection,
  app,
  onReconnect,
  onActivity,
  onDisconnect,
}: {
  connection: AppConnection;
  app: AppRef;
  onReconnect: () => void;
  onActivity: () => void;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const impact = useQuery(personalAppImpactOptions(wsId, connection.id));
  const restore = useRestoreAppAccess();
  const agentNames = [...new Set((impact.data?.agents ?? []).map((a) => a.name))];
  const expired = connection.status === "expired";

  return (
    <div className="space-y-2 px-4 py-3">
      <div className="flex items-center gap-3">
        <ComposioToolkitLogo slug={app.slug} name={app.name} fallbackLogo={app.logo} className="size-8" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-body font-medium">{app.name}</p>
          <p className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
            <AppStatusLabel status={expired ? "expired" : "connected"} className="shrink-0" />
            {agentNames.length > 0 && (
              <span className="truncate">
                · {t(($) => $.connected_apps.accounts_page.used_by, { agents: agentNames.join(", ") })}
              </span>
            )}
          </p>
        </div>
        {expired && (
          <Button size="sm" variant="outline" onClick={onReconnect}>
            {t(($) => $.connected_apps.reconnect)}
          </Button>
        )}
        <AccountMenu
          label={t(($) => $.connected_apps.accounts_page.actions, { app: app.name })}
          onActivity={onActivity}
          onDisconnect={onDisconnect}
        />
      </div>
      {connection.actions_blocked && (
        <SavedAppRestriction kind="account" onRestore={() => restore.mutateAsync({ slug: app.slug, scope: "personal" })} />
      )}
    </div>
  );
}
