"use client";

import type { RuntimeDevice } from "@multica/core/types";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { ChevronDown } from "@multica/ui/icons";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";

/**
 * Runtime dropdown used per agent row and for the whole squad. `options` are
 * the runtimes the viewer can bind; the server rejects the rest, so they are not
 * offered. `valueLabel` is passed in because the current value may not be in
 * `options` (for example, a runtime owned by someone else).
 */
export function SquadRuntimeSelect({
  options,
  value,
  valueLabel,
  ariaLabel,
  disabled = false,
  onSelect,
  className,
}: {
  options: RuntimeDevice[];
  value: RuntimeDevice | null;
  valueLabel: string;
  ariaLabel: string;
  disabled?: boolean;
  onSelect: (runtimeId: string) => void;
  className?: string;
}) {
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
        {value && <ProviderLogo provider={value.provider} className="size-3.5 shrink-0" />}
        <span className="truncate">{valueLabel}</span>
        {!disabled && <ChevronDown className="size-3 shrink-0" />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-72 w-auto min-w-56">
        <DropdownMenuRadioGroup
          value={value?.id ?? ""}
          onValueChange={(id) => {
            if (id && id !== value?.id) onSelect(id);
          }}
        >
          {options.map((runtime) => (
            <DropdownMenuRadioItem key={runtime.id} value={runtime.id}>
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 shrink-0 rounded-full",
                  runtime.status === "online" ? "bg-success" : "bg-muted-foreground/40",
                )}
              />
              <ProviderLogo provider={runtime.provider} className="size-3.5 shrink-0" />
              <span className="truncate">{runtimeDisplayLabel(runtime)}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
