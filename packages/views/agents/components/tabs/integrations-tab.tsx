"use client";

import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import {
  dingtalkAgentGroupsOptions,
  dingtalkInstallationsOptions,
} from "@multica/core/dingtalk";
import { wecomInstallationsOptions } from "@multica/core/wecom";
import { telegramInstallationsOptions } from "@multica/core/telegram";
import { memberListOptions } from "@multica/core/workspace/queries";
import { LarkAgentBindButton } from "../../../settings/components/lark-tab";
import { LarkMark } from "../../../settings/components/lark-mark";
import { SlackAgentBindButton } from "../../../settings/components/slack-tab";
import { SlackMark } from "../../../settings/components/slack-mark";
import {
  DingTalkAgentBindButton,
  DingTalkBotGroups,
  DingTalkConnectionLabel,
  getDingTalkBotIdentity,
} from "../../../settings/components/dingtalk-tab";
import { DingTalkMark } from "../../../settings/components/dingtalk-mark";
import { WecomAgentBindButton } from "../../../settings/components/wecom-tab";
import { WecomMark } from "../../../settings/components/wecom-mark";
import { TelegramAgentBindButton } from "../../../settings/components/telegram-tab";
import { TelegramMark } from "../../../settings/components/telegram-mark";
import { useT } from "../../../i18n";

/**
 * Integrations tab on the agent detail page. Surfaces the same external-
 * channel bind entry point as the inspector's "Integrations" section
 * (Lark Bot today) — scan-to-bind when unbound, connected info when bound —
 * but with the room a tab affords for a heading, a description, and the
 * not-configured / coming-soon / members-only states the cramped sidebar
 * section has no space for.
 *
 * The actionable affordance is the shared `LarkAgentBindButton`, the single
 * source of truth for "scan to bind vs. already connected". This tab only
 * adds the explanatory chrome around it, so the two entry points can never
 * drift.
 */
