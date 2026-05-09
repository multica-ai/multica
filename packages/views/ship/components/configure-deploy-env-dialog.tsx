"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  useConfigureDeployAdapter,
  useDeployAdapters,
  usePollDeployEnvironment,
  useRollbackDeployEnvironment,
  useUpsertDeployEnvironment,
} from "@multica/core/ship";
import type {
  CreateDeployEnvironmentRequest,
  DeployEnvironment,
  DeployEnvironmentKind,
} from "@multica/core/types";
import { useT } from "../../i18n";
import { adapterLabelKey } from "./adapter-icons";

interface ConfigureDeployEnvDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId: string;
  /** When provided, the dialog edits this environment instead of creating
   *  a new one. The backend upsert is keyed on (project, kind), so editing
   *  the kind is effectively a "move + create" — we lock the kind in edit
   *  mode to keep the UX honest. */
  existing?: DeployEnvironment;
}

export function ConfigureDeployEnvDialog({
  open,
  onOpenChange,
  projectId,
  existing,
}: ConfigureDeployEnvDialogProps) {
  const { t } = useT("ship");
  const upsert = useUpsertDeployEnvironment(projectId);
  const adaptersQuery = useDeployAdapters(open);
  const configureAdapter = useConfigureDeployAdapter(
    existing?.id ?? "",
    projectId,
  );
  const pollNow = usePollDeployEnvironment(existing?.id ?? "", projectId);
  const rollback = useRollbackDeployEnvironment(
    existing?.id ?? "",
    projectId,
  );

  const [kind, setKind] = useState<DeployEnvironmentKind>(
    existing?.kind ?? "staging",
  );
  const [name, setName] = useState(existing?.name ?? "");
  const [branch, setBranch] = useState(existing?.target_branch ?? "main");
  const [url, setUrl] = useState(existing?.target_url ?? "");
  const [autoPromote, setAutoPromote] = useState(existing?.auto_promote ?? false);
  const [error, setError] = useState<string | null>(null);

  // Phase 6 — adapter config state. Defaults to whatever the env was
  // configured with (or "github_actions" for envs migrated from
  // pre-Phase-6 schema). Editing here is independent of the basic
  // env-config save path: the user clicks "Save" twice — once for the
  // env basics, once for the adapter. This keeps the API contract clean
  // (two endpoints, two responsibilities) and means the dialog is
  // useful even for envs whose adapter is just "github_actions" with no
  // additional config.
  const [adapterKind, setAdapterKind] = useState<string>(
    existing?.adapter_kind ?? "github_actions",
  );
  const [adapterConfig, setAdapterConfig] = useState<string>("");
  const [webhookSecret, setWebhookSecret] = useState<string>("");
  const [adapterError, setAdapterError] = useState<string | null>(null);
  const [pollFeedback, setPollFeedback] = useState<string | null>(null);
  const [rollbackSha, setRollbackSha] = useState<string>("");

  // Reset form whenever the dialog opens — different `existing` values
  // share the same component instance.
  useEffect(() => {
    if (!open) return;
    setKind(existing?.kind ?? "staging");
    setName(existing?.name ?? "");
    setBranch(existing?.target_branch ?? "main");
    setUrl(existing?.target_url ?? "");
    setAutoPromote(existing?.auto_promote ?? false);
    setError(null);
    setAdapterKind(existing?.adapter_kind ?? "github_actions");
    setAdapterConfig("");
    setWebhookSecret("");
    setAdapterError(null);
    setPollFeedback(null);
    setRollbackSha("");
  }, [open, existing]);

  const handleSubmit = async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setError(t(($) => $.configure_dialog.name_required));
      return;
    }
    setError(null);
    const payload: CreateDeployEnvironmentRequest = {
      kind,
      name: trimmedName,
      target_branch: branch.trim() || null,
      target_url: url.trim() || null,
      auto_promote: autoPromote,
    };
    try {
      await upsert.mutateAsync(payload);
      onOpenChange(false);
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.configure_dialog.save_failed),
      );
    }
  };

  // The adapter list comes from the server, so we render whatever it
  // returns — adding a new adapter server-side surfaces here automatically.
  // Wrapped in useMemo so the .find below doesn't see a fresh array
  // every render (avoids the corresponding eslint-disable on its deps).
  const adapters = useMemo(
    () => adaptersQuery.data?.adapters ?? [],
    [adaptersQuery.data],
  );
  const selectedAdapter = useMemo(
    () => adapters.find((a) => a.kind === adapterKind),
    [adapters, adapterKind],
  );

  const handleSaveAdapter = async () => {
    if (!existing) return;
    setAdapterError(null);
    let parsedConfig: Record<string, unknown> = {};
    if (adapterConfig.trim()) {
      try {
        const parsed = JSON.parse(adapterConfig);
        if (parsed && typeof parsed === "object") {
          parsedConfig = parsed as Record<string, unknown>;
        }
      } catch {
        setAdapterError("Invalid JSON in adapter configuration.");
        return;
      }
    }
    try {
      await configureAdapter.mutateAsync({
        adapter_kind: adapterKind,
        config: parsedConfig,
        webhook_secret: webhookSecret || undefined,
      });
      toast.success(t(($) => $.configure_dialog.save));
      setWebhookSecret("");
      setAdapterConfig("");
    } catch (e) {
      const msg =
        e instanceof Error
          ? e.message
          : t(($) => $.configure_dialog.adapter_save_failed);
      setAdapterError(msg);
      toast.error(msg);
    }
  };

  const handleTestConnection = async () => {
    if (!existing) return;
    if (selectedAdapter && selectedAdapter.supports_poll === false) {
      setPollFeedback(t(($) => $.configure_dialog.adapter_test_no_poll));
      return;
    }
    setPollFeedback(t(($) => $.configure_dialog.adapter_test_polling));
    try {
      const res = await pollNow.mutateAsync();
      if (!res.changed) {
        setPollFeedback(t(($) => $.configure_dialog.adapter_test_unchanged));
        return;
      }
      const sha = (res.current_sha ?? "").slice(0, 7);
      setPollFeedback(
        t(($) => $.configure_dialog.adapter_test_changed, { sha }),
      );
    } catch (e) {
      const msg =
        e instanceof Error
          ? e.message
          : t(($) => $.configure_dialog.adapter_test_failed);
      setPollFeedback(msg);
    }
  };

  const handleRollback = async () => {
    if (!existing) return;
    if (selectedAdapter && selectedAdapter.supports_rollback === false) {
      setAdapterError(
        t(($) => $.configure_dialog.rollback_unsupported),
      );
      return;
    }
    if (!rollbackSha.trim()) return;
    try {
      await rollback.mutateAsync({ target_sha: rollbackSha.trim() });
      toast.success(t(($) => $.configure_dialog.rollback_action));
      setRollbackSha("");
    } catch (e) {
      const msg =
        e instanceof Error
          ? e.message
          : t(($) => $.configure_dialog.rollback_failed);
      toast.error(msg);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {existing
              ? t(($) => $.configure_dialog.title_edit)
              : t(($) => $.configure_dialog.title_create)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.configure_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.configure_dialog.kind_label)}
            </Label>
            <Select
              value={kind}
              onValueChange={(v) => {
                if (v) setKind(v as DeployEnvironmentKind);
              }}
              disabled={!!existing}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="staging">
                  {t(($) => $.configure_dialog.kind_staging)}
                </SelectItem>
                <SelectItem value="production">
                  {t(($) => $.configure_dialog.kind_production)}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-env-name">
              {t(($) => $.configure_dialog.name_label)}
            </Label>
            <Input
              id="ship-env-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.configure_dialog.name_placeholder)}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-env-branch">
              {t(($) => $.configure_dialog.branch_label)}
            </Label>
            <Input
              id="ship-env-branch"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder={t(($) => $.configure_dialog.branch_placeholder)}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-env-url">
              {t(($) => $.configure_dialog.url_label)}
            </Label>
            <Input
              id="ship-env-url"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t(($) => $.configure_dialog.url_placeholder)}
            />
          </div>

          <div className="flex items-start justify-between gap-3 rounded-md border bg-muted/20 p-3">
            <div>
              <p className="text-sm font-medium">
                {t(($) => $.configure_dialog.auto_promote_label)}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.configure_dialog.auto_promote_hint)}
              </p>
            </div>
            <Switch
              checked={autoPromote}
              onCheckedChange={setAutoPromote}
              aria-label={t(($) => $.configure_dialog.auto_promote_label)}
            />
          </div>

          {error && (
            <p className="text-xs text-destructive" role="alert">
              {error}
            </p>
          )}

          {/* Phase 6 — Adapter section. Only meaningful for an existing
              env (we need an env id to PUT against), so we hide it
              entirely in create mode. The user can come back after
              creating to configure the adapter. */}
          {existing && (
            <div className="space-y-3 rounded-md border bg-muted/10 p-3">
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">
                  {t(($) => $.configure_dialog.adapter_label)}
                </Label>
                <Select
                  value={adapterKind}
                  onValueChange={(v) => {
                    if (v) setAdapterKind(v);
                    setPollFeedback(null);
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {adapters.map((a) => (
                      <SelectItem key={a.kind} value={a.kind}>
                        {/* Map known adapter slugs to a friendly label;
                            fall through to the raw kind string for
                            forward-compat with new adapters. */}
                        {(() => {
                          const key = adapterLabelKey(a.kind);
                          if (key) return t(($) => $.configure_dialog[key]);
                          return a.kind;
                        })()}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground">
                  {t(($) => $.configure_dialog.adapter_hint)}
                </p>
              </div>

              {selectedAdapter?.webhook_url && (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">
                    {t(($) => $.configure_dialog.adapter_webhook_url)}
                  </Label>
                  <Input
                    readOnly
                    value={selectedAdapter.webhook_url}
                    className="font-mono text-[11px]"
                    onFocus={(e) => e.currentTarget.select()}
                  />
                  <p className="text-[11px] text-muted-foreground">
                    {t(($) => $.configure_dialog.adapter_webhook_url_hint)}
                  </p>
                </div>
              )}

              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground" htmlFor="ship-adapter-config">
                  {t(($) => $.configure_dialog.adapter_config_label)}
                </Label>
                <textarea
                  id="ship-adapter-config"
                  className="min-h-20 w-full rounded-md border bg-background p-2 font-mono text-[11px]"
                  value={adapterConfig}
                  onChange={(e) => setAdapterConfig(e.target.value)}
                  placeholder='{"project_id":"prj_…","token":"…"}'
                />
                <p className="text-[11px] text-muted-foreground">
                  {t(($) => $.configure_dialog.adapter_config_hint)}
                </p>
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground" htmlFor="ship-adapter-secret">
                  {t(($) => $.configure_dialog.adapter_webhook_secret)}
                </Label>
                <Input
                  id="ship-adapter-secret"
                  type="password"
                  value={webhookSecret}
                  onChange={(e) => setWebhookSecret(e.target.value)}
                  placeholder={t(
                    ($) => $.configure_dialog.adapter_webhook_secret_placeholder,
                  )}
                />
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleSaveAdapter}
                  disabled={configureAdapter.isPending}
                >
                  {t(($) => $.configure_dialog.save)}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={handleTestConnection}
                  disabled={pollNow.isPending}
                >
                  {t(($) => $.configure_dialog.adapter_test_connection)}
                </Button>
              </div>
              {pollFeedback && (
                <p className="text-[11px] text-muted-foreground" role="status">
                  {pollFeedback}
                </p>
              )}
              {adapterError && (
                <p className="text-[11px] text-destructive" role="alert">
                  {adapterError}
                </p>
              )}

              {selectedAdapter?.supports_rollback && (
                <div className="space-y-1.5 rounded-md border border-dashed p-2">
                  <Label className="text-xs text-muted-foreground" htmlFor="ship-rollback-sha">
                    {t(($) => $.configure_dialog.rollback_label)}
                  </Label>
                  <div className="flex gap-2">
                    <Input
                      id="ship-rollback-sha"
                      value={rollbackSha}
                      onChange={(e) => setRollbackSha(e.target.value)}
                      placeholder={t(($) => $.configure_dialog.rollback_placeholder)}
                      className="font-mono text-[11px]"
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleRollback}
                      disabled={rollback.isPending || !rollbackSha.trim()}
                    >
                      {t(($) => $.configure_dialog.rollback_action)}
                    </Button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.configure_dialog.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={upsert.isPending}>
            {upsert.isPending
              ? t(($) => $.configure_dialog.saving)
              : t(($) => $.configure_dialog.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
