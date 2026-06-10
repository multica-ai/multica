"use client";

import { useEffect, useState } from "react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { ApiError, api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

type RedeemState =
  | { kind: "idle" }
  | { kind: "redeeming" }
  | { kind: "done" }
  | { kind: "needs-auth" }
  | { kind: "error"; reason: string; localBindUrl?: string };

/** HTTPS bind page cannot call http://localhost:8080 — browser blocks mixed content. */
function isBlockedLocalApiFromHttpsPage(): boolean {
  if (typeof window === "undefined") return false;
  const base = api.getBaseUrl().trim();
  if (!base) return false;
  try {
    const apiUrl = new URL(base);
    return window.location.protocol === "https:" && apiUrl.protocol === "http:";
  } catch {
    return false;
  }
}

function localBindUrl(token: string): string {
  return `http://localhost:3000/wecom/bind?token=${encodeURIComponent(token)}`;
}

function redemptionFailureReason(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return "needs_auth";
      case 403:
        return "not_member";
      case 409:
        return "already_bound";
      case 410:
        return "expired";
    }
  }
  const msg = err instanceof Error ? err.message : "";
  const lower = msg.toLowerCase();
  if (lower.includes("invalid") || lower.includes("expired") || lower.includes("410")) {
    return "expired";
  }
  if (lower.includes("already bound") || lower.includes("409")) {
    return "already_bound";
  }
  if (lower.includes("workspace member") || lower.includes("403")) {
    return "not_member";
  }
  return "unknown";
}

export function WecomBindPage({ token }: { token: string | null }) {
  const { t } = useT("common");
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const navigation = useNavigation();
  const [state, setState] = useState<RedeemState>({ kind: "idle" });
  const [authWaitTimedOut, setAuthWaitTimedOut] = useState(false);

  useEffect(() => {
    if (!token) {
      setState({ kind: "error", reason: "missing_token" });
      return;
    }
    if (isBlockedLocalApiFromHttpsPage()) {
      setState({
        kind: "error",
        reason: "mixed_content",
        localBindUrl: localBindUrl(token),
      });
    }
  }, [token]);

  useEffect(() => {
    if (!isLoading) {
      setAuthWaitTimedOut(false);
      return;
    }
    const timer = window.setTimeout(() => setAuthWaitTimedOut(true), 8000);
    return () => window.clearTimeout(timer);
  }, [isLoading]);

  useEffect(() => {
    if (!token) return;
    if (state.kind === "error") return;
    if (isLoading && !authWaitTimedOut) return;
    if (!user) {
      if (state.kind !== "needs-auth") {
        setState({ kind: "needs-auth" });
      }
      return;
    }
    if (state.kind !== "idle" && state.kind !== "needs-auth") return;

    setState({ kind: "redeeming" });
    void (async () => {
      try {
        await api.redeemWecomBindingToken(token);
        setState({ kind: "done" });
      } catch (err) {
        const reason = redemptionFailureReason(err);
        if (reason === "needs_auth") {
          setState({ kind: "needs-auth" });
          return;
        }
        setState({
          kind: "error",
          reason,
          localBindUrl: reason === "unknown" ? localBindUrl(token) : undefined,
        });
      }
    })();
  }, [token, user, isLoading, authWaitTimedOut, state.kind]);

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center p-6">
      <Card className="w-full">
        <CardContent className="space-y-4">
          <h1 className="text-lg font-semibold">{t(($) => $.wecom_bind.page_title)}</h1>
          {isLoading || state.kind === "idle" || state.kind === "redeeming" ? (
            <p className="text-sm text-muted-foreground">
              {isLoading
                ? t(($) => $.wecom_bind.verifying_auth)
                : t(($) => $.wecom_bind.redeeming)}
            </p>
          ) : state.kind === "needs-auth" ? (
            <>
              <p className="text-sm text-muted-foreground">
                {t(($) => $.wecom_bind.needs_auth_description)}
              </p>
              <Button
                size="sm"
                onClick={() =>
                  navigation.push(
                    `/login?next=${encodeURIComponent(
                      `/wecom/bind?token=${encodeURIComponent(token ?? "")}`,
                    )}`,
                  )
                }
              >
                {t(($) => $.wecom_bind.sign_in)}
              </Button>
            </>
          ) : state.kind === "done" ? (
            <p className="text-sm text-muted-foreground">{t(($) => $.wecom_bind.done_description)}</p>
          ) : state.kind === "error" ? (
            <div className="space-y-3">
              <p className="text-sm text-destructive">
                {(() => {
                  switch (state.reason) {
                    case "missing_token":
                      return t(($) => $.wecom_bind.error_missing_token);
                    case "mixed_content":
                      return t(($) => $.wecom_bind.error_mixed_content);
                    case "expired":
                      return t(($) => $.wecom_bind.error_expired);
                    case "already_bound":
                      return t(($) => $.wecom_bind.error_already_bound);
                    case "not_member":
                      return t(($) => $.wecom_bind.error_not_member);
                    default:
                      return t(($) => $.wecom_bind.error_unknown);
                  }
                })()}
              </p>
              {state.localBindUrl ? (
                <Button
                  size="sm"
                  onClick={() => {
                    window.location.assign(state.localBindUrl!);
                  }}
                >
                  {t(($) => $.wecom_bind.open_localhost_bind)}
                </Button>
              ) : null}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
