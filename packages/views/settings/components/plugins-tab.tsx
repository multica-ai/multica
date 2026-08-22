"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Loader2, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { useCurrentMember } from "@multica/core/permissions";
import {
  pluginInstallationsOptions,
  pluginPackagesOptions,
  useConfigurePlugin,
  useDeletePluginPackage,
  useInstallPlugin,
  usePreviewPlugin,
  usePublishPluginPackage,
  useSetPluginEnabled,
  useUninstallPlugin,
} from "@multica/core/plugins";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  PluginConfigField,
  PluginInstallation,
  PluginPackage,
  PluginPreview,
} from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
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
import { Switch } from "@multica/ui/components/ui/switch";
import { mcpHooks, PluginHookActivity, PluginMCPApproval } from "../../plugins";
import { useT } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * The scope list is the entire trust model: there is no signature, no
 * publisher verification, and no trust tier. So the consent screen shows the
 * raw scope strings alongside a plain-language line, and never summarizes them
 * away.
 */
function ScopeList({ scopes, highlighted }: { scopes: string[]; highlighted?: string[] }) {
  const { t } = useT("settings");
  const added = new Set(highlighted ?? []);
  return (
    <ul className="space-y-1.5">
      {scopes.map((scope) => (
        <li key={scope} className="flex items-baseline gap-2 text-caption">
          <code className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono">{scope}</code>
          <span className="text-muted-foreground">{scopeDescription(scope, t)}</span>
          {added.has(scope) ? (
            <Badge variant="secondary">{t(($) => $.plugins.consent.new_scope)}</Badge>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

type Translate = ReturnType<typeof useT<"settings">>["t"];

function scopeDescription(scope: string, t: Translate): string {
  if (scope.startsWith("net:")) {
    return t(($) => $.plugins.scopes.net, { domain: scope.slice("net:".length) });
  }
  switch (scope) {
    case "issues:read": return t(($) => $.plugins.scopes.issues_read);
    case "issues:write": return t(($) => $.plugins.scopes.issues_write);
    case "comments:read": return t(($) => $.plugins.scopes.comments_read);
    case "comments:write": return t(($) => $.plugins.scopes.comments_write);
    case "tasks:read": return t(($) => $.plugins.scopes.tasks_read);
    case "tasks:write": return t(($) => $.plugins.scopes.tasks_write);
    case "agents:read": return t(($) => $.plugins.scopes.agents_read);
    case "members:read": return t(($) => $.plugins.scopes.members_read);
    case "storage:user": return t(($) => $.plugins.scopes.storage_user);
    case "storage:workspace": return t(($) => $.plugins.scopes.storage_workspace);
    default: return t(($) => $.plugins.scopes.unknown);
  }
}

/**
 * The configuration form is generated from the manifest, not supplied by the
 * plugin: rendering plugin-authored form markup in the host would put plugin
 * code on our origin.
 */
function ConfigForm({
  installation,
  canManage,
  wsId,
}: {
  installation: PluginInstallation;
  canManage: boolean;
  wsId: string;
}) {
  const { t } = useT("settings");
  const configureMutation = useConfigurePlugin(wsId);
  const [values, setValues] = useState<Record<string, unknown>>(() => ({ ...installation.config }));
  const [secrets, setSecrets] = useState<Record<string, string>>({});

  if (installation.config_schema.length === 0) return null;

  const setValue = (key: string, value: unknown) => setValues((current) => ({ ...current, [key]: value }));
  const configuredSecrets = new Set(installation.configured_secrets);

  const submit = async () => {
    const payload: Record<string, unknown> = { ...values };
    // Only send a secret the admin actually typed. Sending "" would clear a
    // stored secret every time an unrelated field is saved.
    for (const [key, value] of Object.entries(secrets)) {
      if (value.length > 0) payload[key] = value;
    }
    try {
      await configureMutation.mutateAsync({ installationId: installation.id, values: payload });
      setSecrets({});
      toast.success(t(($) => $.plugins.config.saved));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.plugins.action_failed));
    }
  };

  return (
    <div className="space-y-4 border-t border-surface-border px-4 py-4">
      <div className="text-caption font-medium">{t(($) => $.plugins.config.title)}</div>
      {installation.config_schema.map((field) => (
        <ConfigField
          key={field.key}
          field={field}
          value={values[field.key]}
          secretValue={secrets[field.key] ?? ""}
          secretConfigured={configuredSecrets.has(field.key)}
          disabled={!canManage || configureMutation.isPending}
          onValueChange={(value) => setValue(field.key, value)}
          onSecretChange={(value) => setSecrets((current) => ({ ...current, [field.key]: value }))}
        />
      ))}
      <div className="flex justify-end">
        <Button size="sm" disabled={!canManage || configureMutation.isPending} onClick={submit}>
          {configureMutation.isPending ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.plugins.config.save)}
        </Button>
      </div>
    </div>
  );
}

function ConfigField({
  field,
  value,
  secretValue,
  secretConfigured,
  disabled,
  onValueChange,
  onSecretChange,
}: {
  field: PluginConfigField;
  value: unknown;
  secretValue: string;
  secretConfigured: boolean;
  disabled: boolean;
  onValueChange: (value: unknown) => void;
  onSecretChange: (value: string) => void;
}) {
  const { t } = useT("settings");
  const label = (
    <div className="min-w-0">
      <div className="text-caption font-medium">
        {field.label}
        {field.required ? <span className="ml-1 text-destructive">*</span> : null}
      </div>
      {field.description ? (
        <p className="mt-0.5 text-caption text-muted-foreground">{field.description}</p>
      ) : null}
    </div>
  );

  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      {label}
      <div className="w-full sm:w-96">
        {field.type === "secret" ? (
          <Input
            type="password"
            autoComplete="off"
            disabled={disabled}
            value={secretValue}
            placeholder={secretConfigured
              ? t(($) => $.plugins.config.secret_set)
              : field.placeholder ?? ""}
            onChange={(event) => onSecretChange(event.target.value)}
          />
        ) : field.type === "bool" ? (
          <Switch
            disabled={disabled}
            checked={value === true}
            onCheckedChange={(checked) => onValueChange(checked === true)}
          />
        ) : field.type === "enum" ? (
          <Select
            items={(field.options ?? []).map((option) => ({ value: option, label: option }))}
            value={typeof value === "string" ? value : ""}
            onValueChange={(next) => next && onValueChange(next)}
          >
            <SelectTrigger disabled={disabled}><SelectValue /></SelectTrigger>
            <SelectContent>
              {(field.options ?? []).map((option) => (
                <SelectItem key={option} value={option}>{option}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : field.type === "number" ? (
          <Input
            type="number"
            disabled={disabled}
            value={typeof value === "number" ? String(value) : ""}
            placeholder={field.placeholder ?? ""}
            onChange={(event) => {
              const parsed = Number(event.target.value);
              onValueChange(event.target.value === "" || Number.isNaN(parsed) ? undefined : parsed);
            }}
          />
        ) : field.multiline === true ? (
          // A field whose value is a list of lines is unreadable in a
          // single-line input — and the generated form is the one piece of
          // plugin UI the host owns, so getting it wrong is our bug.
          <Textarea
            rows={4}
            disabled={disabled}
            value={typeof value === "string" ? value : ""}
            placeholder={field.placeholder ?? ""}
            onChange={(event) => onValueChange(event.target.value)}
          />
        ) : (
          <Input
            disabled={disabled}
            value={typeof value === "string" ? value : ""}
            placeholder={field.placeholder ?? ""}
            onChange={(event) => onValueChange(event.target.value)}
          />
        )}
      </div>
    </div>
  );
}

/**
 * Publishing, and installing what was published.
 *
 * These are one screen because they are two halves of one story: an author
 * uploads an artifact, and an administrator installs one specific version of it.
 * There is no way to install code that was not published here first — which is
 * what makes the consent screen below a statement about the code and not just
 * about the manifest.
 */
function PublishAndInstall({ wsId, canManage }: { wsId: string; canManage: boolean }) {
  const { t } = useT("settings");
  const { data, isLoading } = useQuery(pluginPackagesOptions(wsId));
  const publishMutation = usePublishPluginPackage(wsId);
  const deleteMutation = useDeletePluginPackage(wsId);
  const previewMutation = usePreviewPlugin(wsId);
  const installMutation = useInstallPlugin(wsId);
  const fileRef = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<PluginPreview | null>(null);

  const packages = useMemo(() => data?.packages ?? [], [data]);

  const reportError = (error: unknown) => {
    toast.error(error instanceof Error ? error.message : t(($) => $.plugins.action_failed));
  };

  const publish = async (file: File) => {
    try {
      const published = await publishMutation.mutateAsync(file);
      toast.success(t(($) => $.plugins.publish.published, {
        name: published.name,
        version: published.versions[0]?.version ?? "",
      }));
    } catch (error) {
      reportError(error);
    } finally {
      // Cleared unconditionally so picking the same file again re-fires change,
      // which is what a person does after fixing a rejected bundle.
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const review = async (versionId: string) => {
    try {
      setPreview(await previewMutation.mutateAsync({ version_id: versionId }));
    } catch (error) {
      setPreview(null);
      reportError(error);
    }
  };

  const confirmInstall = async () => {
    if (!preview) return;
    try {
      await installMutation.mutateAsync({ version_id: preview.version_id, granted_scopes: preview.scopes });
      setPreview(null);
      toast.success(t(($) => $.plugins.consent.installed));
    } catch (error) {
      reportError(error);
    }
  };

  return (
    <SettingsSection title={t(($) => $.plugins.publish.title)} description={t(($) => $.plugins.publish.description)}>
      <SettingsCard>
        <div className="flex flex-col gap-2 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-caption text-muted-foreground">{t(($) => $.plugins.publish.hint)}</p>
          <input
            ref={fileRef}
            type="file"
            accept=".zip,application/zip"
            className="sr-only"
            aria-label={t(($) => $.plugins.publish.upload)}
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void publish(file);
            }}
          />
          <Button
            disabled={!canManage || publishMutation.isPending}
            onClick={() => fileRef.current?.click()}
          >
            {publishMutation.isPending ? <Loader2 className="animate-spin" /> : <Upload />}
            {t(($) => $.plugins.publish.upload)}
          </Button>
        </div>

        {isLoading ? (
          <div className="border-t border-surface-border px-4 py-4">
            <Skeleton className="h-16 w-full" aria-label={t(($) => $.plugins.loading)} />
          </div>
        ) : packages.length === 0 ? (
          <p className="border-t border-surface-border px-4 py-6 text-caption text-muted-foreground">
            {t(($) => $.plugins.publish.empty)}
          </p>
        ) : (
          packages.map((pluginPackage) => (
            <PublishedPackage
              key={pluginPackage.id}
              pluginPackage={pluginPackage}
              canManage={canManage}
              busy={previewMutation.isPending || deleteMutation.isPending}
              onReview={review}
              onDelete={(packageId) => deleteMutation
                .mutateAsync(packageId)
                .then(() => toast.success(t(($) => $.plugins.publish.deleted)))
                .catch(reportError)}
            />
          ))
        )}

        {preview ? (
          <div className="space-y-4 border-t border-surface-border px-4 py-4">
            <div>
              <div className="text-body font-semibold">{preview.manifest.name}</div>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.plugins.byline, {
                  author: preview.manifest.author.name,
                  version: preview.version,
                })}
                {preview.installed
                  ? t(($) => $.plugins.consent.upgrade_from, { version: preview.installed_version ?? "" })
                  : ""}
              </p>
              {preview.manifest.description ? (
                <p className="mt-2 text-caption">{preview.manifest.description}</p>
              ) : null}
            </div>

            <Alert>
              <AlertCircle />
              <AlertTitle>{t(($) => $.plugins.consent.title)}</AlertTitle>
              <AlertDescription>{t(($) => $.plugins.consent.description)}</AlertDescription>
            </Alert>

            <ScopeList scopes={preview.scopes} highlighted={preview.added_scopes} />

            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setPreview(null)}>
                {t(($) => $.plugins.consent.cancel)}
              </Button>
              <Button disabled={!canManage || installMutation.isPending} onClick={confirmInstall}>
                {installMutation.isPending ? <Loader2 className="animate-spin" /> : null}
                {preview.installed
                  ? t(($) => $.plugins.consent.confirm_upgrade)
                  : t(($) => $.plugins.consent.confirm)}
              </Button>
            </div>
          </div>
        ) : null}
      </SettingsCard>
    </SettingsSection>
  );
}

function PublishedPackage({
  pluginPackage,
  canManage,
  busy,
  onReview,
  onDelete,
}: {
  pluginPackage: PluginPackage;
  canManage: boolean;
  busy: boolean;
  onReview: (versionId: string) => void;
  onDelete: (packageId: string) => void;
}) {
  const { t } = useT("settings");
  // Versions arrive newest first, and the installed one is marked rather than
  // inferred: after a publish those are different rows, and that difference is
  // the whole reason an upgrade is a decision somebody makes.
  const installed = pluginPackage.versions.find((version) => version.installed === true);

  return (
    <div className="space-y-3 border-t border-surface-border px-4 py-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-body font-medium">{pluginPackage.name}</div>
          <p className="text-caption text-muted-foreground">{pluginPackage.plugin_key}</p>
        </div>
        <Button
          size="icon"
          variant="ghost"
          aria-label={t(($) => $.plugins.publish.delete)}
          disabled={!canManage || busy}
          onClick={() => onDelete(pluginPackage.id)}
        >
          <Trash2 />
        </Button>
      </div>

      <ul className="space-y-1.5">
        {pluginPackage.versions.map((version) => (
          <li key={version.id} className="flex flex-wrap items-center gap-2 text-caption">
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono">{version.version}</code>
            {version.installed === true ? (
              <Badge variant="secondary">{t(($) => $.plugins.publish.installed_version)}</Badge>
            ) : null}
            <span className="text-muted-foreground">{version.published_at.slice(0, 10)}</span>
            <span className="font-mono text-muted-foreground" title={version.digest}>
              {version.digest.slice(0, 12)}
            </span>
            {version.installed === true ? null : (
              <Button
                size="sm"
                variant="ghost"
                className="ml-auto"
                disabled={!canManage || busy}
                onClick={() => onReview(version.id)}
              >
                {installed ? t(($) => $.plugins.publish.review_upgrade) : t(($) => $.plugins.publish.review_install)}
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function InstalledPlugin({
  installation,
  wsId,
  canManage,
}: {
  installation: PluginInstallation;
  wsId: string;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const enabledMutation = useSetPluginEnabled(wsId);
  const uninstallMutation = useUninstallPlugin(wsId);
  const isMutating = enabledMutation.isPending || uninstallMutation.isPending;

  const reportError = (error: unknown) => {
    toast.error(error instanceof Error ? error.message : t(($) => $.plugins.action_failed));
  };

  // Only rendered when a hook contributes one: a plugin with no hooks has no
  // failing calls to report.
  const hasHooks = (installation.hooks ?? []).length > 0;

  // An mcp hook is inert until its tools are approved, so the panel appears for
  // every one of them rather than only when something is already pinned.
  const mcpContributions = mcpHooks(installation.hooks ?? []);

  const contributions = [
    ...installation.surfaces.map((surface) => `${surface.name} (${surface.type})`),
    ...installation.hooks.map((hook) => `${hook.name} (${hook.triggers.join(", ")})`),
    ...installation.resources.map((resource) => `${resource.key} (${resource.type})`),
  ];

  return (
    <SettingsCard>
      <div className="space-y-4 px-4 py-4">
        {hasHooks ? <PluginHookActivity wsId={wsId} installationId={installation.id} /> : null}
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-body font-semibold">{installation.name}</span>
              <Badge variant="secondary">{t(($) => $.plugins.version, { version: installation.version })}</Badge>
              {!installation.enabled ? (
                <Badge variant="outline">{t(($) => $.plugins.states.disabled)}</Badge>
              ) : null}
            </div>
            <p className="text-caption text-muted-foreground">{installation.plugin_key}</p>
            {installation.description ? (
              <p className="mt-2 max-w-2xl text-caption">{installation.description}</p>
            ) : null}
          </div>
          <div className="flex items-center gap-2">
            <Switch
              aria-label={t(($) => $.plugins.enabled_label)}
              disabled={!canManage || isMutating}
              checked={installation.enabled}
              onCheckedChange={(checked) => enabledMutation
                .mutateAsync({ installationId: installation.id, enabled: checked === true })
                .then(() => toast.success(checked
                  ? t(($) => $.plugins.enabled)
                  : t(($) => $.plugins.disabled)))
                .catch(reportError)}
            />
            <Button
              size="icon"
              variant="ghost"
              aria-label={t(($) => $.plugins.uninstall)}
              disabled={!canManage || isMutating}
              onClick={() => uninstallMutation.mutateAsync(installation.id)
                .then(() => toast.success(t(($) => $.plugins.uninstalled)))
                .catch(reportError)}
            >
              {uninstallMutation.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
            </Button>
          </div>
        </div>

        <div className="space-y-2">
          <div className="text-caption font-medium">{t(($) => $.plugins.granted_scopes)}</div>
          <ScopeList scopes={installation.granted_scopes} />
        </div>

        {mcpContributions.length > 0 ? (
          <div className="space-y-2">
            <div className="text-caption font-medium">{t(($) => $.plugins.mcp.badge)}</div>
            {mcpContributions.map((hook) => (
              <PluginMCPApproval
                key={hook.key}
                wsId={wsId}
                installationId={installation.id}
                hook={hook}
                canManage={canManage}
              />
            ))}
          </div>
        ) : null}

        {contributions.length > 0 ? (
          <div className="space-y-1">
            <div className="text-caption font-medium">{t(($) => $.plugins.contributes)}</div>
            <p className="text-caption text-muted-foreground">{contributions.join(" · ")}</p>
          </div>
        ) : null}

      </div>

      <ConfigForm installation={installation} canManage={canManage} wsId={wsId} />
    </SettingsCard>
  );
}

export function PluginsTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const { role } = useCurrentMember(wsId);
  const canManage = role === "owner" || role === "admin";

  const { data, isLoading, isError } = useQuery(pluginInstallationsOptions(wsId));
  const installations = useMemo(() => data?.plugins ?? [], [data]);

  return (
    <SettingsTab title={t(($) => $.plugins.title)} description={t(($) => $.plugins.description)}>
      {!canManage ? (
        <Alert>
          <AlertCircle />
          <AlertTitle>{t(($) => $.plugins.read_only)}</AlertTitle>
          <AlertDescription>{t(($) => $.plugins.read_only_description)}</AlertDescription>
        </Alert>
      ) : null}

      {canManage ? <PublishAndInstall wsId={wsId} canManage={canManage} /> : null}

      <SettingsSection title={t(($) => $.plugins.installed.title)}>
        {isLoading ? (
          <Skeleton className="h-24 w-full" aria-label={t(($) => $.plugins.loading)} />
        ) : isError ? (
          <Alert variant="destructive">
            <AlertCircle />
            <AlertTitle>{t(($) => $.plugins.load_failed)}</AlertTitle>
            <AlertDescription>{t(($) => $.plugins.load_failed_description)}</AlertDescription>
          </Alert>
        ) : installations.length === 0 ? (
          <SettingsCard>
            <p className="px-4 py-6 text-caption text-muted-foreground">{t(($) => $.plugins.empty)}</p>
          </SettingsCard>
        ) : (
          <div className="space-y-4">
            {installations.map((installation) => (
              <InstalledPlugin
                key={installation.id}
                installation={installation}
                wsId={wsId}
                canManage={canManage}
              />
            ))}
          </div>
        )}
      </SettingsSection>
    </SettingsTab>
  );
}
