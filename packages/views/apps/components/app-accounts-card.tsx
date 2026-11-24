"use client";

import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { AppAccountScope, AppConnection, AppsOverview } from "@multica/core/types";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { ObjectSection, ObjectSectionCard } from "../../layout/object-page";
import { useT } from "../../i18n";
import { AccountRow, type AccountRowHandlers } from "./account-row";
import { AppAccountDialog } from "./app-account-dialog";
import { DisconnectAccountDialog } from "./disconnect-account-dialog";

/** The workspace's team account and the viewer's own account for this app. */
export function AppAccountsSection({
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
  const workspace = useCurrentWorkspace()?.name ?? t(($) => $.page.workspace_fallback);
  const [connecting, setConnecting] = useState<AppAccountScope | null>(null);
  const [activity, setActivity] = useState<AppAccountScope | null>(null);
  const [disconnecting, setDisconnecting] = useState<{ scope: AppAccountScope; connection: AppConnection } | null>(null);
  const on: AccountRowHandlers = {
    connect: setConnecting,
    activity: setActivity,
    disconnect: (scope, connection) => setDisconnecting({ scope, connection }),
  };

  return (
    <ObjectSection title={t(($) => $.connected_apps.app_page.accounts_title)}>
      <p className="-mt-1 text-caption text-muted-foreground">
        {t(($) => $.connected_apps.app_page.accounts_description, { app: name, workspace })}
      </p>
      <ObjectSectionCard>
        <AccountRow
          scope="team"
          slug={slug}
          name={name}
          overview={overview}
          title={t(($) => $.connected_apps.owner.team)}
          subtitle={
            <p className="truncate text-caption text-muted-foreground">
              {t(($) => $.connected_apps.app_page.team_account_hint, { workspace })}
            </p>
          }
          on={on}
        />
        <AccountRow
          scope="personal"
          slug={slug}
          name={name}
          overview={overview}
          title={t(($) => $.connected_apps.owner.personal)}
          subtitle={
            <p className="truncate text-caption text-muted-foreground">
              {t(($) => $.connected_apps.app_page.your_account_hint)}
            </p>
          }
          on={on}
        />
      </ObjectSectionCard>

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
      {disconnecting && (
        <DisconnectAccountDialog
          scope={disconnecting.scope}
          connectionId={disconnecting.connection.id}
          slug={slug}
          name={name}
          logo={logo}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </ObjectSection>
  );
}
