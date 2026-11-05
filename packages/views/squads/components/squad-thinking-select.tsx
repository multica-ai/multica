"use client";

import { useQuery } from "@tanstack/react-query";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { Brain, ChevronDown } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { findModelCapabilityEntry } from "../../agents/components/inspector/model-capability";

// The radio value that stands for "follow CLI config". Radio items need a
// non-empty value, and "" is how an agent stores "no thinking level chosen".
const DEFAULT_LEVEL_VALUE = "__cli_default__";

/**
 * Thinking dropdown for one agent row. The levels come from the selected
 * model's catalog entry, so the control stays empty until a model is chosen.
 * A model that advertises no levels shows a dash, which keeps the column
 * aligned across rows. `value` is the agent's stored thinking_level: "" means
 * the local CLI config decides.
 */
export function SquadThinkingSelect({
  runtimeId,
  runtimeOnline,
  provider,
  model,
  value,
  ariaLabel,
  defaultLabel,
  disabled = false,
  onSelect,
  className,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  provider: string;
  model: string;
  value: string;
  ariaLabel: string;
  defaultLabel: string;
  disabled?: boolean;
  onSelect: (thinkingLevel: string) => void;
  className?: string;
}) {
  const modelsQuery = useQuery(runtimeModelsOptions(runtimeOnline ? runtimeId : null));
  const models = modelsQuery.data?.models ?? [];
  const levels = model
    ? (findModelCapabilityEntry(models, model, provider)?.thinking?.supported_levels ?? [])
    : [];
  const selectable = levels.length > 0 && !disabled;

  if (levels.length === 0 && !value) {
    return (
      <span aria-hidden="true" className={cn("shrink-0 px-1.5 py-0.5 text-caption text-muted-foreground", className)}>
        —
      </span>
    );
  }

  // An unknown stored token is shown raw so the user can see what is persisted.
  const selectedLabel = levels.find((l) => l.value === value)?.label ?? value;
  const triggerLabel = value ? selectedLabel : defaultLabel;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={!selectable}
        aria-label={ariaLabel}
        className={cn(
          "inline-flex min-w-0 max-w-full items-center gap-1 rounded-md px-1.5 py-0.5 text-caption text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-60 data-[popup-open]:bg-accent data-[popup-open]:text-foreground",
          className,
        )}
      >
        <Brain className="size-3 shrink-0" aria-hidden="true" />
        <span className="truncate">{triggerLabel}</span>
        {selectable && <ChevronDown className="size-3 shrink-0" />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-72 w-auto min-w-48">
        <DropdownMenuRadioGroup
          value={value || DEFAULT_LEVEL_VALUE}
          onValueChange={(next) => {
            const level = next === DEFAULT_LEVEL_VALUE ? "" : next;
            if (level !== value) onSelect(level);
          }}
        >
          <DropdownMenuRadioItem value={DEFAULT_LEVEL_VALUE}>
            <span className="truncate">{defaultLabel}</span>
          </DropdownMenuRadioItem>
          {levels.map((level) => (
            <DropdownMenuRadioItem key={level.value} value={level.value}>
              <span className="truncate">{level.label || level.value}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
