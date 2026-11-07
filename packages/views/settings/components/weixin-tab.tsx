"use client";

import { useEffect, useState } from "react";
import { QRCode } from "react-qr-code";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, clientErrorMessage } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import type { WeixinInstallation, WeixinLogin } from "@multica/core/types";
import {
  weixinInstallationsOptions,
  weixinKeys,
  isWeixinLoginPending,
} from "@multica/core/weixin";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { WeChat } from "@multica/ui/icons";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";

export function WeixinTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const query = useQuery(weixinInstallationsOptions(wsId));
  const { getAgentName } = useActorName();
  if (query.isPending) return <p role="status">{t(($) => $.weixin.loading)}</p>;
  if (query.isError)
    return <p role="alert">{t(($) => $.weixin.load_failed)}</p>;
  if (!query.data.configured)
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.weixin.not_enabled)}
      </p>
    );
  if (!query.data.installations.length)
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.weixin.empty)}
      </p>
    );
  return (
    <div className="space-y-4">
      {query.data.installations.map((installation) => (
        <div
          key={installation.id}
          className="flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4"
        >
          <div className="flex min-w-0 items-center gap-3">
            <ActorAvatar
              actorType="agent"
              actorId={installation.agent_id}
              size="lg"
              profileLink
            />
            <span className="truncate text-body font-medium">
              {getAgentName(installation.agent_id)}
            </span>
          </div>
          <WeixinAgentBindButton
            agentId={installation.agent_id}
            agentName={getAgentName(installation.agent_id)}
          />
        </div>
      ))}
    </div>
  );
}

export function WeixinAgentBindButton({
  agentId,
  agentName,
}: {
  agentId: string;
  agentName: string;
}) {
  const wsId = useWorkspaceId();
  const { t } = useT("settings");
  const { member } = useCurrentMember(wsId);
  const listing = useQuery(weixinInstallationsOptions(wsId));
  const canManage = member?.role === "owner" || member?.role === "admin";
  const active = listing.data?.installations.find(
    (item) => item.agent_id === agentId && item.status === "active",
  );
  if (listing.isError)
    return (
      <p role="alert" className="text-caption">
        {t(($) => $.weixin.load_failed)}
      </p>
    );
  if (listing.isPending)
    return (
      <p role="status" className="text-caption">
        {t(($) => $.weixin.loading)}
      </p>
    );
  if (!listing.data.configured)
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.weixin.not_enabled)}
      </p>
    );
  if (active)
    return <WeixinDisconnect installation={active} canManage={canManage} />;
  if (!canManage)
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.weixin.admin_only)}
      </p>
    );
  if (listing.data.install_supported !== true)
    return <p className="text-caption">{t(($) => $.weixin.not_enabled)}</p>;
  return <WeixinConnect agentId={agentId} agentName={agentName} />;
}

