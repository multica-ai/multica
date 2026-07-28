"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Brain,
  Check,
  Cpu,
  SlidersHorizontal,
  Zap,
} from "lucide-react";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import type {
  RuntimeModel,
  RuntimeModelThinkingLevel,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  getCloudPreviewModels,
  isCloudPreviewRuntimeId,
} from "../../runtimes/components/cloud-preview";
import { useT } from "../../i18n";
import { ModelDropdown } from "./model-dropdown";

export function ModelConfiguration({
  runtimeId,
  runtimeOnline,
  model,
  thinkingLevel,
  serviceTier,
  onModelChange,
  onThinkingLevelChange,
  onServiceTierChange,
  disabled = false,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  model: string;
  thinkingLevel: string;
  serviceTier: string;
  onModelChange: (value: string) => void;
  onThinkingLevelChange: (value: string) => void;
  onServiceTierChange: (value: string) => void;
  disabled?: boolean;
}) {
  const { t } = useT("agents");
  const isMockRuntime = isCloudPreviewRuntimeId(runtimeId);
  const modelsQuery = useQuery(
    runtimeModelsOptions(
      runtimeOnline && !isMockRuntime ? runtimeId : null,
    ),
  );
  const models = useMemo(
    () =>
      isMockRuntime
        ? getCloudPreviewModels(runtimeId)
        : (modelsQuery.data?.models ?? []),
    [isMockRuntime, modelsQuery.data, runtimeId],
  );
  const selectedModel =
    models.find((candidate) => candidate.id === model) ?? null;
  const thinkingLevels =
    selectedModel?.thinking?.supported_levels ?? [];
  const serviceTiers = selectedModel?.service_tiers ?? [];
  const hasModelOptions =
    thinkingLevels.length > 0 || serviceTiers.length > 0;

  return (
    <div className="border-t px-4 py-4">
      <div className="flex min-w-0 items-start gap-3">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-foreground text-xs font-semibold text-background">
          3
        </span>
        <div className="min-w-0">
          <div className="font-semibold">
            {t(($) => $.create_dialog.execution_picker.model_title)}
          </div>
          <p className="mt-0.5 text-pretty text-sm leading-5 text-muted-foreground">
            {t(($) => $.create_dialog.execution_picker.model_description)}
          </p>
        </div>
      </div>

      <div className="mt-4">
        {isMockRuntime ? (
          <ModelCardGrid
            models={models}
            value={model}
            disabled={disabled}
            onChange={onModelChange}
          />
        ) : (
          <ModelDropdown
            runtimeId={runtimeId}
            runtimeOnline={runtimeOnline}
            value={model}
            onChange={onModelChange}
            disabled={disabled || !runtimeId}
          />
        )}
      </div>

      {selectedModel && hasModelOptions ? (
        <div className="mt-4 rounded-xl border bg-muted/20 p-4">
          <div className="flex items-start gap-3">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-background">
              <SlidersHorizontal
                aria-hidden="true"
                className="size-4 text-muted-foreground"
              />
            </span>
            <div className="min-w-0">
              <div className="text-sm font-semibold">
                {t(
                  ($) => $.create_dialog.execution_picker.model_parameters,
                )}
              </div>
              <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
                {t(
                  ($) =>
                    $.create_dialog.execution_picker
                      .model_parameters_description,
                )}
              </p>
            </div>
          </div>

          <div className="mt-4 grid gap-4 lg:grid-cols-2">
            {thinkingLevels.length > 0 ? (
              <ThinkingLevelControl
                levels={thinkingLevels}
                value={thinkingLevel}
                disabled={disabled}
                onChange={onThinkingLevelChange}
              />
            ) : null}
            {serviceTiers.length > 0 ? (
              <FastModeControl
                tierId={serviceTiers[0]?.id ?? ""}
                description={serviceTiers[0]?.description}
                enabled={serviceTier === serviceTiers[0]?.id}
                disabled={disabled}
                onChange={(enabled) =>
                  onServiceTierChange(
                    enabled ? (serviceTiers[0]?.id ?? "") : "",
                  )
                }
              />
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ModelCardGrid({
  models,
  value,
  disabled,
  onChange,
}: {
  models: RuntimeModel[];
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useT("agents");
  return (
    <div
      className="grid gap-3 sm:grid-cols-2"
      role="radiogroup"
      aria-label={t(($) => $.create_dialog.execution_picker.model_title)}
    >
      {models.map((model) => {
        const selected = model.id === value;
        return (
          <button
            key={model.id}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={disabled}
            onClick={() => onChange(model.id)}
            className={cn(
              "flex min-h-24 touch-manipulation items-start gap-3 rounded-xl border p-4 text-left transition-colors",
              "hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
              selected
                ? "border-foreground bg-muted/40"
                : "border-border bg-background",
            )}
          >
            <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-background">
              <Cpu
                aria-hidden="true"
                className="size-4 text-muted-foreground"
              />
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{model.label}</span>
                {model.default ? (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                    {t(($) => $.pickers.model_default)}
                  </span>
                ) : null}
              </span>
              <span className="mt-1 block truncate text-xs text-muted-foreground">
                {[model.provider, model.id].filter(Boolean).join(" · ")}
              </span>
            </span>
            {selected ? (
              <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-foreground text-background">
                <Check aria-hidden="true" className="size-3" />
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}

function ThinkingLevelControl({
  levels,
  value,
  disabled,
  onChange,
}: {
  levels: RuntimeModelThinkingLevel[];
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useT("agents");
  const options = [
    {
      value: "",
      label: t(
        ($) => $.create_dialog.execution_picker.thinking_default,
      ),
    },
    ...levels.map((level) => ({
      value: level.value,
      label: level.label,
    })),
  ];

  return (
    <fieldset disabled={disabled} className="min-w-0">
      <legend className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Brain aria-hidden="true" className="size-4" />
        {t(($) => $.create_dialog.execution_picker.thinking_label)}
      </legend>
      <div
        className="mt-2 flex flex-wrap gap-2"
        role="radiogroup"
        aria-label={t(
          ($) => $.create_dialog.execution_picker.thinking_label,
        )}
      >
        {options.map((option) => {
          const selected = option.value === value;
          return (
            <button
              key={option.value || "default"}
              type="button"
              role="radio"
              aria-checked={selected}
              onClick={() => onChange(option.value)}
              className={cn(
                "touch-manipulation rounded-lg border px-3 py-2 text-xs font-medium transition-colors",
                "hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                selected
                  ? "border-foreground bg-foreground text-background"
                  : "border-border bg-background text-foreground",
              )}
            >
              {option.label}
            </button>
          );
        })}
      </div>
    </fieldset>
  );
}

function FastModeControl({
  tierId,
  description,
  enabled,
  disabled,
  onChange,
}: {
  tierId: string;
  description?: string;
  enabled: boolean;
  disabled: boolean;
  onChange: (enabled: boolean) => void;
}) {
  const { t } = useT("agents");
  return (
    <div className="min-w-0">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Zap aria-hidden="true" className="size-4" />
        {t(($) => $.create_dialog.execution_picker.fast_mode)}
      </div>
      <button
        type="button"
        aria-pressed={enabled}
        disabled={disabled || !tierId}
        onClick={() => onChange(!enabled)}
        className={cn(
          "mt-2 flex min-h-16 w-full touch-manipulation items-center gap-3 rounded-xl border bg-background px-3 py-2.5 text-left transition-colors",
          "hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
          enabled && "border-foreground bg-muted/50",
        )}
      >
        <span
          className={cn(
            "flex size-5 shrink-0 items-center justify-center rounded-md border",
            enabled
              ? "border-foreground bg-foreground text-background"
              : "border-border",
          )}
        >
          {enabled ? <Check aria-hidden="true" className="size-3" /> : null}
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2 text-sm font-medium">
            {t(($) => $.create_dialog.execution_picker.fast_mode)}
            <span className="rounded bg-info/10 px-1.5 py-0.5 text-[10px] text-info">
              {t(
                ($) => $.create_dialog.execution_picker.fast_mode_badge,
              )}
            </span>
          </span>
          <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">
            {description ??
              t(
                ($) =>
                  $.create_dialog.execution_picker.fast_mode_description,
              )}
          </span>
        </span>
      </button>
    </div>
  );
}
