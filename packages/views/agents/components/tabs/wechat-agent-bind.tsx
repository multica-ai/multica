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

export function WechatAgentBindSection({
  agent,
  canManage,
}: {
  agent: Agent;
  canManage: boolean;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

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
          <h3 className="text-sm font-medium">企业微信</h3>
          <p className="text-xs leading-relaxed text-muted-foreground">
            将企业微信智能机器人绑定到此智能体。发送给该机器人的消息将由此智能体处理。
          </p>
        </div>
      </div>
      <div className="border-t px-4 py-3">
        {!canManage ? (
          <p className="text-xs text-muted-foreground">
            仅工作区所有者和管理员可管理机器人绑定。
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

  async function handleDisconnect() {
    setDisconnecting(true);
    try {
      await api.deleteWechatInstallation(wsId, installationId);
      toast.success("机器人已断开");
      onDisconnected();
    } catch (e: any) {
      if (e?.status === 404 || e?.message?.includes("404")) {
        toast.success("机器人已断开");
        onDisconnected();
      } else {
        toast.error(e instanceof Error ? e.message : "断开连接失败");
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
          <p className="text-sm font-medium text-green-600">已连接</p>
          <p className="text-xs text-muted-foreground">
            Bot ID: <code className="rounded bg-muted px-1 py-0.5">{botId}</code>
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="text-muted-foreground hover:text-destructive"
          onClick={() => setShowConfirm(true)}
        >
          <Trash2 className="mr-1 h-3.5 w-3.5" />
          断开连接
        </Button>
      </div>

      <AlertDialog open={showConfirm} onOpenChange={setShowConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>断开机器人连接？</AlertDialogTitle>
            <AlertDialogDescription>
              断开后机器人将不再接收消息。你可以稍后重新连接。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDisconnect}
              disabled={disconnecting}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {disconnecting ? "断开中..." : "断开连接"}
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

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!botId.trim() || !secret.trim()) {
      toast.error("Bot ID 和 Secret 不能为空");
      return;
    }
    setSubmitting(true);
    try {
      await api.createWechatInstallation(wsId, agentId, botId.trim(), secret.trim());
      toast.success("机器人已连接");
      setBotId("");
      setSecret("");
      onBound();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "连接机器人失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3">
      <div className="space-y-1.5">
        <Label htmlFor="wechat-bot-id" className="text-xs">
          Bot ID
        </Label>
        <Input
          id="wechat-bot-id"
          placeholder="企业微信智能机器人 Bot ID"
          value={botId}
          onChange={(e) => setBotId(e.target.value)}
          className="h-8 text-sm"
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="wechat-secret" className="text-xs">
          Secret
        </Label>
        <Input
          id="wechat-secret"
          type="password"
          placeholder="企业微信管理后台的机器人 Secret"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          className="h-8 text-sm"
        />
      </div>
      <Button type="submit" size="sm" disabled={submitting}>
        {submitting ? "连接中..." : "连接机器人"}
      </Button>
    </form>
  );
}
