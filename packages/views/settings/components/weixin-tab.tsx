"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw, Trash2 } from "lucide-react";
// Named import: see lark-tab.tsx — the default import breaks under electron-vite.
import { QRCode } from "react-qr-code";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent, WeixinInstallation, WeixinLogin } from "@multica/core/types";
import { weixinInstallationsOptions, weixinKeys, weixinLoginOptions } from "@multica/core/weixin";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { WeixinMark } from "./weixin-mark";
import { useT } from "../../i18n";

/**
 * The WeChat card on an agent's Integrations tab. Unlike the workspace-bot
 * platforms, every member connects their own WeChat: the relay chats with the
 * agent as that member, so anyone who may chat with the agent may connect,
 * and the installer or a workspace owner/admin may disconnect.
 */
export function WeixinAgentSection({ agent }: { agent: Agent }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const [dialogOpen, setDialogOpen] = useState(false);

  const { data: listing } = useQuery(weixinInstallationsOptions(wsId));
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: !!wsId });
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const isAdmin = currentMember?.role === "owner" || currentMember?.role === "admin";

  const installations = (listing?.installations ?? [])
    .filter((inst) => inst.agent_id === agent.id)
    // The caller's own connection first.
    .sort((a, b) => Number(b.installer_user_id === user?.id) - Number(a.installer_user_id === user?.id));
  const mine = installations.find((inst) => inst.installer_user_id === user?.id);

  return (
    <section className="rounded-lg border" data-testid="weixin-agent-section">
      <div className="flex items-start gap-3 p-4">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-muted/40 text-muted-foreground">
          <WeixinMark className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1 space-y-1">
          <h3 className="text-body font-medium">{t(($) => $.weixin.section_title)}</h3>
          <p className="text-caption leading-relaxed text-muted-foreground">
            {t(($) => $.weixin.page_description)}
          </p>
        </div>
      </div>
      <div className="space-y-3 border-t px-4 py-3">
        {!listing ? null : !listing.configured ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.weixin.not_enabled_title)}
          </p>
        ) : (
          <>
            {installations.map((inst) => (
              <WeixinInstallationRow
                key={inst.id}
                installation={inst}
                isMine={inst.installer_user_id === user?.id}
                canDisconnect={inst.installer_user_id === user?.id || isAdmin}
              />
            ))}
            {!mine &&
              (listing.install_supported ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setDialogOpen(true)}
                  data-testid="weixin-agent-connect"
                >
                  <WeixinMark className="h-3 w-3" />
                  {t(($) => $.weixin.connect_button)}
                </Button>
              ) : (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.weixin.unavailable)}
                </p>
              ))}
          </>
        )}
      </div>
      {dialogOpen && (
        <WeixinConnectDialog agent={agent} onClose={() => setDialogOpen(false)} />
      )}
    </section>
  );
}

