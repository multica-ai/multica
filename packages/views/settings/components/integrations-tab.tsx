"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw, Save } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  feishuProjectIntegrationOptions,
  feishuProjectIssueStatusesOptions,
  feishuProjectKeys,
  feishuProjectSyncOptions,
} from "@multica/core/feishu-project/queries";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

const MULTICA_STATUS_OPTIONS = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
] as const;

const NO_MAPPING = "__none__";

// GitHub integration moved to its own Settings tab (see github-tab.tsx).
// This tab now hosts only third-party integrations that remain workspace-
// scoped under "Integrations" — currently just Feishu Project.
export function IntegrationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [savingFeishu, setSavingFeishu] = useState(false);
  const [syncingFeishu, setSyncingFeishu] = useState(false);
  const [activeSyncRunId, setActiveSyncRunId] = useState<string | null>(null);
  const [lastNotifiedSyncRunId, setLastNotifiedSyncRunId] = useState<string | null>(null);
  const [feishuEnabled, setFeishuEnabled] = useState(false);
  const [projectKey, setProjectKey] = useState("");
  const [pluginId, setPluginId] = useState("");
  const [pluginSecret, setPluginSecret] = useState("");
  const [actorUserKey, setActorUserKey] = useState("");
  const [syncWorkItemId, setSyncWorkItemId] = useState("");
  const [statusMapping, setStatusMapping] = useState<Record<string, string>>({});
  const [reverseStatusMapping, setReverseStatusMapping] = useState<Record<string, string>>({});

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: feishuProject } = useQuery({
    ...feishuProjectIntegrationOptions(wsId),
    enabled: !!wsId && canManage,
  });
  const {
    data: issueStatusesData,
    isFetching: loadingIssueStatuses,
    refetch: refetchIssueStatuses,
  } = useQuery({
    ...feishuProjectIssueStatusesOptions(wsId, canManage && !!projectKey.trim() && !!pluginId.trim()),
  });
  const { data: feishuSync } = useQuery({
    ...feishuProjectSyncOptions(wsId, canManage && !!feishuProject?.id),
    refetchInterval: (query) => (query.state.data?.status === "running" ? 2000 : false),
  });
  const issueStatuses = issueStatusesData?.statuses ?? [];
  const syncRun = feishuSync?.run ?? null;
  const syncRunning = syncingFeishu || feishuSync?.status === "running";
  const syncProcessed = syncRun?.processed ?? 0;
  const syncTotal = syncRun?.total ?? 0;
  const syncProgress = syncTotal > 0 ? Math.min(100, Math.max(8, Math.round((syncProcessed / syncTotal) * 100))) : 50;
  const issueStatusKeys = useMemo(
    () => new Set(issueStatuses.map((status) => status.key)),
    [issueStatuses],
  );

  useEffect(() => {
    if (!feishuProject) return;
    setFeishuEnabled(feishuProject.enabled);
    setProjectKey(feishuProject.project_name || feishuProject.project_key);
    setPluginId(feishuProject.plugin_id);
    setPluginSecret("");
    setActorUserKey(feishuProject.actor_user_key ?? "");
    setStatusMapping(feishuProject.status_mapping);
    setReverseStatusMapping(feishuProject.reverse_status_mapping);
  }, [feishuProject]);

  useEffect(() => {
    if (syncRun?.status === "running" && !activeSyncRunId) {
      setActiveSyncRunId(syncRun.id);
      return;
    }
    if (
      !syncRun ||
      syncRun.status === "running" ||
      syncRun.id !== activeSyncRunId ||
      syncRun.id === lastNotifiedSyncRunId
    ) {
      return;
    }
    setLastNotifiedSyncRunId(syncRun.id);
    setActiveSyncRunId(null);
    queryClient.invalidateQueries({ queryKey: feishuProjectKeys.integration(wsId) });
    if (syncRun.status === "failed") {
      toast.error(syncRun.error ?? t(($) => $.integrations.feishu_project_sync_failed));
      return;
    }
    toast.success(
      t(($) => $.integrations.feishu_project_sync_done, {
        created: syncRun.created,
        updated: syncRun.updated,
      }),
    );
  }, [activeSyncRunId, lastNotifiedSyncRunId, queryClient, syncRun, t, wsId]);

  useEffect(() => {
    if (issueStatuses.length === 0) return;

    setStatusMapping((prev) => {
      const next = Object.fromEntries(
        Object.entries(prev).filter(([key]) => issueStatusKeys.has(key)),
      );
      return shallowEqualRecord(prev, next) ? prev : next;
    });
    setReverseStatusMapping((prev) => {
      const next = Object.fromEntries(
        Object.entries(prev).filter(([, value]) => issueStatusKeys.has(value)),
      );
      return shallowEqualRecord(prev, next) ? prev : next;
    });
  }, [issueStatuses.length, issueStatusKeys]);

  async function handleSaveFeishuProject() {
    setSavingFeishu(true);
    try {
      await api.updateFeishuProjectIntegration(wsId, {
        enabled: feishuEnabled,
        project_name: projectKey.trim(),
        plugin_id: pluginId.trim(),
        plugin_secret: pluginSecret.trim() || undefined,
        actor_user_key: actorUserKey.trim() || null,
        sync_story: false,
        sync_issue: true,
        mql_filter: "",
        status_mapping: compactMapping(statusMapping),
        reverse_status_mapping: compactMapping(reverseStatusMapping),
      });
      await queryClient.invalidateQueries({ queryKey: feishuProjectKeys.integration(wsId) });
      await queryClient.invalidateQueries({ queryKey: feishuProjectKeys.issueStatuses(wsId) });
      toast.success(t(($) => $.integrations.feishu_project_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.feishu_project_save_failed));
    } finally {
      setSavingFeishu(false);
    }
  }

  async function handleSyncFeishuProject() {
    setSyncingFeishu(true);
    try {
      const resp = await api.syncFeishuProjectIntegration(wsId, {
        work_item_id: syncWorkItemId.trim() || undefined,
      });
      queryClient.setQueryData(feishuProjectKeys.sync(wsId), resp);
      setActiveSyncRunId(resp.run?.id ?? null);
      await queryClient.invalidateQueries({ queryKey: feishuProjectKeys.sync(wsId) });
      if (resp.status === "failed") {
        toast.error(resp.error ?? t(($) => $.integrations.feishu_project_sync_failed));
        return;
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.feishu_project_sync_failed));
    } finally {
      setSyncingFeishu(false);
    }
  }

  async function handleRefreshIssueStatuses() {
    try {
      const result = await refetchIssueStatuses();
      if (result.error) {
        toast.error(result.error instanceof Error ? result.error.message : t(($) => $.integrations.feishu_project_statuses_refresh_failed));
        return;
      }
      const count = result.data?.statuses.length ?? 0;
      if (count === 0) {
        toast.error(t(($) => $.integrations.feishu_project_statuses_refresh_empty));
        return;
      }
      toast.success(t(($) => $.integrations.feishu_project_statuses_refreshed, { count }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.feishu_project_statuses_refresh_failed));
    }
  }

  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.integrations.section_title)}</h2>

        <Card>
          <CardContent className="space-y-6">
            <div className="flex items-start justify-between gap-6">
              <div className="space-y-1">
                <p className="text-sm font-medium">{t(($) => $.integrations.feishu_project_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.integrations.feishu_project_description)}
                </p>
              </div>
              {canManage && <Switch checked={feishuEnabled} onCheckedChange={setFeishuEnabled} />}
            </div>

            {canManage ? (
              <div className="space-y-6">
                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3 border-b border-border/70 pb-2">
                    <p className="text-xs font-medium text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_basic_section)}
                    </p>
                  </div>
                  <div className="grid gap-4 md:grid-cols-2">
                    <label className="space-y-1.5 text-xs font-medium">
                      {t(($) => $.integrations.feishu_project_project_key)}
                      <Input value={projectKey} onChange={(e) => setProjectKey(e.target.value)} placeholder="my_project" />
                    </label>
                    <label className="space-y-1.5 text-xs font-medium">
                      {t(($) => $.integrations.feishu_project_plugin_id)}
                      <Input value={pluginId} onChange={(e) => setPluginId(e.target.value)} placeholder="MII_xxx" />
                    </label>
                    <label className="space-y-1.5 text-xs font-medium">
                      {t(($) => $.integrations.feishu_project_plugin_secret)}
                      <Input
                        type="password"
                        value={pluginSecret}
                        onChange={(e) => setPluginSecret(e.target.value)}
                        placeholder={feishuProject?.has_plugin_secret ? "****" : ""}
                      />
                      {feishuProject?.has_plugin_secret && (
                        <span className="text-[11px] font-normal text-muted-foreground">
                          {t(($) => $.integrations.secret_placeholder_set)}
                        </span>
                      )}
                    </label>
                    <label className="space-y-1.5 text-xs font-medium">
                      {t(($) => $.integrations.feishu_project_actor_user_key)}
                      <Input value={actorUserKey} onChange={(e) => setActorUserKey(e.target.value)} />
                    </label>
                  </div>
                </div>

                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3 border-b border-border/70 pb-2">
                    <p className="text-xs font-medium text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_mapping_section)}
                    </p>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={handleRefreshIssueStatuses}
                      disabled={!projectKey.trim() || !pluginId.trim() || loadingIssueStatuses}
                    >
                      {loadingIssueStatuses ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <RefreshCw className="h-3.5 w-3.5" />
                      )}
                      {t(($) => $.integrations.feishu_project_refresh_statuses)}
                    </Button>
                  </div>

                  {issueStatuses.length === 0 ? (
                    <p className="rounded-md border border-border/70 px-3 py-3 text-xs text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_statuses_empty)}
                    </p>
                  ) : (
                    <div className="grid gap-5 xl:grid-cols-2">
                      <div className="space-y-2">
                        <p className="text-xs font-medium">
                          {t(($) => $.integrations.feishu_project_status_mapping)}
                        </p>
                        <div className="overflow-hidden rounded-md border border-border/70">
                          {issueStatuses.map((status) => (
                            <div key={status.key} className="grid grid-cols-[1fr_180px] items-center gap-3 border-b border-border/70 px-3 py-2 last:border-b-0">
                              <div className="min-w-0">
                                <p className="truncate text-xs font-medium">{status.name}</p>
                                <p className="truncate font-mono text-[11px] text-muted-foreground">{status.key}</p>
                              </div>
                              <Select
                                value={statusMapping[status.key] || NO_MAPPING}
                                onValueChange={(value) => {
                                  setStatusMapping((prev) => setMappingValue(prev, status.key, value || NO_MAPPING));
                                }}
                              >
                                <SelectTrigger size="sm" className="w-full">
                                  <span className="flex-1 truncate text-left">
                                    {statusMapping[status.key] || t(($) => $.integrations.feishu_project_no_mapping)}
                                  </span>
                                </SelectTrigger>
                                <SelectContent align="start">
                                  <SelectItem value={NO_MAPPING}>{t(($) => $.integrations.feishu_project_no_mapping)}</SelectItem>
                                  {MULTICA_STATUS_OPTIONS.map((option) => (
                                    <SelectItem key={option} value={option}>{option}</SelectItem>
                                  ))}
                                </SelectContent>
                              </Select>
                            </div>
                          ))}
                        </div>
                      </div>

                      <div className="space-y-2">
                        <p className="text-xs font-medium">
                          {t(($) => $.integrations.feishu_project_reverse_mapping)}
                        </p>
                        <div className="overflow-hidden rounded-md border border-border/70">
                          {MULTICA_STATUS_OPTIONS.map((status) => {
                            const current = reverseStatusMapping[status];
                            const selected = current && issueStatusKeys.has(current) ? current : NO_MAPPING;
                            return (
                              <div key={status} className="grid grid-cols-[1fr_180px] items-center gap-3 border-b border-border/70 px-3 py-2 last:border-b-0">
                                <p className="font-mono text-xs font-medium">{status}</p>
                                <Select
                                  value={selected}
                                  onValueChange={(value) => {
                                    setReverseStatusMapping((prev) => setMappingValue(prev, status, value || NO_MAPPING));
                                  }}
                                >
                                  <SelectTrigger size="sm" className="w-full">
                                    <span className="flex-1 truncate text-left">
                                      {selected === NO_MAPPING
                                        ? t(($) => $.integrations.feishu_project_no_mapping)
                                        : statusOptionLabel(issueStatuses, selected)}
                                    </span>
                                  </SelectTrigger>
                                  <SelectContent align="start">
                                    <SelectItem value={NO_MAPPING}>{t(($) => $.integrations.feishu_project_no_mapping)}</SelectItem>
                                    {issueStatuses.map((option) => (
                                      <SelectItem key={option.key} value={option.key}>
                                        {option.name} ({option.key})
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    </div>
                  )}
                </div>

                <div className="flex flex-col gap-4 border-t border-border/70 pt-4 lg:flex-row lg:items-end lg:justify-between">
                  <div className="min-h-12 flex-1 space-y-2">
                    <p className="text-xs text-muted-foreground">
                      {syncRunning
                        ? syncTotal > 0
                          ? t(($) => $.integrations.feishu_project_sync_progress_count, { processed: syncProcessed, total: syncTotal })
                          : t(($) => $.integrations.feishu_project_sync_progress)
                        : feishuProject?.last_error
                          ? feishuProject.last_error
                          : feishuProject?.last_synced_at
                            ? t(($) => $.integrations.feishu_project_last_synced, { time: feishuProject.last_synced_at })
                            : t(($) => $.integrations.feishu_project_never_synced)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_auto_sync_hint)}
                    </p>
                    {syncRunning && (
                      <div className="h-1.5 max-w-xl overflow-hidden rounded-full bg-muted">
                        <div
                          className="h-full rounded-full bg-primary transition-[width] duration-300"
                          style={{ width: `${syncProgress}%` }}
                        />
                      </div>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center justify-end gap-2">
                    <Input
                      value={syncWorkItemId}
                      onChange={(event) => setSyncWorkItemId(event.target.value)}
                      placeholder={t(($) => $.integrations.feishu_project_sync_id_placeholder)}
                      className="h-9 w-44"
                      disabled={syncRunning}
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleSyncFeishuProject}
                      disabled={syncRunning || !feishuProject?.id}
                    >
                      {syncRunning ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <RefreshCw className="h-3.5 w-3.5" />
                      )}
                      {syncRunning ? t(($) => $.integrations.feishu_project_syncing) : t(($) => $.integrations.feishu_project_sync_now)}
                    </Button>
                    <Button size="sm" onClick={handleSaveFeishuProject} disabled={savingFeishu || syncRunning}>
                      {savingFeishu ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Save className="h-3.5 w-3.5" />
                      )}
                      {savingFeishu ? t(($) => $.integrations.feishu_project_saving) : t(($) => $.integrations.feishu_project_save)}
                    </Button>
                  </div>
                </div>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function setMappingValue(mapping: Record<string, string>, key: string, value: string): Record<string, string> {
  const next = { ...mapping };
  if (!value || value === NO_MAPPING) {
    delete next[key];
  } else {
    next[key] = value;
  }
  return next;
}

function compactMapping(mapping: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(mapping)) {
    if (key && value && value !== NO_MAPPING) {
      out[key] = value;
    }
  }
  return out;
}

function statusOptionLabel(options: Array<{ key: string; name: string }>, key: string): string {
  const option = options.find((item) => item.key === key);
  return option ? `${option.name} (${option.key})` : key;
}

function shallowEqualRecord(a: Record<string, string>, b: Record<string, string>): boolean {
  const aEntries = Object.entries(a);
  if (aEntries.length !== Object.keys(b).length) return false;
  return aEntries.every(([key, value]) => b[key] === value);
}
