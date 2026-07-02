"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { GitMerge, Tag } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { gitlabConnectionsOptions, useDeleteGitLabConnection, deriveGitLabSettings } from "@multica/core/gitlab";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { useT } from "../../i18n";
import { GitLabMark } from "./gitlab-mark";

type SettingsKey = "gitlab_enabled" | "gitlab_mr_sidebar_enabled" | "gitlab_issue_sync_enabled" | "gitlab_comment_sync_enabled";

export function GitLabTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const [connecting, setConnecting] = useState(false);
  const [namespace, setNamespace] = useState("");
  const [savingKey, setSavingKey] = useState<SettingsKey | null>(null);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canView = !!currentMember;

  const { data: connectionData } = useQuery({
    ...gitlabConnectionsOptions(wsId),
    enabled: !!wsId && canView,
  });
  const connections = connectionData?.connections ?? [];
  const configured = connectionData?.configured ?? false;
  const canManage = connectionData?.can_manage === true;

  const deleteMutation = useDeleteGitLabConnection(wsId);

  const flags = deriveGitLabSettings(workspace);

  async function persistSetting(key: SettingsKey, next: boolean) {
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
      toast.error(e instanceof Error ? e.message : t(($) => $.gitlab.toast_failed));
    } finally {
      setSavingKey(null);
    }
  }

  function handleConnect() {
    setConnecting(true);
    const ns = namespace.trim();
    const url = `/api/workspaces/${wsId}/gitlab/connect${ns ? `?ns=${encodeURIComponent(ns)}` : ""}`;
    window.location.href = url;
  }

  async function handleDisconnect(connectionId: string) {
    try {
      await deleteMutation.mutateAsync(connectionId);
      toast.success(t(($) => $.gitlab.toast_disconnected));
    } catch {
      toast.error(t(($) => $.gitlab.toast_disconnect_failed));
    }
  }

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  <GitLabMark className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="gitlab-master" className="text-sm font-medium">
                    {t(($) => $.gitlab.section_master)}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {flags.enabled
                      ? t(($) => $.gitlab.master_description_on)
                      : t(($) => $.gitlab.master_description_off)}
                  </p>
                </div>
              </div>
              <Switch
                id="gitlab-master"
                checked={flags.enabled}
                onCheckedChange={(v) => persistSetting("gitlab_enabled", v)}
                disabled={!canManage || savingKey === "gitlab_enabled"}
              />
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.gitlab.section_connection)}</h2>
        <Card>
          <CardContent className="space-y-4">
            {!configured && (
              <p className="text-sm text-muted-foreground">{t(($) => $.gitlab.not_configured)}</p>
            )}

            {connections.map((conn) => (
              <div key={conn.id} className="flex items-center justify-between rounded-md border p-3">
                <div className="flex items-center gap-2">
                  {conn.avatar_url && conn.namespace_type === "user" && (
                    <img src={conn.avatar_url} alt="" className="h-6 w-6 rounded-full" />
                  )}
                  <span className="text-sm">
                    {t(($) => $.gitlab.connected_as, { namespace: conn.namespace })}
                  </span>
                </div>
                {canManage && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => handleDisconnect(conn.id)}
                  >
                    {t(($) => $.gitlab.disconnect)}
                  </Button>
                )}
              </div>
            ))}

            {configured && canManage && (
              <div className="space-y-2">
                <Label htmlFor="gitlab-namespace">{t(($) => $.gitlab.namespace_label)}</Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="gitlab-namespace"
                    placeholder={t(($) => $.gitlab.namespace_placeholder)}
                    value={namespace}
                    onChange={(e) => setNamespace(e.target.value)}
                    className="max-w-xs"
                  />
                  <Button onClick={handleConnect} disabled={connecting || !namespace.trim()} variant="outline">
                    {t(($) => $.gitlab.connect)}
                  </Button>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.gitlab.section_features)}</h2>
        <Card>
          <CardContent className="space-y-4">
            <FeatureRow
              id="gitlab-mr-sidebar"
              icon={<GitMerge className="h-4 w-4" />}
              label={t(($) => $.gitlab.feature_mr_sidebar_label)}
              description={t(($) => $.gitlab.feature_mr_sidebar_description)}
              checked={flags.mrSidebar}
              disabled={!canManage || !flags.enabled || savingKey === "gitlab_mr_sidebar_enabled"}
              onCheckedChange={(v) => persistSetting("gitlab_mr_sidebar_enabled", v)}
            />
            <FeatureRow
              id="gitlab-issue-sync"
              icon={<Tag className="h-4 w-4" />}
              label={t(($) => $.gitlab.feature_issue_sync_label)}
              description={t(($) => $.gitlab.feature_issue_sync_description)}
              checked={flags.issueSync}
              disabled={!canManage || !flags.enabled || savingKey === "gitlab_issue_sync_enabled"}
              onCheckedChange={(v) => persistSetting("gitlab_issue_sync_enabled", v)}
            />
            <FeatureRow
              id="gitlab-comment-sync"
              icon={<Tag className="h-4 w-4" />}
              label={t(($) => $.gitlab.feature_comment_sync_label)}
              description={t(($) => $.gitlab.feature_comment_sync_description)}
              checked={flags.commentSync}
              disabled={!canManage || !flags.enabled || savingKey === "gitlab_comment_sync_enabled"}
              onCheckedChange={(v) => persistSetting("gitlab_comment_sync_enabled", v)}
            />
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function FeatureRow({
  id,
  icon,
  label,
  description,
  checked,
  disabled,
  onCheckedChange,
}: {
  id: string;
  icon: React.ReactNode;
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onCheckedChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="flex items-start gap-3">
        <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">{icon}</div>
        <div className="space-y-1">
          <Label htmlFor={id} className="text-sm font-medium">{label}</Label>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
      </div>
      <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} />
    </div>
  );
}
