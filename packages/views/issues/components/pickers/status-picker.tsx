"use client";

import { useState } from "react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { useWorkspaceStatuses } from "@multica/core/issues/workspace-statuses";
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
}: {
  status: IssueStatus;
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
  const { statuses: workspaceStatuses, configMap } = useWorkspaceStatuses();

  // Determine the list of statuses to render.
  // If workspace statuses were fetched (non-default), use them. Otherwise
  // fall back to the built-in ALL_STATUSES for backward compatibility.
  const statusList =
    workspaceStatuses.length > 0
      ? workspaceStatuses
      : ALL_STATUSES.map((s) => ({
          name: s,
          label: STATUS_CONFIG[s]?.label ?? s,
          color: "",
          category: "not_started" as const,
          position: 0,
          isDefault: true,
        }));

  // Resolve the current status label — prefer workspace definition, then
  // i18n, then the raw name as ultimate fallback.
  const currentDef = statusList.find((d) => d.name === status);
  const currentLabel = currentDef?.label ?? (STATUS_CONFIG[status]?.label) ?? status;
  const currentColor = currentDef?.color;

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
            <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" color={currentColor} />
            <span className="truncate">{currentLabel}</span>
          </>
        )
      }
    >
      {statusList.map((def) => {
        const c = configMap[def.name];
        return (
          <PickerItem
            key={def.name}
            selected={def.name === status}
            hoverClassName={c?.hoverBg ?? "hover:bg-accent"}
            onClick={() => {
              onUpdate({ status: def.name });
              setOpen(false);
            }}
          >
            <StatusIcon status={def.name} className="h-3.5 w-3.5" color={def.color} />
            <span>{def.label}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
