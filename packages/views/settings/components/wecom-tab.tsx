"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { wecomInstallationsOptions, wecomKeys } from "@multica/core/wecom";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

export function WecomTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const [agentId, setAgentId] = useState("");
  const [botId, setBotId] = useState("");
  const [botSecret, setBotSecret] = useState("");
  const [corpId, setCorpId] = useState("");
  const [corpSecret, setCorpSecret] = useState("");
  const [selfBuildAgentId, setSelfBuildAgentId] = useState("");
  const [saving, setSaving] = useState(false);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...wecomInstallationsOptions(wsId),
    enabled: !!wsId,
  });

  const configured = data?.configured === true;
  const installations = data?.installations ?? [];

  async function handleConnect() {
    if (!wsId) return;
    setSaving(true);
    try {
      await api.createWecomInstallation(wsId, {
        agent_id: agentId.trim(),
        bot_id: botId.trim(),
        bot_secret: botSecret.trim(),
        corp_id: corpId.trim(),
        corp_secret: corpSecret.trim(),
        self_build_agent_id: selfBuildAgentId.trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
      toast.success(t(($) => $.wecom.toast_connected));
      setBotSecret("");
      setCorpSecret("");
    } catch {
      toast.error(t(($) => $.wecom.toast_connect_failed));
    } finally {
      setSaving(false);
    }
  }

  async function handleDisconnect(installationId: string) {
    if (!wsId) return;
    try {
      await api.deleteWecomInstallation(wsId, installationId);
      await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
      toast.success(t(($) => $.wecom.toast_disconnected));
    } catch {
      toast.error(t(($) => $.wecom.toast_disconnect_failed));
    }
  }

  if (!configured) {
    return (
      <Card>
        <CardContent className="space-y-2 pt-6">
          <p className="text-sm font-medium">{t(($) => $.wecom.not_enabled_title)}</p>
          <p className="text-sm text-muted-foreground">
            {t(($) => $.wecom.not_enabled_description)}
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      {canManage && (
        <Card>
          <CardContent className="space-y-4 pt-6">
            <p className="text-sm text-muted-foreground">{t(($) => $.wecom.connect_description)}</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>{t(($) => $.wecom.field_agent_id)}</Label>
                <Input value={agentId} onChange={(e) => setAgentId(e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label>{t(($) => $.wecom.field_bot_id)}</Label>
                <Input value={botId} onChange={(e) => setBotId(e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label>{t(($) => $.wecom.field_bot_secret)}</Label>
                <Input type="password" value={botSecret} onChange={(e) => setBotSecret(e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label>{t(($) => $.wecom.field_corp_id)}</Label>
                <Input value={corpId} onChange={(e) => setCorpId(e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label>{t(($) => $.wecom.field_corp_secret)}</Label>
                <Input type="password" value={corpSecret} onChange={(e) => setCorpSecret(e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label>{t(($) => $.wecom.field_self_build_agent_id)}</Label>
                <Input value={selfBuildAgentId} onChange={(e) => setSelfBuildAgentId(e.target.value)} />
              </div>
            </div>
            <Button size="sm" disabled={saving} onClick={() => void handleConnect()}>
              {saving ? t(($) => $.wecom.connecting) : t(($) => $.wecom.connect)}
            </Button>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        <h3 className="text-sm font-medium">{t(($) => $.wecom.connected_bots)}</h3>
        {isLoading ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.wecom.loading)}</p>
        ) : installations.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t(($) => $.wecom.empty)}</p>
        ) : (
          installations.map((inst) => (
            <Card key={inst.id}>
              <CardContent className="flex items-center justify-between gap-4 pt-6">
                <div className="min-w-0 text-sm">
                  <p className="font-medium truncate">Bot {inst.bot_id}</p>
                  <p className="text-muted-foreground truncate">Agent {inst.agent_id}</p>
                </div>
                {canManage && inst.status === "active" && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void handleDisconnect(inst.id)}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                )}
              </CardContent>
            </Card>
          ))
        )}
      </div>
    </div>
  );
}
