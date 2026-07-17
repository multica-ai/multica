"use client";

import { useMemo, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

export interface RuntimeSettingOption {
  value: string;
  label: string;
  description?: string | undefined;
}

export function RuntimeSettingSearchSelect({
  variant,
  ariaLabel,
  value,
  options,
  defaultOption,
  defaultOptionTitle,
  searchPlaceholder,
  onChange,
}: {
  variant: "form" | "property";
  ariaLabel: string;
  value: string;
  options: RuntimeSettingOption[];
  defaultOption?: RuntimeSettingOption | undefined;
  defaultOptionTitle?: string | undefined;
  searchPlaceholder: string;
  onChange: (value: string) => void | Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return options;
    return options.filter((option) =>
      [option.label, option.value, option.description]
        .filter(Boolean)
        .some((text) => text!.toLowerCase().includes(needle)),
    );
  }, [options, search]);
  const selected = options.find((option) => option.value === value);
  const selectedLabel = (selected?.label ?? value) || defaultOption?.label || "";
  const select = async (next: string) => {
    setOpen(false);
    setSearch("");
    if (next !== value) await onChange(next);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setSearch("");
      }}
    >
      <PopoverTrigger
        aria-label={`${ariaLabel}: ${selectedLabel}`}
        className={
          variant === "form"
            ? "mt-1.5 flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted"
            : "group flex min-w-0 cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 text-xs transition-colors hover:bg-accent"
        }
      >
        <span className={variant === "form" ? "min-w-0 flex-1 truncate font-medium" : "min-w-0 truncate font-mono text-[11px]"}>
          {selectedLabel}
        </span>
        {variant === "form" && (
          <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`} />
        )}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[var(--anchor-width)] min-w-56 gap-0 overflow-hidden p-0">
        <div className="border-b border-border p-2">
          <Input
            autoFocus
            aria-label={searchPlaceholder}
            placeholder={searchPlaceholder}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="h-8"
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {filtered.map((option) => (
            <button
              type="button"
              key={option.value || "runtime-default"}
              onClick={() => void select(option.value)}
              className={`flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors ${option.value === value ? "bg-accent" : "hover:bg-accent/50"}`}
            >
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{option.label}</span>
                {option.description && (
                  <span className="mt-0.5 block text-xs text-muted-foreground">{option.description}</span>
                )}
              </span>
              {option.value === value && <Check className="h-4 w-4 shrink-0 text-primary" />}
            </button>
          ))}
          {defaultOption && (
            <button
              type="button"
              title={defaultOptionTitle}
              onClick={() => void select(defaultOption.value)}
              className="mt-1 flex w-full items-center border-t border-border px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50"
            >
              {defaultOption.label}
            </button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
