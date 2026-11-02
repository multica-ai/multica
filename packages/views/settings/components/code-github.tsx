"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, LoaderCircle, MoreHorizontal, Unplug } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { githubKeys } from "@multica/core/github";
import { api } from "@multica/core/api";
import type { GitHubInstallation, ListGitHubInstallationsResponse } from "@multica/core/types";
import { useT } from "../../i18n";
import { SettingsRow } from "./settings-layout";
import {
  GitHubAccountAvatar,
  GitHubReuseMenuItems,
  useAccountTypeLabel,
  useGitHubReuseConnect,
} from "./github-connect-reuse";

/**
 * Connects the first GitHub account, or another one. Accounts the caller
 * already manages in other workspaces are bound directly from a menu: GitHub's
 * install page cannot do it, since it parks an account that already has the
 * App on its Configure page and never calls back. With nothing to reuse, the
 * button goes straight to GitHub.
 */
export function GitHubConnectButton({
  installationData,
}: {
  installationData: ListGitHubInstallationsResponse;
}) {
  const { t } = useT("settings");
  const { t: tForkUi } = useT("fork-ui");
  const installations = installationData.installations;
  const {
    reusable,
    opening,
    binding,
    busy,
    menuOpen,
    setMenuOpen,
    chooseConnectPath,
    openGitHubInstall,
    bind,
  } = useGitHubReuseConnect({ installationData });

  // Base UI's own open requests are refused. A press raises one a frame after
  // mousedown, too late to show the wait, and it would skip the decision
  // above; the button's onClick decides instead. An arrow key raises one with
  // no click to follow, so that request is handed to the decision. Closing
  // (Escape, outside press, choosing an item) still goes through.
  function handleMenuOpenChange(
    open: boolean,
    details: { cancel: () => void; reason: string },
  ) {
    if (!open) {
      setMenuOpen(false);
      return;
    }
    details.cancel();
    if (details.reason === "list-navigation") void chooseConnectPath();
  }

  const disabled = busy || !installationData.configured;
  const label = opening
    ? t(($) => $.github.connect_opening)
    : binding
      ? tForkUi(($) => $.settings.github.connecting)
      : installations.length > 0
        ? tForkUi(($) => $.settings.github.add_account)
        : t(($) => $.github.connect_github);
  const content = (
    <>
      {busy ? <LoaderCircle className="animate-spin" /> : null}
      {label}
    </>
  );

  // An open menu stays mounted even if a refresh empties the list under it.
  if (reusable.length === 0 && !menuOpen) {
    return (
      <Button
        variant="outline"
        size="sm"
        onClick={() => void chooseConnectPath()}
        disabled={disabled}
        aria-busy={busy || undefined}
      >
        {content}
      </Button>
    );
  }

  return (
    <DropdownMenu open={menuOpen} onOpenChange={handleMenuOpenChange}>
      <DropdownMenuTrigger
        disabled={disabled}
        render={
          <Button
            variant="outline"
            size="sm"
            onClick={() => (menuOpen ? setMenuOpen(false) : void chooseConnectPath())}
            aria-busy={busy || undefined}
          />
        }
      >
        {content}
        <ChevronDown />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto min-w-56 max-w-80">
        <GitHubReuseMenuItems
          reusable={reusable}
          binding={binding}
          onBind={(item) => void bind(item)}
          onInstall={() => void openGitHubInstall()}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * One row per connected GitHub account, beneath the GitHub row. Each account
 * disconnects on its own, after a confirmation that names it.
 */
export function GitHubAccountRows({
  installations,
  canManage,
}: {
  installations: GitHubInstallation[];
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const { t: tForkUi } = useT("fork-ui");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const accountTypeLabel = useAccountTypeLabel();
  const [disconnectTarget, setDisconnectTarget] = useState<GitHubInstallation | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteGitHubInstallation(wsId, disconnectTarget.id);
      await qc.invalidateQueries({ queryKey: githubKeys.all(wsId) });
      toast.success(t(($) => $.github.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <>
      {installations.map((installation) => {
        const type = accountTypeLabel(installation.account_type);
        return (
          <SettingsRow
            key={installation.id}
            className="min-h-12 py-2.5"
            label={
              // Indented under the GitHub label: these are its accounts.
              <span className="flex min-w-0 items-center gap-3 pl-12">
                <GitHubAccountAvatar
                  login={installation.account_login}
                  avatarUrl={installation.account_avatar_url}
                />
                <span className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                  <span className="break-all">{installation.account_login}</span>
                  {type ? (
                    <>
                      {" "}
                      <span className="text-caption font-normal text-muted-foreground">{type}</span>
                    </>
                  ) : null}
                </span>
              </span>
            }
            description={
              installation.connected_by ? (
                <span className="block pl-21">
                  {t(($) => $.github.connected_by, { name: installation.connected_by })}
                </span>
              ) : undefined
            }
          >
            {canManage ? (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={tForkUi(($) => $.settings.github.account_actions, {
                        login: installation.account_login,
                      })}
                    />
                  }
                >
                  <MoreHorizontal />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-auto">
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDisconnectTarget(installation)}
                  >
                    <Unplug />
                    {t(($) => $.github.disconnect)}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : null}
          </SettingsRow>
        );
      })}

      <AlertDialog
        open={!!disconnectTarget}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setDisconnectTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {tForkUi(($) => $.settings.github.disconnect_account_confirm_title, {
                login: disconnectTarget?.account_login ?? "",
              })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.github.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.github.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.github.disconnecting)
                : t(($) => $.github.disconnect_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
