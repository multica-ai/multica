"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Check, Sparkles } from "lucide-react";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import { resolveThinkingLevels } from "./inspector/thinking-levels";

// ReasoningPicker — the create-dialog counterpart of the inspector's
// ThinkingPropRow (MUL-3772 / REQ-2). It shares the level-resolution logic
// (`resolveThinkingLevels`) and the empty-string "follow CLI config" sentinel
// with the inspector, but wears the full-width form chrome the other create
// fields use instead of the inspector's inline chip.
//
// Mounted unconditionally by the dialog; renders nothing until the selected
// model advertises a reasoning catalog, so providers without reasoning never
// show an empty row. An already-set value (duplicate pre-fill) also forces the
// row so a stale token stays visible and clearable.
export function ReasoningPicker({
  runtimeId,
  runtimeOnline,
  model,
  value,
  onChange,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  model: string;
  value: string;
  onChange: (next: string) => void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );
  const models = modelsQuery.data?.models ?? [];
  const levels = resolveThinkingLevels(models, model);

  // Hidden until the model exposes reasoning levels (or a value is already
  // persisted) — mirrors ThinkingPropRow's gate so behavior matches the
  // inspector exactly.
  if (levels.length === 0 && !value) return null;

  const selected = value ? levels.find((l) => l.value === value) : undefined;
  // Unknown-but-set value (model swap that dropped the option): show the raw
  // token so the user can see and clear what is actually persisted.
  const triggerLabel = selected
    ? selected.label
    : value || t(($) => $.pickers.thinking_default);

  const select = (next: string) => {
    setOpen(false);
    if (next !== value) onChange(next);
  };

  return (
    <div className="flex flex-col min-w-0">
      <div className="flex h-6 items-center">
        <Label className="text-xs text-muted-foreground">
          {t(($) => $.create_dialog.reasoning_label)}
        </Label>
      </div>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50">
          <Sparkles className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-medium">{triggerLabel}</span>
            </div>
            <div className="truncate text-xs text-muted-foreground">
              {t(($) => $.create_dialog.reasoning_hint)}
            </div>
          </div>
          <ChevronDown
            className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
              open ? "rotate-180" : ""
            }`}
          />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-[var(--anchor-width)] p-1 max-h-72 overflow-y-auto"
        >
          {/* "Follow CLI config" (value "") is a first-class, selectable
              option — Multica omits the effort flag and the local CLI config
              decides. Mirrors the inspector picker's empty-sentinel meaning. */}
          <ReasoningOption
            label={t(($) => $.pickers.thinking_default)}
            selected={value === ""}
            onClick={() => select("")}
          />
          {levels.map((level) => (
            <ReasoningOption
              key={level.value}
              label={level.label}
              description={level.description}
              selected={level.value === value}
              onClick={() => select(level.value)}
            />
          ))}
        </PopoverContent>
      </Popover>
    </div>
  );
}

function ReasoningOption({
  label,
  description,
  selected,
  onClick,
}: {
  label: string;
  description?: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors ${
        selected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{label}</div>
        {description && (
          <div className="mt-0.5 text-xs leading-snug text-muted-foreground">
            {description}
          </div>
        )}
      </div>
      {selected && <Check className="h-4 w-4 shrink-0 text-primary" />}
    </button>
  );
}
