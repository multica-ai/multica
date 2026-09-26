"use client";

import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import { dingtalkInstallationsOptions } from "@multica/core/dingtalk";
import { wecomInstallationsOptions } from "@multica/core/wecom";
import { telegramInstallationsOptions } from "@multica/core/telegram";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { LarkTab } from "./lark-tab";
import { SlackTab } from "./slack-tab";
import { DingTalkTab } from "./dingtalk-tab";
import { WecomTab } from "./wecom-tab";
import { TelegramTab } from "./telegram-tab";
import { SettingsCard, SettingsTab } from "./settings-layout";
import { IntegrationChannelIcon } from "./integration-channel-icon";
import {
  resolveSettingsLocation,
  settingsHref,
  type ChannelIntegration,
} from "./settings-navigation";

interface ConnectionState {
  data?: boolean;
  isPending: boolean;
  isError: boolean;
}

interface ChannelEntry {
  id: ChannelIntegration;
  label: string;
  description: string;
  content: ReactNode;
  state: ConnectionState;
}

// The IM channels soft-revoke: the row survives with status 'revoked', so a row
// count never falls back to zero and would report a torn-down bot as connected
// forever (#8496).
const hasActiveInstallation = (data: {
  installations?: { status: string }[];
}) => data.installations?.some((inst) => inst.status === "active") ?? false;

/**
 * Messaging channels: a directory of the chat apps agents can be reached
 * from, each opening its own detail page. Code hosting lives on the Code page
 * and account-level app connections on Connected apps.
 */
export function ChannelsTab() {
  const { t } = useT("settings");
  const navigation = useNavigation();
  const wsId = useWorkspaceId();
  const { member } = useCurrentMember(wsId);
  const canView = !!wsId && !!member;

  // Reuse the detail pages' query caches. Never report a failed or pending
  // read as disconnected.
  const lark = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: canView,
    select: hasActiveInstallation,
  });
  const slack = useQuery({
    ...slackInstallationsOptions(wsId),
    enabled: canView,
    select: hasActiveInstallation,
  });
  const dingtalk = useQuery({
    ...dingtalkInstallationsOptions(wsId),
    enabled: canView,
    select: hasActiveInstallation,
  });
  const wecom = useQuery({
    ...wecomInstallationsOptions(wsId),
    enabled: canView,
    select: hasActiveInstallation,
  });
  const telegram = useQuery({
    ...telegramInstallationsOptions(wsId),
    enabled: canView,
    select: hasActiveInstallation,
  });
  const channels: ChannelEntry[] = [
    {
      id: "lark",
      label: t(($) => $.lark.section_title),
      description: t(($) => $.lark.page_description),
      content: <LarkTab />,
      state: lark,
    },
    {
      id: "slack",
      label: t(($) => $.slack.section_title),
      description: t(($) => $.slack.page_description),
      content: <SlackTab />,
      state: slack,
    },
    {
      id: "dingtalk",
      label: t(($) => $.dingtalk.section_title),
      description: t(($) => $.dingtalk.page_description),
      content: <DingTalkTab />,
      state: dingtalk,
    },
    {
      id: "wecom",
      label: t(($) => $.wecom.section_title),
      description: t(($) => $.wecom.page_description),
      content: <WecomTab />,
      state: wecom,
    },
    {
      id: "telegram",
      label: t(($) => $.telegram.section_title),
      description: t(($) => $.telegram.page_description),
      content: <TelegramTab />,
      state: telegram,
    },
  ];
  const requested = resolveSettingsLocation(navigation.searchParams).integration;
  const selected = channels.find((channel) => channel.id === requested);
  const href = (integration?: string) =>
    settingsHref(navigation.pathname, navigation.searchParams, "channels", {
      integration,
    });

  if (selected) {
    return (
      <SettingsTab
        title={selected.label}
        description={selected.description}
        scope="workspace"
        breadcrumb={
          <nav
            aria-label={t(($) => $.channels.breadcrumb)}
            className="flex items-center gap-1.5 text-caption text-muted-foreground"
          >
            <AppLink
              href={href()}
              className="rounded-sm hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
            >
              {t(($) => $.page.tabs.channels)}
            </AppLink>
            <ChevronRight aria-hidden="true" className="size-3" />
            <span aria-current="page" className="text-foreground">
              {selected.label}
            </span>
          </nav>
        }
      >
        {selected.content}
      </SettingsTab>
    );
  }

  return (
    <SettingsTab title={t(($) => $.page.tabs.channels)} scope="workspace">
      <SettingsCard>
        {channels.map((channel) => (
          <AppLink
            key={channel.id}
            href={href(channel.id)}
            className="group flex items-center gap-4 px-4 py-4 first:rounded-t-xl last:rounded-b-xl hover:bg-surface-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
          >
            <span
              aria-hidden="true"
              className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-surface-border bg-background text-foreground"
            >
              <IntegrationChannelIcon channel={channel.id} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="text-body font-medium text-foreground">
                  {channel.label}
                </span>{" "}
                <ConnectionBadge state={channel.state} />
              </span>
              <span className="mt-1 block text-caption leading-5 text-muted-foreground">
                {channel.description}
              </span>
            </span>
            <ChevronRight
              className="size-4 shrink-0 text-muted-foreground group-hover:text-foreground"
              aria-hidden="true"
            />
          </AppLink>
        ))}
      </SettingsCard>
    </SettingsTab>
  );
}

export function ConnectionBadge({ state }: { state: ConnectionState }) {
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
