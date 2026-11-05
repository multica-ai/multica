"use client";

import { useQuery } from "@tanstack/react-query";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { ChevronDown } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

// The radio value that stands for "runtime default". Radio items need a
// non-empty value, and "" is how an agent stores "no model chosen".
const DEFAULT_MODEL_VALUE = "__runtime_default__";

/**
 * Model dropdown for one agent row. The choices come from the agent's runtime,
 * so the control is empty and disabled until a runtime is set. `value` is the
 * agent's stored model: "" means the runtime default.
 */
export function SquadModelSelect({
  runtimeId,
  runtimeOnline,
  value,
  ariaLabel,
  defaultLabel,
  disabled = false,
  onSelect,
  className,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  value: string;
  ariaLabel: string;
  defaultLabel: string;
  disabled?: boolean;
  onSelect: (model: string) => void;
  className?: string;
}) {
  const modelsQuery = useQuery(runtimeModelsOptions(runtimeOnline ? runtimeId : null));
  const models = modelsQuery.data?.models ?? [];
  const supported = modelsQuery.data?.supported ?? true;
  const selectable = Boolean(runtimeId) && supported && !disabled;
  const selectedLabel = models.find((m) => m.id === value)?.label ?? value;
  const triggerLabel = !runtimeId
    ? ""
    : value
      ? selectedLabel
      : defaultLabel;

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
        <span className="truncate">{triggerLabel}</span>
        {selectable && <ChevronDown className="size-3 shrink-0" />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-72 w-auto min-w-56">
        <DropdownMenuRadioGroup
          value={value || DEFAULT_MODEL_VALUE}
          onValueChange={(next) => {
            const model = next === DEFAULT_MODEL_VALUE ? "" : next;
            if (model !== value) onSelect(model);
          }}
        >
          <DropdownMenuRadioItem value={DEFAULT_MODEL_VALUE}>
            <span className="truncate">{defaultLabel}</span>
          </DropdownMenuRadioItem>
          {models.map((model) => (
            <DropdownMenuRadioItem key={model.id} value={model.id}>
              <span className="truncate">{model.label || model.id}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
