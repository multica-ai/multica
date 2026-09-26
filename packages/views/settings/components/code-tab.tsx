"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowRight, MoreHorizontal, Pause, Play, Unplug } from "lucide-react";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useConfigStore } from "@multica/core/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { deriveGitHubSettings, githubInstallationsOptions } from "@multica/core/github";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsReadOnlyNotice,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import { GitHubMark } from "./github-mark";
import { VCSConnectionRows } from "./code-vcs";
import { HostMark, HostStatus } from "./code-host";
import { RepositoriesSection } from "./repositories-section";
import { settingsHref } from "./settings-navigation";
import { usePRMergeStatus } from "./pr-merge-status-row";

type GitHubSettingsKey =
  | "github_enabled"
  | "github_pr_sidebar_enabled"
  | "co_authored_by_enabled"
  | "github_auto_link_prs_enabled";

/**
 * Everything about code: which hosts are connected, which repositories agents
 * work in, and how pull requests link back to issues. One page, so connecting
 * GitHub and picking its repositories no longer spans two tabs.
 */
export function CodeTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { role, member } = useCurrentMember(wsId);
  const isManager = role === "owner" || role === "admin";
  const vcsAvailable = useConfigStore((s) => s.vcsIntegrationAvailable);

  // Every member can see the installation list (MUL-2413); connect and
  // disconnect follow the backend's `can_manage` so the UI never offers what
  // the server would reject.
  const { data: installationData } = useQuery({
    ...githubInstallationsOptions(wsId),
    enabled: !!wsId && !!member,
  });
  const installations = installationData?.installations ?? [];
  const configured = installationData?.configured ?? false;
  const canManageGitHub = installationData?.can_manage === true;
  const connected = installations.length > 0;
  const primaryInstallation = installations[0] ?? null;

  const flags = deriveGitHubSettings(workspace);
  const [savingKey, setSavingKey] = useState<GitHubSettingsKey | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function persistSetting(key: GitHubSettingsKey, next: boolean) {
    if (!workspace || savingKey) return;
    setSavingKey(key);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        [key]: next,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_failed));
    } finally {
      setSavingKey(null);
    }
  }

  async function handleConnect() {
    setConnecting(true);
    try {
      const resp = await api.getGitHubConnectURL(wsId);
      if (!resp.configured || !resp.url) {
        toast.error(t(($) => $.github.toast_not_configured));
        return;
      }
      window.open(resp.url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_open_failed));
    } finally {
      setConnecting(false);
    }
  }

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteGitHubInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: ["github", wsId] });
      toast.success(t(($) => $.github.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.github.toast_disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  if (!workspace) return null;

  const githubStatus = !connected
    ? { tone: "muted" as const, label: t(($) => $.integrations.status_not_connected) }
    : flags.enabled
      ? { tone: "success" as const, label: t(($) => $.integrations.status_connected) }
      : { tone: "muted" as const, label: t(($) => $.github.status_paused) };

  const githubDetail = connected ? (
    <>
      {t(($) => $.github.connected_to, {
        login: installations.map((i) => i.account_login).join(", "),
      })}
      {primaryInstallation?.connected_by
        ? ` · ${t(($) => $.github.connected_by, { name: primaryInstallation.connected_by })}`
        : null}
      {!flags.enabled ? (
        <span className="block">{t(($) => $.github.master_description_off)}</span>
      ) : null}
    </>
  ) : !canManageGitHub ? (
    t(($) => $.github.contact_admin_to_connect)
  ) : !configured ? (
    <>
      {t(($) => $.github.not_configured)}{" "}
      <code className="rounded-xs bg-muted px-1 py-0.5 text-micro">GITHUB_APP_SLUG</code>{" "}
      {t(($) => $.github.not_configured_and)}{" "}
      <code className="rounded-xs bg-muted px-1 py-0.5 text-micro">GITHUB_WEBHOOK_SECRET</code>
    </>
  ) : null;

  const featureDisabled = (key: GitHubSettingsKey) =>
    !canManageGitHub || !flags.enabled || savingKey === key;

  return (
    <SettingsTab title={t(($) => $.page.tabs.code)} scope="workspace">
      {member && !isManager ? <SettingsReadOnlyNotice wsId={wsId} /> : null}

      <SettingsSection title={t(($) => $.code.hosting_title)} anchor="code-hosting">
        <SettingsCard>
          <SettingsRow
            anchor="github"
            label={
              <span className="flex min-w-0 items-center gap-3">
                <HostMark>
                  <GitHubMark className="size-4" />
                </HostMark>
                <span className="flex min-w-0 flex-wrap items-center gap-x-2">
                  {t(($) => $.page.tabs.github)}
                  <HostStatus tone={githubStatus.tone} label={githubStatus.label} />
                </span>
              </span>
            }
            description={githubDetail ? <span className="block pl-12">{githubDetail}</span> : undefined}
          >
            {canManageGitHub ? (
              connected && primaryInstallation ? (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t(($) => $.code.github_actions)}
                      />
                    }
                  >
                    <MoreHorizontal />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-auto">
                    <DropdownMenuItem
                      disabled={savingKey === "github_enabled"}
                      onClick={() => persistSetting("github_enabled", !flags.enabled)}
                    >
                      {flags.enabled ? <Pause /> : <Play />}
                      {flags.enabled
                        ? t(($) => $.github.pause)
                        : t(($) => $.github.resume)}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => setDisconnectTarget(primaryInstallation.id)}
                    >
                      <Unplug />
                      {t(($) => $.github.disconnect)}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleConnect}
                  disabled={connecting || !configured}
                  aria-busy={connecting || undefined}
                >
                  {connecting
                    ? t(($) => $.github.connect_opening)
                    : t(($) => $.github.connect_github)}
                </Button>
              )
            ) : null}
          </SettingsRow>
          {vcsAvailable ? <VCSConnectionRows /> : null}
        </SettingsCard>
      </SettingsSection>

      <RepositoriesSection />

      <SettingsSection
        title={t(($) => $.code.pr_linking_title)}
        anchor="pr-linking"
      >
        <SettingsCard>
          <SettingsRow
            anchor="pr-sidebar"
            label={t(($) => $.github.feature_pr_sidebar_label)}
            description={t(($) => $.github.feature_pr_sidebar_description)}
          >
            <Switch
              checked={flags.prSidebar}
              disabled={featureDisabled("github_pr_sidebar_enabled")}
              onCheckedChange={(v) => persistSetting("github_pr_sidebar_enabled", v)}
              aria-label={t(($) => $.github.feature_pr_sidebar_label)}
            />
          </SettingsRow>
          <SettingsRow
            anchor="auto-link"
            label={t(($) => $.github.feature_auto_link_label)}
            description={t(($) => $.github.feature_auto_link_description, {
              example: `${workspace.issue_prefix || "MUL"}-123`,
            })}
          >
            <Switch
              checked={flags.autoLinkPRs}
              disabled={featureDisabled("github_auto_link_prs_enabled")}
              onCheckedChange={(v) => persistSetting("github_auto_link_prs_enabled", v)}
              aria-label={t(($) => $.github.feature_auto_link_label)}
            />
          </SettingsRow>
          <SettingsRow
            anchor="co-author"
            label={t(($) => $.github.feature_co_author_label)}
            description={
              <>
                {t(($) => $.github.feature_co_author_description_prefix)}{" "}
                <code className="rounded-xs bg-muted px-1 py-0.5 text-caption">
                  {"Co-authored-by: multica-agent <github@multica.ai>"}
                </code>
                {t(($) => $.github.feature_co_author_description_suffix)}
              </>
            }
          >
            <Switch
              checked={flags.coAuthor}
              disabled={featureDisabled("co_authored_by_enabled")}
              onCheckedChange={(v) => persistSetting("co_authored_by_enabled", v)}
              aria-label={t(($) => $.github.feature_co_author_label)}
            />
          </SettingsRow>
          <PRMergeStatusSummary />
        </SettingsCard>
      </SettingsSection>

      <AlertDialog
        open={!!disconnectTarget}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setDisconnectTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.github.disconnect_confirm_title)}</AlertDialogTitle>
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
    </SettingsTab>
  );
}

/**
 * The merge rule is an issue-workflow setting owned by Statuses & transitions;
 * the Code page shows its current value and links there instead of editing a
 * second copy.
 */
function PRMergeStatusSummary() {
  const { t } = useT("settings");
  const navigation = useNavigation();
  const { value, renderOption } = usePRMergeStatus();
  return (
    <SettingsRow label={t(($) => $.pr_merge_status.label)}>
      <span className="flex items-center gap-3 text-body">
        <span className="flex items-center gap-1.5 [&_svg]:size-3.5">
          {renderOption(value)}
        </span>
        <AppLink
          href={settingsHref(
            navigation.pathname,
            navigation.searchParams,
            "issue-statuses",
            { section: "pr-merge-status" },
          )}
          className={buttonVariants({ variant: "ghost", size: "sm" })}
        >
          {t(($) => $.code.edit_in_statuses)}
          <ArrowRight />
        </AppLink>
      </span>
    </SettingsRow>
  );
}
