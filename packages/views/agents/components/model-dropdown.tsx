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

// ModelDropdown renders a searchable, creatable model picker for an agent.
// It fetches the supported-model catalog from the selected runtime — the
// daemon enumerates models on demand via heartbeat piggyback. Providers
// that don't honour per-agent model selection at runtime (currently
// hermes) return supported=false, and the dropdown renders disabled
// with an explanation instead of silently accepting a value the
// backend would ignore.
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
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );

  const supported = modelsQuery.data?.supported ?? true;
  const models = modelsQuery.data?.models ?? [];
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

  const select = (id: string) => {
    onChange(id);
    setOpen(false);
    setSearch("");
  };

  const triggerLabel =
    value ||
    (disabled
      ? "Select a runtime first"
      : runtimeOnline
        ? "Default (provider)"
        : "Runtime offline — enter manually");

  if (!supported && !modelsQuery.isLoading) {
    // Provider doesn't honour per-agent model selection — show a
    // clearly-disabled state so the user knows why the control is
    // inert. (Hermes reads its model from ~/.hermes/.env.)
    return (
      <div className="min-w-0">
        <Label className="text-xs text-muted-foreground">Model</Label>
        <div className="mt-1.5 flex items-start gap-2 rounded-lg border border-dashed border-border bg-muted/30 px-3 py-2.5 text-sm text-muted-foreground">
          <Info className="mt-0.5 h-4 w-4 shrink-0" />
          <div className="min-w-0">
            <div>Model selection is managed by this runtime.</div>
            <div className="mt-0.5 text-xs">
              Configure the model on the runtime host (e.g. Hermes reads it
              from its own config file).
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-w-0">
      <div className="flex items-center justify-between">
        <Label className="text-xs text-muted-foreground">Model</Label>
        {modelsQuery.isError && (
          <span className="text-xs text-muted-foreground">discovery failed</span>
        )}
      </div>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          disabled={disabled}
          className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
        >
          <Cpu className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">
              {triggerLabel}
            </div>
            {value && (
              <div className="truncate text-xs text-muted-foreground">
                {modelLabel(models, value)}
              </div>
            )}
          </div>
          <ChevronDown
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
              placeholder="Search or type a model ID"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-8"
            />
          </div>
          <div className="max-h-72 overflow-y-auto p-1">
            {modelsQuery.isLoading && (
              <div className="flex items-center gap-2 px-3 py-6 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                Discovering models…
              </div>
            )}

            {!modelsQuery.isLoading &&
              Object.entries(filtered).map(([provider, list]) => (
                <div key={provider} className="mb-1">
                  {provider && (
                    <div className="px-2 pt-1.5 pb-0.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                      {provider}
                    </div>
                  )}
                  {list.map((m) => (
                    <button
                      key={m.id}
                      onClick={() => select(m.id)}
                      className={`flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors ${
                        m.id === value ? "bg-accent" : "hover:bg-accent/50"
                      }`}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate font-medium">{m.label}</span>
                          {m.default && (
                            <span className="shrink-0 rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                              default
                            </span>
                          )}
                        </div>
                        {m.label !== m.id && (
                          <div className="truncate text-xs text-muted-foreground">
                            {m.id}
                          </div>
                        )}
                      </div>
                      {m.id === value && (
                        <Check className="h-4 w-4 shrink-0 text-primary" />
                      )}
                    </button>
                  ))}
                </div>
              ))}

            {!modelsQuery.isLoading &&
              Object.keys(filtered).length === 0 &&
              !canCreate && (
                <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                  No models available.
                </div>
              )}

            {canCreate && (
              <button
                onClick={() => select(trimmedSearch)}
                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-primary transition-colors hover:bg-accent/50"
              >
                <Plus className="h-4 w-4 shrink-0" />
                <span className="truncate">
                  Use “{trimmedSearch}”
                </span>
              </button>
            )}

            {value && (
              <button
                onClick={() => select("")}
                className="mt-1 flex w-full items-center gap-2 border-t border-border px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50"
              >
                Clear selection (use provider default)
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

function modelLabel(models: RuntimeModel[], id: string): string {
  const found = models.find((m) => m.id === id);
  if (!found) return "custom";
  return found.provider ? found.provider : "model";
}
