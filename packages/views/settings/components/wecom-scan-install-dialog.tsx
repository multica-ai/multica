"use client";

// wecom-scan-install-dialog.tsx — the QR half of connecting a WeCom bot.
//
// Structure follows LarkInstallDialog, which solves the same problem: a bounded,
// one-shot polled session whose whole state collapses when the dialog closes.
// The shape is deliberately not useQuery — TanStack's refetch heuristics (window
// focus, online/offline, retry backoff) are wrong for a session with its own
// server-driven cadence and expiry.
//
// One difference from Lark worth knowing: WeCom's begin does NOT return the QR.
// It returns a session in `creating` and the install worker fetches the QR out of
// band, so this dialog renders a "preparing" state first and the QR appears on a
// later poll once the status turns `pending`.

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
// Named import: the default export is an object, not a component, and using it
// throws "Element type is invalid" the moment the QR mounts.
import { QRCode } from "react-qr-code";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { api, ApiError } from "@multica/core/api";
import { wecomKeys } from "@multica/core/wecom";
import { useT } from "../../i18n";

// Terminal error reasons this dialog renders copy for. Anything else falls back
// to the generic message, so a new backend reason never renders blank.
const SCAN_ERROR_KEYS = [
  "expired",
  "generate_failed",
  "integration_unconfigured",
  "installation_conflict",
  "wecom_protocol_error",
  "internal_error",
  "session_lost",
] as const;

type ScanErrorReason = (typeof SCAN_ERROR_KEYS)[number];

function isKnownScanError(reason: string | null): reason is ScanErrorReason {
  return reason !== null && (SCAN_ERROR_KEYS as readonly string[]).includes(reason);
}

