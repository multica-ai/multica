"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, Loader2, PackageCheck, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useCurrentMember } from "@multica/core/permissions";
import {
  comparePluginVersions,
  pluginCatalogOptions,
  pluginInstallationsOptions,
  useInstallPlugin,
  useRollbackPlugin,
  useSetPluginEnabled,
  useUpgradePlugin,
} from "@multica/core/plugins";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { PluginCatalogRelease, PluginInstallation } from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

type BindingScope = "workspace" | "agent";

function installationState(installation: PluginInstallation): "disabled" | "activating" | "healthy" | "degraded" | "failed" {
  if (installation.enabled !== true) return "disabled";
  if (installation.lifecycle_status === "activating") return "activating";
  if (installation.health_state === "error" || installation.lifecycle_status === "error") return "failed";
  if (installation.health_state === "degraded" || installation.lifecycle_status === "degraded") return "degraded";
  return "healthy";
}

export function PluginsTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentMember = useCurrentMember(wsId);
  const canManage = currentMember.role === "owner" || currentMember.role === "admin";
  const catalogQuery = useQuery(pluginCatalogOptions(wsId));
  const installationsQuery = useQuery(pluginInstallationsOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const installMutation = useInstallPlugin(wsId);
  const upgradeMutation = useUpgradePlugin(wsId);
  const enabledMutation = useSetPluginEnabled(wsId);
  const rollbackMutation = useRollbackPlugin(wsId);
  const [selectedVersions, setSelectedVersions] = useState<Record<string, string>>({});
  const [selectedScopes, setSelectedScopes] = useState<Record<string, BindingScope>>({});
  const [selectedAgents, setSelectedAgents] = useState<Record<string, string>>({});

  const releasesByPlugin = useMemo(() => {
    const grouped = new Map<string, PluginCatalogRelease[]>();
    for (const release of catalogQuery.data?.releases ?? []) {
      const versions = grouped.get(release.plugin_key) ?? [];
      versions.push(release);
      grouped.set(release.plugin_key, versions);
    }
    for (const versions of grouped.values()) {
      versions.sort((left, right) => comparePluginVersions(right.version, left.version));
    }
    return [...grouped.entries()];
  }, [catalogQuery.data?.releases]);

  const installations = useMemo(
    () => new Map((installationsQuery.data?.plugins ?? []).map((installation) => [installation.plugin_key, installation])),
    [installationsQuery.data?.plugins],
  );
  const agents = (agentsQuery.data ?? []).filter((agent) => !agent.archived_at);
  const isMutating = installMutation.isPending || upgradeMutation.isPending || enabledMutation.isPending || rollbackMutation.isPending;

  const reportError = (error: unknown) => {
    toast.error(error instanceof Error ? error.message : t(($) => $.plugins.action_failed));
  };

  if (catalogQuery.isPending || installationsQuery.isPending) {
    return (
      <SettingsTab title={t(($) => $.plugins.title)} description={t(($) => $.plugins.description)}>
        <SettingsCard>
          <div className="space-y-3 p-4" aria-label={t(($) => $.plugins.loading)}>
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-8 w-28" />
          </div>
        </SettingsCard>
      </SettingsTab>
    );
  }

  if (catalogQuery.isError || installationsQuery.isError) {
    return (
      <SettingsTab title={t(($) => $.plugins.title)} description={t(($) => $.plugins.description)}>
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.plugins.load_failed)}</AlertTitle>
          <AlertDescription>{t(($) => $.plugins.load_failed_description)}</AlertDescription>
        </Alert>
      </SettingsTab>
    );
  }

  if (catalogQuery.data?.supported !== true) {
    return (
      <SettingsTab title={t(($) => $.plugins.title)} description={t(($) => $.plugins.description)}>
        <Alert>
          <AlertCircle />
          <AlertTitle>{t(($) => $.plugins.backend_unavailable)}</AlertTitle>
          <AlertDescription>{t(($) => $.plugins.backend_unavailable_description)}</AlertDescription>
        </Alert>
      </SettingsTab>
    );
  }

  return (
    <SettingsTab title={t(($) => $.plugins.title)} description={t(($) => $.plugins.description)}>
      {catalogQuery.data.diagnostics.length > 0 ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.plugins.catalog_degraded)}</AlertTitle>
          <AlertDescription>{t(($) => $.plugins.catalog_degraded_description)}</AlertDescription>
        </Alert>
      ) : null}

      {!canManage && !currentMember.isLoading ? (
        <Alert>
          <ShieldCheck />
          <AlertTitle>{t(($) => $.plugins.read_only)}</AlertTitle>
          <AlertDescription>{t(($) => $.plugins.read_only_description)}</AlertDescription>
        </Alert>
      ) : null}

      {releasesByPlugin.length === 0 ? (
        <SettingsCard>
          <div className="p-6 text-center text-body text-muted-foreground">
            {t(($) => $.plugins.empty)}
          </div>
        </SettingsCard>
      ) : null}

      {releasesByPlugin.map(([pluginKey, versions]) => {
        const latest = versions[0];
        if (!latest) return null;
        const installation = installations.get(pluginKey) ?? latest.installation;
        const selectedVersion = selectedVersions[pluginKey] ?? latest.version;
        const selectedRelease = versions.find((release) => release.version === selectedVersion) ?? latest;
        const upgrade = installation && comparePluginVersions(latest.version, installation.desired_version) > 0 ? latest : null;
        const rollback = installation
          ? versions.find((release) => comparePluginVersions(release.version, installation.desired_version) < 0)
          : null;
        const scope = installation ? selectedScopes[installation.id] ?? "workspace" : "workspace";
        const selectedAgent = installation ? selectedAgents[installation.id] ?? agents[0]?.id ?? "" : "";
        const state = installation ? installationState(installation) : null;
        const activeBindings = installation?.bindings.filter((binding) => binding.enabled === true) ?? [];
        const workspaceBindingActive = activeBindings.some((binding) => binding.scope_type === "workspace");

        return (
          <SettingsSection key={pluginKey}>
            <SettingsCard>
              <div className="space-y-5 p-4 sm:p-5">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <PackageCheck className="size-4 text-brand" />
                      <h3 className="text-title font-semibold">{latest.name}</h3>
                      <Badge variant="outline">{t(($) => $.plugins.official)}</Badge>
                      {latest.signature_verified === true ? (
                        <Badge variant="secondary"><ShieldCheck />{t(($) => $.plugins.signed)}</Badge>
                      ) : (
                        <Badge variant="destructive">{t(($) => $.plugins.signature_unverified)}</Badge>
                      )}
                      {state ? (
                        <Badge variant={state === "failed" ? "destructive" : state === "healthy" ? "default" : "secondary"}>
                          {state === "activating" ? <Loader2 className="animate-spin" /> : null}
                          {t(($) => $.plugins.states[state])}
                        </Badge>
                      ) : null}
                    </div>
                    <p className="mt-1 text-body text-muted-foreground">{latest.description}</p>
                    <p className="mt-1 break-all font-mono text-caption text-muted-foreground">{pluginKey}</p>
                  </div>
                  <Badge variant="outline">{installation?.desired_version ?? selectedRelease.version}</Badge>
                </div>

                <div className="grid gap-4 text-caption sm:grid-cols-2">
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.contributes)}</div>
                    <ul className="mt-1 space-y-1 text-muted-foreground">
                      {selectedRelease.contributions.map((contribution) => (
                        <li key={contribution.key}>
                          <span className="font-medium text-foreground">{contribution.name}</span>
                          {" · "}{contribution.type}{" — "}{contribution.description}
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.permissions)}</div>
                    <p className="mt-1 text-muted-foreground">
                      {selectedRelease.requested_capabilities.join(", ") || t(($) => $.plugins.review.none)}
                    </p>
                  </div>
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.compatibility)}</div>
                    <p className="mt-1 text-muted-foreground">
                      {t(($) => $.plugins.review.host_api)} {selectedRelease.host_api}{" · "}
                      {selectedRelease.required_daemon_features.join(", ")}
                    </p>
                  </div>
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.publisher)}</div>
                    <p className="mt-1 break-all text-muted-foreground">
                      {selectedRelease.publisher}{" · "}{selectedRelease.signature_key_id}
                    </p>
                  </div>
                </div>

                {selectedRelease.compatible !== true ? (
                  <Alert variant="destructive">
                    <AlertCircle />
                    <AlertTitle>{t(($) => $.plugins.incompatible)}</AlertTitle>
                    <AlertDescription>{t(($) => $.plugins.incompatible_description)}</AlertDescription>
                  </Alert>
                ) : null}

                {!installation ? (
                  <div className="flex flex-col gap-2 border-t border-surface-border pt-4 sm:flex-row sm:items-center sm:justify-between">
                    <p className="text-caption text-muted-foreground">{t(($) => $.plugins.install_disabled_hint)}</p>
                    <div className="flex items-center gap-2">
                      <Select
                        items={versions.map((release) => ({ value: release.version, label: release.version }))}
                        value={selectedVersion}
                        onValueChange={(value) => value && setSelectedVersions((current) => ({ ...current, [pluginKey]: value }))}
                      >
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          {versions.map((release) => <SelectItem key={release.version} value={release.version}>{release.version}</SelectItem>)}
                        </SelectContent>
                      </Select>
                      <Button
                        disabled={!canManage || isMutating || selectedRelease.compatible !== true || selectedRelease.signature_verified !== true}
                        onClick={() => installMutation.mutateAsync({ plugin_key: pluginKey, version: selectedRelease.version })
                          .then(() => toast.success(t(($) => $.plugins.installed_disabled)))
                          .catch(reportError)}
                      >
                        {installMutation.isPending ? <Loader2 className="animate-spin" /> : null}
                        {t(($) => $.plugins.install)}
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="space-y-4 border-t border-surface-border pt-4">
                    <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                      <span>{t(($) => $.plugins.active_version)} {installation.active_version || t(($) => $.plugins.none)}</span>
                      <span>·</span>
                      <span>{t(($) => $.plugins.health)} {installation.health_state || installation.lifecycle_status}</span>
                    </div>

                    {activeBindings.length > 0 ? (
                      <div className="space-y-2">
                        <div className="text-caption font-medium">{t(($) => $.plugins.bindings)}</div>
                        {activeBindings.map((binding) => {
                          const agentName = binding.scope_type === "agent"
                            ? agents.find((agent) => agent.id === binding.scope_id)?.name ?? t(($) => $.plugins.unknown_agent)
                            : workspace?.name ?? t(($) => $.plugins.workspace_scope);
                          return (
                            <div key={`${binding.scope_type}:${binding.scope_id}`} className="flex items-center justify-between gap-3 rounded-lg bg-muted px-3 py-2">
                              <span className="text-caption">{binding.scope_type === "agent" ? t(($) => $.plugins.agent_scope) : t(($) => $.plugins.workspace_scope)} · {agentName}</span>
                              <Button
                                size="xs"
                                variant="ghost"
                                disabled={!canManage || isMutating}
                                onClick={() => enabledMutation.mutateAsync({
                                  installationId: installation.id,
                                  enabled: false,
                                  binding: { scope_type: binding.scope_type === "agent" ? "agent" : "workspace", scope_id: binding.scope_id },
                                }).then(() => toast.success(t(($) => $.plugins.binding_disabled))).catch(reportError)}
                              >
                                {t(($) => $.plugins.disable_binding)}
                              </Button>
                            </div>
                          );
                        })}
                      </div>
                    ) : (
                      <p className="text-caption text-muted-foreground">{t(($) => $.plugins.no_bindings)}</p>
                    )}

                    <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                      <Select
                        items={[
                          { value: "workspace", label: t(($) => $.plugins.workspace_scope) },
                          { value: "agent", label: t(($) => $.plugins.agent_scope) },
                        ]}
                        value={scope}
                        onValueChange={(value) => value && setSelectedScopes((current) => ({ ...current, [installation.id]: value as BindingScope }))}
                      >
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="workspace">{t(($) => $.plugins.workspace_scope)}</SelectItem>
                          <SelectItem value="agent">{t(($) => $.plugins.agent_scope)}</SelectItem>
                        </SelectContent>
                      </Select>
                      {scope === "agent" ? (
                        <Select
                          items={agents.map((agent) => ({ value: agent.id, label: agent.name }))}
                          value={selectedAgent}
                          onValueChange={(value) => value && setSelectedAgents((current) => ({ ...current, [installation.id]: value }))}
                        >
                          <SelectTrigger className="max-w-56"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            {agents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.name}</SelectItem>)}
                          </SelectContent>
                        </Select>
                      ) : null}
                      <Button
                        disabled={!canManage || isMutating || (scope === "agent" && !selectedAgent)}
                        onClick={() => enabledMutation.mutateAsync({
                          installationId: installation.id,
                          enabled: true,
                          binding: {
                            scope_type: scope,
                            scope_id: scope === "workspace" ? wsId : selectedAgent,
                          },
                        }).then(() => toast.success(t(($) => $.plugins.enabled))).catch(reportError)}
                      >
                        {enabledMutation.isPending ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
                        {t(($) => $.plugins.enable_scope)}
                      </Button>
                      <Button
                        variant="outline"
                        disabled={!canManage || isMutating || !workspaceBindingActive}
                        onClick={() => enabledMutation.mutateAsync({
                          installationId: installation.id,
                          enabled: false,
                          binding: { scope_type: "workspace", scope_id: wsId },
                        }).then(() => toast.success(t(($) => $.plugins.disabled))).catch(reportError)}
                      >
                        {t(($) => $.plugins.disable_workspace)}
                      </Button>
                    </div>

                    <div className="flex flex-wrap gap-2">
                      {upgrade ? (
                        <Button
                          variant="outline"
                          disabled={!canManage || isMutating || upgrade.compatible !== true}
                          onClick={() => upgradeMutation.mutateAsync({
                            installationId: installation.id,
                            plugin_key: pluginKey,
                            version: upgrade.version,
                          }).then(() => toast.success(t(($) => $.plugins.upgraded))).catch(reportError)}
                        >
                          {upgradeMutation.isPending ? <Loader2 className="animate-spin" /> : null}
                          {t(($) => $.plugins.upgrade_to, { version: upgrade.version })}
                        </Button>
                      ) : null}
                      {rollback ? (
                        <Button
                          variant="outline"
                          disabled={!canManage || isMutating}
                          onClick={() => rollbackMutation.mutateAsync({ installationId: installation.id, version: rollback.version })
                            .then(() => toast.success(t(($) => $.plugins.rolled_back)))
                            .catch(reportError)}
                        >
                          {rollbackMutation.isPending ? <Loader2 className="animate-spin" /> : null}
                          {t(($) => $.plugins.rollback_to, { version: rollback.version })}
                        </Button>
                      ) : null}
                    </div>
                  </div>
                )}
              </div>
            </SettingsCard>
          </SettingsSection>
        );
      })}
    </SettingsTab>
  );
}
