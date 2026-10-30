"use client";

import { useEffect, useState } from "react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

type RedeemState =
  | { kind: "idle" }
  | { kind: "redeeming" }
  | { kind: "done"; workspaceId: string; installationId: string }
  | { kind: "needs-auth" }
  | { kind: "error"; reason: string };

// TelegramBindPage is the destination the bot's "link your account" prompt
// points at, mirroring SlackBindPage. Auth is required before redeeming: the
// redeemer's Multica identity comes from the session, never the token.
export function TelegramBindPage({ token }: { token: string | null }) {
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
        const resp = await api.redeemTelegramBindingToken(token);
        if (!resp.workspace_id || !resp.installation_id || !resp.telegram_user_id) {
          throw new Error("Telegram binding returned a malformed response");
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
          <h1 className="text-title font-semibold">{t(($) => $.telegram_bind.page_title)}</h1>
          {state.kind === "idle" || state.kind === "redeeming" ? (
            <p className="text-body text-muted-foreground">{t(($) => $.telegram_bind.redeeming)}</p>
          ) : state.kind === "needs-auth" ? (
            <>
              <p className="text-body text-muted-foreground">
                {t(($) => $.telegram_bind.needs_auth_description)}
              </p>
              <Button
                size="sm"
                onClick={() =>
                  navigation.push(
                    `/login?next=${encodeURIComponent(
                      `/telegram/bind?token=${encodeURIComponent(token ?? "")}`,
                    )}`,
                  )
                }
              >
                {t(($) => $.telegram_bind.sign_in)}
              </Button>
            </>
          ) : state.kind === "done" ? (
            <>
              <p className="text-body font-medium">{t(($) => $.telegram_bind.done_title)}</p>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.telegram_bind.done_description)}
              </p>
            </>
          ) : (
            <>
              <p className="text-body font-medium">{t(($) => $.telegram_bind.error_title)}</p>
              <p className="text-caption text-muted-foreground">
                {(() => {
                  switch (state.reason) {
                    case "missing_token":
                      return t(($) => $.telegram_bind.error_missing_token);
                    case "expired":
                      return t(($) => $.telegram_bind.error_expired);
                    case "already_bound":
                      return t(($) => $.telegram_bind.error_already_bound);
                    case "not_member":
                      return t(($) => $.telegram_bind.error_not_member);
                    default:
                      return t(($) => $.telegram_bind.error_unknown);
                  }
                })()}
              </p>
              <p className="text-micro text-muted-foreground">
                {t(($) => $.telegram_bind.error_admin_hint)}
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function redemptionFailureReason(err: unknown): string {
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