export function WecomScanInstallDialog({
  wsId,
  agentId,
  agentName,
  onClose,
}: {
  wsId: string;
  agentId: string;
  agentName?: string;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();

  const [sessionId, setSessionId] = useState<string | null>(null);
  const [pollIntervalSeconds, setPollIntervalSeconds] = useState(1);
  const [qrCodeURL, setQrCodeURL] = useState<string | null>(null);
  const [expiresInSeconds, setExpiresInSeconds] = useState<number | null>(null);
  const [status, setStatus] = useState<string>("creating");
  const [errorReason, setErrorReason] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [beginning, setBeginning] = useState(false);
  const closedRef = useRef(false);

  const beginSession = useCallback(async () => {
    setBeginning(true);
    setStatus("creating");
    setErrorReason(null);
    setErrorMessage(null);
    setSessionId(null);
    setQrCodeURL(null);
    setExpiresInSeconds(null);
    try {
      // A fresh key per attempt: reusing one would make "get a new code" replay
      // the previous session — including an expired one — instead of starting
      // over. Within a single attempt the key is stable, so a retried or
      // double-submitted POST still collapses onto one session.
      const idempotencyKey = crypto.randomUUID();
      const res = await api.beginWecomInstall(wsId, agentId, idempotencyKey);
      if (closedRef.current) return;
      if (!res.session_id) {
        setStatus("error");
        setErrorReason("internal_error");
        return;
      }
      setSessionId(res.session_id);
      setStatus(res.status);
      setPollIntervalSeconds(res.poll_interval_seconds);
    } catch (e) {
      if (closedRef.current) return;
      setStatus("error");
      setErrorReason(
        e instanceof ApiError && e.status === 503 ? "integration_unconfigured" : "internal_error",
      );
      setErrorMessage(e instanceof Error ? e.message : String(e));
    } finally {
      if (!closedRef.current) setBeginning(false);
    }
  }, [wsId, agentId]);

  // Kick off on mount.
  //
  // closedRef is reset AT THE START of every mount, not only at construction:
  // StrictMode runs effects twice (mount → cleanup → mount) on the same
  // instance, so the ref survives. Without the reset, mount #1's cleanup sets
  // closedRef=true and every await in mount #2 early-exits before setting state
  // — the dialog then renders permanently empty.
  useEffect(() => {
    closedRef.current = false;
    void beginSession();
    return () => {
      closedRef.current = true;
    };
  }, [beginSession]);

  // Polling loop. Runs while the session is not terminal — through `creating`
  // too, since that is when the QR is still being fetched.
  useEffect(() => {
    if (!sessionId) return;
    if (status !== "creating" && status !== "pending") return;

    const intervalMs = Math.max(1000, pollIntervalSeconds * 1000);
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (cancelled) return;
      try {
        const res = await api.getWecomInstallStatus(wsId, sessionId);
        if (cancelled) return;
        setStatus(res.status);
        setPollIntervalSeconds(res.poll_interval_seconds);
        if (res.qr_code_url) setQrCodeURL(res.qr_code_url);
        if (typeof res.expires_in_seconds === "number") {
          setExpiresInSeconds(res.expires_in_seconds);
        }
        if (res.status === "success") {
          await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
          toast.success(t(($) => $.wecom.scan_success_toast));
          // The close itself is deliberately NOT scheduled here: `status` is a
          // dependency of this effect, so setting it to "success" re-runs the
          // effect and the cleanup would cancel the very timer meant to close
          // the dialog. A dedicated effect below owns that.
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
        // Terminal HTTP states must not be retried: the session is gone or the
        // caller lost permission, and polling on would trap the user watching a
        // stale QR with no feedback.
        if (e instanceof ApiError) {
          if (e.status === 404) {
            // Purged by the retention sweep, or never existed.
            setStatus("error");
            setErrorReason("session_lost");
            setErrorMessage(e.message);
            return;
          }
          if (e.status === 401 || e.status === 403) {
            setStatus("error");
            setErrorReason("session_lost");
            setErrorMessage(e.message);
            return;
          }
        }
        // A blip or 5xx: poll again. The next read either confirms the session
        // or surfaces the terminal reason the worker recorded.
        timer = setTimeout(poll, intervalMs);
      }
    };

    timer = setTimeout(poll, intervalMs);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [wsId, sessionId, status, pollIntervalSeconds, qc, t, onClose]);

  // Close shortly after success so the success state is visible for a beat.
  // Separate from the poll effect for the reason noted above.
  useEffect(() => {
    if (status !== "success") return;
    const timer = setTimeout(onClose, 800);
    return () => clearTimeout(timer);
  }, [status, onClose]);

  const scanErrorText = isKnownScanError(errorReason)
    ? {
        expired: t(($) => $.wecom.scan_error_expired),
        generate_failed: t(($) => $.wecom.scan_error_generate_failed),
        integration_unconfigured: t(($) => $.wecom.scan_error_integration_unconfigured),
        installation_conflict: t(($) => $.wecom.scan_error_installation_conflict),
        wecom_protocol_error: t(($) => $.wecom.scan_error_wecom_protocol_error),
        internal_error: t(($) => $.wecom.scan_error_internal_error),
        session_lost: t(($) => $.wecom.scan_error_session_lost),
      }[errorReason]
    : t(($) => $.wecom.scan_error_internal_error);

  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DialogContent className="max-w-sm" data-testid="wecom-scan-dialog">
        <DialogHeader>
          <DialogTitle>{t(($) => $.wecom.scan_dialog_title)}</DialogTitle>
          <DialogDescription>
            {agentName
              ? t(($) => $.wecom.bind_button_title, { agent: agentName })
              : t(($) => $.wecom.scan_hint)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-2">
          {(beginning || status === "creating") && !qrCodeURL && status !== "error" && (
            <p className="text-body text-muted-foreground" data-testid="wecom-scan-starting">
              {t(($) => $.wecom.scan_starting)}
            </p>
          )}

          {status === "pending" && qrCodeURL && (
            <>
              {/* White plate regardless of theme: a QR needs light quiet zones
                  to scan, and the dark-mode surface token would invert it. */}
              <div className="rounded-md border bg-white p-3">
                <QRCode value={qrCodeURL} size={192} />
              </div>
              <p className="text-caption text-muted-foreground text-center">
                {t(($) => $.wecom.scan_hint)}
              </p>
              {expiresInSeconds !== null && expiresInSeconds > 0 && (
                <p className="text-caption text-muted-foreground" data-testid="wecom-scan-expiry">
                  {t(($) => $.wecom.scan_expires_in, { seconds: expiresInSeconds })}
                </p>
              )}
            </>
          )}

          {status === "error" && (
            <div className="space-y-2 text-center" data-testid="wecom-scan-error">
              <p className="text-body">{scanErrorText}</p>
              {errorMessage && (
                <p className="text-caption text-muted-foreground break-words">{errorMessage}</p>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>
            {t(($) => $.wecom.scan_close)}
          </Button>
          {status === "error" && (
            <Button
              size="sm"
              onClick={() => void beginSession()}
              disabled={beginning}
              data-testid="wecom-scan-retry"
            >
              {t(($) => $.wecom.scan_again)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
