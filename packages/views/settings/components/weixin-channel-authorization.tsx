"use client";

// The QR half of connecting WeChat, rendered inside the shared connection
// dialog like the Lark and WeCom scan flows: the login starts on mount, the QR
// shows immediately, and a confirmed login closes the dialog.

import { useCallback, useEffect, useRef, useState } from "react";
import { QRCode } from "react-qr-code";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, clientErrorMessage } from "@multica/core/api";
import type { WeixinLogin } from "@multica/core/types";
import {
  weixinKeys,
  isWeixinLoginPending,
  isWeixinVerificationRequired,
  weixinConflict,
} from "@multica/core/weixin";
import { Button } from "@multica/ui/components/ui/button";
import { DialogFooter } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { BookOpen } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const pollsProvider = (status?: string) =>
  isWeixinLoginPending(status) && !isWeixinVerificationRequired(status);

export function WeixinChannelAuthorization({
  wsId,
  agentId,
  onOpenGuide,
  onClose,
}: {
  wsId: string;
  agentId: string;
  onOpenGuide: () => void;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [loginId, setLoginId] = useState("");
  const [code, setCode] = useState("");
  const [beginning, setBeginning] = useState(false);
  const [beginError, setBeginError] = useState<unknown>(null);
  // The login that closing this dialog must cancel, and whether a begin that
  // resolves later still has a dialog to show its QR in.
  const pendingLogin = useRef("");
  const mounted = useRef(false);
  const announced = useRef("");
  const key = weixinKeys.login(wsId, loginId);

  // Plain async state rather than useMutation: this starts from a mount
  // effect, and StrictMode's replay detaches a mutation observer from a
  // mutation started there, leaving it pending forever.
  const startLogin = useCallback(async () => {
    setBeginning(true);
    setBeginError(null);
    try {
      const login = await api.beginWeixinLogin(wsId, agentId);
      if (!login.id || login.status === "error")
        throw new Error(t(($) => $.weixin.action_failed));
      // A QR requested just before the dialog closed is cancelled instead of
      // left pending.
      if (!mounted.current) {
        void api.cancelWeixinLogin(wsId, login.id).catch(() => undefined);
        return;
      }
      qc.setQueryData(weixinKeys.login(wsId, login.id), login);
      pendingLogin.current = login.id;
      setLoginId(login.id);
      setCode("");
    } catch (reason) {
      if (mounted.current) setBeginError(reason);
    } finally {
      if (mounted.current) setBeginning(false);
    }
  }, [agentId, qc, t, wsId]);

  // Closing the dialog unmounts this step; cancel the login it left pending.
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      const id = pendingLogin.current;
      pendingLogin.current = "";
      if (id) void api.cancelWeixinLogin(wsId, id).catch(() => undefined);
    };
  }, [wsId]);
  // Runs after the effect above. StrictMode replays effects on the same
  // instance; the guard keeps that replay from requesting a second QR.
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void startLogin();
  }, [startLogin]);

  const poll = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => api.pollWeixinLogin(wsId, loginId, "", signal),
    enabled: (query) =>
      !!loginId &&
      pollsProvider((query.state.data as WeixinLogin | undefined)?.status),
    refetchInterval: (query) =>
      !weixinConflict(query.state.error) &&
      pollsProvider((query.state.data as WeixinLogin | undefined)?.status)
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
    if (!login || !isWeixinLoginPending(login.status)) return;
    const delay = Date.parse(login.expires_at) - Date.now();
    if (!Number.isFinite(delay)) return;
    const timer = setTimeout(
      () => {
        void refetchLogin();
      },
      Math.max(0, delay),
    );
    return () => clearTimeout(timer);
  }, [login, refetchLogin]);

  const confirmedId = login?.status === "confirmed" ? login.id : "";
  useEffect(() => {
    if (!confirmedId || announced.current === confirmedId) return;
    announced.current = confirmedId;
    pendingLogin.current = "";
    void qc.invalidateQueries({ queryKey: weixinKeys.installations(wsId) });
    toast.success(t(($) => $.weixin.success));
  }, [confirmedId, qc, t, wsId]);
  // Close after a beat so the confirmed state is visible. Kept apart from the
  // effect above: onClose changes identity on every parent render.
  useEffect(() => {
    if (!confirmedId) return;
    const timer = setTimeout(onClose, 800);
    return () => clearTimeout(timer);
  }, [confirmedId, onClose]);

  const pending = isWeixinLoginPending(login?.status);
  const status = login?.status;
  const statusText =
    status === "confirmed"
      ? t(($) => $.weixin.success)
      : status === "scaned" || status === "scaned_but_redirect"
        ? t(($) => $.weixin.scanned)
        : isWeixinVerificationRequired(status)
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
  const failed = !!beginError || poll.isError || verify.isError;
  const error = beginError ?? poll.error ?? verify.error;
  const conflict = weixinConflict(error);
  const errorText = conflict
    ? conflict.agent_name
      ? t(($) => $.weixin.already_connected, { agent: conflict.agent_name })
      : t(($) => $.weixin.connected_elsewhere)
    : (clientErrorMessage(error) ?? t(($) => $.weixin.action_failed));

  const showPlate =
    !conflict && (beginning || (pending && !!login?.qr_code_url));
  // Once scanned, the code is spent: fade it so the next step stands out.
  const scanned = pending && status !== "wait";
  return (
    <>
      <div className="flex flex-col items-center gap-4 py-2 text-center">
        {showPlate && (
          // White plate in every theme: a QR needs light quiet zones. The
          // placeholder keeps its size so the dialog does not jump.
          <div
            className={cn(
              "flex size-52 items-center justify-center rounded-lg border",
              beginning ? "bg-muted/50" : "bg-white",
            )}
          >
            {beginning ? (
              <p role="status" className="text-caption text-muted-foreground">
                {t(($) => $.weixin.loading)}
              </p>
            ) : (
              login?.qr_code_url && (
                <QRCode
                  value={login.qr_code_url}
                  size={176}
                  title={t(($) => $.weixin.qr_label)}
                  className={cn("transition-opacity", scanned && "opacity-30")}
                />
              )
            )}
          </div>
        )}
        {login && !beginning && !conflict && (
          <div className="max-w-md space-y-1">
            <p role="status" className="text-body font-medium text-balance">
              {statusText}
            </p>
            {status === "wait" && (
              <p className="text-caption text-balance text-muted-foreground">
                {t(($) => $.weixin.privacy_hint)}
              </p>
            )}
          </div>
        )}
        {!beginning && isWeixinVerificationRequired(status) && (
          <form
            className="w-full max-w-xs space-y-1.5 text-left"
            onSubmit={(event) => {
              event.preventDefault();
              verify.mutate();
            }}
          >
            <Label htmlFor={`weixin-code-${agentId}`}>
              {t(($) => $.weixin.verify_label)}
            </Label>
            <div className="flex gap-2">
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
            </div>
          </form>
        )}
        {failed && (
          <p
            role="alert"
            className="max-w-sm text-caption text-balance text-destructive"
          >
            {errorText}
          </p>
        )}
      </div>
      <DialogFooter className="items-center sm:justify-between">
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          onClick={onOpenGuide}
        >
          <BookOpen />
          {t(($) => $.channels.setup_guide)}
        </Button>
        <div className="flex flex-col-reverse gap-2 sm:flex-row">
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.weixin.close)}
          </Button>
          {!beginning &&
            status !== "confirmed" &&
            (!pending || !!beginError) && (
              <Button onClick={() => void startLogin()}>
                {t(($) => $.weixin.refresh)}
              </Button>
            )}
          {poll.isError && pending && (
            <Button onClick={() => void refetchLogin()}>
              {t(($) => $.weixin.retry)}
            </Button>
          )}
        </div>
      </DialogFooter>
    </>
  );
}
