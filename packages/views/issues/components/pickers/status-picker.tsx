"use client";

import { useMemo, useState } from "react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
// CEREBRO-PATCH(status-picker-model): FIR-1550 status-model options/labels/colors
import { useStatusPresentation } from "@multica/cerebro-status-models/views";
import { useT } from "../../../i18n";

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
  const { t } = useT("issues");
  // CEREBRO-PATCH(status-picker-model): model statuses first, uncovered bases fall back to upstream
  const pres = useStatusPresentation();
  const triggerColor = pres.getColor(status);
  const statusOptions = useMemo<IssueStatus[]>(() => {
    if (!pres.orderedBaseStatuses) return ALL_STATUSES;
    const covered = new Set(pres.orderedBaseStatuses);
    return [...pres.orderedBaseStatuses, ...ALL_STATUSES.filter((s) => !covered.has(s))];
  }, [pres.orderedBaseStatuses]);

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
            {/* CEREBRO-PATCH(status-picker-model): model color + label on the trigger */}
            <span style={triggerColor ? { color: triggerColor } : undefined}>
              <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" inheritColor={!!triggerColor} />
            </span>
            <span className="truncate">{pres.getLabel(status) ?? t(($) => $.status[status])}</span>
          </>
        )
      }
    >
      {statusOptions.map((s) => {
        const c = STATUS_CONFIG[s];
        // CEREBRO-PATCH(status-picker-model): model color + label per option
        const optColor = pres.getColor(s);
        return (
          <PickerItem
            key={s}
            selected={s === status}
            hoverClassName={c.hoverBg}
            onClick={() => {
              onUpdate({ status: s });
              setOpen(false);
            }}
          >
            <span style={optColor ? { color: optColor } : undefined}>
              <StatusIcon status={s} className="h-3.5 w-3.5" inheritColor={!!optColor} />
            </span>
            <span>{pres.getLabel(s) ?? t(($) => $.status[s])}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
