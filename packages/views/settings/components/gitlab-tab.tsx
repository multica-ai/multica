"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { GitMerge, Tag, Copy, RefreshCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import {
  gitlabConnectionsOptions,
  useDeleteGitLabConnection,
  useRotateGitLabWebhookSecret,
  deriveGitLabSettings,
  DEFAULT_GITLAB_ISSUE_SYNC_LABEL,
} from "@multica/core/gitlab";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { useT } from "../../i18n";
import { GitLabMark } from "./gitlab-mark";
import { SettingsSaveState } from "./settings-layout";
import { useAutoSave } from "./use-auto-save";

type SettingsKey =
  | "gitlab_enabled"
  | "gitlab_mr_sidebar_enabled"
  | "gitlab_issue_sync_enabled"
  | "gitlab_comment_sync_enabled";

export function GitLabTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const [connecting, setConnecting] = useState(false);
  const [namespace, setNamespace] = useState("");
  const [savingKey, setSavingKey] = useState<SettingsKey | null>(null);
  const [rotatingId, setRotatingId] = useState<string | null>(null);

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
  const rotateMutation = useRotateGitLabWebhookSecret(wsId);

  const flags = deriveGitLabSettings(workspace);
  const [issueSyncLabelDraft, setIssueSyncLabelDraft] = useState(flags.issueSyncLabel);

  useEffect(() => {
    setIssueSyncLabelDraft(flags.issueSyncLabel);
    // Cache updates after auto-save replace the Workspace object. Keying on
    // identity prevents that response from wiping a newer local keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally keyed on workspace identity
  }, [workspace?.id]);

  const persistWorkspaceSettings = useCallback(
    async (patch: Record<string, unknown>) => {
      if (!workspace) return;
      // Prefer the latest cached workspace so concurrent flag toggles and label
      // autosaves do not clobber each other with a stale settings object.
      const cached = qc.getQueryData<Workspace[]>(workspaceKeys.list())?.find(
        (ws) => ws.id === workspace.id,
      );
      const base = cached ?? workspace;
      const merged = {
        ...((base.settings as Record<string, unknown>) ?? {}),
        ...patch,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    },
    [qc, workspace],
  );

  const labelAutoSave = useAutoSave({
    value: issueSyncLabelDraft,
    savedValue: flags.issueSyncLabel,
    enabled: canManage && !!workspace,
    onSave: async (value) => {
      const next = value.trim() || DEFAULT_GITLAB_ISSUE_SYNC_LABEL;
      await persistWorkspaceSettings({ gitlab_issue_sync_label: next });
      if (next !== value) {
        setIssueSyncLabelDraft(next);
      }
    },
    onError: (e) => {
      toast.error(e instanceof Error ? e.message : t(($) => $.gitlab.toast_failed));
    },
    isEqual: (a, b) =>
      a.trim() === b.trim() || (a.trim() === "" && b === DEFAULT_GITLAB_ISSUE_SYNC_LABEL),
  });

  async function persistSetting(key: SettingsKey, next: boolean) {
    if (!workspace || savingKey) return;
    setSavingKey(key);
    try {
      await persistWorkspaceSettings({ [key]: next });
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

  async function handleRotate(connectionId: string) {
    setRotatingId(connectionId);
    try {
      await rotateMutation.mutateAsync(connectionId);
      toast.success(t(($) => $.gitlab.webhook_secret_rotated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.gitlab.webhook_secret_rotate_failed));
    } finally {
      setRotatingId(null);
    }
  }

  if (!workspace) return null;

  const webhookURL =
    typeof window !== "undefined" ? `${window.location.origin}/api/webhooks/gitlab/${wsId}` : "";

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

      {configured && connections.length > 0 && wsId && (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold">{t(($) => $.gitlab.webhook_url_label)}</h2>
          <Card>
            <CardContent className="space-y-4">
              <p className="text-sm text-muted-foreground">{t(($) => $.gitlab.webhook_url_description)}</p>
              <div className="space-y-2">
                <Label className="text-xs text-muted-foreground">
                  {t(($) => $.gitlab.webhook_url_field_label)}
                </Label>
                <div className="flex items-center gap-2">
                  <Input readOnly value={webhookURL} className="font-mono text-xs" />
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={() => {
                      navigator.clipboard.writeText(webhookURL);
                      toast.success(t(($) => $.gitlab.webhook_url_copied));
                    }}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>

              {canManage &&
                connections.map((conn) =>
                  conn.webhook_secret ? (
                    <div key={conn.id} className="space-y-2 border-t pt-4">
                      <Label className="text-xs text-muted-foreground">
                        {t(($) => $.gitlab.webhook_secret_label)}
                        {connections.length > 1 ? ` · ${conn.namespace}` : ""}
                      </Label>
                      <p className="text-xs text-muted-foreground">
                        {t(($) => $.gitlab.webhook_secret_description)}
                      </p>
                      <div className="flex items-center gap-2">
                        <Input
                          readOnly
                          value={conn.webhook_secret}
                          className="font-mono text-xs"
                        />
                        <Button
                          variant="outline"
                          size="icon"
                          onClick={() => {
                            navigator.clipboard.writeText(conn.webhook_secret!);
                            toast.success(t(($) => $.gitlab.webhook_secret_copied));
                          }}
                        >
                          <Copy className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={rotatingId === conn.id}
                          onClick={() => handleRotate(conn.id)}
                        >
                          <RefreshCw
                            className={`mr-1.5 h-3.5 w-3.5 ${rotatingId === conn.id ? "animate-spin" : ""}`}
                          />
                          {t(($) => $.gitlab.webhook_secret_rotate)}
                        </Button>
                      </div>
                    </div>
                  ) : null,
                )}
            </CardContent>
          </Card>
        </section>
      )}

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">{t(($) => $.gitlab.section_features)}</h2>
          <SettingsSaveState
            status={labelAutoSave.status}
            savingLabel={t(($) => $.auto_save.saving)}
            savedLabel={t(($) => $.auto_save.saved)}
            errorLabel={t(($) => $.auto_save.failed)}
          />
        </div>
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
            <div className="flex items-start justify-between gap-4 border-t pt-4">
              <div className="space-y-1">
                <Label htmlFor="gitlab-issue-sync-label" className="text-sm font-medium">
                  {t(($) => $.gitlab.issue_sync_label_label)}
                </Label>
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.gitlab.issue_sync_label_description)}
                </p>
              </div>
              <Input
                id="gitlab-issue-sync-label"
                value={issueSyncLabelDraft}
                onChange={(e) => setIssueSyncLabelDraft(e.target.value)}
                onBlur={labelAutoSave.flush}
                disabled={!canManage || !flags.enabled}
                placeholder={DEFAULT_GITLAB_ISSUE_SYNC_LABEL}
                spellCheck={false}
                autoComplete="off"
                className="max-w-[12rem] font-mono text-xs"
              />
            </div>
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
