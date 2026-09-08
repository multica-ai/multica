"use client";

import type { ReactNode } from "react";
import { ArrowLeft, ChevronRight, FolderGit2, Blocks } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  composioToolkitsOptions,
  composioConnectionsOptions,
} from "@multica/core/composio";
import { githubInstallationsOptions } from "@multica/core/github";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import { dingtalkInstallationsOptions } from "@multica/core/dingtalk";
import { wecomInstallationsOptions } from "@multica/core/wecom";
import { telegramInstallationsOptions } from "@multica/core/telegram";
import { vcsConnectionsOptions } from "@multica/core/vcs";
import { useConfigStore, useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { LarkTab } from "./lark-tab";
import { ComposioTab } from "./composio-tab";
import { SlackTab } from "./slack-tab";
import { DingTalkTab } from "./dingtalk-tab";
import { VCSTab } from "./vcs-tab";
import { WecomTab } from "./wecom-tab";
import { TelegramTab } from "./telegram-tab";
import { GitHubTab } from "./github-tab";
import { GitHubMark } from "./github-mark";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";
import { IntegrationChannelIcon } from "./integration-channel-icon";
import { resolveSettingsLocation, settingsHref } from "./settings-navigation";

interface ConnectionState {
  data?: boolean;
  isPending: boolean;
  isError: boolean;
}

interface IntegrationEntry {
  id: string;
  label: string;
  description: string;
  icon: ReactNode;
  content: ReactNode;
  state: ConnectionState;
}

export function IntegrationsTab() {
  const { t } = useT("settings");
  const navigation = useNavigation();
  const wsId = useWorkspaceId();
  const { member } = useCurrentMember(wsId);
  const canView = !!wsId && !!member;
  const composioEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const vcsAvailable = useConfigStore((s) => s.vcsIntegrationAvailable);
  const toolkits = useQuery({
    ...composioToolkitsOptions(),
    enabled: composioEnabled,
  });
  const composioAvailable =
    composioEnabled &&
    !(toolkits.error instanceof ApiError && toolkits.error.status === 503);

  // Reuse the detail pages' query caches. Never report a failed or pending read
  // as disconnected, and do not issue deployment-disabled integration queries.
  const github = useQuery({
    ...githubInstallationsOptions(wsId),
    enabled: canView,
    select: (data) => (data.installations?.length ?? 0) > 0,
  });
  const lark = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: canView,
    select: (data) => (data.installations?.length ?? 0) > 0,
  });
  const slack = useQuery({
    ...slackInstallationsOptions(wsId),
    enabled: canView,
    select: (data) => (data.installations?.length ?? 0) > 0,
  });
  const dingtalk = useQuery({
    ...dingtalkInstallationsOptions(wsId),
    enabled: canView,
    select: (data) => (data.installations?.length ?? 0) > 0,
  });
  const wecom = useQuery({
    ...wecomInstallationsOptions(wsId),
    enabled: canView,
    select: (data) => (data.installations?.length ?? 0) > 0,
  });
  const telegram = useQuery({
    ...telegramInstallationsOptions(wsId),
    enabled: canView,
    select: (data) => (data.installations?.length ?? 0) > 0,
  });
  const vcs = useQuery({
    ...vcsConnectionsOptions(wsId),
    enabled: canView && vcsAvailable,
    select: (data) => (data.connections?.length ?? 0) > 0,
  });
  const composio = useQuery({
    ...composioConnectionsOptions(),
    enabled: composioAvailable,
    select: (data) => data.some((connection) => connection.status === "active"),
  });
  const groups: {
    id: string;
    label: string;
    description?: string;
    entries: IntegrationEntry[];
  }[] = [
    {
      id: "code",
      label: t(($) => $.integrations.code_title),
      entries: [
        {
          id: "github",
          label: t(($) => $.page.tabs.github),
          description: t(($) => $.integrations.github_hint),
          icon: <GitHubMark className="size-5" />,
          content: <GitHubTab />,
          state: github,
        },
        ...(vcsAvailable
          ? [
              {
                id: "vcs",
                label: t(($) => $.vcs.section_title),
                description: t(($) => $.integrations.vcs_hint),
                icon: <FolderGit2 className="size-5" />,
                content: <VCSTab />,
                state: vcs,
              },
            ]
          : []),
      ],
    },
    {
      id: "messaging",
      label: t(($) => $.integrations.messaging_title),
      entries: [
        {
          id: "lark",
          label: t(($) => $.lark.section_title),
          description: t(($) => $.lark.page_description),
          icon: <IntegrationChannelIcon channel="lark" />,
          content: <LarkTab />,
          state: lark,
        },
        {
          id: "slack",
          label: t(($) => $.slack.section_title),
          description: t(($) => $.slack.page_description),
          icon: <IntegrationChannelIcon channel="slack" />,
          content: <SlackTab />,
          state: slack,
        },
        {
          id: "dingtalk",
          label: t(($) => $.dingtalk.section_title),
          description: t(($) => $.dingtalk.page_description),
          icon: <IntegrationChannelIcon channel="dingtalk" />,
          content: <DingTalkTab />,
          state: dingtalk,
        },
        {
          id: "wecom",
          label: t(($) => $.wecom.section_title),
          description: t(($) => $.wecom.page_description),
          icon: <IntegrationChannelIcon channel="wecom" />,
          content: <WecomTab />,
          state: wecom,
        },
        {
          id: "telegram",
          label: t(($) => $.telegram.section_title),
          description: t(($) => $.telegram.page_description),
          icon: <IntegrationChannelIcon channel="telegram" />,
          content: <TelegramTab />,
          state: telegram,
        },
      ],
    },
    ...(composioAvailable
      ? [
          {
            id: "apps",
            label: t(($) => $.integrations.apps_title),
            description: t(($) => $.integrations.apps_scope),
            entries: [
              {
                id: "composio",
                label: t(($) => $.composio.section_title),
                description: t(($) => $.integrations.apps_hint),
                icon: <Blocks className="size-5" />,
                content: <ComposioTab />,
                state: composio,
              },
            ],
          },
        ]
      : []),
  ];
  // The OAuth service still returns to ?tab=integrations with one-shot params.
  // Mount its owning page so it can consume the callback and refresh its cache.
  const hasComposioCallback =
    navigation.searchParams.has("connected") ||
    navigation.searchParams.get("error") === "composio_connect_failed";
  const requested = hasComposioCallback
    ? "composio"
    : resolveSettingsLocation(navigation.searchParams).integration;
  const selected = groups
    .flatMap((group) => group.entries)
    .find((item) => item.id === requested);
  const href = (integration?: string) =>
    settingsHref(navigation.pathname, navigation.searchParams, "integrations", {
      integration,
    });

  if (selected) {
    return (
      <div className="space-y-6">
        <AppLink
          href={href()}
          className="inline-flex items-center gap-2 rounded-md text-body text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
        >
          <ArrowLeft className="size-4" aria-hidden="true" />
          {t(($) => $.integrations.back)}
        </AppLink>
        {selected.id === "github" ? (
          selected.content
        ) : (
          <SettingsTab
            title={selected.label}
            description={selected.description}
          >
            {selected.content}
          </SettingsTab>
        )}
      </div>
    );
  }

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.integrations)}
      description={t(($) => $.integrations.description)}
    >
      {groups.map((group) => (
        <SettingsSection
          key={group.id}
          title={group.label}
          description={group.description}
        >
          <SettingsCard>
            {group.entries.map((item) => (
              <AppLink
                key={item.id}
                href={href(item.id)}
                className="group flex items-center gap-4 px-4 py-5 first:rounded-t-xl last:rounded-b-xl hover:bg-surface-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
              >
                <span
                  aria-hidden="true"
                  className="flex size-11 shrink-0 items-center justify-center rounded-xl border border-surface-border bg-background text-foreground"
                >
                  {item.icon}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <span className="text-body font-medium text-foreground">
                      {item.label}
                    </span>{" "}
                    <ConnectionBadge state={item.state} />{" "}
                  </span>
                  <span className="mt-1 block text-caption leading-5 text-muted-foreground">
                    {group.id === "messaging"
                      ? t(($) => $.integrations.channel_hint, {
                          channel: item.label,
                        })
                      : item.description}
                  </span>
                </span>
                <ChevronRight
                  className="size-4 shrink-0 text-muted-foreground group-hover:text-foreground"
                  aria-hidden="true"
                />
              </AppLink>
            ))}
          </SettingsCard>
        </SettingsSection>
      ))}
    </SettingsTab>
  );
}

function ConnectionBadge({ state }: { state: ConnectionState }) {
  const { t } = useT("settings");
  const connected = !state.isError && !state.isPending && state.data === true;
  const label = state.isError
    ? t(($) => $.integrations.status_unknown)
    : state.isPending
      ? t(($) => $.integrations.status_loading)
      : connected
        ? t(($) => $.integrations.status_connected)
        : t(($) => $.integrations.status_not_connected);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-caption",
        connected ? "text-success" : "text-muted-foreground",
      )}
    >
      {connected && (
        <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
      )}
      {label}
    </span>
  );
}
