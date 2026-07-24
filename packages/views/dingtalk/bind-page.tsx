"use client";

import { useEffect, useState } from "react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

type RedeemState =
  | { kind: "idle" }
  | { kind: "redeeming" }
  | { kind: "done"; workspaceId: string; installationId: string }
  | { kind: "needs-auth" }
  | { kind: "error"; reason: string };

// DingTalkBindPage is the destination the bot's "link your account" prompt
// points at. The user lands here logged out OR logged in; we require auth
// before redeeming because the redeemer's Multica identity is taken from the
// session (the token alone never proves who is binding).
//
// The token comes in via `?token=<raw>`. We POST it to
// /api/dingtalk/binding/redeem; the backend returns 410 (invalid/expired),
// 409 (already bound to another user), 403 (not a workspace member) or 200 with
// the bound installation. Each maps to distinct copy via dingtalk_bind in
// common.json.
export function DingTalkBindPage({ token }: { token: string | null }) {
  const { t } = useT("common");
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const navigation = useNavigation();
  const [state, setState] = useState<RedeemState>({ kind: "idle" });

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
    setState({ kind: "redeeming" });
    (async () => {
      try {
        const resp = await api.redeemDingTalkBindingToken(token);
        setState({
          kind: "done",
          workspaceId: resp.workspace_id,
          installationId: resp.installation_id,
        });
      } catch (e) {
        setState({
          kind: "error",
          reason: redemptionFailureReason(e),
        });
      }
    })();
  }, [token, user, isAuthLoading, state.kind]);

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center p-6">
      <Card className="w-full">
        <CardContent className="space-y-4">
          <h1 className="text-lg font-semibold">{t(($) => $.dingtalk_bind.page_title)}</h1>
          {state.kind === "idle" || state.kind === "redeeming" ? (
            <p className="text-sm text-muted-foreground">{t(($) => $.dingtalk_bind.redeeming)}</p>
          ) : state.kind === "needs-auth" ? (
            <>
              <p className="text-sm text-muted-foreground">
                {t(($) => $.dingtalk_bind.needs_auth_description)}
              </p>
              <Button
                size="sm"
                onClick={() =>
                  navigation.push(
                    `/login?next=${encodeURIComponent(
                      `/dingtalk/bind?token=${encodeURIComponent(token ?? "")}`,
                    )}`,
                  )
                }
              >
                {t(($) => $.dingtalk_bind.sign_in)}
              </Button>
            </>
          ) : state.kind === "done" ? (
            <>
              <p className="text-sm font-medium">{t(($) => $.dingtalk_bind.done_title)}</p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.dingtalk_bind.done_description)}
              </p>
            </>
          ) : (
            <>
              <p className="text-sm font-medium">{t(($) => $.dingtalk_bind.error_title)}</p>
              <p className="text-xs text-muted-foreground">
                {(() => {
                  switch (state.reason) {
                    case "missing_token":
                      return t(($) => $.dingtalk_bind.error_missing_token);
                    case "expired":
                      return t(($) => $.dingtalk_bind.error_expired);
                    case "already_bound":
                      return t(($) => $.dingtalk_bind.error_already_bound);
                    case "not_member":
                      return t(($) => $.dingtalk_bind.error_not_member);
                    default:
                      return t(($) => $.dingtalk_bind.error_unknown);
                  }
                })()}
              </p>
              <p className="text-[10px] text-muted-foreground">
                {t(($) => $.dingtalk_bind.error_admin_hint)}
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// Classify a redeem failure by the server's HTTP status, not the error text:
// the backend returns 410 (invalid/expired), 409 (already bound to another
// user) or 403 (not a workspace member). Matching ApiError.status keeps the
// branch copy decoupled from the exact server wording.
function redemptionFailureReason(err: unknown): string {
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
