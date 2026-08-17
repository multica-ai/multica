"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, Loader2, PackageCheck, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useCurrentMember } from "@multica/core/permissions";
import {
  comparePluginVersions,
  pluginCatalogOptions,
  pluginInstallationsOptions,
  useApprovePluginRemoteMCPTools,
  useConfigurePluginRemoteMCP,
  useInstallPlugin,
  useRevokePluginRemoteMCPCredential,
  useRollbackPlugin,
  useSetPluginEnabled,
  useTestPluginRemoteMCP,
  useStartPluginRemoteMCPOAuth,
  useUninstallPlugin,
  useUpgradePlugin,
} from "@multica/core/plugins";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  PluginCatalogRelease,
  PluginInstallation,
  PluginRemoteMCPConfig,
  RemoteMCPDiscoveryResponse,
} from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { isDesktopShell } from "../../platform/local-directory";
import { openExternal } from "../../platform/open-external";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

type BindingScope = "workspace" | "agent";

type RemoteMCPAuthType = "none" | "oauth" | "bearer" | "header";

function RemoteMCPConfiguration({
  wsId,
  installationId,
  config,
  canManage,
}: {
  wsId: string;
  installationId: string;
  config: PluginRemoteMCPConfig;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const configureMutation = useConfigurePluginRemoteMCP(wsId);
  const testMutation = useTestPluginRemoteMCP(wsId);
  const approveMutation = useApprovePluginRemoteMCPTools(wsId);
  const revokeMutation = useRevokePluginRemoteMCPCredential(wsId);
  const oauthMutation = useStartPluginRemoteMCPOAuth(wsId);
  const [endpoint, setEndpoint] = useState(config.endpoint ?? config.default_endpoint ?? "");
  const [authType, setAuthType] = useState<RemoteMCPAuthType>(
    config.auth_type === "oauth" || config.auth_type === "bearer" || config.auth_type === "header" || config.auth_type === "none"
      ? config.auth_type
      : config.preferred_auth === "oauth" ? "oauth" : "none",
  );
  const [authHeader, setAuthHeader] = useState(config.auth_header ?? "Authorization");
  const [credential, setCredential] = useState("");
  const [oauthScope, setOAuthScope] = useState("");
  const [oauthClientId, setOAuthClientId] = useState("");
  const [oauthClientSecret, setOAuthClientSecret] = useState("");
  const [oauthAuthorizationEndpoint, setOAuthAuthorizationEndpoint] = useState("");
  const [oauthTokenEndpoint, setOAuthTokenEndpoint] = useState("");
  const [oauthTokenAuthMethod, setOAuthTokenAuthMethod] = useState<"none" | "client_secret_basic" | "client_secret_post">("none");
  const [publicConfig, setPublicConfig] = useState(() => JSON.stringify(config.public_config ?? {}, null, 2));
  const [failurePolicy, setFailurePolicy] = useState<"required" | "optional">(
    config.failure_policy === "optional" ? "optional" : "required",
  );
  const [discovery, setDiscovery] = useState<RemoteMCPDiscoveryResponse | null>(() =>
    !config.reviewed && config.discovered_tools.length > 0
      ? {
          config_revision: config.config_revision ?? 0,
          discovered_tools: config.discovered_tools,
          discovered_schema_digest: config.discovered_schema_digest ?? "",
        }
      : null,
  );
  const [approvedTools, setApprovedTools] = useState<string[]>(() =>
    !config.reviewed ? config.discovered_tools.map((tool) => tool.name) : [],
  );
  const isPending = configureMutation.isPending || testMutation.isPending
    || approveMutation.isPending || revokeMutation.isPending || oauthMutation.isPending;
  const isConnected = Boolean(config.config_revision)
    && (config.credential_state === "configured" || config.credential_state === "not_required");

  const reportError = (error: unknown) => {
    toast.error(error instanceof Error ? error.message : t(($) => $.plugins.action_failed));
  };
  const rememberDiscovery = (result: RemoteMCPDiscoveryResponse) => {
    setDiscovery(result);
    setApprovedTools(result.discovered_tools.map((tool) => tool.name));
  };
  const credentialStateLabel = config.credential_state === "configured"
    ? t(($) => $.plugins.remote_mcp.credential_states.configured)
    : config.credential_state === "revoked"
      ? t(($) => $.plugins.remote_mcp.credential_states.revoked)
      : config.credential_state === "not_required"
        ? t(($) => $.plugins.remote_mcp.credential_states.not_required)
        : t(($) => $.plugins.remote_mcp.credential_states.missing);
  const configure = async (requestedAuth: RemoteMCPAuthType = authType) => {
    let parsedPublicConfig: Record<string, unknown>;
    try {
      const parsed: unknown = JSON.parse(publicConfig);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
      parsedPublicConfig = parsed as Record<string, unknown>;
    } catch {
      toast.error(t(($) => $.plugins.remote_mcp.invalid_public_config));
      return;
    }
    try {
      if (requestedAuth === "oauth") {
        const result = await oauthMutation.mutateAsync({
          installationId,
          contributionKey: config.contribution_key,
          request: {
            endpoint: endpoint.trim() || config.default_endpoint,
            public_config: parsedPublicConfig,
            failure_policy: failurePolicy,
            scope: oauthScope.trim() || undefined,
            client_id: oauthClientId.trim() || undefined,
            client_secret: oauthClientSecret || undefined,
            token_endpoint_auth_method: oauthClientId.trim() ? oauthTokenAuthMethod : undefined,
            authorization_endpoint: oauthAuthorizationEndpoint.trim() || undefined,
            token_endpoint: oauthTokenEndpoint.trim() || undefined,
            return_to: `${window.location.pathname}${window.location.search}`,
          },
        });
        if (!result.authorization_url) {
          throw new Error(t(($) => $.plugins.remote_mcp.oauth_connect_failed));
        }
        if (isDesktopShell()) {
          openExternal(result.authorization_url);
        } else {
          window.location.assign(result.authorization_url);
        }
        return;
      }
      const result = await configureMutation.mutateAsync({
        installationId,
        contributionKey: config.contribution_key,
        request: {
          endpoint: endpoint.trim(),
          public_config: parsedPublicConfig,
          auth_type: requestedAuth,
          auth_header: requestedAuth === "header" ? authHeader.trim() : undefined,
          credential: requestedAuth === "none" ? undefined : credential,
          failure_policy: failurePolicy,
        },
      });
      setCredential("");
      rememberDiscovery(result);
      toast.success(t(($) => $.plugins.remote_mcp.configured_success));
    } catch (error) {
      reportError(error);
    }
  };

  return (
    <div className="space-y-4 rounded-xl border border-surface-border bg-muted/30 p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <div className="text-body font-medium">{t(($) => $.plugins.remote_mcp.title)} · {config.contribution_key}</div>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
            {config.endpoint_domain ? <span>{config.endpoint_domain}</span> : null}
            <Badge variant={config.credential_state === "revoked" || config.credential_state === "missing" ? "destructive" : "secondary"}>
              {credentialStateLabel}
            </Badge>
            <Badge variant={config.reviewed ? "default" : "secondary"}>
              {config.reviewed ? t(($) => $.plugins.remote_mcp.reviewed) : t(($) => $.plugins.remote_mcp.pending_review)}
            </Badge>
            {config.credential_hint ? <span>{config.credential_hint}</span> : null}
          </div>
        </div>
        {config.credential_state !== "not_required" ? (
          <Button
            size="xs"
            variant="ghost"
            disabled={!canManage || isPending || config.credential_state !== "configured"}
            onClick={() => revokeMutation.mutateAsync({ installationId, contributionKey: config.contribution_key })
              .then(() => toast.success(t(($) => $.plugins.remote_mcp.revoked_success)))
              .catch(reportError)}
          >
            {t(($) => $.plugins.remote_mcp.revoke)}
          </Button>
        ) : null}
      </div>

      {config.default_endpoint && (config.preferred_auth === "oauth" || config.preferred_auth === "none") ? (
        <div className="flex flex-wrap items-center gap-3 rounded-lg border border-surface-border bg-background p-3">
          <div className="min-w-0 flex-1">
            <div className="text-caption font-medium">
              {isConnected ? t(($) => $.plugins.remote_mcp.connected) : t(($) => $.plugins.remote_mcp.ready_to_connect)}
            </div>
            <div className="mt-0.5 truncate text-caption text-muted-foreground">{config.default_endpoint}</div>
          </div>
          <Button
            size="sm"
            disabled={!canManage || isPending || isConnected}
            onClick={() => configure(config.preferred_auth as RemoteMCPAuthType)}
          >
            {oauthMutation.isPending || configureMutation.isPending ? <Loader2 className="animate-spin" /> : null}
            {isConnected ? t(($) => $.plugins.remote_mcp.connected) : t(($) => $.plugins.remote_mcp.connect)}
          </Button>
        </div>
      ) : null}

      <details className="group rounded-lg border border-surface-border bg-background">
        <summary className="cursor-pointer select-none px-3 py-2 text-caption font-medium">
          {t(($) => $.plugins.remote_mcp.advanced)}
        </summary>
        <div className="grid gap-3 border-t border-surface-border p-3 sm:grid-cols-2">
        <label className="space-y-1 text-caption sm:col-span-2">
          <span className="font-medium">{t(($) => $.plugins.remote_mcp.endpoint)}</span>
          <Input
            value={endpoint}
            onChange={(event) => setEndpoint(event.target.value)}
            placeholder={t(($) => $.plugins.remote_mcp.endpoint_placeholder)}
            disabled={!canManage || isPending}
          />
        </label>
        <label className="space-y-1 text-caption">
          <span className="font-medium">{t(($) => $.plugins.remote_mcp.auth)}</span>
          <Select
            items={(["none", "oauth", "bearer", "header"] as const).map((value) => ({
              value,
              label: t(($) => $.plugins.remote_mcp.auth_types[value]),
            }))}
            value={authType}
            onValueChange={(value) => value && setAuthType(value as RemoteMCPAuthType)}
            disabled={!canManage || isPending}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="none">{t(($) => $.plugins.remote_mcp.auth_types.none)}</SelectItem>
              <SelectItem value="oauth">{t(($) => $.plugins.remote_mcp.auth_types.oauth)}</SelectItem>
              <SelectItem value="bearer">{t(($) => $.plugins.remote_mcp.auth_types.bearer)}</SelectItem>
              <SelectItem value="header">{t(($) => $.plugins.remote_mcp.auth_types.header)}</SelectItem>
            </SelectContent>
          </Select>
        </label>
        <label className="space-y-1 text-caption">
          <span className="font-medium">{t(($) => $.plugins.remote_mcp.failure_policy)}</span>
          <Select
            items={(["required", "optional"] as const).map((value) => ({
              value,
              label: t(($) => $.plugins.remote_mcp.failure_policies[value]),
            }))}
            value={failurePolicy}
            onValueChange={(value) => value && setFailurePolicy(value as "required" | "optional")}
            disabled={!canManage || isPending}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="required">{t(($) => $.plugins.remote_mcp.failure_policies.required)}</SelectItem>
              <SelectItem value="optional">{t(($) => $.plugins.remote_mcp.failure_policies.optional)}</SelectItem>
            </SelectContent>
          </Select>
        </label>
        {authType === "header" ? (
          <label className="space-y-1 text-caption">
            <span className="font-medium">{t(($) => $.plugins.remote_mcp.header_name)}</span>
            <Input value={authHeader} onChange={(event) => setAuthHeader(event.target.value)} disabled={!canManage || isPending} />
          </label>
        ) : null}
        {authType === "bearer" || authType === "header" ? (
          <label className="space-y-1 text-caption">
            <span className="font-medium">{t(($) => $.plugins.remote_mcp.credential)}</span>
            <Input
              type="password"
              autoComplete="new-password"
              value={credential}
              onChange={(event) => setCredential(event.target.value)}
              disabled={!canManage || isPending}
            />
          </label>
        ) : null}
        {authType === "oauth" ? (
          <>
            <label className="space-y-1 text-caption sm:col-span-2">
              <span className="font-medium">{t(($) => $.plugins.remote_mcp.oauth_scope)}</span>
              <Input value={oauthScope} onChange={(event) => setOAuthScope(event.target.value)} disabled={!canManage || isPending} />
            </label>
            <label className="space-y-1 text-caption">
              <span className="font-medium">{t(($) => $.plugins.remote_mcp.oauth_client_id)}</span>
              <Input value={oauthClientId} onChange={(event) => setOAuthClientId(event.target.value)} disabled={!canManage || isPending} />
            </label>
            <label className="space-y-1 text-caption">
              <span className="font-medium">{t(($) => $.plugins.remote_mcp.oauth_client_secret)}</span>
              <Input type="password" autoComplete="new-password" value={oauthClientSecret} onChange={(event) => setOAuthClientSecret(event.target.value)} disabled={!canManage || isPending} />
            </label>
            <label className="space-y-1 text-caption">
              <span className="font-medium">{t(($) => $.plugins.remote_mcp.oauth_authorization_endpoint)}</span>
              <Input value={oauthAuthorizationEndpoint} onChange={(event) => setOAuthAuthorizationEndpoint(event.target.value)} disabled={!canManage || isPending} />
            </label>
            <label className="space-y-1 text-caption">
              <span className="font-medium">{t(($) => $.plugins.remote_mcp.oauth_token_endpoint)}</span>
              <Input value={oauthTokenEndpoint} onChange={(event) => setOAuthTokenEndpoint(event.target.value)} disabled={!canManage || isPending} />
            </label>
            {oauthClientSecret ? (
              <label className="space-y-1 text-caption sm:col-span-2">
                <span className="font-medium">{t(($) => $.plugins.remote_mcp.oauth_token_auth_method)}</span>
                <Select
                  items={(["none", "client_secret_basic", "client_secret_post"] as const).map((value) => ({ value, label: value }))}
                  value={oauthTokenAuthMethod}
                  onValueChange={(value) => value && setOAuthTokenAuthMethod(value as typeof oauthTokenAuthMethod)}
                  disabled={!canManage || isPending}
                >
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">{t(($) => $.plugins.remote_mcp.oauth_token_auth_methods.none)}</SelectItem>
                    <SelectItem value="client_secret_basic">{t(($) => $.plugins.remote_mcp.oauth_token_auth_methods.client_secret_basic)}</SelectItem>
                    <SelectItem value="client_secret_post">{t(($) => $.plugins.remote_mcp.oauth_token_auth_methods.client_secret_post)}</SelectItem>
                  </SelectContent>
                </Select>
              </label>
            ) : null}
          </>
        ) : null}
        <label className="space-y-1 text-caption sm:col-span-2">
          <span className="font-medium">{t(($) => $.plugins.remote_mcp.public_config)}</span>
          <Textarea
            className="font-mono"
            value={publicConfig}
            onChange={(event) => setPublicConfig(event.target.value)}
            disabled={!canManage || isPending}
          />
        </label>
        </div>

        <div className="flex flex-wrap gap-2 border-t border-surface-border p-3">
        <Button
          size="sm"
          disabled={!canManage || isPending || endpoint.trim() === "" || ((authType === "bearer" || authType === "header") && credential === "")}
          onClick={() => configure()}
        >
          {configureMutation.isPending || oauthMutation.isPending ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.plugins.remote_mcp.configure_and_discover)}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={!canManage || isPending || !config.config_revision}
          onClick={() => testMutation.mutateAsync({ installationId, contributionKey: config.contribution_key })
            .then((result) => {
              rememberDiscovery(result);
              toast.success(t(($) => $.plugins.remote_mcp.test_success));
            }).catch(reportError)}
        >
          {testMutation.isPending ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.plugins.remote_mcp.test)}
        </Button>
        </div>
      </details>

      {discovery ? (
        <div className="space-y-3 border-t border-surface-border pt-3">
          <div className="text-caption font-medium">{t(($) => $.plugins.remote_mcp.discovery)}</div>
          {discovery.discovered_tools.length > 0 ? discovery.discovered_tools.map((tool) => (
            <label key={tool.name} className="flex items-start gap-2 rounded-lg bg-background px-3 py-2 text-caption">
              <Checkbox
                checked={approvedTools.includes(tool.name)}
                disabled={!canManage || isPending}
                onCheckedChange={() => setApprovedTools((current) => current.includes(tool.name)
                  ? current.filter((name) => name !== tool.name)
                  : [...current, tool.name])}
              />
              <span className="min-w-0">
                <span className="font-medium">{tool.name}</span>
                <Badge className="ml-2" variant={tool.risk === "write" ? "destructive" : "secondary"}>
                  {tool.risk === "write" ? t(($) => $.plugins.remote_mcp.write) : t(($) => $.plugins.remote_mcp.read)}
                </Badge>
                {tool.description ? <span className="mt-0.5 block text-muted-foreground">{tool.description}</span> : null}
              </span>
            </label>
          )) : <p className="text-caption text-muted-foreground">{t(($) => $.plugins.remote_mcp.no_tools)}</p>}
          <Button
            size="sm"
            disabled={!canManage || isPending || approvedTools.length === 0}
            onClick={() => approveMutation.mutateAsync({
              installationId,
              contributionKey: config.contribution_key,
              tools: approvedTools,
            }).then(() => {
              setDiscovery(null);
              toast.success(t(($) => $.plugins.remote_mcp.approved_success));
            }).catch(reportError)}
          >
            {approveMutation.isPending ? <Loader2 className="animate-spin" /> : <ShieldCheck />}
            {t(($) => $.plugins.remote_mcp.approve)}
          </Button>
        </div>
      ) : config.approved_tools.length > 0 ? (
        <div className="text-caption text-muted-foreground">
          {t(($) => $.plugins.remote_mcp.approved_tools)}: {config.approved_tools.map((tool) => tool.name).join(", ")}
        </div>
      ) : null}
    </div>
  );
}

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
  const refetchInstallations = installationsQuery.refetch;
  const agentsQuery = useQuery(agentListOptions(wsId));
  const membersQuery = useQuery(memberListOptions(wsId));
  const installMutation = useInstallPlugin(wsId);
  const upgradeMutation = useUpgradePlugin(wsId);
  const enabledMutation = useSetPluginEnabled(wsId);
  const rollbackMutation = useRollbackPlugin(wsId);
  const uninstallMutation = useUninstallPlugin(wsId);
  const [selectedVersions, setSelectedVersions] = useState<Record<string, string>>({});
  const [selectedScopes, setSelectedScopes] = useState<Record<string, BindingScope>>({});
  const [selectedAgents, setSelectedAgents] = useState<Record<string, string>>({});

  useEffect(() => {
    const search = new URLSearchParams(window.location.search);
    const connected = search.get("remote_mcp_connected") === "1";
    const failed = search.has("remote_mcp_error");
    if (!connected && !failed) return;

    if (connected) {
      toast.success(t(($) => $.plugins.remote_mcp.oauth_connected_success));
      void refetchInstallations();
    } else {
      toast.error(t(($) => $.plugins.remote_mcp.oauth_connect_failed));
    }
    search.delete("remote_mcp_connected");
    search.delete("remote_mcp_error");
    const query = search.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash}`);
  }, [refetchInstallations, t]);

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

  const officialInstallations = useMemo(
    () => new Map((installationsQuery.data?.plugins ?? [])
      .filter((installation) => installation.source_kind !== "private_dev")
      .map((installation) => [installation.plugin_key, installation])),
    [installationsQuery.data?.plugins],
  );
  const privateInstallations = (installationsQuery.data?.plugins ?? [])
    .filter((installation) => installation.source_kind === "private_dev");
  const agents = (agentsQuery.data ?? []).filter((agent) => !agent.archived_at);
  const members = membersQuery.data ?? [];
  const isMutating = installMutation.isPending || upgradeMutation.isPending || enabledMutation.isPending
    || rollbackMutation.isPending || uninstallMutation.isPending;

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

      {releasesByPlugin.length === 0 && privateInstallations.length === 0 ? (
        <SettingsCard>
          <div className="p-6 text-center text-body text-muted-foreground">
            {t(($) => $.plugins.empty)}
          </div>
        </SettingsCard>
      ) : null}

      {releasesByPlugin.map(([pluginKey, versions]) => {
        const latest = versions[0];
        if (!latest) return null;
        const installation = officialInstallations.get(pluginKey) ?? latest.installation;
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
        const remoteMCPReady = installation?.remote_mcp.every((config) => config.ready) ?? true;

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

                    {installation.remote_mcp.map((config) => (
                      <RemoteMCPConfiguration
                        key={config.contribution_key}
                        wsId={wsId}
                        installationId={installation.id}
                        config={config}
                        canManage={canManage}
                      />
                    ))}

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

                    {!remoteMCPReady ? (
                      <p className="text-caption text-muted-foreground">
                        {t(($) => $.plugins.remote_mcp.complete_before_enabling)}
                      </p>
                    ) : null}

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
                        disabled={!canManage || isMutating || !remoteMCPReady || (scope === "agent" && !selectedAgent)}
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
                      <Button
                        variant="destructive"
                        disabled={!canManage || isMutating}
                        onClick={() => uninstallMutation.mutateAsync(installation.id)
                          .then(() => toast.success(t(($) => $.plugins.uninstalled))).catch(reportError)}
                      >
                        {uninstallMutation.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
                        {t(($) => $.plugins.uninstall)}
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            </SettingsCard>
          </SettingsSection>
        );
      })}

      {privateInstallations.map((installation) => {
        const scope = selectedScopes[installation.id] ?? "workspace";
        const selectedAgent = selectedAgents[installation.id] ?? agents[0]?.id ?? "";
        const state = installationState(installation);
        const activeBindings = installation.bindings.filter((binding) => binding.enabled === true);
        const remoteMCPReady = installation.remote_mcp.every((config) => config.ready);
        const uploaderName = members.find((member) => member.user_id === installation.uploader_id)?.name;
        const rollbackVersion = [...installation.available_versions]
          .sort((left, right) => comparePluginVersions(right, left))
          .find((version) => comparePluginVersions(version, installation.desired_version) < 0);
        return (
          <SettingsSection key={installation.id}>
            <SettingsCard>
              <div className="space-y-5 p-4 sm:p-5">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <PackageCheck className="size-4 text-brand" />
                      <h3 className="text-title font-semibold">{installation.display_name}</h3>
                      <Badge variant="outline">{t(($) => $.plugins.private)}</Badge>
                      <Badge variant="destructive">{t(($) => $.plugins.unverified)}</Badge>
                      <Badge variant={state === "failed" ? "destructive" : state === "healthy" ? "default" : "secondary"}>
                        {state === "activating" ? <Loader2 className="animate-spin" /> : null}
                        {t(($) => $.plugins.states[state])}
                      </Badge>
                    </div>
                    {installation.description ? <p className="mt-1 text-body text-muted-foreground">{installation.description}</p> : null}
                    <p className="mt-1 break-all font-mono text-caption text-muted-foreground">{installation.plugin_key}</p>
                  </div>
                  <Badge variant="outline">{installation.desired_version}</Badge>
                </div>

                <div className="grid gap-4 text-caption sm:grid-cols-2">
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.contributes)}</div>
                    <ul className="mt-1 space-y-1 text-muted-foreground">
                      {installation.contribution_details.map((contribution) => (
                        <li key={contribution.key}>
                          <span className="font-medium text-foreground">{contribution.name || contribution.key}</span>
                          {" · "}{contribution.type}{contribution.description ? ` — ${contribution.description}` : ""}
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.permissions)}</div>
                    <p className="mt-1 text-muted-foreground">
                      {installation.requested_capabilities.join(", ") || t(($) => $.plugins.review.none)}
                    </p>
                  </div>
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.review.publisher)}</div>
                    <p className="mt-1 break-all text-muted-foreground">{installation.publisher}</p>
                  </div>
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.plugins.source)}</div>
                    <p className="mt-1 text-muted-foreground">
                      {t(($) => $.plugins.private_upload)}
                      {installation.uploader_id ? ` · ${t(($) => $.plugins.uploaded_by)} ${uploaderName ?? t(($) => $.plugins.unknown_member)}` : ""}
                    </p>
                  </div>
                </div>

                <div className="space-y-4 border-t border-surface-border pt-4">
                  <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                    <span>{t(($) => $.plugins.active_version)} {installation.active_version || t(($) => $.plugins.none)}</span>
                    <span>·</span>
                    <span>{t(($) => $.plugins.health)} {installation.health_state || installation.lifecycle_status}</span>
                  </div>

                  {installation.remote_mcp.map((config) => (
                    <RemoteMCPConfiguration
                      key={config.contribution_key}
                      wsId={wsId}
                      installationId={installation.id}
                      config={config}
                      canManage={canManage}
                    />
                  ))}

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
                  ) : <p className="text-caption text-muted-foreground">{t(($) => $.plugins.no_bindings)}</p>}

                  {!remoteMCPReady ? (
                    <p className="text-caption text-muted-foreground">
                      {t(($) => $.plugins.remote_mcp.complete_before_enabling)}
                    </p>
                  ) : null}

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
                      disabled={!canManage || isMutating || !remoteMCPReady || (scope === "agent" && !selectedAgent)}
                      onClick={() => enabledMutation.mutateAsync({
                        installationId: installation.id,
                        enabled: true,
                        binding: { scope_type: scope, scope_id: scope === "workspace" ? wsId : selectedAgent },
                      }).then(() => toast.success(t(($) => $.plugins.enabled))).catch(reportError)}
                    >
                      {enabledMutation.isPending ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
                      {t(($) => $.plugins.enable_scope)}
                    </Button>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    {rollbackVersion ? (
                      <Button
                        variant="outline"
                        disabled={!canManage || isMutating}
                        onClick={() => rollbackMutation.mutateAsync({ installationId: installation.id, version: rollbackVersion })
                          .then(() => toast.success(t(($) => $.plugins.rolled_back))).catch(reportError)}
                      >
                        {t(($) => $.plugins.rollback_to, { version: rollbackVersion })}
                      </Button>
                    ) : null}
                    <Button
                      variant="destructive"
                      disabled={!canManage || isMutating}
                      onClick={() => uninstallMutation.mutateAsync(installation.id)
                        .then(() => toast.success(t(($) => $.plugins.uninstalled))).catch(reportError)}
                    >
                      {uninstallMutation.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
                      {t(($) => $.plugins.uninstall)}
                    </Button>
                  </div>
                </div>
              </div>
            </SettingsCard>
          </SettingsSection>
        );
      })}
    </SettingsTab>
  );
}
