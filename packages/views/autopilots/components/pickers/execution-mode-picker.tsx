"use client";

import { useState } from "react";
import { FilePlus2, Play } from "lucide-react";
import type { AutopilotExecutionMode } from "@multica/core/types";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import {
  PropertyPicker,
  PickerItem,
} from "../../../issues/components/pickers/property-picker";

const OPTIONS: { value: AutopilotExecutionMode; label: string; description: string; Icon: typeof FilePlus2 }[] = [
  { value: "create_issue", label: "Create Issue", description: "File an issue with the agent assigned", Icon: FilePlus2 },
  { value: "run_only", label: "Run Only", description: "Run the agent without creating an issue", Icon: Play },
];

export function ExecutionModePicker({
  mode,
  onChange,
  trigger: customTrigger,
  triggerRender,
  align = "start",
}: {
  mode: AutopilotExecutionMode;
  onChange: (mode: AutopilotExecutionMode) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
}) {
  const [open, setOpen] = useState(false);
  const current = OPTIONS.find((o) => o.value === mode) ?? OPTIONS[0]!;
  const CurrentIcon = current.Icon;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-52"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ?? (
          <>
            <CurrentIcon className="size-3 shrink-0" />
            <span className="truncate">{current.label}</span>
          </>
        )
      }
    >
      {OPTIONS.map((o) => {
        const Icon = o.Icon;
        return (
          <Tooltip key={o.value}>
            <TooltipTrigger
              render={
                <PickerItem
                  selected={o.value === mode}
                  onClick={() => {
                    onChange(o.value);
                    setOpen(false);
                  }}
                >
                  <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                  <span>{o.label}</span>
                </PickerItem>
              }
            />
            <TooltipContent side="right" sideOffset={8}>
              {o.description}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </PropertyPicker>
  );
}
