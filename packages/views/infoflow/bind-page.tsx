"use client";

import { useEffect, useRef, useState } from "react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { api, ApiError, errorCode } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { AppLink } from "../navigation";
import { useT } from "../i18n";

type RedeemState =
  | { kind: "idle" }
  | { kind: "redeeming" }
  | { kind: "done"; workspaceId: string; installationId: string }
  | { kind: "needs-auth" }
  | { kind: "error"; reason: string };

// InfoFlowBindPage is the destination the bot's "link your account" prompt
// points at: /infoflow/bind?token=<raw>. The user lands here logged out OR
// logged in; we require auth before redeeming because the redeemer's Multica
// identity is taken from the session (the token alone never proves who is
// binding).
//
// The redemption is POST /api/infoflow/binding/redeem. Its failure modes map to
// distinct HTTP statuses — 410 (invalid/expired/consumed), 409 (this InfoFlow id
// is already bound to another Multica user) and 403 (not a workspace member, or
// the deployment has no MULTICA_INFOFLOW_SECRET_KEY) — so the copy below
// branches on ApiError.status rather than on the server's wording, which is free
// to change.
//
// Mirrors DingTalkBindPage: the engine's replier builds this URL from
// OutboundReplierConfig.BindingPath (default "/infoflow/bind"), so this route
// and that constant must move together.
export function InfoFlowBindPage({ token }: { token: string | null }) {
  const { t } = useT("common");
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const [state, setState] = useState<RedeemState>({ kind: "idle" });
  // Guards a one-time token against React StrictMode's double-invoked effect:
  // redemption consumes the token server-side, so the second attempt would
  // otherwise land on the "expired" branch and overwrite a success.
  const redeemingToken = useRef<string | null>(null);

  useEffect(() => {
    if (!token) {
      setState({ kind: "error", reason: "missing_token" });
      return;
    }
    if (isAuthLoading) return;
    if (!user) {
      setState({ kind: "needs-auth" });
      return;
    }
    if (state.kind !== "idle" && state.kind !== "needs-auth") return;
    if (redeemingToken.current === token) return;
    redeemingToken.current = token;
    setState({ kind: "redeeming" });
    (async () => {
      try {
        const resp = await api.redeemInfoFlowBindingToken(token);
        if (!resp.workspace_id || !resp.installation_id) {
          throw new Error("InfoFlow binding returned a malformed response");
        }
        setState({
          kind: "done",
          workspaceId: resp.workspace_id,
          installationId: resp.installation_id,
        });
      } catch (e) {
        setState({ kind: "error", reason: redemptionFailureReason(e) });
      }
    })();
  }, [token, user, isAuthLoading, state.kind]);

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center p-6">
      <Card className="w-full">
        <CardContent className="space-y-4">
          <h1 className="text-title font-semibold">{t(($) => $.infoflow_bind.page_title)}</h1>
          {state.kind === "idle" || state.kind === "redeeming" ? (
            <p className="text-body text-muted-foreground">
              {t(($) => $.infoflow_bind.redeeming)}
            </p>
          ) : state.kind === "needs-auth" ? (
            <>
              <p className="text-body text-muted-foreground">
                {t(($) => $.infoflow_bind.needs_auth_description)}
              </p>
              <Button
                size="sm"
                render={
                  <AppLink
                    href={`/login?next=${encodeURIComponent(
                      `/infoflow/bind?token=${encodeURIComponent(token ?? "")}`,
                    )}`}
                  />
                }
                nativeButton={false}
              >
                {t(($) => $.infoflow_bind.sign_in)}
              </Button>
            </>
          ) : state.kind === "done" ? (
            <>
              <p className="text-body font-medium">{t(($) => $.infoflow_bind.done_title)}</p>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.infoflow_bind.done_description)}
              </p>
            </>
          ) : (
            <>
              <p className="text-body font-medium">{t(($) => $.infoflow_bind.error_title)}</p>
              <p className="text-caption text-muted-foreground">
                {(() => {
                  switch (state.reason) {
                    case "missing_token":
                      return t(($) => $.infoflow_bind.error_missing_token);
                    case "expired":
                      return t(($) => $.infoflow_bind.error_expired);
                    case "already_bound":
                      return t(($) => $.infoflow_bind.error_already_bound);
                    case "not_member":
                      return t(($) => $.infoflow_bind.error_not_member);
                    case "not_configured":
                      return t(($) => $.infoflow_bind.error_not_configured);
                    default:
                      return t(($) => $.infoflow_bind.error_unknown);
                  }
                })()}
              </p>
              <p className="text-micro text-muted-foreground">
                {t(($) => $.infoflow_bind.error_admin_hint)}
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// Classify a redeem failure by the server's HTTP status (and, for 403, by the
// machine-readable code) rather than the error text: the backend returns 410
// (invalid/expired/consumed), 409 (already bound to another user) or 403 (not a
// workspace member / integration not configured). ApiError.body carries the
// `code` field, so the two 403 causes stay distinguishable — a deployment
// without MULTICA_INFOFLOW_SECRET_KEY must not be told to go ask their admin for
// workspace membership.
function redemptionFailureReason(err: unknown): string {
  if (err instanceof ApiError && err.status === 403) {
    if (errorCode(err) === "infoflow_not_configured") return "not_configured";
  }
  if (!(err instanceof ApiError)) return "unknown";
  switch (err.status) {
    case 410:
      return "expired";
    case 409:
      return "already_bound";
    case 403:
      return "not_member";
    default:
      return "unknown";
  }
}