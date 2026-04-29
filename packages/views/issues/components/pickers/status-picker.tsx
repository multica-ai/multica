"use client";

import { useState } from "react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";

export function StatusPicker({
  status,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  showSelection = true,
}: {
  status: IssueStatus;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  showSelection?: boolean;
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const cfg = STATUS_CONFIG[status];

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
            <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{cfg.label}</span>
          </>
        )
      }
    >
      {ALL_STATUSES.map((s) => {
        const c = STATUS_CONFIG[s];
        return (
          <PickerItem
            key={s}
            selected={showSelection && s === status}
            hoverClassName={c.hoverBg}
            onClick={() => {
              onUpdate({ status: s });
              setOpen(false);
            }}
          >
            <StatusIcon status={s} className="h-3.5 w-3.5" />
            <span>{c.label}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
