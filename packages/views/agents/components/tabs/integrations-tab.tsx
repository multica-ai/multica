"use client";

import { useQuery } from "@tanstack/react-query";
import { MessagesSquare, Webhook } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import { wechatInstallationsOptions } from "@multica/core/wechat";
import { memberListOptions } from "@multica/core/workspace/queries";
import { LarkAgentBindButton } from "../../../settings/components/lark-tab";
import { SlackAgentBindButton } from "../../../settings/components/slack-tab";
import { WechatAgentBindButton } from "../../../settings/components/wechat-tab";
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
  const { data: wechatListing } = useQuery({
    ...wechatInstallationsOptions(wsId),
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
    !!user?.id && agent.owner_id != null && agent.owner_id === user.id;
  // Lark bind/manage is authorized for the agent's owner OR a workspace
  // owner/admin (server/internal/handler/lark.go canManageAgent, MUL-4213).
  // Slack's install/revoke routes are still workspace owner/admin-only, so
  // its gate stays admin-only — the agent owner must not see a Slack CTA the
  // backend would 403.
  const canManageLark = isWorkspaceAdmin || isAgentOwner;
  const canManageSlack = isWorkspaceAdmin;
  // WeChat bind is device-flow scan (like Lark), so the same gate applies: the
  // agent's owner OR a workspace owner/admin may bind.
  const canManageWechat = isWorkspaceAdmin || isAgentOwner;
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

  const wechatConfigured = wechatListing?.configured === true;
  const wechatInstallSupported = wechatListing?.install_supported === true;
  const wechatHasActiveInstall =
    wechatListing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === "active",
    ) ?? false;
  const wechatExisting = wechatListing?.installations.find(
    (inst) => inst.agent_id === agent.id && inst.status === "active",
  );

  // A member who can manage none of the platforms (not a workspace admin and
  // not this agent's owner) gets the read-only note instead of the sections.
  if (!canManageLark && !canManageSlack && !canManageWechat) {
    return (
      <div className="space-y-6">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.integrations.intro)}
        </p>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.integrations.members_note)}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.integrations.intro)}
      </p>

      <section className="rounded-lg border">
        <div className="flex items-start gap-3 p-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
            <Webhook className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-sm font-medium">{ts(($) => $.lark.section_title)}</h3>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {ts(($) => $.lark.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!configured ? (
            // No at-rest key on this deployment. The tab is only mounted
            // when the feature is configured, so this is the rare "key was
            // removed after an install existed" race.
            <p className="text-xs text-muted-foreground">
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
              <p className="text-xs font-medium">{ts(($) => $.lark.preview_title)}</p>
              <p className="text-xs text-muted-foreground">
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
            <MessagesSquare className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-sm font-medium">{ts(($) => $.slack.section_title)}</h3>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {ts(($) => $.slack.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!canManageSlack ? (
            // Slack install/revoke stay workspace owner/admin-only, so an
            // agent owner who is not an admin only gets the read-only note
            // here (unlike Lark above). Reuses the shared members note.
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.integrations.members_note)}
            </p>
          ) : !slackConfigured ? (
            <p className="text-xs text-muted-foreground">
              {ts(($) => $.slack.not_enabled_title)}
            </p>
          ) : !slackInstallSupported && !slackHasActiveInstall ? (
            // Secret key is set but the OAuth client credentials aren't, so a
            // fresh "Connect Slack" would 503. Surface the "coming soon" notice
            // instead of a broken CTA; an already-bound agent still renders.
            <div className="space-y-1">
              <p className="text-xs font-medium">{ts(($) => $.slack.preview_title)}</p>
              <p className="text-xs text-muted-foreground">
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
            <MessagesSquare className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1 space-y-1">
            <h3 className="text-sm font-medium">{ts(($) => $.wechat.section_title)}</h3>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {ts(($) => $.wechat.page_description)}
            </p>
          </div>
        </div>
        <div className="border-t px-4 py-3">
          {!wechatConfigured ? (
            <p className="text-xs text-muted-foreground">
              {ts(($) => $.wechat.not_enabled_title)}
            </p>
          ) : !wechatInstallSupported && !wechatHasActiveInstall ? (
            <div className="space-y-1">
              <p className="text-xs font-medium">{ts(($) => $.wechat.preview_title)}</p>
              <p className="text-xs text-muted-foreground">
                {ts(($) => $.wechat.preview_description)}
              </p>
            </div>
          ) : (
            <WechatAgentBindButton
              wsId={wsId}
              agentId={agent.id}
              agentName={agent.name}
              existing={wechatExisting}
            />
          )}
        </div>
      </section>
    </div>
  );
}
