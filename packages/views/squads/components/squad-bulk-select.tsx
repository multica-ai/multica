"use client";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ChevronDown } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";

// Radio items need a non-empty value, and "" stands for the default choice.
const DEFAULT_VALUE = "__default__";
const NO_VALUE = "__none__";

export interface SquadBulkOption {
  value: string;
  label: string;
}

/**
 * Squad-wide dropdown that applies one choice to every agent. `value` is the
 * shared value, `null` when agents differ, which shows `customLabel`. Choosing
 * any option, including the one already shown when the value is custom, reports
 * it through `onSelect` for the parent to confirm before anything is saved.
 * "" is the default choice when `defaultLabel` is given.
 */
export function SquadBulkSelect({
  icon: Icon,
  options,
  value,
  customLabel,
  defaultLabel,
  ariaLabel,
  disabled = false,
  onSelect,
  className,
}: {
  icon?: React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;
  options: SquadBulkOption[];
  value: string | null;
  customLabel: string;
  defaultLabel?: string;
  ariaLabel: string;
  disabled?: boolean;
  onSelect: (value: string) => void;
  className?: string;
}) {
  const valueLabel =
    value === null
      ? customLabel
      : value === "" && defaultLabel
        ? defaultLabel
        : (options.find((o) => o.value === value)?.label ?? value);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={ariaLabel}
        className={cn(
          "inline-flex min-w-0 max-w-full items-center gap-1 rounded-md px-1.5 py-0.5 text-caption text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-60 data-[popup-open]:bg-accent data-[popup-open]:text-foreground",
          className,
        )}
      >
        {Icon ? <Icon className="size-3 shrink-0" aria-hidden /> : null}
        <span className="truncate">{valueLabel}</span>
        {!disabled && <ChevronDown className="size-3 shrink-0" />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-72 w-auto min-w-48">
        <DropdownMenuRadioGroup
          value={value === null ? NO_VALUE : value === "" ? DEFAULT_VALUE : value}
          onValueChange={(next) => {
            if (next === NO_VALUE) return;
            onSelect(next === DEFAULT_VALUE ? "" : next);
          }}
        >
          {defaultLabel !== undefined && (
            <DropdownMenuRadioItem value={DEFAULT_VALUE}>
              <span className="truncate">{defaultLabel}</span>
            </DropdownMenuRadioItem>
          )}
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              <span className="truncate">{option.label || option.value}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
