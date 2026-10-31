"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { errorCode } from "@multica/core/api";
import { useCompleteGitHubAccount } from "@multica/core/github";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

const CODE = "github_account_code";
const STATE = "github_account_state";
const ERROR = "github_account_error";

/**
 * Finishes "connect your GitHub account": GitHub's callback sends the browser
 * back to the page that started it with a one-time code, and the signed-in
 * session redeems it, so a link started by someone else can't attach their
 * GitHub account to this user. The params are stripped right away so a
 * reload or a shared URL doesn't replay them.
 */
export function GitHubAccountCallback() {
  const { t } = useT("issues");
  const navigation = useNavigation();
  const complete = useCompleteGitHubAccount();
  const handled = useRef<string | null>(null);
  const code = navigation.searchParams.get(CODE);
  const state = navigation.searchParams.get(STATE);
  const error = navigation.searchParams.get(ERROR);

  useEffect(() => {
    if (!code && !error) return;
    const key = code ?? `error:${error}`;
    if (handled.current === key) return;
    handled.current = key;

    const params = new URLSearchParams(navigation.searchParams);
    params.delete(CODE);
    params.delete(STATE);
    params.delete(ERROR);
    const query = params.toString();
    navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}${navigation.hash}`);

    if (error) {
      toast.error(
        error === "access_denied"
          ? t(($) => $.pr_actions.connect.denied)
          : error === "invalid_state"
            ? t(($) => $.pr_actions.connect.expired)
            : t(($) => $.pr_actions.connect.failed),
      );
      return;
    }
    if (!code || !state) return;
    complete.mutate(
      { code, state },
      {
        onSuccess: (account) =>
          toast.success(t(($) => $.pr_actions.connect.connected, { login: account.login ?? "" })),
        onError: (err) => {
          const reason = errorCode(err);
          toast.error(
            reason === "github_account_state_mismatch"
              ? t(($) => $.pr_actions.connect.state_mismatch)
              : reason === "github_account_invalid_state"
                ? t(($) => $.pr_actions.connect.expired)
                : t(($) => $.pr_actions.connect.failed),
          );
        },
      },
    );
    // Runs once per code; navigation and the mutation are stable enough here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code, error]);

  return null;
}
