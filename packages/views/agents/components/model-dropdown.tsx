"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Cpu, Loader2, Plus, Check, Info } from "lucide-react";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import type { RuntimeModel } from "@multica/core/types";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import {
  getCloudPreviewModels,
  isCloudPreviewRuntimeId,
} from "../../runtimes/components/cloud-preview";

// ModelDropdown renders a searchable, creatable model picker for an agent.
// It fetches the supported-model catalog from the selected runtime — the
// daemon enumerates models on demand via heartbeat piggyback. Providers
// whose runtime ignores per-agent model selection return supported=false,
// and the dropdown renders disabled with an explanation instead of silently
// accepting a value the backend would ignore. No built-in provider does so
// today — Antigravity gained `--model` in agy 1.0.6 — but the path stays for
// any future model-less runtime.
export function ModelDropdown({
  runtimeId,
  runtimeOnline,
  value,
  onChange,
  disabled,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const usesMockCatalog = isCloudPreviewRuntimeId(runtimeId);

  const modelsQuery = useQuery(
    runtimeModelsOptions(
      runtimeOnline && !usesMockCatalog ? runtimeId : null,
    ),
  );

  const supported = usesMockCatalog
    ? true
    : (modelsQuery.data?.supported ?? true);
  // Stable reference for the model list — `?? []` would mint a fresh
  // array each render and force every downstream useMemo to invalidate.
  const models = useMemo(
    () =>
      usesMockCatalog
        ? getCloudPreviewModels(runtimeId)
        : (modelsQuery.data?.models ?? []),
    [modelsQuery.data, runtimeId, usesMockCatalog],
  );
  const grouped = useMemo(() => groupByProvider(models), [models]);

  // When the selected runtime reports it doesn't support per-agent
  // model selection, clear any previously-saved value so we don't
  // persist a ghost configuration that never takes effect.
  useEffect(() => {
    if (!supported && value !== "") {
      onChange("");
    }
  }, [supported, value, onChange]);

  const filtered = useMemo(() => {
    if (!search.trim()) return grouped;
    const needle = search.toLowerCase();
    const out: Record<string, RuntimeModel[]> = {};
    for (const [provider, list] of Object.entries(grouped)) {
      const matches = list.filter(
        (m) =>
          m.id.toLowerCase().includes(needle) ||
          m.label.toLowerCase().includes(needle),
      );
      if (matches.length > 0) out[provider] = matches;
    }
    return out;
  }, [grouped, search]);

  const trimmedSearch = search.trim();
  const exactMatch = models.some(
    (m) => m.id === trimmedSearch || m.label === trimmedSearch,
  );
  const canCreate = trimmedSearch.length > 0 && !exactMatch;
  const selectedModel = value
    ? models.find((model) => model.id === value) ?? null
    : null;

  const select = (id: string) => {
    onChange(id);
    setOpen(false);
    setSearch("");
  };

  const triggerLabel =
    selectedModel?.label ||
    value ||
    (disabled
      ? t(($) => $.model_dropdown.select_runtime_first)
      : runtimeOnline
        ? t(($) => $.model_dropdown.default_provider)
        : t(($) => $.model_dropdown.runtime_offline_manual));

  const modelsLoading = !usesMockCatalog && modelsQuery.isLoading;
  const modelsError = !usesMockCatalog && modelsQuery.isError;

  if (!supported && !modelsLoading) {
    return (
      <div className="flex flex-col min-w-0">
        <div className="flex h-6 items-center">
          <Label className="text-xs text-muted-foreground">{t(($) => $.model_dropdown.label)}</Label>
        </div>
        <div className="mt-1.5 flex items-start gap-2 rounded-lg border border-dashed border-border bg-muted/30 px-3 py-2.5 text-sm text-muted-foreground">
          <Info aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
          <div className="min-w-0">
            <div>{t(($) => $.model_dropdown.managed_by_runtime_title)}</div>
            <div className="mt-0.5 text-xs">
              {t(($) => $.model_dropdown.managed_by_runtime_hint)}
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col min-w-0">
      <div className="flex h-6 items-center justify-between">
        <Label className="text-xs text-muted-foreground">{t(($) => $.model_dropdown.label)}</Label>
        {modelsError && (
          <span className="text-xs text-muted-foreground">{t(($) => $.model_dropdown.discovery_failed)}</span>
        )}
      </div>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          disabled={disabled}
          className="mt-1.5 flex w-full min-w-0 touch-manipulation items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
        >
          <Cpu
            aria-hidden="true"
            className="h-4 w-4 shrink-0 text-muted-foreground"
          />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium">{triggerLabel}</span>
            </div>
            {selectedModel && (
              <div className="truncate text-xs text-muted-foreground">
                {[selectedModel.provider, selectedModel.id]
                  .filter(Boolean)
                  .join(" · ")}
              </div>
            )}
          </div>
          <ChevronDown
            aria-hidden="true"
            className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`}
          />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-[var(--anchor-width)] p-0 overflow-hidden"
        >
          <div className="border-b border-border p-2">
            <Input
              autoFocus
              name="model-search"
              autoComplete="off"
              aria-label={t(($) => $.pickers.model_search_placeholder)}
              placeholder={t(($) => $.pickers.model_search_placeholder)}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-8"
            />
          </div>
          <div className="max-h-72 overflow-y-auto p-1">
            {modelsLoading && (
              <div className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground">
                <Loader2
                  aria-hidden="true"
                  className="h-4 w-4 animate-spin"
                />
                {t(($) => $.pickers.model_discovering)}
              </div>
            )}

            {!modelsLoading &&
              Object.entries(filtered).map(([provider, list]) => (
                <div key={provider} className="mb-1">
                  {provider && (
                    <div className="px-2 pt-1.5 pb-0.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                      {provider}
                    </div>
                  )}
                  {list.map((m) => (
                    <button
                      type="button"
                      key={m.id}
                      onClick={() => select(m.id)}
                      className={`flex w-full touch-manipulation items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring ${
                        m.id === value ? "bg-accent" : "hover:bg-accent/50"
                      }`}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">{m.label}</div>
                        {m.label !== m.id && (
                          <div className="truncate text-xs text-muted-foreground">
                            {m.id}
                          </div>
                        )}
                      </div>
                      {m.id === value && (
                        <Check
                          aria-hidden="true"
                          className="h-4 w-4 shrink-0 text-primary"
                        />
                      )}
                    </button>
                  ))}
                </div>
              ))}

            {!modelsLoading &&
              Object.keys(filtered).length === 0 &&
              !canCreate && (
                <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                  {t(($) => $.pickers.model_empty_with_dot)}
                </div>
              )}

            {canCreate && (
              <button
                type="button"
                onClick={() => select(trimmedSearch)}
                className="flex w-full touch-manipulation items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-primary transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              >
                <Plus aria-hidden="true" className="h-4 w-4 shrink-0" />
                <span className="truncate">
                  {t(($) => $.pickers.model_custom_use, { value: trimmedSearch })}
                </span>
              </button>
            )}

            {value && (
              <button
                type="button"
                onClick={() => select("")}
                className="mt-1 flex w-full touch-manipulation items-center gap-2 border-t border-border px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              >
                {t(($) => $.model_dropdown.clear_full)}
              </button>
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

function groupByProvider(models: RuntimeModel[]): Record<string, RuntimeModel[]> {
  const out: Record<string, RuntimeModel[]> = {};
  for (const m of models) {
    const key = m.provider ?? "";
    if (!out[key]) out[key] = [];
    out[key].push(m);
  }
  return out;
}
