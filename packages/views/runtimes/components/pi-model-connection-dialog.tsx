"use client";

import { useEffect, useMemo, useState } from "react";
import { Loader2, LockKeyhole, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  PI_API_TYPES,
  type PiApiType,
  type PiRuntimeConfig,
  parsePiRuntimeConfig,
  validatePiRuntimeConfig,
} from "@multica/core/agents";
import {
  useDeleteRuntimeModelConnection,
  useUpdateRuntimeModelConnection,
} from "@multica/core/runtimes/mutations";
import type { AgentRuntime } from "@multica/core/types";
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
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import {
  PI_DEFAULT_PROVIDER_PRESET,
  PI_PROVIDER_PRESETS,
  findPiProviderPreset,
  getPiProviderPreset,
  type PiProviderPreset,
  type PiProviderPresetId,
} from "../model-presets";

const CUSTOM_PROVIDER_PRESET = "custom";

type ProviderPreset = PiProviderPresetId | typeof CUSTOM_PROVIDER_PRESET;

type Draft = {
  preset: ProviderPreset;
  provider: string;
  api: PiApiType;
  baseUrl: string;
  model: string;
  apiKey: string;
};

function presetToDraft(
  preset: PiProviderPreset,
  model: string | undefined,
): Draft {
  return {
    preset: preset.id as PiProviderPresetId,
    provider: preset.provider,
    api: preset.api,
    baseUrl: preset.baseUrl,
    model: model && preset.models.includes(model) ? model : preset.defaultModel,
    apiKey: "",
  };
}

function configToDraft(runtime: AgentRuntime): Draft {
  const config = parsePiRuntimeConfig(runtime.default_model_config);
  if (Object.keys(config).length === 0) {
    return presetToDraft(PI_DEFAULT_PROVIDER_PRESET, undefined);
  }

  const preset = findPiProviderPreset(config);
  if (preset && config.model && preset.models.includes(config.model)) {
    return presetToDraft(preset, config.model);
  }

  return {
    preset: CUSTOM_PROVIDER_PRESET,
    provider: config.provider ?? "",
    api: config.api ?? "openai-responses",
    baseUrl: config.baseUrl ?? "",
    model: config.model ?? "",
    apiKey: "",
  };
}

function draftToConfig(draft: Draft): PiRuntimeConfig {
  return {
    provider: draft.provider.trim(),
    api: draft.api,
    baseUrl: draft.baseUrl.trim(),
    model: draft.model.trim(),
  };
}

