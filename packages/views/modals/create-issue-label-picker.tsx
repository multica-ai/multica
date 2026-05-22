"use client";

import { useState } from "react";
import { Check, Tag } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import type { Label } from "@multica/core/types";
import { PillButton } from "../common/pill-button";

interface CreateIssueLabelPickerProps {
  labels: Label[];
  selectedIds: string[];
  onChange: (ids: string[]) => void;
  align?: "start" | "center" | "end";
}

export function CreateIssueLabelPicker({
  labels,
  selectedIds,
  onChange,
  align = "start",
}: CreateIssueLabelPickerProps) {
  const [open, setOpen] = useState(false);

  const toggle = (id: string) => {
    onChange(
      selectedIds.includes(id)
        ? selectedIds.filter((x) => x !== id)
        : [...selectedIds, id],
    );
  };

  const selectedLabels = labels.filter((l) => selectedIds.includes(l.id));

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <PillButton>
            <Tag className="size-3 text-muted-foreground" />
            {selectedLabels.length > 0 ? (
              <span className="flex items-center gap-1">
                {selectedLabels.slice(0, 3).map((l) => (
                  <span
                    key={l.id}
                    className="inline-flex items-center gap-1 text-xs"
                  >
                    <span
                      className="size-2 rounded-full shrink-0"
                      style={{ backgroundColor: l.color }}
                    />
                    {l.name}
                  </span>
                ))}
                {selectedLabels.length > 3 && (
                  <span className="text-xs text-muted-foreground">
                    +{selectedLabels.length - 3}
                  </span>
                )}
              </span>
            ) : (
              <span className="text-muted-foreground">Label</span>
            )}
          </PillButton>
        }
      />
      <PopoverContent className="w-60 p-0" align={align}>
        <Command>
          <CommandInput placeholder="Search labels..." />
          <CommandList>
            <CommandEmpty>No labels found.</CommandEmpty>
            <CommandGroup>
              {labels.map((label) => (
                <CommandItem
                  key={label.id}
                  value={label.name}
                  onSelect={() => toggle(label.id)}
                >
                  <span
                    className="size-3 rounded-full shrink-0 mr-2"
                    style={{ backgroundColor: label.color }}
                  />
                  <span className="flex-1 truncate">{label.name}</span>
                  {selectedIds.includes(label.id) && (
                    <Check className={cn("size-3.5 ml-auto")} />
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
