"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink } from "@multica/ui/icons";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { githubAppInstallationsOptions, githubKeys } from "@multica/core/github";
import { ApiError, api, errorCode } from "@multica/core/api";
import type {
  GitHubAppInstallation,
  GitHubInstallation,
  ListGitHubInstallationsResponse,
} from "@multica/core/types";
import { useT } from "../../i18n";

/** Installations the caller manages elsewhere that this workspace lacks. */
function reusableInstallations(
  available: readonly GitHubAppInstallation[],
  connected: readonly GitHubInstallation[],
): GitHubAppInstallation[] {
  return available.filter(
    (item) =>
      !item.connected &&
      !connected.some((installation) => installation.installation_id === item.installation_id),
  );
}

export function useAccountTypeLabel() {
  const { t } = useT("fork-ui");
  return (type: string) =>
    type === "Organization"
      ? t(($) => $.settings.github.account_type_organization)
      : type === "User"
        ? t(($) => $.settings.github.account_type_user)
        : type;
}

/** Decorative: the login is always written next to it. */
export function GitHubAccountAvatar({
  login,
  avatarUrl,
  className = "size-6",
}: {
  login: string;
  avatarUrl: string | null;
  className?: string;
}) {
  return (
    <Avatar className={className} aria-hidden="true">
      {avatarUrl ? <AvatarImage src={avatarUrl} alt="" /> : null}
      <AvatarFallback className="text-micro">
        {(login.trim()[0] ?? "?").toUpperCase()}
      </AvatarFallback>
    </Avatar>
  );
}

/**
 * The connect decision both GitHub connect entries share: accounts the caller
 * already manages in other workspaces are bound directly from a menu — GitHub's
 * install page cannot do it, since it parks an account that already has the
 * App on its Configure page and never calls back. With nothing to reuse, the
 * entry goes straight to GitHub. `returnTo` signs where the install flow
 * returns to; `onBound` runs once a reuse binding lands.
 */
