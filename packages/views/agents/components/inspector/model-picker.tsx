"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Cpu, Loader2, Plus } from "lucide-react";
import {
  runtimeDisplayLabel,
  runtimeModelsOptions,
} from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";
import { useT } from "../../../i18n";
import { runtimeDefaultModelDisplay } from "../model-display";
import {
  isPiModelDiscoveryNoise,
  visibleRuntimeModels,
} from "../model-options";

/**
 * Inline model picker for the agent inspector. Lighter cousin of
 * `ModelDropdown` (which is used in the create-agent dialog) — same data
 * source via `runtimeModelsOptions`, but renders inside a PropertyPicker so
 * it fits a single PropRow. Drops the "select a runtime first" state because
 * the inspector only renders this picker after a runtime is bound.
 *
 * Providers whose runtime ignores per-agent model selection report
 * `supported=false` and render an inert italic "Managed by runtime" label
 * instead of a clickable picker. No built-in provider sets this today
 * (Antigravity gained `--model` in agy 1.0.6), but the branch stays for any
 * future model-less runtime.
 */
export function ModelPicker({
  runtime,
  runtimeId,
  runtimeOnline,
  value,
  canEdit = true,
  variant = "chip",
  showLabel = true,
  onChange,
}: {
  runtime?: AgentRuntime | null;
  runtimeId: string | null;
  runtimeOnline: boolean;
  value: string;
  /** When false, render a static read-only display and skip the popover. */
  canEdit?: boolean;
  variant?: "chip" | "field";
  showLabel?: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const resolvedRuntimeId = runtime?.id ?? runtimeId;
  const resolvedRuntimeOnline = runtime
    ? runtime.status === "online"
    : runtimeOnline;
  const runtimeDefault = useMemo(
    () => runtimeDefaultModelDisplay(runtime),
    [runtime],
  );

  const modelsQuery = useQuery(
    runtimeModelsOptions(resolvedRuntimeOnline ? resolvedRuntimeId : null),
  );
  const supported = modelsQuery.data?.supported ?? true;
  // Memoise the model list so every downstream useMemo gets a stable
  // reference; `?? []` would mint a fresh array on every render and
  // invalidate filters needlessly.
  const models = useMemo(
    () => visibleRuntimeModels(modelsQuery.data?.models ?? [], runtime),
    [modelsQuery.data, runtime],
  );

  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase();
    if (!s) return models;
    return models.filter(
      (m) =>
        m.id.toLowerCase().includes(s) || m.label.toLowerCase().includes(s),
    );
  }, [models, search]);

  const trimmedSearch = search.trim();
  const exactMatch = models.some(
    (m) => m.id === trimmedSearch || m.label === trimmedSearch,
  ) || Boolean(
    runtimeDefault &&
      (runtimeDefault.model === trimmedSearch ||
        (runtimeDefault.provider
          ? `${runtimeDefault.provider}/${runtimeDefault.model}`
          : runtimeDefault.model) === trimmedSearch),
  );
  const canCreate =
    trimmedSearch.length > 0 &&
    !exactMatch &&
    !(runtime?.provider === "pi" && isPiModelDiscoveryNoise(trimmedSearch));

  const triggerLabel =
    value ||
    (runtimeDefault
      ? t(($) => $.model_dropdown.runtime_default_value, {
          model: runtimeDefault.model,
        })
      : t(($) => $.pickers.model_default));
  const triggerTitle = t(($) => $.pickers.model_tooltip, { value: triggerLabel });
  const runtimeDefaultSubtitle =
    runtimeDefault && runtime
      ? runtimeDefault.provider
        ? t(($) => $.model_dropdown.runtime_default_subtitle, {
            provider: runtimeDefault.provider,
            runtime: runtimeDisplayLabel(runtime),
          })
        : t(($) => $.model_dropdown.runtime_default_subtitle_no_provider, {
            runtime: runtimeDisplayLabel(runtime),
          })
      : "";

  const select = async (id: string) => {
    setOpen(false);
    setSearch("");
    if (id !== value) await onChange(id);
  };

  if (!supported && !modelsQuery.isLoading) {
    if (variant === "field") {
      const control = (
        <div className="flex min-h-10 items-center gap-2 rounded-lg border border-dashed border-input bg-input/50 px-3 text-body text-muted-foreground">
          <Cpu className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span className="truncate italic">
            {t(($) => $.pickers.model_managed_by_runtime)}
          </span>
        </div>
      );
      if (!showLabel) return control;
      return (
        <div className="flex min-w-0 flex-col">
          <Label>{t(($) => $.inspector.prop_model)}</Label>
          <div className="mt-1.5">{control}</div>
        </div>
      );
    }
    return (
      <span className="truncate italic text-muted-foreground">
        {t(($) => $.pickers.model_managed_by_runtime)}
      </span>
    );
  }

  if (!canEdit) {
    if (variant === "field") {
      const control = (
        <div className="flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-body text-muted-foreground">
          <Cpu className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span className="min-w-0 truncate font-mono">{triggerLabel}</span>
        </div>
      );
      if (!showLabel) return control;
      return (
        <div className="flex min-w-0 flex-col">
          <Label>{t(($) => $.inspector.prop_model)}</Label>
          <div className="mt-1.5">{control}</div>
        </div>
      );
    }
    return (
      <span
        className="min-w-0 truncate px-1.5 py-0.5 font-mono text-micro text-muted-foreground"
        title={triggerTitle}
      >
        {triggerLabel}
      </span>
    );
  }

  const picker = (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width={
        variant === "field"
          ? "w-[var(--anchor-width)] min-w-[16rem] max-w-md"
          : "w-auto min-w-[16rem] max-w-md"
      }
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className={
            variant === "field"
              ? `${showLabel ? "mt-1.5 " : ""}flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-body transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50`
              : CHIP_CLASS
          }
          aria-label={triggerTitle}
        />
      }
      trigger={
        <>
          {variant === "field" ? (
            <Cpu
              className="h-4 w-4 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
          ) : null}
          {variant === "field" ? (
            <span className="min-w-0 flex-1">
              <span className="block truncate font-mono">{triggerLabel}</span>
              {!value && runtimeDefaultSubtitle ? (
                <span className="block truncate text-caption text-muted-foreground">
                  {runtimeDefaultSubtitle}
                </span>
              ) : null}
            </span>
          ) : (
            <span className="min-w-0 truncate font-mono text-micro">
              {triggerLabel}
            </span>
          )}
          {variant === "field" ? (
            <ChevronDown
              className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
                open ? "rotate-180" : ""
              }`}
              aria-hidden="true"
            />
          ) : null}
        </>
      }
      header={
        <div className="p-1.5">
          <Input
            autoFocus
            name="agent-model-search"
            autoComplete="off"
            aria-label={t(($) => $.pickers.model_search_placeholder)}
            placeholder={t(($) => $.pickers.model_search_placeholder)}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-7 text-caption"
          />
        </div>
      }
    >
      {modelsQuery.isLoading && (
        <div className="flex items-center gap-2 p-3 text-caption text-muted-foreground">
          <Loader2
            className="h-3 w-3 animate-spin motion-reduce:animate-none"
            aria-hidden="true"
          />
          {t(($) => $.pickers.model_discovering)}
        </div>
      )}

      {runtimeDefault && (
        <PickerItem
          selected={value === ""}
          onClick={() => void select("")}
          tooltip={
            runtimeDefault.provider
              ? `${runtimeDefault.provider}/${runtimeDefault.model}`
              : runtimeDefault.model
          }
        >
          <span className="block min-w-0 flex-1 text-left">
            <span className="block truncate text-label font-medium">
              {t(($) => $.model_dropdown.runtime_default_option)}
            </span>
            <span className="mt-0.5 block truncate font-mono text-micro leading-snug text-muted-foreground">
              {runtimeDefault.provider
                ? `${runtimeDefault.model} · ${runtimeDefault.provider}`
                : runtimeDefault.model}
            </span>
          </span>
          {value === "" && (
            <Check className="h-3.5 w-3.5 shrink-0 text-primary" />
          )}
        </PickerItem>
      )}

      {!modelsQuery.isLoading &&
        filtered.map((m) => (
          <PickerItem
            key={m.id}
            selected={m.id === value}
            onClick={() => void select(m.id)}
            // Tooltip carries the canonical model id even when the chip
            // shows the friendlier label, so users can always see what
            // string actually ships to the agent.
            tooltip={m.label !== m.id ? `${m.label} · ${m.id}` : m.id}
          >
            {/* PickerItem wraps children in a flex `<span>`. Putting a
                `<div>` inside that <span> is block-in-inline (invalid
                HTML5) and triggers the browser-default centering quirk
                that pushes descendants off-axis (model IDs floated to the
                center instead of left-aligning under their labels). Use
                `<span block text-left>` to keep layout deterministic —
                matches the fix already applied in thinking-picker.tsx. */}
            <span className="block min-w-0 flex-1 text-left">
              <span className="block truncate text-label font-medium">{m.label}</span>
              {m.label !== m.id && (
                <span className="mt-0.5 block truncate font-mono text-micro leading-snug text-muted-foreground">
                  {m.id}
                </span>
              )}
            </span>
          </PickerItem>
        ))}

      {!modelsQuery.isLoading &&
        filtered.length === 0 &&
        !canCreate &&
        !runtimeDefault && (
        <p className="px-3 py-3 text-center text-caption text-muted-foreground">
          {t(($) => $.pickers.model_empty)}
        </p>
      )}

      {canCreate && (
        <PickerItem
          selected={false}
          onClick={() => void select(trimmedSearch)}
          tooltip={t(($) => $.pickers.model_custom_tooltip, { value: trimmedSearch })}
        >
          <Plus className="h-3.5 w-3.5 shrink-0 text-primary" />
          <span className="truncate text-primary">
            {t(($) => $.pickers.model_custom_use, { value: trimmedSearch })}
          </span>
        </PickerItem>
      )}

      {value && (
        <button
          type="button"
          onClick={() => void select("")}
          className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-caption text-muted-foreground transition-colors hover:bg-accent/50"
          title={
            runtimeDefault
              ? t(($) => $.pickers.model_clear_runtime_default_title)
              : t(($) => $.pickers.model_clear_title)
          }
        >
          {runtimeDefault
            ? t(($) => $.pickers.model_clear_runtime_default)
            : t(($) => $.pickers.model_clear)}
        </button>
      )}
    </PropertyPicker>
  );

  if (variant === "field") {
    if (!showLabel) return picker;
    return (
      <div className="flex min-w-0 flex-col">
        <Label>{t(($) => $.inspector.prop_model)}</Label>
        {picker}
      </div>
    );
  }

  return picker;
}
