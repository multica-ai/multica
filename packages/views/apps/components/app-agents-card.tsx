"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { useWorkspaceId } from "@multica/core/hooks";
import { canEditAgent, useCurrentMember } from "@multica/core/permissions";
import { connectedAppsKeys, useUpdateAgentApps } from "@multica/core/connected-apps";
import { refreshFeaturesOnLockedError } from "@multica/core/billing";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, AppAccountScope, AppCatalogEntry, AppsOverview } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { agentAppsWith } from "../lib/agent-apps";
import { scopeForNewApp } from "../../agents/components/agent-app-status";
import { useTeamAppAccess } from "../../common/use-team-app-access";
import { usePaidFeatureErrorMessage } from "../../billing";
import { appAccountStatus } from "../../common/app-status";
import { AppStatusLabel } from "./app-status-label";
import { ObjectSection, ObjectSectionCard, ObjectSectionEmpty } from "../../layout/object-page";

/** Every agent in the workspace, whether it uses this app, and whose account. */
export function AppAgentsCard({
  slug,
  name,
  entry,
  overview,
}: {
  slug: string;
  name: string;
  entry: AppCatalogEntry | undefined;
  overview: AppsOverview;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const agents = useQuery(agentListOptions(wsId));
  const viewer = useCurrentMember(wsId);
  const live = (agents.data ?? [])
    .filter((a) => !a.archived_at)
    .sort((a, b) => a.name.localeCompare(b.name));

  return (
    <ObjectSection title={t(($) => $.connected_apps.app_page.agents_title)} count={agents.data ? live.length : undefined}>
      <p className="-mt-1 text-caption text-muted-foreground">{t(($) => $.connected_apps.app_page.agents_description)}</p>
      {agents.isLoading ? (
        <ObjectSectionEmpty description={t(($) => $.connected_apps.loading)} />
      ) : live.length === 0 ? (
        <ObjectSectionEmpty description={t(($) => $.connected_apps.app_page.agents_empty)} />
      ) : (
        <ObjectSectionCard>
          {live.map((agent) => (
            <AgentAppRow
              key={agent.id}
              agent={agent}
              slug={slug}
              appName={name}
              entry={entry}
              overview={overview}
              canEdit={canEditAgent(agent, viewer).allowed}
            />
          ))}
        </ObjectSectionCard>
      )}
    </ObjectSection>
  );
}

function AgentAppRow({
  agent,
  slug,
  appName,
  entry,
  overview,
  canEdit,
}: {
  agent: Agent;
  slug: string;
  appName: string;
  entry: AppCatalogEntry | undefined;
  overview: AppsOverview;
  canEdit: boolean;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const update = useUpdateAgentApps(agent.id);
  const teamAccess = useTeamAppAccess(overview);
  const paidFeatureError = usePaidFeatureErrorMessage();
  const [saving, setSaving] = useState(false);
  const current = overview.agent_apps.find((a) => a.agent_id === agent.id && a.toolkit_slug === slug);
  const teamLocked = current?.account_scope === "team" && !overview.can_manage;
  const disabled = !canEdit || saving || teamLocked;
  const status = current ? appAccountStatus(overview, current.account_scope, slug) : null;
  const readiness = !current || !status
    ? undefined
    : current.account_scope === "team"
      ? status === "connected"
        ? t(($) => $.connected_apps.app_page.ready_team)
        : t(($) => $.connected_apps.app_page.needs_team)
      : status === "connected"
        ? t(($) => $.connected_apps.app_page.ready_yours)
        : t(($) => $.connected_apps.app_page.needs_yours);
  const sourceItems = {
    team: t(($) => $.connected_apps.app_page.source_team),
    personal: t(($) => $.connected_apps.app_page.source_personal),
  };

  // The row reads the overview, so it stays busy until the refetch lands
  // instead of flicking back to the old value in between.
  const save = (scope: AppAccountScope | null) => {
    setSaving(true);
    update
      .mutateAsync(agentAppsWith(overview, agent.id, slug, scope))
      .then(() => qc.invalidateQueries({ queryKey: connectedAppsKeys.overview(wsId) }))
      .catch((e: unknown) => {
        refreshFeaturesOnLockedError(qc, wsId, e);
        toast.error(paidFeatureError(e, e instanceof Error && e.message ? e.message : t(($) => $.connected_apps.app_page.save_failed)));
      })
      .finally(() => setSaving(false));
  };

  return (
    <div className="flex items-center gap-3 px-3 py-2" title={teamLocked ? t(($) => $.connected_apps.app_page.team_admin_only) : undefined}>
      <ActorAvatar actorType="agent" actorId={agent.id} size="sm" />
      <span className="w-36 min-w-0 shrink-0 truncate text-body font-medium">{agent.name}</span>
      <div className="min-w-0 flex-1">
        {current ? (
          <Select
            items={sourceItems}
            value={current.account_scope}
            onValueChange={(value) => value && value !== current.account_scope && save(value as AppAccountScope)}
            disabled={disabled}
          >
            <SelectTrigger
              size="sm"
              className="max-w-full"
              aria-label={t(($) => $.connected_apps.app_page.source_label, { agent: agent.name, app: appName })}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="team" disabled={!overview.can_manage || !teamAccess.canAdd}>
                {sourceItems.team}
              </SelectItem>
              <SelectItem value="personal" disabled={teamLocked}>
                {sourceItems.personal}
              </SelectItem>
            </SelectContent>
          </Select>
        ) : (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.connected_apps.app_page.not_using, { app: appName })}
          </span>
        )}
      </div>
      {status && readiness && (
        <AppStatusLabel status={status} label={readiness} className="hidden max-w-48 shrink-0 text-caption md:inline-flex" />
      )}
      <Switch
        checked={!!current}
        disabled={disabled}
        onCheckedChange={(on) => save(on ? scopeForNewApp(overview, slug, entry?.recommended_scope, teamAccess.canAdd) : null)}
        aria-label={t(($) => $.connected_apps.app_page.enable, { agent: agent.name, app: appName })}
      />
    </div>
  );
}
