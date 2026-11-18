"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Plug, Plus, Settings2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useAuthStore } from "@multica/core/auth";
import { useRequestTeamApp } from "@multica/core/connected-apps";
import type { Agent, AppAccountScope } from "@multica/core/types";
import { AppStatusLabel } from "../../apps/components/app-status-label";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { ConnectAppDialog } from "../../common/connect-app-dialog";
import {
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionEmpty,
} from "../../layout/object-page";
import { useT } from "../../i18n";
import { AddAgentAppDialog } from "./add-agent-app-dialog";
import { agentAppsSectionId } from "./agent-app-status";
import { useAgentApps, type AgentAppRow } from "./use-agent-apps";

/**
 * The apps the agent works in and whether it can use each for the viewer.
 * Everyone sees it; only editors change the list, while anyone can connect
 * their own account or ask an admin for a team one.
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
  const viewerId = useAuthStore((s) => s.user?.id);
  const requestTeam = useRequestTeamApp();
  const [adding, setAdding] = useState(false);
  const [connect, setConnect] = useState<{ scope: AppAccountScope; row: AgentAppRow } | null>(null);
  const canManageTeam = overview?.can_manage ?? false;

  const askAdmin = (row: AgentAppRow) =>
    requestTeam.mutateAsync({ slug: row.app.toolkit_slug, agentId: agent.id }).then(
      () => toast.success(t(($) => $.apps_section.asked_toast, { app: row.name })),
      () => toast.error(t(($) => $.apps_section.ask_failed_toast)),
    );

  const action = (row: AgentAppRow) => {
    const { account_scope: scope, toolkit_slug: slug } = row.app;
    if (row.status === "connected") return null;
    if (scope === "personal" || canManageTeam) {
      const label = row.status === "expired"
        ? t(($) => $.apps_section.reconnect)
        : scope === "team"
          ? t(($) => $.apps_section.connect_team)
          : t(($) => $.apps_section.connect_yours);
      return (
        <Button
          size="xs"
          variant={row.status === "needs_your_account" ? "default" : "outline"}
          onClick={() => setConnect({ scope, row })}
        >
          {label}
        </Button>
      );
    }
    if (row.status !== "no_team_account") return null;
    const requested = !!viewerId && (overview?.team_requests ?? []).some(
      (r) => r.toolkit_slug === slug && r.requested_by.includes(viewerId),
    );
    if (requested) {
      return <span className="px-2 text-caption text-muted-foreground">{t(($) => $.apps_section.requested)}</span>;
    }
    return (
      <Button size="xs" variant="outline" disabled={requestTeam.isPending} onClick={() => askAdmin(row)}>
        {t(($) => $.apps_section.ask_admin)}
      </Button>
    );
  };

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
        ) : isError ? (
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
          <ul className="divide-y overflow-hidden rounded-lg border bg-card">
            {rows.map((row) => (
              <li key={row.app.toolkit_slug} className="flex items-center gap-3 px-3 py-2">
                <ComposioToolkitLogo
                  slug={row.app.toolkit_slug}
                  name={row.name}
                  fallbackLogo={row.logo}
                  className="h-6 w-6"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-body font-medium">{row.name}</p>
                  <p className="truncate text-caption text-muted-foreground">
                    {t(($) => $.apps_section.scopes[row.app.account_scope])}
                  </p>
                </div>
                <AppStatusLabel status={row.status} className="shrink-0 text-caption" />
                <span className="shrink-0">{action(row)}</span>
              </li>
            ))}
          </ul>
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
    </div>
  );
}