export function IntegrationsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const { t: ts } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);

  // Both queries are already issued by LarkAgentBindButton (and keyed per
  // workspace), so re-reading them here is free — TanStack dedupes by key.
  // We only need the derived booleans to pick which copy sits next to the
  // button, mirroring the branch order LarkTab uses in Settings.
  const { data: listing } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: slackListing } = useQuery({
    ...slackInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: dingtalkListing } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
  });
  const { data: wecomListing } = useQuery({
    ...wecomInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: telegramListing } = useQuery({
    ...telegramInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });

  const configured = listing?.configured === true;
  const installSupported = listing?.install_supported === true;
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const isWorkspaceAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const isAgentOwner =
    currentMember != null &&
    !!user?.id &&
    agent.owner_id != null &&
    agent.owner_id === user.id;
  // Lark bind/manage is authorized for the agent's owner OR a workspace
  // owner/admin (server/internal/handler/lark.go canManageAgent, MUL-4213).
  // Slack's install/revoke routes are still workspace owner/admin-only, so
  // its gate stays admin-only — the agent owner must not see a Slack CTA the
  // backend would 403.
  const canManageLark = isWorkspaceAdmin || isAgentOwner;
  const canManageSlack = isWorkspaceAdmin;
  const canManageWecom = isWorkspaceAdmin;
  const canManageTelegram = isWorkspaceAdmin;
  const hasActiveInstall =
    listing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === "active",
    ) ?? false;

  const slackConfigured = slackListing?.configured === true;
  const slackInstallSupported = slackListing?.install_supported === true;
  const slackHasActiveInstall =
    slackListing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === "active",
    ) ?? false;

  const dingtalkConfigured = dingtalkListing?.configured === true;
  const dingtalkInstallation = dingtalkListing?.installations.find(
    (inst) => inst.agent_id === agent.id && inst.status === "active",
  );
  const dingtalkHasActiveInstall = !!dingtalkInstallation;
  const {
    data: dingtalkGroupsListing,
    isLoading: dingtalkGroupsLoading,
    isError: dingtalkGroupsError,
    refetch: retryDingtalkGroups,
  } = useQuery({
    ...dingtalkAgentGroupsOptions(wsId, agent.id),
    enabled: !!wsId && dingtalkHasActiveInstall,
  });
  const dingtalkGroups = dingtalkGroupsListing?.groups ?? [];
  const dingtalkGroupDiscoverySupported =
    dingtalkGroupsListing?.group_discovery_supported === true;
  const showDingtalkGroupDiscovery =
    dingtalkGroupDiscoverySupported ||
    dingtalkGroupsLoading ||
    dingtalkGroupsError;
  const dingtalkBotIdentity = dingtalkInstallation
    ? dingtalkGroupsListing?.bot_identities?.[dingtalkInstallation.id] ??
      getDingTalkBotIdentity(dingtalkGroups, dingtalkInstallation.id)
    : undefined;
  // DingTalk uses the same per-agent management rule as Lark: the target
  // agent's owner or a workspace owner/admin may connect and disconnect it.
  const canManageDingtalk = isWorkspaceAdmin || isAgentOwner;

  const wecomConfigured = wecomListing?.configured === true;
  const wecomInstallSupported = wecomListing?.install_supported === true;
  const wecomHasActiveInstall =
    wecomListing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === "active",
    ) ?? false;

  const telegramConfigured = telegramListing?.configured === true;
  const telegramInstallSupported = telegramListing?.install_supported === true;
  const telegramHasActiveInstall =
    telegramListing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === "active",
    ) ?? false;

  // Preserve the established Integrations management gate: a member who can
  // manage no platform gets the read-only note instead of install controls.
  // The agent-scoped DingTalk relationship remains visible because reaching
  // this Agent detail already passed the server's Agent view permission gate.
  if (
    !canManageLark &&
    !canManageSlack &&
    !canManageDingtalk &&
    !canManageWecom &&
    !canManageTelegram
  ) {
    return (
      <div className="space-y-6">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.tab_body.integrations.intro)}
        </p>
        {dingtalkInstallation ? (
          <section className="rounded-lg border">
            <div className="flex items-start gap-3 p-4">
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
                <DingTalkMark className="h-5 w-5" />
              </span>
              <div className="min-w-0 flex-1 space-y-1">
                <h3 className="text-body font-medium">
                  {ts(($) => $.dingtalk.section_title)}
                </h3>
                <p className="text-caption leading-relaxed text-muted-foreground">
                  {ts(($) => $.dingtalk.agent_page_description)}
                </p>
              </div>
            </div>
            <div className="border-t px-4 py-3">
              <DingTalkConnectionLabel
                botName={dingtalkBotIdentity?.bot_name ?? ""}
                botIdentityIssue={dingtalkBotIdentity?.bot_identity_issue ?? ""}
                showBotIdentity={
                  dingtalkGroupDiscoverySupported && !dingtalkGroupsLoading
                }
                showPermissionHelp={false}
              />
            </div>
            {showDingtalkGroupDiscovery && (
              <div className="border-t px-4 pb-4">
                <DingTalkBotGroups
                  workspaceId={wsId}
                  agentId={agent.id}
                  installationId={dingtalkInstallation.id}
                  groups={dingtalkGroups}
                  inactiveCount={dingtalkGroupsListing?.inactive_group_counts?.[dingtalkInstallation.id] ?? 0}
                  isLoading={dingtalkGroupsLoading}
                  isError={dingtalkGroupsError}
                  onRetry={() => void retryDingtalkGroups()}
                />
              </div>
            )}
          </section>
        ) : (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.tab_body.integrations.members_note)}
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <p className="text-caption text-muted-foreground">
        {t(($) => $.tab_body.integrations.intro)}
      </p>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <LarkMark className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-body font-medium">{ts(($) => $.lark.section_title)}</h3>
            <p className="text-caption leading-relaxed text-muted-foreground">
              {ts(($) => $.lark.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!configured ? (
            // No at-rest key on this deployment. The tab is only mounted
            // when the feature is configured, so this is the rare "key was
            // removed after an install existed" race.
            <p className="text-caption text-muted-foreground">
              {ts(($) => $.lark.not_enabled_title)}
            </p>
          ) : !installSupported && !hasActiveInstall ? (
            // Key is set but the device-flow transport isn't wired in this
            // build — a fresh scan would fail at the post-poll bot-info step,
            // so we surface the "coming soon" notice instead of a broken CTA.
            // An agent that is ALREADY bound is exempt: install_supported only
            // governs NEW installs, so the bound state must still render below
            // (server/internal/handler/lark.go).
            <div className="space-y-1">
              <p className="text-caption font-medium">{ts(($) => $.lark.preview_title)}</p>
              <p className="text-caption text-muted-foreground">
                {ts(($) => $.lark.preview_description)}
              </p>
            </div>
          ) : (
            // Agent owner or workspace owner/admin with either a supported
            // transport or an existing bot: the shared button renders the
            // scan-to-bind CTA or the already-connected "Manage in Lark"
            // badge. It self-authorizes on agentOwnerId + role.
            <LarkAgentBindButton
              agentId={agent.id}
              agentName={agent.name}
              agentOwnerId={agent.owner_id}
            />
          )}
        </div>
      </section>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <SlackMark className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-body font-medium">{ts(($) => $.slack.section_title)}</h3>
            <p className="text-caption leading-relaxed text-muted-foreground">
              {ts(($) => $.slack.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!canManageSlack ? (
            // Slack install/revoke stay workspace owner/admin-only, so an
            // agent owner who is not an admin only gets the read-only note
            // here (unlike Lark above). Reuses the shared members note.
            <p className="text-caption text-muted-foreground">
              {t(($) => $.tab_body.integrations.members_note)}
            </p>
          ) : !slackConfigured ? (
            <p className="text-caption text-muted-foreground">
              {ts(($) => $.slack.not_enabled_title)}
            </p>
          ) : !slackInstallSupported && !slackHasActiveInstall ? (
            // Secret key is set but the OAuth client credentials aren't, so a
            // fresh "Connect Slack" would 503. Surface the "coming soon" notice
            // instead of a broken CTA; an already-bound agent still renders.
            <div className="space-y-1">
              <p className="text-caption font-medium">{ts(($) => $.slack.preview_title)}</p>
              <p className="text-caption text-muted-foreground">
                {ts(($) => $.slack.preview_description)}
              </p>
            </div>
          ) : (
            <SlackAgentBindButton agentId={agent.id} agentName={agent.name} />
          )}
        </div>
      </section>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <DingTalkMark className="h-5 w-5" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-body font-medium">{ts(($) => $.dingtalk.section_title)}</h3>
            <p className="text-caption leading-relaxed text-muted-foreground">
              {ts(($) => $.dingtalk.agent_page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!canManageDingtalk ? (
            // Viewers who cannot manage this agent still see its connected
            // DingTalk identity and discovered groups as read-only details.
            dingtalkInstallation ? (
              <DingTalkConnectionLabel
                botName={dingtalkBotIdentity?.bot_name ?? ""}
                botIdentityIssue={dingtalkBotIdentity?.bot_identity_issue ?? ""}
                showBotIdentity={
                  dingtalkGroupDiscoverySupported && !dingtalkGroupsLoading
                }
                showPermissionHelp={false}
              />
            ) : (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.tab_body.integrations.members_note)}
              </p>
            )
          ) : !dingtalkConfigured ? (
            <p className="text-caption text-muted-foreground">
              {ts(($) => $.dingtalk.not_enabled_title)}
            </p>
          ) : (
            <DingTalkAgentBindButton
              agentId={agent.id}
              agentName={agent.name}
              agentOwnerId={agent.owner_id}
              botName={
                dingtalkGroupsLoading || !dingtalkGroupDiscoverySupported
                  ? undefined
                  : (dingtalkBotIdentity?.bot_name ?? "")
              }
              botIdentityIssue={
                dingtalkGroupsLoading || !dingtalkGroupDiscoverySupported
                  ? undefined
                  : (dingtalkBotIdentity?.bot_identity_issue ?? "")
              }
            />
          )}
        </div>
        {dingtalkInstallation && showDingtalkGroupDiscovery && (
          <div className="border-t px-4 pb-4">
            <DingTalkBotGroups
              workspaceId={wsId}
              agentId={agent.id}
              installationId={dingtalkInstallation.id}
              groups={dingtalkGroups}
              inactiveCount={dingtalkGroupsListing?.inactive_group_counts?.[dingtalkInstallation.id] ?? 0}
              isLoading={dingtalkGroupsLoading}
              isError={dingtalkGroupsError}
              onRetry={() => void retryDingtalkGroups()}
            />
          </div>
        )}
      </section>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <WecomMark className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-body font-medium">{ts(($) => $.wecom.section_title)}</h3>
            <p className="text-caption leading-relaxed text-muted-foreground">
              {ts(($) => $.wecom.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!canManageWecom ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.tab_body.integrations.members_note)}
            </p>
          ) : !wecomConfigured ? (
            <p className="text-caption text-muted-foreground">
              {ts(($) => $.wecom.not_enabled_title)}
            </p>
          ) : !wecomInstallSupported && !wecomHasActiveInstall ? (
            <div className="space-y-1">
              <p className="text-caption font-medium">{ts(($) => $.wecom.preview_title)}</p>
              <p className="text-caption text-muted-foreground">
                {ts(($) => $.wecom.preview_description)}
              </p>
            </div>
          ) : (
            <WecomAgentBindButton agentId={agent.id} agentName={agent.name} />
          )}
        </div>
      </section>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <TelegramMark className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-body font-medium">{ts(($) => $.telegram.section_title)}</h3>
            <p className="text-caption leading-relaxed text-muted-foreground">
              {ts(($) => $.telegram.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!canManageTelegram ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.tab_body.integrations.members_note)}
            </p>
          ) : !telegramConfigured ? (
            <p className="text-caption text-muted-foreground">
              {ts(($) => $.telegram.not_enabled_title)}
            </p>
          ) : !telegramInstallSupported && !telegramHasActiveInstall ? (
            <div className="space-y-1">
              <p className="text-caption font-medium">{ts(($) => $.telegram.preview_title)}</p>
              <p className="text-caption text-muted-foreground">
                {ts(($) => $.telegram.preview_description)}
              </p>
            </div>
          ) : (
            <TelegramAgentBindButton agentId={agent.id} agentName={agent.name} />
          )}
        </div>
      </section>
    </div>
  );
}
