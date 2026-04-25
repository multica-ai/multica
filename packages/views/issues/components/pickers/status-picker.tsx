"use client";

import { useState } from "react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, getStatusConfig } from "@multica/core/issues/config";
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
  pipelineColumns,
}: {
  status: string;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  pipelineColumns?: { status_key: string; label: string }[];
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const cfg = getStatusConfig(status);
  const displayLabel = pipelineColumns?.find((c) => c.status_key === status)?.label ?? cfg.label;

  const filteredItems = pipelineColumns
    ? pipelineColumns.map((c) => ({ status_key: c.status_key, label: c.label }))
    : ALL_STATUSES.map((s) => ({ status_key: s, label: getStatusConfig(s).label }));

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
            <span className="truncate">{displayLabel}</span>
          </>
        )
      }
    >
      {filteredItems.map((item) => {
        const c = getStatusConfig(item.status_key);
        return (
          <PickerItem
            key={item.status_key}
            selected={item.status_key === status}
            hoverClassName={c.hoverBg}
            onClick={() => {
              onUpdate({ status: item.status_key });
              setOpen(false);
            }}
          >
            <StatusIcon status={item.status_key} className="h-3.5 w-3.5" />
            <span>{item.label}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