function WeixinDisconnect({
  installation,
  canManage,
}: {
  installation: WeixinInstallation;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const revoke = useMutation({
    mutationFn: () => api.deleteWeixinInstallation(wsId, installation.id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: weixinKeys.installations(wsId) });
      setOpen(false);
    },
  });
  return (
    <div className="flex items-center gap-3">
      <span className="text-caption text-muted-foreground">
        {t(($) => $.weixin.connected)}
      </span>
      {canManage && (
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          {t(($) => $.weixin.disconnect)}
        </Button>
      )}
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!revoke.isPending) setOpen(value);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.weixin.disconnect_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.weixin.disconnect_description)}
            </DialogDescription>
          </DialogHeader>
          {revoke.isError && (
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.weixin.action_failed)}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              disabled={revoke.isPending}
              onClick={() => setOpen(false)}
            >
              {t(($) => $.weixin.cancel)}
            </Button>
            <Button
              variant="destructive"
              disabled={revoke.isPending}
              onClick={() => revoke.mutate()}
            >
              {t(($) => $.weixin.disconnect)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function WeixinConnect({
  agentId,
  agentName,
}: {
  agentId: string;
  agentName: string;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [loginId, setLoginId] = useState("");
  const [code, setCode] = useState("");
  const [closing, setClosing] = useState(false);
  const [closeError, setCloseError] = useState(false);
  const key = weixinKeys.login(wsId, loginId);
  const begin = useMutation({
    mutationFn: async () => {
      const login = await api.beginWeixinLogin(wsId, agentId);
      if (!login.id || login.status === "error")
        throw new Error(t(($) => $.weixin.action_failed));
      return login;
    },
    onSuccess: (login) => {
      qc.setQueryData(weixinKeys.login(wsId, login.id), login);
      setLoginId(login.id);
      setCode("");
    },
  });
  const poll = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => api.pollWeixinLogin(wsId, loginId, "", signal),
    enabled: (query) =>
      open &&
      !!loginId &&
      !closing &&
      isWeixinLoginPending(
        (query.state.data as WeixinLogin | undefined)?.status,
      ) &&
      (query.state.data as WeixinLogin | undefined)?.status !==
        "need_verifycode",
    refetchInterval: (query) =>
      isWeixinLoginPending(
        (query.state.data as WeixinLogin | undefined)?.status,
      ) &&
      (query.state.data as WeixinLogin | undefined)?.status !==
        "need_verifycode"
        ? 2000
        : false,
    refetchOnWindowFocus: false,
    retry: false,
    gcTime: 0,
  });
  const login = poll.data;
  const refetchLogin = poll.refetch;
  const verify = useMutation({
    mutationFn: () => api.pollWeixinLogin(wsId, loginId, code.trim()),
    onSuccess: (value) => {
      qc.setQueryData(key, value);
      setCode("");
    },
  });
  useEffect(() => {
    if (login?.status === "confirmed")
      void qc.invalidateQueries({ queryKey: weixinKeys.installations(wsId) });
  }, [login?.status, qc, wsId]);
  useEffect(() => {
    if (!open || !login || !isWeixinLoginPending(login.status)) return;
    const delay = Date.parse(login.expires_at) - Date.now();
    if (!Number.isFinite(delay)) return;
    const timer = setTimeout(
      () => {
        void refetchLogin();
      },
      Math.max(0, delay),
    );
    return () => clearTimeout(timer);
  }, [open, login, refetchLogin]);

  async function close() {
    if (begin.isPending || verify.isPending || closing) return;
    setClosing(true);
    setCloseError(false);
    try {
      await qc.cancelQueries({ queryKey: key });
      if (loginId) await api.cancelWeixinLogin(wsId, loginId);
      await qc.invalidateQueries({ queryKey: weixinKeys.installations(wsId) });
      qc.removeQueries({ queryKey: key });
      setOpen(false);
      setLoginId("");
      setCode("");
    } catch {
      setCloseError(true);
    } finally {
      setClosing(false);
    }
  }

  function start() {
    setOpen(true);
    setLoginId("");
    begin.mutate();
  }

  const pending = isWeixinLoginPending(login?.status);
  const status = login?.status;
  const statusText =
    status === "confirmed"
      ? t(($) => $.weixin.success)
      : status === "scaned" || status === "scaned_but_redirect"
        ? t(($) => $.weixin.scanned)
        : status === "need_verifycode"
          ? t(($) => $.weixin.verify_hint)
          : status === "expired"
            ? t(($) => $.weixin.expired)
            : status === "verify_code_blocked"
              ? t(($) => $.weixin.blocked)
              : status === "binded_redirect"
                ? t(($) => $.weixin.already_bound)
                : status === "error" || status === "cancelled"
                  ? t(($) => $.weixin.action_failed)
                  : t(($) => $.weixin.scan_hint);
  const failed = begin.isError || poll.isError || verify.isError || closeError;
  return (
    <>
      <Button variant="outline" size="sm" onClick={start}>
        <WeChat className="size-4" aria-hidden="true" />
        {t(($) => $.weixin.connect)}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!value) void close();
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.weixin.connect_title, { agent: agentName })}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.weixin.privacy_hint)}
            </DialogDescription>
          </DialogHeader>
          {begin.isPending ? (
            <p role="status">{t(($) => $.weixin.loading)}</p>
          ) : (
            <>
              {pending && login?.qr_code_url && (
                <div className="mx-auto rounded-lg bg-white p-4">
                  <QRCode
                    value={login.qr_code_url}
                    size={200}
                    title={t(($) => $.weixin.qr_label)}
                  />
                </div>
              )}
              <p role="status" className="text-body">
                {statusText}
              </p>
              {status === "need_verifycode" && (
                <form
                  className="space-y-3"
                  onSubmit={(event) => {
                    event.preventDefault();
                    verify.mutate();
                  }}
                >
                  <Label htmlFor={`weixin-code-${agentId}`}>
                    {t(($) => $.weixin.verify_label)}
                  </Label>
                  <Input
                    id={`weixin-code-${agentId}`}
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    pattern="[0-9]+"
                    maxLength={12}
                    value={code}
                    onChange={(event) => setCode(event.target.value)}
                    disabled={verify.isPending}
                  />
                  <Button
                    type="submit"
                    disabled={verify.isPending || !/^\d{1,12}$/.test(code)}
                  >
                    {t(($) => $.weixin.verify)}
                  </Button>
                </form>
              )}
            </>
          )}
          {failed && (
            <p role="alert" className="text-caption text-destructive">
              {clientErrorMessage(begin.error ?? poll.error ?? verify.error) ??
                t(($) => $.weixin.action_failed)}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => void close()}
              disabled={begin.isPending || verify.isPending || closing}
            >
              {t(($) => $.weixin.close)}
            </Button>
            {!begin.isPending &&
              status !== "confirmed" &&
              (!pending || begin.isError) && (
                <Button onClick={start}>{t(($) => $.weixin.refresh)}</Button>
              )}
            {poll.isError && pending && (
              <Button onClick={() => void poll.refetch()}>
                {t(($) => $.weixin.retry)}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
