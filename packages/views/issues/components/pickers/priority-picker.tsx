"use client";

import { useState } from "react";
import type { IssuePriority, UpdateIssueRequest } from "@multica/core/types";
import { PRIORITY_ORDER, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { PriorityIcon } from "../priority-icon";
import { PropertyPicker, PickerItem } from "./property-picker";

export function PriorityPicker({
  priority,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  priority: IssuePriority;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const cfg = PRIORITY_CONFIG[priority];

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-44"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ?? (
          <>
            <PriorityIcon priority={priority} className="shrink-0" />
            <span className="truncate">{cfg.label}</span>
          </>
        )
      }
    >
      {PRIORITY_ORDER.map((p) => {
        const c = PRIORITY_CONFIG[p];
        return (
          <PickerItem
            key={p}
            selected={p === priority}
            onClick={() => {
              onUpdate({ priority: p });
              setOpen(false);
            }}
          >
            <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${c.badgeBg} ${c.badgeText}`}>
              <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
              {c.label}
            </span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