function WeixinInstallationRow({
  installation,
  isMine,
  canDisconnect,
}: {
  installation: WeixinInstallation;
  isMine: boolean;
  canDisconnect: boolean;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { getMemberName } = useActorName();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const healthy = installation.status === "active";

  async function handleDisconnect() {
    if (disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteWeixinInstallation(wsId, installation.id);
      await qc.invalidateQueries({ queryKey: weixinKeys.installations(wsId) });
      toast.success(t(($) => $.weixin.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.weixin.toast_disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-1" data-testid="weixin-installation">
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span
            className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${healthy ? "bg-emerald-500" : "bg-destructive"}`}
          />
          <span className="truncate">
            {isMine
              ? t(($) => $.weixin.connected_mine)
              : t(($) => $.weixin.connected_member, {
                  name: getMemberName(installation.installer_user_id),
                })}
          </span>
        </span>
        {canDisconnect && (
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirmOpen(true)}
            disabled={disconnecting}
            data-testid="weixin-installation-disconnect"
          >
            <Trash2 className="h-3 w-3" />
            {disconnecting ? t(($) => $.weixin.disconnecting) : t(($) => $.weixin.disconnect)}
          </Button>
        )}
      </div>
      {!healthy && installation.last_error && (
        <p className="text-caption text-destructive">{installation.last_error}</p>
      )}

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setConfirmOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.weixin.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.weixin.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.weixin.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting ? t(($) => $.weixin.disconnecting) : t(($) => $.weixin.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * Starts a QR login on mount, polls it, and turns it into an installation
 * the moment it connects. Weixin may ask for the number shown on the phone.
 */
function WeixinConnectDialog({ agent, onClose }: { agent: Agent; onClose: () => void }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [started, setStarted] = useState<WeixinLogin | null>(null);
  const [startError, setStartError] = useState<string | null>(null);
  const [verifyCode, setVerifyCode] = useState("");
  const [submittingCode, setSubmittingCode] = useState(false);
  const [completing, setCompleting] = useState(false);
  const startedFor = useRef<number>(-1);
  const completedFor = useRef<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    // One login per attempt, even under StrictMode's double effect.
    if (startedFor.current === attempt) return;
    startedFor.current = attempt;
    setStarted(null);
    setStartError(null);
    api
      .startWeixinLogin(wsId, agent.id)
      .then(setStarted)
      .catch((e: unknown) =>
        setStartError(e instanceof Error ? e.message : t(($) => $.weixin.dialog_failed)),
      );
  }, [attempt, wsId, agent.id, t]);

  const { data: polled } = useQuery({
    ...weixinLoginOptions(wsId, started?.id ?? ""),
    enabled: !!started?.id,
  });
  const login = polled ?? started;

  useEffect(() => {
    if (login?.state !== "connected" || completedFor.current === login.id) return;
    completedFor.current = login.id;
    setCompleting(true);
    api
      .completeWeixinLogin(wsId, login.id)
      .then(async () => {
        await qc.invalidateQueries({ queryKey: weixinKeys.installations(wsId) });
        toast.success(t(($) => $.weixin.connect_success_toast));
        onClose();
      })
      .catch((e: unknown) => {
        toast.error(e instanceof Error ? e.message : t(($) => $.weixin.connect_failed_toast));
        setCompleting(false);
      });
  }, [login?.state, login?.id, wsId, qc, t, onClose]);

  async function submitVerifyCode() {
    const code = verifyCode.trim();
    if (!login || !code || submittingCode) return;
    setSubmittingCode(true);
    try {
      const updated = await api.submitWeixinVerifyCode(wsId, login.id, code);
      qc.setQueryData(weixinKeys.login(wsId, login.id), updated);
      setVerifyCode("");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.weixin.connect_failed_toast));
    } finally {
      setSubmittingCode(false);
    }
  }

  const failed = !!startError || login?.state === "failed";

  return (
    <Dialog open onOpenChange={(v) => !v && !completing && onClose()}>
      <DialogContent className="sm:max-w-md" data-testid="weixin-connect-dialog">
        <DialogHeader>
          <DialogTitle>{t(($) => $.weixin.dialog_title, { agent: agent.name })}</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col items-center gap-3 py-2">
          {failed ? (
            <div className="space-y-1 text-center">
              <p className="text-body font-medium">{t(($) => $.weixin.dialog_failed)}</p>
              <p className="text-caption text-muted-foreground">{startError ?? login?.message}</p>
            </div>
          ) : completing || login?.state === "connected" ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.weixin.dialog_connecting)}
            </p>
          ) : login?.state === "need_verify_code" ? (
            <div className="w-full space-y-1.5">
              <Label htmlFor="weixin-verify-code">{t(($) => $.weixin.dialog_verify_label)}</Label>
              <Input
                id="weixin-verify-code"
                data-testid="weixin-verify-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                value={verifyCode}
                onChange={(e) => setVerifyCode(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && void submitVerifyCode()}
                disabled={submittingCode}
              />
              {login.verify_code_invalid && (
                <p className="text-caption text-destructive">
                  {t(($) => $.weixin.dialog_verify_invalid)}
                </p>
              )}
            </div>
          ) : login?.qr_content ? (
            <>
              <div className="rounded-md border bg-white p-3" data-testid="weixin-qr">
                <QRCode value={login.qr_content} size={192} />
              </div>
              <p className="text-center text-caption text-muted-foreground">
                {login.state === "scanned"
                  ? t(($) => $.weixin.dialog_scanned)
                  : t(($) => $.weixin.dialog_scan_hint, { agent: agent.name })}
              </p>
            </>
          ) : (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.weixin.dialog_loading)}
            </p>
          )}
        </div>

        <DialogFooter>
          {failed && (
            <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
              <RefreshCw className="h-3 w-3" />
              {t(($) => $.weixin.dialog_retry)}
            </Button>
          )}
          {login?.state === "need_verify_code" && (
            <Button
              size="sm"
              onClick={() => void submitVerifyCode()}
              disabled={!verifyCode.trim() || submittingCode}
            >
              {t(($) => $.weixin.dialog_verify_submit)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
