"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { MessageSquare, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { wechatInstallationsOptions, wechatKeys } from "@multica/core/wechat";
import { api } from "@multica/core/api";
import { useT } from "../../../i18n";

export function WechatAgentBindSection({
  agent,
  canManage,
}: {
  agent: Agent;
  canManage: boolean;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { t } = useT("settings");

  const { data } = useQuery({
    ...wechatInstallationsOptions(wsId),
    enabled: !!wsId,
  });

  const binding = data?.installations.find(
    (i) => i.agent_id === agent.id && i.status === "active",
  );

  return (
    <section className="rounded-lg border">
      <div className="flex items-start gap-3 p-4">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
          <MessageSquare className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1 space-y-1">
          <h3 className="text-sm font-medium">
            {t(($) => $.wechat.agent_section_title)}
          </h3>
          <p className="text-xs leading-relaxed text-muted-foreground">
            {t(($) => $.wechat.agent_section_description)}
          </p>
        </div>
      </div>
      <div className="border-t px-4 py-3">
        {!canManage ? (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.wechat.no_manage_permission)}
          </p>
        ) : binding ? (
          <BoundState
            botId={binding.bot_id}
            installationId={binding.id}
            wsId={wsId}
            onDisconnected={() =>
              qc.invalidateQueries({ queryKey: wechatKeys.installations(wsId) })
            }
          />
        ) : (
          <BindForm
            agentId={agent.id}
            wsId={wsId}
            onBound={() =>
              qc.invalidateQueries({ queryKey: wechatKeys.installations(wsId) })
            }
          />
        )}
      </div>
    </section>
  );
}

function BoundState({
  botId,
  installationId,
  wsId,
  onDisconnected,
}: {
  botId: string;
  installationId: string;
  wsId: string;
  onDisconnected: () => void;
}) {
  const [showConfirm, setShowConfirm] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const { t } = useT("settings");

  async function handleDisconnect() {
    setDisconnecting(true);
    try {
      await api.deleteWechatInstallation(wsId, installationId);
      toast.success(t(($) => $.wechat.toast_disconnected));
      onDisconnected();
    } catch (e: any) {
      if (e?.status === 404 || e?.message?.includes("404")) {
        toast.success(t(($) => $.wechat.toast_disconnected));
        onDisconnected();
      } else {
        toast.error(
          e instanceof Error
            ? e.message
            : t(($) => $.wechat.toast_disconnect_failed),
        );
      }
    } finally {
      setDisconnecting(false);
      setShowConfirm(false);
    }
  }

  return (
    <>
      <div className="flex items-center justify-between">
        <div className="space-y-0.5">
          <p className="text-sm font-medium text-green-600">
            {t(($) => $.wechat.connected)}
          </p>
          <p className="text-xs text-muted-foreground">
            {"Bot ID: "}
            <code className="rounded bg-muted px-1 py-0.5">{botId}</code>
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="text-muted-foreground hover:text-destructive"
          onClick={() => setShowConfirm(true)}
        >
          <Trash2 className="mr-1 h-3.5 w-3.5" />
          {t(($) => $.wechat.disconnect)}
        </Button>
      </div>

      <AlertDialog open={showConfirm} onOpenChange={setShowConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.wechat.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.wechat.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.wechat.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDisconnect}
              disabled={disconnecting}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {disconnecting
                ? t(($) => $.wechat.disconnecting)
                : t(($) => $.wechat.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function BindForm({
  agentId,
  wsId,
  onBound,
}: {
  agentId: string;
  wsId: string;
  onBound: () => void;
}) {
  const [botId, setBotId] = useState("");
  const [secret, setSecret] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const { t } = useT("settings");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!botId.trim() || !secret.trim()) {
      toast.error(t(($) => $.wechat.toast_fields_required));
      return;
    }
    setSubmitting(true);
    try {
      await api.createWechatInstallation(wsId, agentId, botId.trim(), secret.trim());
      toast.success(t(($) => $.wechat.toast_connected));
      setBotId("");
      setSecret("");
      onBound();
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.wechat.toast_connect_failed),
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3">
      <div className="space-y-1.5">
        <Label htmlFor="wechat-bot-id" className="text-xs">
          {t(($) => $.wechat.bot_id_label)}
        </Label>
        <Input
          id="wechat-bot-id"
          placeholder={t(($) => $.wechat.bot_id_placeholder)}
          value={botId}
          onChange={(e) => setBotId(e.target.value)}
          className="h-8 text-sm"
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="wechat-secret" className="text-xs">
          {t(($) => $.wechat.secret_label)}
        </Label>
        <Input
          id="wechat-secret"
          type="password"
          placeholder={t(($) => $.wechat.secret_placeholder)}
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          className="h-8 text-sm"
        />
      </div>
      <Button type="submit" size="sm" disabled={submitting}>
        {submitting
          ? t(($) => $.wechat.connecting)
          : t(($) => $.wechat.connect_bot)}
      </Button>
    </form>
  );
}
