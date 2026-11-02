"use client";

import { Trash2 } from "@multica/ui/icons";
import type { LarkInstallation, ListLarkInstallationsResponse } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "../../common/actor-avatar";
import { useLocale, useT } from "../../i18n";
import { LarkConversationForm } from "./lark-conversation-form";

export function LarkInstallationList({
  data,
  isLoading,
  canManage,
  conversationWritable,
  workspaceId,
  onDisconnect,
}: {
  data: ListLarkInstallationsResponse | undefined;
  isLoading: boolean;
  canManage: boolean;
  conversationWritable: boolean;
  workspaceId: string;
  onDisconnect: (id: string) => void;
}) {
  const { t } = useT("settings");
  if (isLoading) {
    return (
      <Card>
        <CardContent>
          <p className="text-body text-muted-foreground">{t(($) => $.lark.loading)}</p>
        </CardContent>
      </Card>
    );
  }
  if (!data) return null;
  if (data.configured !== true) {
    return (
      <Card>
        <CardContent className="space-y-2">
          <p className="text-body font-medium">{t(($) => $.lark.not_enabled_title)}</p>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.lark.not_enabled_description_prefix)}{" "}
            <code className="rounded-xs bg-muted px-1 py-0.5 text-micro">
              MULTICA_LARK_SECRET_KEY
            </code>{" "}
            {t(($) => $.lark.not_enabled_description_suffix)}{" "}
            {t(($) => $.lark.not_enabled_self_host_hint)}
          </p>
        </CardContent>
      </Card>
    );
  }
  const installations = data.installations;
  // An unwired device-flow install hides the empty-state CTA, but existing
  // bots remain visible and manageable.
  if (data.install_supported !== true && installations.length === 0) {
    return (
      <Card>
        <CardContent className="space-y-2">
          <p className="text-body font-medium">{t(($) => $.lark.preview_title)}</p>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.lark.preview_description)}
          </p>
        </CardContent>
      </Card>
    );
  }
  return (
    <section className="space-y-3">
      <h2 className="text-body font-semibold">{t(($) => $.lark.connected_bots)}</h2>
      {installations.length === 0 ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.lark.empty_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.lark.empty_description_prefix)}{" "}
              <strong>{t(($) => $.lark.empty_description_cta)}</strong>{" "}
              {t(($) => $.lark.empty_description_suffix)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="divide-y">
            {installations.map((inst) => (
              <InstallationRow
                key={inst.id}
                installation={inst}
                canManage={canManage}
                conversationSupported={data.conversation_supported === true || inst.conversation != null}
                conversationWritable={conversationWritable}
                workspaceId={workspaceId}
                onDisconnect={() => onDisconnect(inst.id)}
              />
            ))}
          </CardContent>
        </Card>
      )}
    </section>
  );
}

function InstallationRow({
  conversationSupported,
  workspaceId,
  conversationWritable,
  installation,
  canManage,
  onDisconnect,
}: {
  installation: LarkInstallation;
  workspaceId: string;
  conversationWritable: boolean;
  conversationSupported: boolean;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const locale = useLocale();
  // The bot is bound 1:1 to a Multica Agent (per the (workspace_id,
  // agent_id) UNIQUE in lark_installation). Render the Multica agent's
  // identity here rather than the raw Lark app_id / bot_open_id — those
  // mean nothing to product users. getAgentName falls back to
  // "Unknown Agent" when the agent has been deleted; the Disconnect
  // affordance below is the recovery path for that orphan row.
  const { getAgentName } = useActorName();
  const isActive = installation.status === "active";
  const agentName = getAgentName(installation.agent_id);
  return (
    <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
      <div className="flex items-start gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={installation.agent_id}
          size="lg"
          enableHoverCard
          profileLink
        />
        <div className="space-y-1">
          <p className="text-body font-medium">
            {agentName}
            <span className="ml-2 rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
              {installation.region === "lark"
                ? t(($) => $.lark.region_lark)
                : t(($) => $.lark.region_feishu)}
            </span>
            {!isActive && (
              <span className="ml-2 rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.lark.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.lark.installed_at_label, {
              when: new Date(installation.installed_at).toLocaleString(locale),
            })}
          </p>
          {canManage && isActive && conversationSupported && <LarkConversationForm workspaceId={workspaceId} installation={installation} disabled={!conversationWritable} />}
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.lark.disconnect)}
        </Button>
      )}
    </div>
  );
}
