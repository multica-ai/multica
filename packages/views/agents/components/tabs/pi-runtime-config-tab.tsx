"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Eye, EyeOff, Loader2, Lock, Save } from "lucide-react";
import { api } from "@multica/core/api";
import {
  PI_API_KEY_ENV,
  PI_API_TYPES,
  type PiApiType,
  type PiRuntimeConfig,
  parsePiRuntimeConfig,
  piRuntimeConfigEquals,
  serializePiRuntimeConfig,
  validatePiRuntimeConfig,
} from "@multica/core/agents";
import {
  isPiRuntimeModelConfigured,
  runtimeDefaultPiConfig,
} from "@multica/core/runtimes";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useT } from "../../../i18n";

type FormState = {
  provider: string;
  api: PiApiType;
  baseUrl: string;
  model: string;
};

function configToForm(config: PiRuntimeConfig): FormState {
  return {
    provider: config.provider ?? "",
    api: config.api ?? "openai-responses",
    baseUrl: config.baseUrl ?? "",
    model: config.model ?? "",
  };
}

function formToConfig(state: FormState): PiRuntimeConfig {
  const hasFields = Boolean(
    state.provider.trim() || state.baseUrl.trim() || state.model.trim(),
  );
  if (!hasFields) return {};
  return {
    provider: state.provider.trim(),
    api: state.api,
    baseUrl: state.baseUrl.trim(),
    model: state.model.trim(),
  };
}

function sameEndpoint(a: PiRuntimeConfig, b: PiRuntimeConfig): boolean {
  return (
    a.provider?.trim() === b.provider?.trim() &&
    a.api === b.api &&
    a.baseUrl?.trim().replace(/\/+$/, "") ===
      b.baseUrl?.trim().replace(/\/+$/, "")
  );
}

