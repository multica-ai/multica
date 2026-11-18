"use client";

import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useActorName } from "@multica/core/workspace/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  useDismissTeamAppRequests,
  useRequestTeamApp,
  useRestoreAppAccess,
} from "@multica/core/connected-apps";
import type { AppAccountScope, AppConnection, AppsOverview } from "@multica/core/types";
import { accountFor, appAccountStatus } from "../../common/app-status";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { SavedAppRestriction } from "../../common/saved-app-restriction";
import { ObjectPanelSection } from "../../layout/object-page";
import { useT } from "../../i18n";
import { AccountMenu } from "./account-menu";
import { AppAccountDialog } from "./app-account-dialog";
import { AppStatusLabel } from "./app-status-label";
import { DisconnectPersonalAppDialog } from "./disconnect-personal-dialog";
import { DisconnectTeamAppDialog } from "./disconnect-team-dialog";

function errorMessage(e: unknown, fallback: string) {
  return e instanceof Error && e.message ? e.message : fallback;
}

/** The viewer's own account and the workspace's team account for this app. */
export function AppAccountsCard({
  slug,
  name,
  logo,
  overview,
}: {
  slug: string;
  name: string;
  logo?: string;
  overview: AppsOverview;
}) {
  const { t } = useT("settings");
  const [connecting, setConnecting] = useState<AppAccountScope | null>(null);
  const [activity, setActivity] = useState<AppAccountScope | null>(null);
  const [disconnecting, setDisconnecting] = useState<{ scope: AppAccountScope; connection: AppConnection } | null>(null);
  const handlers = {
    connect: setConnecting,
    activity: setActivity,
    disconnect: (scope: AppAccountScope, connection: AppConnection) => setDisconnecting({ scope, connection }),
  };

  return (
    <ObjectPanelSection title={t(($) => $.connected_apps.app_page.accounts_title)}>
      <div className="space-y-1">
        <PersonalAccountRow slug={slug} name={name} overview={overview} on={handlers} />
        <TeamAccountRow slug={slug} name={name} overview={overview} on={handlers} />
      </div>

      {connecting && (
        <ConnectAppDialog
          open
          onOpenChange={(open) => { if (!open) setConnecting(null); }}
          scope={connecting}
          slug={slug}
          name={name}
          logo={logo}
        />
      )}
      {activity && <AppAccountDialog scope={activity} slug={slug} name={name} onClose={() => setActivity(null)} />}
      {disconnecting?.scope === "personal" && (
        <DisconnectPersonalAppDialog
          connectionId={disconnecting.connection.id}
          name={name}
          onClose={() => setDisconnecting(null)}
        />
      )}
      {disconnecting?.scope === "team" && (
        <DisconnectTeamAppDialog
          connectionId={disconnecting.connection.id}
          slug={slug}
          name={name}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </ObjectPanelSection>
  );
}

interface RowHandlers {
  connect: (scope: AppAccountScope) => void;
  activity: (scope: AppAccountScope) => void;
  disconnect: (scope: AppAccountScope, connection: AppConnection) => void;
}

interface RowProps {
  slug: string;
  name: string;
  overview: AppsOverview;
  on: RowHandlers;
}

function PersonalAccountRow({ slug, overview, on }: RowProps) {
  const { t } = useT("settings");
  const restore = useRestoreAppAccess();
  const account = accountFor(overview, "personal", slug);
  const status = appAccountStatus(overview, "personal", slug);
  const label = t(($) => $.connected_apps.app_page.your_account);
  const used = overview.agent_apps.some((a) => a.toolkit_slug === slug && a.account_scope === "personal");

  return (
    <AccountRow
      label={label}
      status={account || used ? <AppStatusLabel status={status} /> : <NotConnected />}
      actions={
        <>
          {status !== "connected" && (
            <Button size="xs" variant="outline" onClick={() => on.connect("personal")}>
              {status === "expired" ? t(($) => $.connected_apps.hub.reconnect) : t(($) => $.connected_apps.hub.connect)}
            </Button>
          )}
          {account && (
            <AccountMenu
              label={t(($) => $.connected_apps.app_page.account_actions, { account: label })}
              onActivity={() => on.activity("personal")}
              onDisconnect={() => on.disconnect("personal", account)}
            />
          )}
        </>
      }
    >
      {account?.actions_blocked && (
        <SavedAppRestriction kind="account" onRestore={() => restore.mutateAsync({ slug, scope: "personal" })} />
      )}
    </AccountRow>
  );
}

function TeamAccountRow({ slug, name, overview, on }: RowProps) {
  const { t } = useT("settings");
  const { getActorName } = useActorName();
  const viewerId = useAuthStore((s) => s.user?.id ?? null);
  const restore = useRestoreAppAccess();
  const requestTeam = useRequestTeamApp();
  const dismiss = useDismissTeamAppRequests();
  const account = accountFor(overview, "team", slug);
  const status = appAccountStatus(overview, "team", slug);
  const label = t(($) => $.connected_apps.app_page.team_account);
  const used = overview.agent_apps.some((a) => a.toolkit_slug === slug && a.account_scope === "team");
  const request = account
    ? undefined
    : overview.team_requests?.find((r) => r.toolkit_slug === slug && r.requested_by.length > 0);
  const requested = !!viewerId && !!request?.requested_by.includes(viewerId);

  const ask = () =>
    requestTeam.mutateAsync({ slug }).then(
      () => toast.success(t(($) => $.connected_apps.hub.toast_requested, { app: name })),
      (e: unknown) => toast.error(errorMessage(e, t(($) => $.connected_apps.toast_failed))),
    );
  const dismissRequests = () =>
    dismiss.mutateAsync(slug).then(
      () => toast.success(t(($) => $.connected_apps.app_page.toast_dismissed)),
      (e: unknown) => toast.error(errorMessage(e, t(($) => $.connected_apps.toast_failed))),
    );

  let actions: ReactNode = null;
  if (overview.can_manage) {
    actions = (
      <>
        {status !== "connected" && (
          <Button size="xs" variant="outline" onClick={() => on.connect("team")}>
            {status === "expired" ? t(($) => $.connected_apps.hub.reconnect) : t(($) => $.connected_apps.hub.connect)}
          </Button>
        )}
        {account && (
          <AccountMenu
            label={t(($) => $.connected_apps.app_page.account_actions, { account: label })}
            onActivity={() => on.activity("team")}
            onDisconnect={() => on.disconnect("team", account)}
          />
        )}
      </>
    );
  } else if (!account) {
    actions = requested ? (
      <Button size="xs" variant="outline" disabled>
        {t(($) => $.connected_apps.hub.requested)}
      </Button>
    ) : (
      <Button size="xs" variant="outline" disabled={requestTeam.isPending} onClick={ask}>
        {t(($) => $.connected_apps.hub.ask_admin)}
      </Button>
    );
  }

  return (
    <AccountRow
      label={label}
      status={account || used ? <AppStatusLabel status={status} /> : <NotConnected />}
      actions={actions}
    >
      {account?.actions_blocked && (
        <SavedAppRestriction
          kind="account"
          onRestore={overview.can_manage ? () => restore.mutateAsync({ slug, scope: "team" }) : undefined}
        />
      )}
      {overview.can_manage && request && (
        <div className="flex items-center gap-2 text-caption text-muted-foreground">
          <span className="min-w-0 flex-1 truncate">
            {t(($) => $.connected_apps.hub.asked_by, {
              names: request.requested_by.map((id) => getActorName("member", id)).join(", "),
            })}
          </span>
          <Button size="xs" variant="ghost" disabled={dismiss.isPending} onClick={dismissRequests}>
            {t(($) => $.connected_apps.app_page.dismiss)}
          </Button>
        </div>
      )}
    </AccountRow>
  );
}

function AccountRow({
  label,
  status,
  actions,
  children,
}: {
  label: string;
  status: ReactNode;
  actions: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="space-y-2 py-1.5">
      <div className="flex items-center gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5 text-caption">
          <p className="font-medium">{label}</p>
          {status}
        </div>
        <div className="flex shrink-0 items-center gap-1">{actions}</div>
      </div>
      {children}
    </div>
  );
}

function NotConnected() {
  const { t } = useT("settings");
  return <p className="text-muted-foreground">{t(($) => $.connected_apps.app_page.not_connected)}</p>;
}
