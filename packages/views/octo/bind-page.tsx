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

// OctoBindPage is the destination the bot's "you're not bound yet" DM points
// at. The user lands here logged out OR logged in; we require auth before
// redeeming because the redeemer's Multica identity is taken from the session
// (the token alone never proves who is binding — see
// octo.BindingTokenService.RedeemAndBind).
//
// The token comes in via `?token=<raw>`. We POST it to
// /api/octo/binding/redeem; the backend returns 410 (invalid/expired),
// 409 (already bound to another user), 403 (not a workspace member) or 200
// with the bound installation. Each maps to distinct user-facing copy via
// octo_bind in common.json.
export function OctoBindPage({ token }: { token: string | null }) {
  const { t } = useT("common");
  const user = useAuthStore((s) => s.user);
  const navigation = useNavigation();
  const [state, setState] = useState<RedeemState>({ kind: "idle" });

  useEffect(() => {
    if (!token) {
      setState({ kind: "error", reason: "missing_token" });
      return;
    }
    if (!user) {
      setState({ kind: "needs-auth" });
      return;
    }
    if (state.kind !== "idle") return;
    setState({ kind: "redeeming" });
    (async () => {
      try {
        const resp = await api.redeemOctoBindingToken(token);
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
  }, [token, user, state.kind]);

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center p-6">
      <Card className="w-full">
        <CardContent className="space-y-4">
          <h1 className="text-lg font-semibold">{t(($) => $.octo_bind.page_title)}</h1>
          {state.kind === "idle" || state.kind === "redeeming" ? (
            <p className="text-sm text-muted-foreground">{t(($) => $.octo_bind.redeeming)}</p>
          ) : state.kind === "needs-auth" ? (
            <>
              <p className="text-sm text-muted-foreground">
                {t(($) => $.octo_bind.needs_auth_description)}
              </p>
              <Button
                size="sm"
                onClick={() =>
                  navigation.push(
                    `/login?redirect=${encodeURIComponent(
                      `/octo/bind?token=${encodeURIComponent(token ?? "")}`,
                    )}`,
                  )
                }
              >
                {t(($) => $.octo_bind.sign_in)}
              </Button>
            </>
          ) : state.kind === "done" ? (
            <>
              <p className="text-sm font-medium">{t(($) => $.octo_bind.done_title)}</p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.octo_bind.done_description)}
              </p>
            </>
          ) : (
            <>
              <p className="text-sm font-medium">{t(($) => $.octo_bind.error_title)}</p>
              <p className="text-xs text-muted-foreground">
                {(() => {
                  switch (state.reason) {
                    case "missing_token":
                      return t(($) => $.octo_bind.error_missing_token);
                    case "expired":
                      return t(($) => $.octo_bind.error_expired);
                    case "already_bound":
                      return t(($) => $.octo_bind.error_already_bound);
                    case "not_member":
                      return t(($) => $.octo_bind.error_not_member);
                    default:
                      return t(($) => $.octo_bind.error_unknown);
                  }
                })()}
              </p>
              <p className="text-[10px] text-muted-foreground">
                {t(($) => $.octo_bind.error_admin_hint)}
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function redemptionFailureReason(err: unknown): string {
  // Classify by the HTTP status the backend assigns each typed failure
  // (handler/octo.go): 410 invalid/expired, 409 already-bound, 403
  // not-a-member. Status codes are a stable contract; matching on the error
  // message text was fragile — a copy change silently downgraded the user to
  // the generic "unknown" branch.
  if (err instanceof ApiError) {
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
  return "unknown";
}
