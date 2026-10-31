"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { githubAccountOptions, useDisconnectGitHubAccount } from "@multica/core/github";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useT } from "../../i18n";
import { useConnectGitHubAccount } from "../../issues/components/pull-request-actions";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

/** The GitHub account PR actions run as. Hidden when the server can't act on GitHub. */
export function GitHubAccountSection() {
  const { t } = useT("settings");
  const { data: account } = useQuery(githubAccountOptions());
  const connect = useConnectGitHubAccount();
  const disconnect = useDisconnectGitHubAccount();
  if (!account?.configured) return null;

  return (
    <SettingsSection title={t(($) => $.account.section_connected)}>
      <SettingsCard>
        <SettingsRow
          anchor="github-account"
          label={t(($) => $.account.github_label)}
          description={t(($) => $.account.github_description)}
          size="none"
        >
          <div className="flex items-center justify-start gap-3 sm:justify-end">
            {account.connected ? (
              <>
                <span className="flex min-w-0 items-center gap-2 text-sm">
                  {account.avatar_url ? <img src={account.avatar_url} alt="" className="size-5 rounded-full" /> : null}
                  <span className="truncate">
                    {t(($) => $.account.github_connected_as, { login: account.login ?? "" })}
                  </span>
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={disconnect.isPending}
                  aria-busy={disconnect.isPending || undefined}
                  onClick={() =>
                    disconnect.mutate(undefined, {
                      onSuccess: () => toast.success(t(($) => $.account.github_disconnected)),
                      onError: () => toast.error(t(($) => $.account.github_disconnect_failed)),
                    })
                  }
                >
                  {disconnect.isPending ? <Spinner /> : null}
                  {t(($) => $.account.github_disconnect)}
                </Button>
              </>
            ) : (
              <>
                <span className="text-sm text-muted-foreground">{t(($) => $.account.github_not_connected)}</span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={connect.pending}
                  aria-busy={connect.pending || undefined}
                  onClick={() => void connect.start()}
                >
                  {connect.pending ? <Spinner /> : null}
                  {t(($) => $.account.github_connect)}
                </Button>
              </>
            )}
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
