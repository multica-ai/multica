"use client";

import { useQuery } from "@tanstack/react-query";
import { Webhook } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
import { wechatInstallationsOptions } from "@multica/core/wechat";
import { memberListOptions } from "@multica/core/workspace/queries";
import { LarkAgentBindButton } from "../../../settings/components/lark-tab";
import { WechatAgentBindSection } from "./wechat-agent-bind";
import { useT } from "../../../i18n";

export function IntegrationsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const { t: ts } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);

  const { data: listing } = useQuery({
    ...larkInstallationsOptions(wsId),
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
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const hasActiveInstall =
    listing?.installations.some(
      (inst) => inst.agent_id === agent.id && inst.status === "active",
    ) ?? false;

  const wechatConfigured = wechatListing?.configured === true;

  return (
    <div className="space-y-6">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.integrations.intro)}
      </p>

      {/* Lark section */}
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
            <p className="text-xs text-muted-foreground">
              {ts(($) => $.lark.not_enabled_title)}
            </p>
          ) : !canManage ? (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.integrations.members_note)}
            </p>
          ) : !installSupported && !hasActiveInstall ? (
            <div className="space-y-1">
              <p className="text-xs font-medium">{ts(($) => $.lark.preview_title)}</p>
              <p className="text-xs text-muted-foreground">
                {ts(($) => $.lark.preview_description)}
              </p>
            </div>
          ) : (
            <LarkAgentBindButton agentId={agent.id} agentName={agent.name} />
          )}
        </div>
      </section>

      {/* WeChat Work section */}
      {wechatConfigured && (
        <WechatAgentBindSection agent={agent} canManage={canManage} />
      )}
    </div>
  );
}
