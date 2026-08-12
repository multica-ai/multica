"use client";

import { useMemo, useState } from "react";
import { ArrowUpRight, CheckCircle2, Loader2, LockKeyhole } from "lucide-react";
import {
  useUpdateRuntimeModelConnection,
  useValidateModelConnection,
} from "@multica/core/runtimes/mutations";
import type {
  AgentRuntime,
  ModelConnectionProbeOutcome,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import {
  PI_PRIMARY_PRESET_COUNT,
  defaultPresetForLocale,
  orderPresetsForLocale,
  type PiProviderPreset,
} from "../model-presets";

/**
 * The one thing setup asks the user for: pick a provider, paste a key.
 *
 * Everything else (API shape, base URL, model) is implied by the provider, so
 * none of it is a question here. The raw fields still exist for people who
 * need them — behind the runtime detail page's advanced connection dialog.
 */
export function BuiltInRuntimeConnectForm({
  runtime,
  wsId,
  onConnected,
  onSkip,
}: {
  runtime: AgentRuntime;
  wsId: string;
  onConnected: () => void;
  /** Lets a user without any API key leave without a dead end. */
  onSkip?: () => void;
}) {
  const { t, i18n } = useT("runtimes");
  const presets = useMemo(
    () => orderPresetsForLocale(i18n.language),
    [i18n.language],
  );
  const [preset, setPreset] = useState<PiProviderPreset>(() =>
    defaultPresetForLocale(i18n.language),
  );
  const [showAllPresets, setShowAllPresets] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [failure, setFailure] = useState<{
    outcome: ModelConnectionProbeOutcome;
    detail?: string;
  } | null>(null);

  const validate = useValidateModelConnection();
  const save = useUpdateRuntimeModelConnection(wsId);
  const busy = validate.isPending || save.isPending;
  const trimmedKey = apiKey.trim();

  const visiblePresets = showAllPresets
    ? presets
    : presets.slice(0, PI_PRIMARY_PRESET_COUNT);

  const connect = async () => {
    if (!trimmedKey || busy) return;
    setFailure(null);
    const connection = {
      provider: preset.provider,
      api: preset.api,
      base_url: preset.baseUrl,
      model: preset.defaultModel,
    };

    // Verify before writing. A key that is wrong, expired, or out of credit
    // has to fail here — where the user is still holding it — rather than
    // inside the first task they run.
    const verdict = await validate.mutateAsync({
      ...connection,
      api_key: trimmedKey,
    });
    if (!verdict.valid) {
      setFailure({
        outcome: verdict.outcome ?? "unknown",
        detail: verdict.detail,
      });
      return;
    }

    await save.mutateAsync({
      runtimeId: runtime.id,
      connection: { ...connection, api_key: trimmedKey },
    });
    onConnected();
  };

  return (
    <div className="flex flex-col gap-5">
      <div>
        <Label className="text-caption">
          {t(($) => $.built_in.connect.provider_label)}
        </Label>
        <div className="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-4">
          {visiblePresets.map((candidate) => {
            const selected = candidate.id === preset.id;
            return (
              <button
                key={candidate.id}
                type="button"
                aria-pressed={selected}
                disabled={busy}
                onClick={() => {
                  setPreset(candidate);
                  setFailure(null);
                }}
                className={cn(
                  "flex min-h-16 flex-col items-start justify-center gap-0.5 rounded-lg border px-3 py-2 text-left transition-colors",
                  // Selected stays distinguishable while hovered: hover only
                  // moves the background, weight and text colour carry state.
                  selected
                    ? "border-primary bg-primary/5 font-medium text-foreground"
                    : "border-input text-muted-foreground hover:bg-muted",
                )}
              >
                <span className="block w-full truncate text-label">
                  {candidate.label}
                </span>
                <span className="block w-full truncate font-mono text-micro text-muted-foreground">
                  {candidate.defaultModel}
                </span>
              </button>
            );
          })}
        </div>
        {!showAllPresets && presets.length > PI_PRIMARY_PRESET_COUNT && (
          <button
            type="button"
            onClick={() => setShowAllPresets(true)}
            className="mt-2 text-caption text-muted-foreground transition-colors hover:text-foreground"
          >
            {t(($) => $.built_in.connect.more_providers)}
          </button>
        )}
      </div>

      <div className="space-y-1.5">
        <div className="flex items-center justify-between gap-3">
          <Label htmlFor="built-in-api-key" className="flex items-center gap-2">
            <LockKeyhole className="h-3.5 w-3.5 text-muted-foreground" />
            {t(($) => $.built_in.connect.api_key_label)}
          </Label>
          {/* The most common blocker is not the form, it is not having a key. */}
          <a
            href={preset.consoleUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-caption text-primary hover:underline"
          >
            {t(($) => $.built_in.connect.get_key, { provider: preset.label })}
            <ArrowUpRight className="h-3 w-3" />
          </a>
        </div>
        <Input
          id="built-in-api-key"
          type="password"
          autoComplete="new-password"
          value={apiKey}
          disabled={busy}
          onChange={(event) => {
            setApiKey(event.target.value);
            setFailure(null);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") void connect();
          }}
          placeholder={t(($) => $.built_in.connect.api_key_placeholder)}
        />
        <p className="text-caption text-muted-foreground">
          {t(($) => $.built_in.connect.api_key_hint)}
        </p>
      </div>

      {failure && (
        <p role="alert" className="text-caption text-destructive">
          {connectFailureMessage(t, failure.outcome)}
          {failure.detail ? ` · ${failure.detail}` : ""}
        </p>
      )}

      <div className="flex flex-col gap-2">
        <Button disabled={!trimmedKey || busy} onClick={() => void connect()}>
          {busy ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <CheckCircle2 className="h-4 w-4" />
          )}
          {validate.isPending
            ? t(($) => $.built_in.connect.verifying)
            : save.isPending
              ? t(($) => $.built_in.connect.saving)
              : t(($) => $.built_in.connect.submit)}
        </Button>
        {onSkip && (
          <Button variant="ghost" disabled={busy} onClick={onSkip}>
            {t(($) => $.built_in.connect.no_key)}
          </Button>
        )}
      </div>
    </div>
  );
}

type Translate = ReturnType<typeof useT<"runtimes">>["t"];

function connectFailureMessage(
  t: Translate,
  outcome: ModelConnectionProbeOutcome,
): string {
  switch (outcome) {
    case "invalid_key":
      return t(($) => $.built_in.connect.error_invalid_key);
    case "insufficient_quota":
      return t(($) => $.built_in.connect.error_quota);
    case "rate_limited":
      return t(($) => $.built_in.connect.error_rate_limited);
    case "model_not_found":
      return t(($) => $.built_in.connect.error_model);
    case "endpoint_not_found":
      return t(($) => $.built_in.connect.error_endpoint);
    case "network_unreachable":
      return t(($) => $.built_in.connect.error_network);
    default:
      // Covers provider_error and any outcome a newer backend introduces.
      return t(($) => $.built_in.connect.error_generic);
  }
}
