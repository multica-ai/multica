"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronsUpDown, Loader2, Plus, RefreshCw, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import { Input } from "@multica/ui/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  feishuProjectBusinessLinesOptions,
  feishuProjectFieldsOptions,
  feishuProjectIntegrationOptions,
  feishuProjectIssueStatusesOptions,
  feishuProjectKeys,
  feishuProjectRoutesOptions,
  feishuProjectSyncOptions,
} from "@multica/core/feishu-project/queries";
import { api } from "@multica/core/api";
import type {
  FeishuProjectBusinessLineNode,
  FeishuProjectFieldMeta,
  FeishuProjectLabelSyncRule,
  FeishuProjectRouteInput,
} from "@multica/core/types";
import { useT } from "../../i18n";
import { FeishuProjectRoutingSection, type RouteRow } from "./feishu-project-routing-section";

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
const NO_FIELD = "__none__";
const NO_MATCH = "__none__";

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
  const [assignOpenItemsToOwnerAgent, setAssignOpenItemsToOwnerAgent] = useState(false);
  const [syncWorkItemId, setSyncWorkItemId] = useState("");
  const [statusMapping, setStatusMapping] = useState<Record<string, string>>({});
  const [reverseStatusMapping, setReverseStatusMapping] = useState<Record<string, string>>({});
  const [labelSyncRules, setLabelSyncRules] = useState<FeishuProjectLabelSyncRule[]>([]);
  // Business-line field config — local while the user is editing, persisted on Save.
  const [businessLineFieldKey, setBusinessLineFieldKey] = useState("");
  const [businessLineFieldName, setBusinessLineFieldName] = useState("");
  // Routes draft state lifted from FeishuProjectRoutingSection so the single Save button
  // can commit both integration fields and the route table in one click.
  const [routeRows, setRouteRows] = useState<RouteRow[]>([]);
  const [routesExpanded, setRoutesExpanded] = useState<Record<string, boolean>>({});

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: feishuProject } = useQuery({
    ...feishuProjectIntegrationOptions(wsId),
    enabled: !!wsId && canManage,
  });
  const { data: issueStatusesData } = useQuery({
    ...feishuProjectIssueStatusesOptions(wsId, canManage && !!projectKey.trim() && !!pluginId.trim()),
  });
  const { data: fieldsData } = useQuery({
    ...feishuProjectFieldsOptions(wsId, "issue", canManage && !!projectKey.trim() && !!pluginId.trim()),
  });
  // Subscribe to the route table so the Save handler can diff/replace it. Section
  // component reads the same query but only for the initial seed — edits flow through
  // routeRows state owned here.
  const { data: routesData } = useQuery({
    ...feishuProjectRoutesOptions(wsId, canManage && !!feishuProject?.id),
  });
  const savedRoutes = routesData?.routes ?? [];
  const { data: feishuSync } = useQuery({
    ...feishuProjectSyncOptions(wsId, canManage && !!feishuProject?.id),
    refetchInterval: (query) => (query.state.data?.status === "running" ? 2000 : false),
  });
  const issueStatuses = issueStatusesData?.statuses ?? [];
  const issueFields = fieldsData?.fields ?? [];
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
    setAssignOpenItemsToOwnerAgent(feishuProject.assign_open_items_to_owner_agent);
    setStatusMapping(feishuProject.status_mapping);
    setReverseStatusMapping(feishuProject.reverse_status_mapping);
    setLabelSyncRules(feishuProject.label_sync_rules ?? []);
    setBusinessLineFieldKey(feishuProject.business_line_field_key);
    setBusinessLineFieldName(feishuProject.business_line_field_name);
  }, [feishuProject]);

  // Seed route draft from the server's saved table whenever it (re)loads. Auto-expand
  // parents that already have a routed child so the user sees their current state
  // without expanding manually.
  useEffect(() => {
    setRouteRows(
      savedRoutes.map((r) => ({
        businessLineId: r.business_line_id,
        businessLineName: r.business_line_name,
        parentBusinessLineId: r.parent_business_line_id ?? "",
        parentBusinessLineName: r.parent_business_line_name ?? "",
        projectId: r.project_id,
        fallbackAgentId: r.fallback_agent_id ?? "",
      })),
    );
    const auto: Record<string, boolean> = {};
    for (const r of savedRoutes) {
      const parentId = r.parent_business_line_id ?? "";
      if (parentId) auto[parentId] = true;
    }
    setRoutesExpanded(auto);
    // savedRoutes identity changes on every render; key by the server's underlying data
    // shape via JSON to avoid resetting the user's in-flight edits on cache touches.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(savedRoutes)]);

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
    // Validate route rows upfront so a user clicking Save with a half-configured route
    // gets a precise row-level error instead of a generic backend 400 toast.
    const bizLineKey = businessLineFieldKey.trim();
    if (bizLineKey) {
      const missing = routeRows.find((r) => !r.projectId);
      if (missing) {
        toast.error(
          t(($) => $.integrations.feishu_project_routes_missing_project, {
            name: missing.businessLineName || missing.businessLineId,
          }),
        );
        return;
      }
    }
    const invalidLabelRule = labelSyncRules.find(
      (rule) =>
        (!rule.field_key.trim() || !rule.match.trim() || !rule.label_name.trim()),
    );
    if (invalidLabelRule) {
      toast.error(t(($) => $.integrations.feishu_project_label_sync_incomplete));
      return;
    }
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
        assign_open_items_to_owner_agent: assignOpenItemsToOwnerAgent,
        label_sync_rules: compactLabelSyncRules(labelSyncRules),
        business_line_field_key: bizLineKey,
        business_line_field_name: businessLineFieldName.trim(),
      });
      // Routes are scoped to a business-line field. Always sync them: if the field was
      // cleared, send [] so the backend deletes stale routes that would otherwise sit
      // orphaned and never match anything.
      const routePayload: FeishuProjectRouteInput[] = bizLineKey
        ? routeRows.map((r) => ({
            project_id: r.projectId,
            business_line_id: r.businessLineId,
            business_line_name: r.businessLineName,
            parent_business_line_id: r.parentBusinessLineId || undefined,
            parent_business_line_name: r.parentBusinessLineName || undefined,
            fallback_agent_id: r.fallbackAgentId || null,
          }))
        : [];
      await api.replaceFeishuProjectRoutes(wsId, { routes: routePayload });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: feishuProjectKeys.integration(wsId) }),
        queryClient.invalidateQueries({ queryKey: feishuProjectKeys.issueStatuses(wsId) }),
        queryClient.invalidateQueries({ queryKey: feishuProjectKeys.routes(wsId) }),
      ]);
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
                    <div>
                      <p className="text-xs font-medium text-muted-foreground">
                        {t(($) => $.integrations.feishu_project_label_sync_section)}
                      </p>
                      <p className="mt-1 text-[11px] text-muted-foreground">
                        {t(($) => $.integrations.feishu_project_label_sync_hint)}
                      </p>
                    </div>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        setLabelSyncRules((prev) => [
                          ...prev,
                          {
                            id: newLocalRuleId(),
                            enabled: true,
                            field_key: "",
                            field_name: "",
                            match: "",
                            label_name: "",
                          },
                        ])
                      }
                    >
                      <Plus className="h-3.5 w-3.5" />
                      {t(($) => $.integrations.feishu_project_label_sync_add)}
                    </Button>
                  </div>

                  {labelSyncRules.length === 0 ? (
                    <p className="rounded-md border border-border/70 px-3 py-3 text-xs text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_label_sync_empty)}
                    </p>
                  ) : (
                    <div className="overflow-hidden rounded-md border border-border/70">
                      {labelSyncRules.map((rule) => (
                        <FeishuProjectLabelSyncRuleRow
                          key={rule.id}
                          workspaceId={wsId}
                          fields={issueFields}
                          integrationReady={canManage && !!projectKey.trim() && !!pluginId.trim()}
                          rule={rule}
                          onChange={(next) =>
                            setLabelSyncRules((prev) => prev.map((item) => (item.id === rule.id ? next : item)))
                          }
                          onRemove={() =>
                            setLabelSyncRules((prev) => prev.filter((item) => item.id !== rule.id))
                          }
                        />
                      ))}
                    </div>
                  )}
                </div>

                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3 border-b border-border/70 pb-2">
                    <p className="text-xs font-medium text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_mapping_section)}
                    </p>
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
                        <p className="text-[11px] text-muted-foreground">
                          {t(($) => $.integrations.feishu_project_reverse_mapping_disable_hint)}
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

                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3 border-b border-border/70 pb-2">
                    <p className="text-xs font-medium text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_routing_section)}
                    </p>
                  </div>
                  <FeishuProjectRoutingSection
                    workspaceId={wsId}
                    integration={feishuProject ?? null}
                    fieldKey={businessLineFieldKey}
                    onFieldChanged={(key, name) => {
                      setBusinessLineFieldKey(key);
                      setBusinessLineFieldName(name);
                    }}
                    rows={routeRows}
                    setRows={setRouteRows}
                    expanded={routesExpanded}
                    setExpanded={setRoutesExpanded}
                  />
                  {/* Assignment policy lives with routing: routing decides the
                      project, this toggle decides who picks up the new issue.
                      The per-route fallback agent (above) covers the case
                      where this lookup misses. */}
                  <div className="flex items-center justify-between gap-4 rounded-md border border-border/70 px-3 py-3">
                    <div className="space-y-1">
                      <p className="text-xs font-medium">
                        {t(($) => $.integrations.feishu_project_assign_owner_agent)}
                      </p>
                      <p className="text-[11px] text-muted-foreground">
                        {t(($) => $.integrations.feishu_project_assign_owner_agent_hint)}
                      </p>
                    </div>
                    <Switch
                      checked={assignOpenItemsToOwnerAgent}
                      onCheckedChange={setAssignOpenItemsToOwnerAgent}
                    />
                  </div>
                </div>

                <div className="flex flex-col gap-4 border-t border-border/70 pt-4 lg:flex-row lg:items-end lg:justify-between">
                  <div className="min-h-12 min-w-0 flex-1 space-y-2">
                    <p className="break-words text-xs text-muted-foreground">
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