export function useGitHubReuseConnect({
  installationData,
  returnTo,
  onBound,
}: {
  installationData: ListGitHubInstallationsResponse | undefined;
  returnTo?: "github" | "repositories";
  onBound?: (installation: GitHubInstallation) => void;
}) {
  const { t } = useT("settings");
  const { t: tForkUi } = useT("fork-ui");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const installations = installationData?.installations ?? [];
  const [opening, setOpening] = useState(false);
  const [checking, setChecking] = useState(false);
  const [binding, setBinding] = useState<GitHubAppInstallation | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);

  const reuseEnabled =
    !!wsId &&
    installationData?.can_manage === true &&
    installationData?.repository_browse_configured === true;
  const appInstallations = useQuery({
    ...githubAppInstallationsOptions(wsId),
    enabled: reuseEnabled,
  });
  // The cache outlives what made it valid: the last list survives while the
  // query is disabled and after a refresh fails. Only a successful read with
  // reuse enabled is offered.
  const reusable =
    reuseEnabled && appInstallations.status === "success"
      ? reusableInstallations(appInstallations.data.installations, installations)
      : [];

  async function openGitHubInstall() {
    setOpening(true);
    try {
      const resp = await api.getGitHubConnectURL(wsId, returnTo);
      if (!resp.configured || !resp.url) {
        toast.error(t(($) => $.github.toast_not_configured));
        return;
      }
      window.open(resp.url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_open_failed));
    } finally {
      setOpening(false);
    }
  }

  // The wait half of the decision, shared with hover previews: a read in
  // flight is awaited and its result wins, even with an older list on
  // screen. A failed read counts as nothing to reuse, so connecting is
  // never blocked.
  async function awaitReusableCandidates(): Promise<GitHubAppInstallation[]> {
    if (!reuseEnabled || !appInstallations.isFetching) return reusable;
    setChecking(true);
    const latest = await qc
      .fetchQuery(githubAppInstallationsOptions(wsId))
      .then((response) => response.installations)
      .catch(() => []);
    setChecking(false);
    return reusableInstallations(latest, installations);
  }

  // Deliberate activations from either entry — presses and arrow keys, never
  // hovers — decide here; both entries refuse their own menu-open requests
  // and defer to this. The awaited list picks between the menu and GitHub.
  async function chooseConnectPath(): Promise<"menu" | "github"> {
    const candidates = await awaitReusableCandidates();
    if (candidates.length > 0) {
      setMenuOpen(true);
      return "menu";
    }
    await openGitHubInstall();
    return "github";
  }

  function bindFailureMessage(e: unknown, login: string): string {
    if (e instanceof ApiError) {
      // Scope, not session: the installation left the caller's reuse scope,
      // or GitHub no longer has it.
      if (e.status === 404) {
        return tForkUi(($) => $.settings.github.toast_account_not_reusable, { login });
      }
      if (e.status === 422) {
        return tForkUi(($) => $.settings.github.toast_account_suspended, { login });
      }
      if (e.status === 403 && errorCode(e) === "github_app_auth_not_configured") {
        return t(($) => $.github.toast_not_configured);
      }
    }
    return tForkUi(($) => $.settings.github.toast_account_connect_failed, { login });
  }

  async function bind(item: GitHubAppInstallation) {
    if (binding) return;
    setBinding(item);
    try {
      const installation = await api.connectGitHubInstallation(wsId, item.installation_id);
      await qc.invalidateQueries({ queryKey: githubKeys.all(wsId) });
      toast.success(
        tForkUi(($) => $.settings.github.toast_account_connected, { login: item.account_login }),
      );
      onBound?.(installation);
    } catch (e) {
      // The list offered an installation that can no longer be bound.
      if (e instanceof ApiError && (e.status === 404 || e.status === 422)) {
        void qc.invalidateQueries({ queryKey: githubKeys.appInstallations(wsId) });
      }
      toast.error(bindFailureMessage(e, item.account_login));
    } finally {
      setBinding(null);
    }
  }

  return {
    reusable,
    opening,
    checking,
    binding,
    busy: opening || checking || binding !== null,
    menuOpen,
    setMenuOpen,
    awaitReusableCandidates,
    chooseConnectPath,
    openGitHubInstall,
    bind,
  };
}

/**
 * The account choice both connect entries offer: bind an account managed
 * elsewhere, or install the App on another GitHub account. An emptied list
 * still shows the install item, so an open menu keeps its escape hatch.
 */
export function GitHubReuseMenuItems({
  reusable,
  binding,
  onBind,
  onInstall,
}: {
  reusable: readonly GitHubAppInstallation[];
  binding: GitHubAppInstallation | null;
  onBind: (item: GitHubAppInstallation) => void;
  onInstall: () => void;
}) {
  const { t: tForkUi } = useT("fork-ui");
  const accountTypeLabel = useAccountTypeLabel();
  return (
    <>
      {reusable.length > 0 ? (
        <>
          <DropdownMenuGroup>
            <DropdownMenuLabel>{tForkUi(($) => $.settings.github.reuse_label)}</DropdownMenuLabel>
            {reusable.map((item) => {
              const type = accountTypeLabel(item.account_type);
              return (
                <DropdownMenuItem
                  key={item.installation_id}
                  disabled={binding !== null}
                  onClick={() => onBind(item)}
                >
                  <GitHubAccountAvatar
                    login={item.account_login}
                    avatarUrl={item.account_avatar_url}
                    className="size-5"
                  />
                  <span className="min-w-0 truncate">{item.account_login}</span>
                  {/* The space separates login and type in the item's accessible name. */}
                  {type ? (
                    <>
                      {" "}
                      <span className="ml-auto shrink-0 pl-3 text-caption text-muted-foreground">
                        {type}
                      </span>
                    </>
                  ) : null}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
        </>
      ) : null}
      <DropdownMenuItem onClick={onInstall}>
        <ExternalLink />
        {tForkUi(($) => $.settings.github.install_on_another_account)}
      </DropdownMenuItem>
    </>
  );
}