export function PiModelConnectionDialog({
  runtime,
  wsId,
  open,
  onOpenChange,
}: {
  runtime: AgentRuntime;
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("runtimes");
  const updateConnection = useUpdateRuntimeModelConnection(wsId);
  const deleteConnection = useDeleteRuntimeModelConnection(wsId);
  const [draft, setDraft] = useState(() => configToDraft(runtime));
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);

  useEffect(() => {
    if (open) setDraft(configToDraft(runtime));
  }, [open, runtime]);

  const config = useMemo(() => draftToConfig(draft), [draft]);
  const storedConfig = useMemo(
    () => parsePiRuntimeConfig(runtime.default_model_config),
    [runtime.default_model_config],
  );
  const hasSavedConnection =
    runtime.has_default_model_api_key === true ||
    Boolean(
      storedConfig.provider?.trim() ||
        storedConfig.api?.trim() ||
        storedConfig.baseUrl?.trim() ||
        storedConfig.model?.trim(),
    );
  const validationError = validatePiRuntimeConfig(config);
  const needsKey = runtime.has_default_model_api_key !== true;
  const busy = updateConnection.isPending || deleteConnection.isPending;
  const canSave =
    !validationError &&
    (!needsKey || draft.apiKey.trim().length > 0) &&
    !busy;
  const activePreset =
    draft.preset === CUSTOM_PROVIDER_PRESET
      ? undefined
      : getPiProviderPreset(draft.preset);

  const selectPreset = (preset: ProviderPreset) => {
    if (preset === CUSTOM_PROVIDER_PRESET) {
      setDraft((current) => ({ ...current, preset }));
      return;
    }
    const nextPreset = getPiProviderPreset(preset);
    if (nextPreset) {
      setDraft((current) => ({
        ...current,
        preset: nextPreset.id as PiProviderPresetId,
        provider: nextPreset.provider,
        api: nextPreset.api,
        baseUrl: nextPreset.baseUrl,
        model: nextPreset.models.includes(current.model)
          ? current.model
          : nextPreset.defaultModel,
      }));
    }
  };

  const save = () => {
    if (!canSave) return;
    const apiKey = draft.apiKey.trim();
    updateConnection.mutate(
      {
        runtimeId: runtime.id,
        connection: {
          provider: config.provider!,
          api: config.api!,
          base_url: config.baseUrl!,
          model: config.model!,
          ...(apiKey ? { api_key: apiKey } : {}),
        },
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.detail.model_connection.toast_saved));
          onOpenChange(false);
        },
        onError: (error) => {
          toast.error(
            error instanceof Error && error.message
              ? error.message
              : t(($) => $.detail.model_connection.toast_failed),
          );
        },
      },
    );
  };

  const remove = () => {
    if (!hasSavedConnection || deleteConnection.isPending) return;
    deleteConnection.mutate(runtime.id, {
      onSuccess: () => {
        toast.success(t(($) => $.detail.model_connection.toast_deleted));
        setConfirmDeleteOpen(false);
        onOpenChange(false);
      },
      onError: (error) => {
        toast.error(
          error instanceof Error && error.message
            ? error.message
            : t(($) => $.detail.model_connection.toast_delete_failed),
        );
      },
    });
  };

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!busy) onOpenChange(next);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.detail.model_connection.dialog_title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.detail.model_connection.dialog_description)}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="pi-connection-preset">
                {t(($) => $.detail.model_connection.provider_label)}
              </Label>
              <select
                id="pi-connection-preset"
                value={draft.preset}
                onChange={(event) =>
                  selectPreset(event.target.value as ProviderPreset)
                }
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-body"
              >
                {PI_PROVIDER_PRESETS.map((preset) => (
                  <option key={preset.id} value={preset.id}>
                    {preset.label}
                  </option>
                ))}
                <option value={CUSTOM_PROVIDER_PRESET}>
                  {t(($) => $.detail.model_connection.custom_provider)}
                </option>
              </select>
            </div>

            {activePreset ? (
              <div className="space-y-1.5">
                <Label id="pi-connection-model-label">
                  {t(($) => $.detail.model_connection.model_label)}
                </Label>
                <div
                  role="listbox"
                  aria-labelledby="pi-connection-model-label"
                  className="grid gap-2"
                >
                  {activePreset.models.map((model) => {
                    const selected = draft.model === model;
                    return (
                      <button
                        key={model}
                        type="button"
                        role="option"
                        aria-selected={selected}
                        onClick={() =>
                          setDraft((current) => ({
                            ...current,
                            model,
                          }))
                        }
                        className={`flex min-h-11 w-full items-center justify-between gap-3 rounded-md border px-3 py-2 text-left transition-colors ${
                          selected
                            ? "border-primary bg-primary/5 text-foreground"
                            : "border-input bg-background text-foreground hover:bg-muted"
                        }`}
                      >
                        <span className="min-w-0 truncate font-mono text-caption font-medium">
                          {model}
                        </span>
                        {selected && (
                          <Badge
                            variant="outline"
                            className="shrink-0 border-primary/30 bg-background text-primary"
                          >
                            {t(
                              ($) => $.detail.model_connection.default_badge,
                            )}
                          </Badge>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2">
                <ConnectionField
                  id="pi-connection-provider"
                  label={t(($) => $.detail.model_connection.provider_id_label)}
                  value={draft.provider}
                  placeholder="openai"
                  onChange={(provider) =>
                    setDraft((current) => ({ ...current, provider }))
                  }
                />
                <div className="space-y-1.5">
                  <Label htmlFor="pi-connection-api">
                    {t(($) => $.detail.model_connection.api_label)}
                  </Label>
                  <select
                    id="pi-connection-api"
                    value={draft.api}
                    onChange={(event) =>
                      setDraft((current) => ({
                        ...current,
                        api: event.target.value as PiApiType,
                      }))
                    }
                    className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-caption"
                  >
                    {PI_API_TYPES.map((api) => (
                      <option key={api} value={api}>
                        {api}
                      </option>
                    ))}
                  </select>
                </div>
                <ConnectionField
                  id="pi-connection-base-url"
                  label={t(($) => $.detail.model_connection.base_url_label)}
                  value={draft.baseUrl}
                  placeholder="https://api.example.com/v1"
                  invalid={validationError === "base_url"}
                  onChange={(baseUrl) =>
                    setDraft((current) => ({ ...current, baseUrl }))
                  }
                  className="sm:col-span-2"
                />
                <ConnectionField
                  id="pi-connection-custom-model"
                  label={t(($) => $.detail.model_connection.model_label)}
                  value={draft.model}
                  placeholder="model-id"
                  onChange={(model) =>
                    setDraft((current) => ({ ...current, model }))
                  }
                  className="sm:col-span-2"
                />
              </div>
            )}

            <div className="space-y-1.5 rounded-md border p-3">
              <Label
                htmlFor="pi-connection-api-key"
                className="flex items-center gap-2"
              >
                <LockKeyhole className="h-3.5 w-3.5 text-muted-foreground" />
                {t(($) => $.detail.model_connection.api_key_label)}
              </Label>
              <Input
                id="pi-connection-api-key"
                type="password"
                autoComplete="new-password"
                value={draft.apiKey}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    apiKey: event.target.value,
                  }))
                }
                placeholder={
                  needsKey
                    ? t(($) => $.detail.model_connection.api_key_placeholder)
                    : t(($) => $.detail.model_connection.api_key_saved)
                }
              />
              <p className="text-caption text-muted-foreground">
                {needsKey
                  ? t(($) => $.detail.model_connection.api_key_hint)
                  : t(($) => $.detail.model_connection.api_key_preserve_hint)}
              </p>
            </div>

            {validationError && (
              <p className="text-caption text-destructive">
                {validationError === "provider"
                  ? t(($) => $.detail.model_connection.provider_error)
                  : validationError === "base_url"
                    ? t(($) => $.detail.model_connection.base_url_error)
                    : t(($) => $.detail.model_connection.incomplete_error)}
              </p>
            )}
          </div>

          <DialogFooter className="sm:justify-between">
            {hasSavedConnection && (
              <Button
                type="button"
                variant="destructive"
                onClick={() => setConfirmDeleteOpen(true)}
                disabled={busy}
                className="sm:mr-auto"
              >
                <Trash2 className="h-4 w-4" />
                {t(($) => $.detail.model_connection.remove)}
              </Button>
            )}
            <Button
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={busy}
            >
              {t(($) => $.detail.model_connection.cancel)}
            </Button>
            <Button onClick={save} disabled={!canSave}>
              {updateConnection.isPending && (
                <Loader2 className="h-4 w-4 animate-spin" />
              )}
              {updateConnection.isPending
                ? t(($) => $.detail.model_connection.saving)
                : t(($) => $.detail.model_connection.save)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={confirmDeleteOpen}
        onOpenChange={(next) => {
          if (!deleteConnection.isPending) setConfirmDeleteOpen(next);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.detail.model_connection.delete_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.model_connection.delete_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteConnection.isPending}>
              {t(($) => $.detail.model_connection.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={remove}
              disabled={deleteConnection.isPending}
            >
              {deleteConnection.isPending && (
                <Loader2 className="h-4 w-4 animate-spin" />
              )}
              {deleteConnection.isPending
                ? t(($) => $.detail.model_connection.deleting)
                : t(($) => $.detail.model_connection.delete_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function ConnectionField({
  id,
  label,
  value,
  placeholder,
  invalid,
  onChange,
  className = "",
}: {
  id: string;
  label: string;
  value: string;
  placeholder: string;
  invalid?: boolean;
  onChange: (value: string) => void;
  className?: string;
}) {
  return (
    <div className={`space-y-1.5 ${className}`}>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        aria-invalid={invalid || undefined}
        className="font-mono text-caption"
      />
    </div>
  );
}
