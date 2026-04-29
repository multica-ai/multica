"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Plus } from "lucide-react";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { Input } from "@multica/ui/components/ui/input";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";

/**
 * Inline model picker for the agent inspector. Lighter cousin of
 * `ModelDropdown` (which is used in the create-agent dialog) — same data
 * source via `runtimeModelsOptions`, but renders inside a PropertyPicker so
 * it fits a single PropRow. Drops the "select a runtime first" state because
 * the inspector only renders this picker after a runtime is bound.
 *
 * Unsupported providers (e.g. hermes, which reads its own config) render an
 * inert italic "Managed by runtime" label instead of a clickable picker —
 * the back-end ignores agent.model for those runtimes anyway.
 */
export function ModelPicker({
  runtimeId,
  runtimeOnline,
  value,
  onChange,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  value: string;
  onChange: (next: string) => Promise<void> | void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );
  const supported = modelsQuery.data?.supported ?? true;
  // Memoise the model list so every downstream useMemo gets a stable
  // reference; `?? []` would mint a fresh array on every render and
  // invalidate filters needlessly.
  const models = useMemo(
    () => modelsQuery.data?.models ?? [],
    [modelsQuery.data],
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
  );
  const canCreate = trimmedSearch.length > 0 && !exactMatch;

  const select = async (id: string) => {
    setOpen(false);
    setSearch("");
    if (id !== value) await onChange(id);
  };

  if (!supported && !modelsQuery.isLoading) {
    return (
      <span className="truncate italic text-muted-foreground">
        Managed by runtime
      </span>
    );
  }

  const triggerLabel = value || "Default";
  const triggerTitle = `Model · ${triggerLabel}`;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[16rem] max-w-md"
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className={CHIP_CLASS}
          aria-label={triggerTitle}
        />
      }
      trigger={
        <span className="min-w-0 truncate font-mono text-[11px]">
          {triggerLabel}
        </span>
      }
      header={
        <div className="p-1.5">
          <Input
            autoFocus
            placeholder="Search or type a model ID"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-7 text-xs"
          />
        </div>
      }
    >
      {modelsQuery.isLoading && (
        <div className="flex items-center gap-2 p-3 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          Discovering models…
        </div>
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
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span className="truncate font-medium">{m.label}</span>
                {m.default && (
                  <span className="shrink-0 rounded bg-primary/10 px-1 text-[10px] font-medium text-primary">
                    default
                  </span>
                )}
              </div>
              {m.label !== m.id && (
                <div className="truncate font-mono text-[10px] text-muted-foreground">
                  {m.id}
                </div>
              )}
            </div>
          </PickerItem>
        ))}

      {!modelsQuery.isLoading && filtered.length === 0 && !canCreate && (
        <p className="px-3 py-3 text-center text-xs text-muted-foreground">
          No models available
        </p>
      )}

      {canCreate && (
        <PickerItem
          selected={false}
          onClick={() => void select(trimmedSearch)}
          tooltip={`Use “${trimmedSearch}” as a custom model id`}
        >
          <Plus className="h-3.5 w-3.5 shrink-0 text-primary" />
          <span className="truncate text-primary">
            Use &ldquo;{trimmedSearch}&rdquo;
          </span>
        </PickerItem>
      )}

      {value && (
        <button
          type="button"
          onClick={() => void select("")}
          className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50"
          title="Clear and fall back to the runtime's provider default"
        >
          Clear (use provider default)
        </button>
      )}
    </PropertyPicker>
  );
}
