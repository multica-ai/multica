"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ChannelConnectDialog } from "./channel-connect-dialog";
import { ChannelDisconnectDialog } from "./channel-disconnect-dialog";
import { IntegrationChannelIcon } from "./integration-channel-icon";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";
import {
  CHANNEL_INTEGRATIONS,
  type ChannelIntegration,
  settingsHref,
} from "./settings-navigation";
import {
  useMessagingChannels,
  type ChannelConnection,
} from "./use-messaging-channels";

export function ChannelsTab() {
  const { t } = useT("settings");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { wsId, queries, connections, isAdmin, available } =
    useMessagingChannels();
  const agentsQuery = useQuery(agentListOptions(wsId));
  const agents = agentsQuery.data ?? [];
  const [connecting, setConnecting] = useState<ChannelIntegration | null>(null);
  const [disconnecting, setDisconnecting] = useState<ChannelConnection | null>(
    null,
  );
  const legacyDetail = navigation.searchParams.get("integration");
  useEffect(() => {
    if (legacyDetail)
      navigation.replace(
        settingsHref(navigation.pathname, navigation.searchParams, "channels"),
      );
  }, [legacyDetail, navigation]);

  return (
    <SettingsTab title={t(($) => $.page.tabs.channels)} scope="workspace">
      <SettingsCard>
        {CHANNEL_INTEGRATIONS.filter(
          (id) =>
            isAdmin ||
            queries[id].data?.configured ||
            queries[id].isPending ||
            queries[id].isError,
        ).map((id) => {
          const query = queries[id];
          const orphaned =
            isAdmin && agentsQuery.isSuccess
              ? connections.filter(
                  (entry) =>
                    entry.channel === id &&
                    !agents.some(
                      (agent) => agent.id === entry.installation.agent_id,
                    ),
                )
              : [];
          const connectedAgents = agents.filter((agent) =>
            connections.some(
              (entry) =>
                entry.channel === id &&
                entry.installation.agent_id === agent.id,
            ),
          );
          return (
            <div
              key={id}
              className="flex flex-wrap items-center gap-3 px-4 py-4"
            >
              <IntegrationChannelIcon channel={id} />
              <span className="w-24 shrink-0 text-body font-medium">
                {t(($) => $[id].section_title)}
              </span>
              <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                {query.isError || agentsQuery.isError ? (
                  <span
                    role="alert"
                    className="text-caption text-muted-foreground"
                  >
                    {t(($) => $.integrations.status_unknown)}
                  </span>
                ) : query.isPending || agentsQuery.isPending ? (
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.integrations.status_loading)}
                  </span>
                ) : connectedAgents.length || orphaned.length ? (
                  connectedAgents.map((agent) => (
                    <AppLink
                      key={agent.id}
                      href={`${paths.agentDetail(agent.id)}?view=channels`}
                      className="flex min-w-0 items-center gap-1.5 rounded-md px-1 py-0.5 text-caption hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      <ActorAvatar
                        actorType="agent"
                        actorId={agent.id}
                        size="xs"
                        profileLink={false}
                      />
                      <span className="max-w-32 truncate">{agent.name}</span>
                    </AppLink>
                  ))
                ) : (
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.channels.disconnected)}
                  </span>
                )}
                {orphaned.map((entry) => (
                  <span
                    key={entry.installation.id}
                    className="inline-flex items-center gap-2 text-caption text-muted-foreground"
                  >
                    {t(($) => $.channels.agent_unavailable)}
                    <Button
                      size="xs"
                      variant="ghost"
                      onClick={() => setDisconnecting(entry)}
                    >
                      {t(($) => $.channels.disconnect)}
                    </Button>
                  </span>
                ))}
                {!query.isPending &&
                !query.isError &&
                !query.data?.configured &&
                isAdmin ? (
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.channels.unavailable)}
                  </span>
                ) : null}
              </div>
              {query.isError ? (
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => void query.refetch()}
                >
                  {t(($) => $.lark.install_retry)}
                </Button>
              ) : isAdmin && available(id) ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={
                    !agents.some(
                      (agent) =>
                        !agent.archived_at &&
                        !connectedAgents.some(
                          (connected) => connected.id === agent.id,
                        ),
                    )
                  }
                  onClick={() => setConnecting(id)}
                >
                  {t(($) => $.channels.connect)}
                </Button>
              ) : null}
            </div>
          );
        })}
      </SettingsCard>
      <SettingsSection title={t(($) => $.channels.permissions_title)}>
        <SettingsCard>
          <div className="flex flex-wrap justify-between gap-2 px-4 py-3 text-caption">
            <span>
              {t(($) => $.lark.section_title)},{" "}
              {t(($) => $.dingtalk.section_title)}
            </span>
            <span className="text-muted-foreground">
              {t(($) => $.channels.owner_or_admin)}
            </span>
          </div>
          <div className="flex flex-wrap justify-between gap-2 px-4 py-3 text-caption">
            <span>
              {t(($) => $.slack.section_title)},{" "}
              {t(($) => $.wecom.section_title)},{" "}
              {t(($) => $.telegram.section_title)}
            </span>
            <span className="text-muted-foreground">
              {t(($) => $.channels.admin_only)}
            </span>
          </div>
        </SettingsCard>
      </SettingsSection>
      <ChannelConnectDialog
        key={connecting ?? "closed"}
        agents={agents}
        initialChannel={connecting ?? undefined}
        open={connecting !== null}
        onOpenChange={(open) => {
          if (!open) setConnecting(null);
        }}
      />
      {disconnecting ? (
        <ChannelDisconnectDialog
          connection={disconnecting}
          open
          onOpenChange={(open) => {
            if (!open) setDisconnecting(null);
          }}
        />
      ) : null}
    </SettingsTab>
  );
}