export function PiRuntimeConfigTab({
  agent,
  runtime,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  runtime?: AgentRuntime | null;
  onSave: (updates: { runtime_config: Record<string, unknown> }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const original = useMemo(
    () => parsePiRuntimeConfig(agent.runtime_config),
    [agent.runtime_config],
  );
  const inheritedConfig = useMemo(
    () => (runtime ? runtimeDefaultPiConfig(runtime) : {}),
    [runtime],
  );
  const runtimeConnectionConfigured = Boolean(
    runtime && isPiRuntimeModelConfigured(runtime),
  );
  const hasAgentOverride = Boolean(
    original.provider &&
      original.api &&
      original.baseUrl &&
      original.model &&
      validatePiRuntimeConfig(original) === null,
  );
  const inheritsRuntimeDefault = Boolean(
    runtimeConnectionConfigured &&
      validatePiRuntimeConfig(original) === null &&
      !original.provider &&
      !original.baseUrl &&
      !original.model,
  );
  const effectiveOriginal = inheritsRuntimeDefault ? inheritedConfig : original;
  const originalForm = useMemo(
    () => configToForm(effectiveOriginal),
    [effectiveOriginal],
  );
  const previousFormRef = useRef(originalForm);
  const [state, setState] = useState(originalForm);
  const [env, setEnv] = useState<Record<string, string> | null>(null);
  const [originalKey, setOriginalKey] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [keyVisible, setKeyVisible] = useState(false);
  const [revealing, setRevealing] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setState((current) =>
      formEquals(current, previousFormRef.current) ? originalForm : current,
    );
    previousFormRef.current = originalForm;
  }, [originalForm]);

  // Agent navigation can reuse this component instance. Never carry revealed
  // secret state across that boundary; the next agent requires a fresh,
  // explicitly audited reveal.
  useEffect(() => {
    setEnv(null);
    setOriginalKey("");
    setApiKey("");
    setKeyVisible(false);
  }, [agent.id]);

  const current = useMemo(() => formToConfig(state), [state]);
  const configDirty = !piRuntimeConfigEquals(effectiveOriginal, current);
  const keyDirty = env !== null && apiKey !== originalKey;
  const dirty = configDirty || keyDirty;
  const validationError = validatePiRuntimeConfig(current);
  const runtimeKeyApplies =
    runtimeConnectionConfigured && sameEndpoint(current, inheritedConfig);
  const needsAgentKeyToSave = Boolean(
    configDirty &&
      current.provider &&
      !runtimeKeyApplies &&
      (env === null || !apiKey.trim()),
  );

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleReveal = async () => {
    setRevealing(true);
    try {
      const response = await api.getAgentEnv(agent.id);
      const values = response.custom_env ?? {};
      const value = values[PI_API_KEY_ENV] ?? "";
      setEnv(values);
      setOriginalKey(value);
      setApiKey(value);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.pi_runtime_config.reveal_failed_toast),
      );
    } finally {
      setRevealing(false);
    }
  };

  const handleSave = async () => {
    if (validationError || needsAgentKeyToSave || saving || !dirty) return;
    setSaving(true);
    try {
      if (keyDirty && env !== null) {
        const nextEnv = { ...env };
        if (apiKey) nextEnv[PI_API_KEY_ENV] = apiKey;
        else delete nextEnv[PI_API_KEY_ENV];
        const response = await api.updateAgentEnv(agent.id, {
          custom_env: nextEnv,
        });
        const savedEnv = response.custom_env ?? nextEnv;
        const savedKey = savedEnv[PI_API_KEY_ENV] ?? "";
        setEnv(savedEnv);
        setOriginalKey(savedKey);
        setApiKey(savedKey);
      }
      if (configDirty) {
        await onSave({ runtime_config: serializePiRuntimeConfig(current) });
      }
      toast.success(t(($) => $.tab_body.pi_runtime_config.saved_toast));
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.tab_body.pi_runtime_config.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full flex-col space-y-4">
      <p className="text-caption text-muted-foreground">
        {t(($) => $.tab_body.pi_runtime_config.intro)}
      </p>

      {inheritsRuntimeDefault && (
        <div className="rounded-md border border-success/30 bg-success/5 p-3">
          <p className="text-caption font-medium text-success">
            {t(($) => $.tab_body.pi_runtime_config.inherited_title)}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">
            {t(($) => $.tab_body.pi_runtime_config.inherited_hint, {
              provider: inheritedConfig.provider,
              model: inheritedConfig.model,
            })}
          </p>
        </div>
      )}

      {runtimeConnectionConfigured && hasAgentOverride && (
        <div className="flex items-start justify-between gap-3 rounded-md border p-3">
          <div className="min-w-0">
            <p className="text-caption font-medium">
              {t(($) => $.tab_body.pi_runtime_config.override_title)}
            </p>
            <p className="mt-1 truncate text-caption text-muted-foreground">
              {t(($) => $.tab_body.pi_runtime_config.override_hint, {
                provider: original.provider,
                model: original.model,
              })}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="shrink-0"
            onClick={() => setState(configToForm({}))}
          >
            {t(($) => $.tab_body.pi_runtime_config.use_runtime_default)}
          </Button>
        </div>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="pi-provider" className="text-caption">
            {t(($) => $.tab_body.pi_runtime_config.provider_label)}
          </Label>
          <Input
            id="pi-provider"
            value={state.provider}
            onChange={(event) =>
              setState((currentState) => ({
                ...currentState,
                provider: event.target.value,
              }))
            }
            placeholder="openai"
            className="font-mono text-caption"
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="pi-api" className="text-caption">
            {t(($) => $.tab_body.pi_runtime_config.api_label)}
          </Label>
          <select
            id="pi-api"
            value={state.api}
            onChange={(event) =>
              setState((currentState) => ({
                ...currentState,
                api: event.target.value as PiApiType,
              }))
            }
            className="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-caption"
          >
            {PI_API_TYPES.map((apiType) => (
              <option key={apiType} value={apiType}>
                {apiType}
              </option>
            ))}
          </select>
        </div>
        <div className="space-y-1.5 sm:col-span-2">
          <Label htmlFor="pi-base-url" className="text-caption">
            {t(($) => $.tab_body.pi_runtime_config.base_url_label)}
          </Label>
          <Input
            id="pi-base-url"
            value={state.baseUrl}
            onChange={(event) =>
              setState((currentState) => ({
                ...currentState,
                baseUrl: event.target.value,
              }))
            }
            placeholder="https://api.example.com/v1"
            aria-invalid={validationError === "base_url" || undefined}
            className="font-mono text-caption"
          />
        </div>
        <div className="space-y-1.5 sm:col-span-2">
          <Label htmlFor="pi-model" className="text-caption">
            {t(($) => $.tab_body.pi_runtime_config.model_label)}
          </Label>
          <Input
            id="pi-model"
            value={state.model}
            onChange={(event) =>
              setState((currentState) => ({
                ...currentState,
                model: event.target.value,
              }))
            }
            placeholder="gpt-5"
            className="font-mono text-caption"
          />
        </div>
      </div>

      {validationError && (
        <p className="text-caption text-destructive">
          {validationError === "incomplete"
            ? t(($) => $.tab_body.pi_runtime_config.incomplete_error)
            : validationError === "provider"
              ? t(($) => $.tab_body.pi_runtime_config.provider_error)
              : t(($) => $.tab_body.pi_runtime_config.base_url_error)}
        </p>
      )}

      <div className="space-y-2 rounded-md border p-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <Label
              htmlFor="pi-api-key"
              className="flex items-center gap-2 text-caption font-medium"
            >
              <Lock className="h-3.5 w-3.5 text-muted-foreground" />
              {t(($) => $.tab_body.pi_runtime_config.api_key_label)}
            </Label>
            <p className="text-caption text-muted-foreground">
              {runtimeKeyApplies
                ? t(($) => $.tab_body.pi_runtime_config.inherited_key_hint)
                : t(($) => $.tab_body.pi_runtime_config.api_key_hint)}
            </p>
          </div>
          {env === null && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={revealing}
              onClick={handleReveal}
            >
              {revealing ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Eye className="h-3.5 w-3.5" />
              )}
              {t(($) => $.tab_body.pi_runtime_config.reveal_action)}
            </Button>
          )}
        </div>
        {env !== null && (
          <div className="flex gap-2">
            <Input
              id="pi-api-key"
              type={keyVisible ? "text" : "password"}
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              autoComplete="off"
              className="font-mono text-caption"
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => setKeyVisible((visible) => !visible)}
              aria-label={t(($) => $.tab_body.pi_runtime_config.toggle_key)}
            >
              {keyVisible ? (
                <EyeOff className="h-3.5 w-3.5" />
              ) : (
                <Eye className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
        )}
      </div>

      {needsAgentKeyToSave && (
        <p className="text-caption text-destructive">
          {t(($) => $.tab_body.pi_runtime_config.agent_key_required)}
        </p>
      )}

      <div className="flex items-center justify-end gap-3 pt-2">
        {dirty && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button
          onClick={handleSave}
          disabled={
            !dirty ||
            Boolean(validationError) ||
            needsAgentKeyToSave ||
            saving
          }
          size="sm"
        >
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}

function formEquals(a: FormState, b: FormState): boolean {
  return (
    a.provider === b.provider &&
    a.api === b.api &&
    a.baseUrl === b.baseUrl &&
    a.model === b.model
  );
}
