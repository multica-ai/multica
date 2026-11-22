"use client";

import { useState } from "react";
import { Plug, Plus, Settings2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import type { Agent, AppAccountScope, AppConnection } from "@multica/core/types";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import { AccountOwner, AccountRow } from "../../apps/components/account-row";
import { AppAccountDialog } from "../../apps/components/app-account-dialog";
import { DisconnectAccountDialog } from "../../apps/components/disconnect-account-dialog";
import {
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionEmpty,
} from "../../layout/object-page";
import { useT } from "../../i18n";
import { AddAgentAppDialog } from "./add-agent-app-dialog";
import { agentAppsSectionId } from "./agent-app-status";
import { useAgentApps, type AgentAppRow } from "./use-agent-apps";

type RowTarget = { scope: AppAccountScope; row: AgentAppRow };

/**
 * The apps the agent works in, whose account each acts as, and where that
 * account stands for the viewer. Everyone sees it; only editors change the
 * list, while anyone can connect their own account or ask an admin for a
 * team one.
 */
export function AgentAppsSection({
  agent,
  canEdit,
  onManage,
}: {
  agent: Agent;
  canEdit: boolean;
  onManage: () => void;
}) {
  const { t } = useT("agents");
  const { rows, overview, isLoading, isError } = useAgentApps(agent.id);
  const [adding, setAdding] = useState(false);
  const [connect, setConnect] = useState<RowTarget | null>(null);
  const [activity, setActivity] = useState<RowTarget | null>(null);
  const [disconnecting, setDisconnecting] = useState<(RowTarget & { connection: AppConnection }) | null>(null);

  return (
    <div id={agentAppsSectionId(agent.id)} tabIndex={-1} className="scroll-mt-6 outline-none">
      <ObjectSection
        title={t(($) => $.apps_section.title)}
        count={isLoading || isError ? undefined : rows.length}
        action={
          canEdit ? (
            <>
              <ObjectSectionAction icon={Settings2} onClick={onManage}>
                {t(($) => $.sections.manage)}
              </ObjectSectionAction>
              <ObjectSectionAction icon={Plus} onClick={() => setAdding(true)} disabled={isLoading || isError}>
                {t(($) => $.sections.add)}
              </ObjectSectionAction>
            </>
          ) : null
        }
      >
        {isLoading ? (
          <p className="text-body text-muted-foreground">{t(($) => $.tab_body.connected_apps.loading)}</p>
        ) : isError || !overview ? (
          <p className="text-body text-destructive">{t(($) => $.tab_body.connected_apps.load_failed)}</p>
        ) : rows.length === 0 ? (
          <ObjectSectionEmpty
            icon={Plug}
            description={t(($) => $.apps_section.empty)}
            action={
              canEdit ? (
                <Button size="xs" variant="outline" onClick={() => setAdding(true)}>
                  <Plus />
                  {t(($) => $.sections.add)}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <div className="divide-y overflow-hidden rounded-lg border bg-card">
            {rows.map((row) => {
              const scope = row.app.account_scope;
              return (
                <AccountRow
                  key={row.app.toolkit_slug}
                  className="px-3 py-2"
                  scope={scope}
                  slug={row.app.toolkit_slug}
                  name={row.name}
                  logo={row.logo}
                  showLogo
                  overview={overview}
                  agentId={agent.id}
                  title={row.name}
                  subtitle={
                    <AccountOwner
                      scope={scope}
                      label={scope === "team" ? t(($) => $.apps_section.acts_as_team) : t(($) => $.apps_section.acts_as_starter)}
                    />
                  }
                  on={{
                    connect: () => setConnect({ scope, row }),
                    activity: () => setActivity({ scope, row }),
                    disconnect: (_, connection) => setDisconnecting({ scope, row, connection }),
                  }}
                />
              );
            })}
          </div>
        )}
      </ObjectSection>

      {canEdit && <AddAgentAppDialog agent={agent} open={adding} onOpenChange={setAdding} />}
      {connect && (
        <ConnectAppDialog
          open
          onOpenChange={(open) => {
            if (!open) setConnect(null);
          }}
          scope={connect.scope}
          slug={connect.row.app.toolkit_slug}
          name={connect.row.name}
          logo={connect.row.logo}
        />
      )}
      {activity && (
        <AppAccountDialog
          scope={activity.scope}
          slug={activity.row.app.toolkit_slug}
          name={activity.row.name}
          onClose={() => setActivity(null)}
        />
      )}
      {disconnecting && (
        <DisconnectAccountDialog
          scope={disconnecting.scope}
          connectionId={disconnecting.connection.id}
          slug={disconnecting.row.app.toolkit_slug}
          name={disconnecting.row.name}
          logo={disconnecting.row.logo}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </div>
  );
}