function FeishuProjectLabelSyncRuleRow({
  workspaceId,
  fields,
  integrationReady,
  rule,
  onChange,
  onRemove,
}: {
  workspaceId: string;
  fields: FeishuProjectFieldMeta[];
  integrationReady: boolean;
  rule: FeishuProjectLabelSyncRule;
  onChange: (rule: FeishuProjectLabelSyncRule) => void;
  onRemove: () => void;
}) {
  const { t } = useT("settings");
  const selectedField = fields.find((field) => field.key === rule.field_key);
  const { data: optionsData } = useQuery({
    ...feishuProjectBusinessLinesOptions(workspaceId, rule.field_key, "issue", integrationReady && !!rule.field_key),
  });
  const fieldOptions = flattenFieldOptions(optionsData?.business_lines ?? []);

  // /field/all returns ~50 fields per work-item type — the old Select scrolls forever,
  // so use a Popover+Command combobox with name/key/pinyin search. Filtering is local
  // (shouldFilter={false}) because cmdk's default fuzzy match doesn't speak pinyin.
  const [fieldPickerOpen, setFieldPickerOpen] = useState(false);
  const [fieldQuery, setFieldQuery] = useState("");
  const visibleFields = useMemo(() => {
    const q = fieldQuery.trim().toLowerCase();
    if (!q) return fields;
    return fields.filter(
      (f) =>
        f.name.toLowerCase().includes(q) ||
        f.key.toLowerCase().includes(q) ||
        matchesPinyin(f.name, q),
    );
  }, [fields, fieldQuery]);

  return (
    <div className="grid gap-3 border-b border-border/70 p-3 last:border-b-0 lg:grid-cols-[76px_minmax(180px,1fr)_minmax(150px,220px)_minmax(150px,220px)_36px] lg:items-center">
      <div className="flex items-center justify-between gap-3 lg:justify-start">
        <span className="text-xs font-medium text-muted-foreground lg:hidden">
          {t(($) => $.integrations.feishu_project_label_sync_enabled)}
        </span>
        <Switch
          checked={rule.enabled}
          onCheckedChange={(enabled) => onChange({ ...rule, enabled })}
        />
      </div>

      <label className="min-w-0 space-y-1.5 text-xs font-medium">
        {t(($) => $.integrations.feishu_project_label_sync_field)}
        <Popover
          open={fieldPickerOpen}
          onOpenChange={(open) => {
            setFieldPickerOpen(open);
            if (!open) setFieldQuery("");
          }}
        >
          <PopoverTrigger
            className="flex h-7 w-full items-center justify-between gap-1.5 rounded-[min(var(--radius-md),10px)] border border-input bg-transparent px-2.5 text-sm whitespace-nowrap transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 dark:hover:bg-input/50"
          >
            <span className={`min-w-0 flex-1 truncate text-left ${selectedField ? "" : "text-muted-foreground"}`}>
              {selectedField
                ? `${selectedField.name} (${selectedField.key})`
                : t(($) => $.integrations.feishu_project_label_sync_field_placeholder)}
            </span>
            <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          </PopoverTrigger>
          <PopoverContent align="start" sideOffset={4} className="w-[var(--anchor-width)] p-0">
            <Command shouldFilter={false}>
              <CommandInput
                placeholder={t(($) => $.integrations.feishu_project_label_sync_field_search_placeholder)}
                value={fieldQuery}
                onValueChange={setFieldQuery}
              />
              <CommandList className="max-h-64">
                {visibleFields.length === 0 && (
                  <CommandEmpty>
                    {t(($) => $.integrations.feishu_project_label_sync_field_no_results)}
                  </CommandEmpty>
                )}
                <CommandGroup>
                  <CommandItem
                    value={NO_FIELD}
                    onSelect={() => {
                      onChange({ ...rule, field_key: "", field_name: "", match: "" });
                      setFieldPickerOpen(false);
                    }}
                  >
                    <span className="text-muted-foreground">
                      {t(($) => $.integrations.feishu_project_label_sync_field_placeholder)}
                    </span>
                  </CommandItem>
                  {visibleFields.map((field) => (
                    <CommandItem
                      key={field.key}
                      value={field.key}
                      onSelect={() => {
                        onChange({
                          ...rule,
                          field_key: field.key,
                          field_name: field.name,
                          match: "",
                        });
                        setFieldPickerOpen(false);
                      }}
                      className="flex items-center gap-2"
                    >
                      <span className="min-w-0 flex-1 truncate">{field.name}</span>
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                        {field.key}
                      </span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </label>

      <label className="min-w-0 space-y-1.5 text-xs font-medium">
        {t(($) => $.integrations.feishu_project_label_sync_match)}
        {fieldOptions.length > 0 ? (
          <Select
            value={rule.match || NO_MATCH}
            onValueChange={(value) => {
              const selected = value ?? NO_MATCH;
              onChange({ ...rule, match: selected === NO_MATCH ? "" : selected });
            }}
          >
            <SelectTrigger size="sm" className="w-full">
              <span className="min-w-0 flex-1 truncate text-left">
                {rule.match || t(($) => $.integrations.feishu_project_label_sync_match_placeholder)}
              </span>
            </SelectTrigger>
            <SelectContent align="start">
              <SelectItem value={NO_MATCH}>
                {t(($) => $.integrations.feishu_project_label_sync_match_placeholder)}
              </SelectItem>
              {fieldOptions.map((option) => (
                <SelectItem key={option.id} value={option.name}>
                  {option.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Input
            value={rule.match}
            onChange={(event) => onChange({ ...rule, match: event.target.value })}
            placeholder={t(($) => $.integrations.feishu_project_label_sync_match_placeholder)}
          />
        )}
      </label>

      <label className="min-w-0 space-y-1.5 text-xs font-medium">
        {t(($) => $.integrations.feishu_project_label_sync_label)}
        <Input
          value={rule.label_name}
          onChange={(event) => onChange({ ...rule, label_name: event.target.value })}
          placeholder={t(($) => $.integrations.feishu_project_label_sync_label_placeholder)}
        />
      </label>

      <Button
        type="button"
        size="icon"
        variant="ghost"
        title={t(($) => $.integrations.feishu_project_label_sync_remove)}
        onClick={onRemove}
        className="h-9 w-9 justify-self-end"
      >
        <Trash2 className="h-4 w-4" />
      </Button>
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

function compactLabelSyncRules(rules: FeishuProjectLabelSyncRule[]): FeishuProjectLabelSyncRule[] {
  return rules.map((rule) => ({
    id: rule.id,
    enabled: rule.enabled,
    field_key: rule.field_key.trim(),
    field_name: rule.field_name.trim(),
    match: rule.match.trim(),
    label_name: rule.label_name.trim(),
  }));
}

function newLocalRuleId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `rule-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function flattenFieldOptions(nodes: FeishuProjectBusinessLineNode[]): Array<{ id: string; name: string }> {
  const out: Array<{ id: string; name: string }> = [];
  const walk = (items: FeishuProjectBusinessLineNode[]) => {
    for (const item of items) {
      if (item.id && item.name) {
        out.push({ id: item.id, name: item.name });
      }
      if (item.children?.length) {
        walk(item.children);
      }
    }
  };
  walk(nodes);
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
