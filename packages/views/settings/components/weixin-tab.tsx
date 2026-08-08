"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { QRCode } from "react-qr-code";
import { CheckCircle2, ScanLine, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import type { WeixinInstallation } from "@multica/core/types";
import { weixinInstallationsOptions, weixinKeys } from "@multica/core/weixin";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export function WeixinTab() {
  const { t } = useT("settings");
  const workspaceId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const { data, isLoading } = useQuery({
    ...weixinInstallationsOptions(workspaceId),
    enabled: !!workspaceId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(workspaceId),
    enabled: !!workspaceId,
  });
  const member = members.find((item) => item.user_id === user?.id);
  const canManage = member?.role === "owner" || member?.role === "admin";

  async function disconnect(installationId: string) {
    try {
      await api.deleteWeixinInstallation(workspaceId, installationId);
      await qc.invalidateQueries({ queryKey: weixinKeys.installations(workspaceId) });
      toast.success(t(($) => $.weixin.toast_disconnected));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.weixin.toast_failed));
    }
  }

  if (!data?.configured) {
    return (
      <Card>
        <CardContent className="space-y-2">
          <p className="text-body font-medium">{t(($) => $.weixin.not_enabled_title)}</p>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.weixin.not_enabled_description)}{" "}
            <code className="rounded bg-muted px-1 py-0.5 text-micro">
              MULTICA_WEIXIN_SECRET_KEY
            </code>
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-3">
      <p className="text-body text-muted-foreground">
        {t(($) => $.weixin.page_description)}
      </p>
      <Card>
        <CardContent className="divide-y">
          {isLoading ? (
            <p className="py-3 text-caption text-muted-foreground">
              {t(($) => $.weixin.loading)}
            </p>
          ) : (data.installations ?? []).length === 0 ? (
            <p className="py-3 text-caption text-muted-foreground">
              {t(($) => $.weixin.empty)}
            </p>
          ) : (
            data.installations.map((installation) => (
              <div key={installation.id} className="flex items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="text-body font-medium">
                    {installation.status === "active"
                      ? t(($) => $.weixin.connected)
                      : t(($) => $.weixin.revoked)}
                  </p>
                  <p className="truncate text-micro text-muted-foreground">
                    {t(($) => $.weixin.bot_id, { botId: installation.bot_id })}
                  </p>
                </div>
                {canManage && installation.status === "active" && (
                  <Button variant="outline" size="sm" onClick={() => disconnect(installation.id)}>
                    <Trash2 className="h-3 w-3" />
                    {t(($) => $.weixin.disconnect)}
                  </Button>
                )}
              </div>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export function WeixinAgentBindButton({
  agentId,
  agentName,
  className,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
}) {
  const { t } = useT("settings");
  const workspaceId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [starting, setStarting] = useState(false);
  const [session, setSession] = useState<{ id: string; qrCodeURL: string } | null>(null);
  const [startError, setStartError] = useState("");
  const completedRef = useRef(false);

  const { data: listing } = useQuery({
    ...weixinInstallationsOptions(workspaceId),
    enabled: !!workspaceId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(workspaceId),
    enabled: !!workspaceId,
  });
  const member = members.find((item) => item.user_id === user?.id);
  const canManage = member?.role === "owner" || member?.role === "admin";
  const existing = listing?.installations.find(
    (item) => item.agent_id === agentId && item.status === "active",
  );

  const statusQuery = useQuery({
    queryKey: weixinKeys.installStatus(workspaceId, session?.id ?? ""),
    queryFn: () => api.getWeixinInstallStatus(workspaceId, session!.id),
    enabled: dialogOpen && !!session?.id,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "confirmed" || status === "expired" ? false : 1500;
    },
    retry: 1,
  });

  useEffect(() => {
    if (statusQuery.data?.status !== "confirmed" || completedRef.current) return;
    completedRef.current = true;
    void qc.invalidateQueries({ queryKey: weixinKeys.installations(workspaceId) });
    toast.success(t(($) => $.weixin.toast_connected));
    setDialogOpen(false);
  }, [qc, statusQuery.data?.status, t, workspaceId]);

  if (!canManage || !listing?.install_supported) return null;

  async function startLogin() {
    setDialogOpen(true);
    setStarting(true);
    setStartError("");
    setSession(null);
    completedRef.current = false;
    try {
      const result = await api.beginWeixinInstall(workspaceId, agentId);
      setSession({ id: result.session_id, qrCodeURL: result.qr_code_url });
    } catch (error) {
      setStartError(error instanceof Error ? error.message : t(($) => $.weixin.toast_failed));
    } finally {
      setStarting(false);
    }
  }

  async function disconnect(installation: WeixinInstallation) {
    try {
      await api.deleteWeixinInstallation(workspaceId, installation.id);
      await qc.invalidateQueries({ queryKey: weixinKeys.installations(workspaceId) });
      toast.success(t(($) => $.weixin.toast_disconnected));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.weixin.toast_failed));
    }
  }

  if (existing) {
    return (
      <div className={cn("flex items-center justify-between gap-3", className)}>
        <span className="inline-flex items-center gap-2 text-caption text-muted-foreground">
          <CheckCircle2 className="h-4 w-4 text-emerald-500" />
          {t(($) => $.weixin.agent_connected)}
        </span>
        <Button variant="outline" size="sm" onClick={() => disconnect(existing)}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.weixin.disconnect)}
        </Button>
      </div>
    );
  }

  return (
    <div className={className}>
      <Button
        variant="outline"
        size="sm"
        onClick={startLogin}
        disabled={starting || !agentId}
        title={agentName ? t(($) => $.weixin.bind_title, { agent: agentName }) : undefined}
      >
        <ScanLine className="h-3 w-3" />
        {starting ? t(($) => $.weixin.starting) : t(($) => $.weixin.bind_button)}
      </Button>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.weixin.dialog_title)}</DialogTitle>
          </DialogHeader>
          <div className="flex min-h-64 flex-col items-center justify-center gap-4 py-4 text-center">
            {starting ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.weixin.starting)}</p>
            ) : startError ? (
              <div className="space-y-3">
                <p className="text-caption text-destructive">{startError}</p>
                <Button size="sm" onClick={startLogin}>{t(($) => $.weixin.retry)}</Button>
              </div>
            ) : session ? (
              <>
                <div className="rounded-lg bg-white p-3">
                  <QRCode value={session.qrCodeURL} size={208} />
                </div>
                <div className="space-y-1">
                  <p className="text-body font-medium">
                    {statusQuery.data?.status === "scanned"
                      ? t(($) => $.weixin.scanned)
                      : statusQuery.data?.status === "expired"
                        ? t(($) => $.weixin.expired)
                        : t(($) => $.weixin.scan_hint)}
                  </p>
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.weixin.private_hint)}
                  </p>
                </div>
              </>
            ) : null}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
