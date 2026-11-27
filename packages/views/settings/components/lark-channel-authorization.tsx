"use client";

import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { QRCode } from "react-qr-code";
import { RefreshCw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { DialogFooter } from "@multica/ui/components/ui/dialog";
import { api, ApiError } from "@multica/core/api";
import { larkKeys } from "@multica/core/lark";
import type { LarkInstallStatusResponse } from "@multica/core/types";
import { useT } from "../../i18n";

export function LarkChannelAuthorization({
  wsId,
  agentId,
  region,
  onClose,
}: {
  wsId: string;
  agentId: string;
  region: "feishu" | "lark";
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();

  // We track session lifecycle as local state because TanStack Query is
  // optimized for cached server reads, and this dialog is a one-shot
  // flow whose entire state collapses on close. Using `useQuery` for
  // the polling would also fight TanStack's default refetch heuristics
  // (window focus, online/offline, retries) that have the wrong shape
  // for a single bounded session.
  const [session, setSession] = useState<null | {
    sessionId: string;
    qrCodeURL: string;
    expiresInSeconds: number;
    pollIntervalSeconds: number;
  }>(null);
  const [status, setStatus] =
    useState<LarkInstallStatusResponse["status"]>("pending");
  const [errorReason, setErrorReason] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [beginning, setBeginning] = useState(false);
  const closedRef = useRef(false);

  // beginSession is callable from both the initial mount and the
  // "scan again" action. Wrapping in a function (instead of a useEffect
  // dependency cascade) makes the retry path explicit.
  async function beginSession() {
    setBeginning(true);
    setStatus("pending");
    setErrorReason(null);
    setErrorMessage(null);
    setSession(null);
    try {
      const res = await api.beginLarkInstall(wsId, agentId, region);
      if (closedRef.current) return;
      setSession({
        sessionId: res.session_id,
        qrCodeURL: res.qr_code_url,
        expiresInSeconds: res.expires_in_seconds,
        pollIntervalSeconds: res.poll_interval_seconds,
      });
    } catch (e) {
      if (closedRef.current) return;
      setStatus("error");
      setErrorReason("internal_error");
      setErrorMessage(e instanceof Error ? e.message : String(e));
    } finally {
      setBeginning(false);
    }
  }

  // Kick off on mount.
  //
  // Reset closedRef AT THE START of every mount, not just at construction.
  // React 18+ / 19 StrictMode dev runs effects twice (mount → cleanup →
  // mount) on the same component instance, which preserves useRef across
  // the simulated remount. Without resetting, the cleanup from mount #1
  // flips closedRef.current=true, and on mount #2 every beginSession
  // promise sees closedRef=true at the await boundary and early-exits
  // before calling setSession — leaving the dialog body empty (no
  // "starting" placeholder, no QR, no error), which is exactly the
  // "QR never appears" bug. Reset on entry so the second mount
  // re-arms the in-flight cancellation guard.
  useEffect(() => {
    closedRef.current = false;
    void beginSession();
    return () => {
      closedRef.current = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Polling loop. Bounded by the device-flow expiry — once that
  // elapses Lark's server returns expired_token and our backend marks
  // the session errored, so we don't need a separate client-side
  // expiry timer.
  useEffect(() => {
    if (!session || status !== "pending") return;
    const intervalMs = Math.max(2000, session.pollIntervalSeconds * 1000);
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (cancelled) return;
      try {
        const res = await api.getLarkInstallStatus(wsId, session.sessionId);
        if (cancelled) return;
        setStatus(res.status);
        if (res.status === "success") {
          await qc.invalidateQueries({
            queryKey: larkKeys.installations(wsId),
          });
          toast.success(t(($) => $.channels.connect_success));
          return;
        }
        if (res.status === "error") {
          setErrorReason(res.error_reason ?? "internal_error");
          setErrorMessage(res.error_message ?? null);
          return;
        }
        timer = setTimeout(poll, intervalMs);
      } catch (e) {
        if (cancelled) return;
        // Terminal HTTP states must NOT be retried — the session is
        // gone or the caller has lost permission, and polling forever
        // would trap the user on a stale QR with no error feedback.
        // 404: server restarted, multi-instance routed elsewhere, or
        //      the in-process GC swept the session. Treat as session
        //      lost — user has to scan a fresh QR.
        // 403: permission revoked mid-session (role downgrade, etc.).
        //      The CTA gate prevents this on entry, but a role change
        //      while the dialog is open would land here.
        // 401: the global ApiClient interceptor handles re-auth, so
        //      reaching the catch with 401 means re-auth itself
        //      failed — treat as terminal so the user doesn't loop.
        if (e instanceof ApiError) {
          if (e.status === 404) {
            setStatus("error");
            setErrorReason("session_lost");
            setErrorMessage(e.message);
            return;
          }
          if (e.status === 403 || e.status === 401) {
            setStatus("error");
            setErrorReason("forbidden");
            setErrorMessage(e.message);
            return;
          }
        }
        // Transient errors (network blip, 5xx) — schedule another
        // poll rather than killing the session. The next backend
        // status read will either confirm pending or surface the
        // terminal error the polling goroutine recorded.
        timer = setTimeout(poll, intervalMs);
        // Surface the message as a non-blocking toast for diagnostics.
        toast.message(
          t(($) => $.lark.install_poll_retry),
          {
            description: e instanceof Error ? e.message : String(e),
          },
        );
      }
    };

    timer = setTimeout(poll, intervalMs);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.sessionId, status]);

  useEffect(() => {
    if (status !== "success") return;
    const timer = setTimeout(onClose, 800);
    return () => clearTimeout(timer);
  }, [status, onClose]);

  return (
    <>
      <div className="flex flex-col items-center gap-4 py-2">
        {beginning && !session && (
          <p className="text-body text-muted-foreground">
            {t(($) => $.lark.install_starting)}
          </p>
        )}

        {session && status === "pending" && (
          <>
            <div className="rounded-md border bg-white p-3">
              {/* react-qr-code renders an inline SVG — no external
                  network image dependency, prints at any DPI. */}
              <QRCode value={session.qrCodeURL} size={192} />
            </div>
            <a
              href={session.qrCodeURL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-caption underline text-muted-foreground"
            >
              {region === "lark"
                ? t(($) => $.lark.install_open_link_fallback_lark)
                : t(($) => $.lark.install_open_link_fallback_feishu)}
            </a>
          </>
        )}

        {status === "success" && (
          <p className="text-body font-medium">
            {t(($) => $.channels.waiting_message)}
          </p>
        )}

        {status === "error" && (
          <div className="space-y-2 text-center">
            <p className="text-body font-medium text-destructive">
              {(() => {
                switch (errorReason) {
                  case "expired":
                    return t(($) => $.lark.install_error_expired);
                  case "access_denied":
                    return t(($) => $.lark.install_error_access_denied);
                  case "lark_protocol_error":
                    return t(($) => $.lark.install_error_protocol);
                  case "bot_info_failed":
                    return t(($) => $.lark.install_error_bot_info);
                  case "installation_conflict":
                    return t(($) => $.lark.install_error_conflict);
                  case "installer_bind_failed":
                    return t(($) => $.lark.install_error_installer_bind);
                  case "session_lost":
                    return t(($) => $.lark.install_error_session_lost);
                  case "forbidden":
                    return t(($) => $.lark.install_error_forbidden);
                  default:
                    return t(($) => $.lark.install_error_generic);
                }
              })()}
            </p>
            {errorMessage && (
              <p className="text-micro text-muted-foreground break-all">
                {errorMessage}
              </p>
            )}
          </div>
        )}
      </div>

      <DialogFooter>
        {status === "error" ? (
          <>
            <Button variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.lark.install_close)}
            </Button>
            <Button size="sm" onClick={beginSession} disabled={beginning}>
              <RefreshCw className="h-3 w-3" />
              {t(($) => $.lark.install_retry)}
            </Button>
          </>
        ) : (
          <Button variant="outline" size="sm" onClick={onClose}>
            {t(($) => $.lark.install_close)}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}
