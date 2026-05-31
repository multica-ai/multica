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
  // CEREBRO-PATCH(status-picker-v2b): FIR-1550 current pinned custom_status_key (if any).
  customStatusKey,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  status: IssueStatus;
  customStatusKey?: string | null;
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

  // v2b: when the project has a model, render every custom status (no dedup
  // by base) plus the uncovered base statuses underneath. Picking a custom
  // status sends both `status` (its base) and `custom_status_key` so the
  // backend resolver pins the sidecar end-to-end.
  const customRows = pres.customStatuses ?? [];
  const triggerColor = pres.getColor(status);
  const triggerLabel = (() => {
    if (customStatusKey && pres.getCustomByKey) {
      const e = pres.getCustomByKey(customStatusKey);
      if (e) return e.label;
    }
    return pres.getLabel(status) ?? t(($) => $.status[status]);
  })();
  const uncoveredBases = useMemo<IssueStatus[]>(() => {
    if (!pres.orderedBaseStatuses) return ALL_STATUSES;
    const covered = new Set(pres.orderedBaseStatuses);
    return ALL_STATUSES.filter((s) => !covered.has(s));
  }, [pres.orderedBaseStatuses]);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-48"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ?? (
          <>
            {/* CEREBRO-PATCH(status-picker-model): model color + label on the trigger */}
            <span style={triggerColor ? { color: triggerColor } : undefined}>
              <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" inheritColor={!!triggerColor} />
            </span>
            <span className="truncate">{triggerLabel}</span>
          </>
        )
      }
    >
      {customRows.map((row) => {
        const c = STATUS_CONFIG[row.baseStatus];
        const selected = customStatusKey === row.key;
        const color = row.color;
        return (
          <PickerItem
            key={`custom:${row.key}`}
            selected={selected}
            hoverClassName={c.hoverBg}
            onClick={() => {
              onUpdate({ status: row.baseStatus, custom_status_key: row.key });
              setOpen(false);
            }}
          >
            <span style={color ? { color } : undefined}>
              <StatusIcon status={row.baseStatus} className="h-3.5 w-3.5" inheritColor={!!color} />
            </span>
            <span className="truncate">{row.label}</span>
          </PickerItem>
        );
      })}
      {uncoveredBases.map((s) => {
        const c = STATUS_CONFIG[s];
        const optColor = pres.getColor(s);
        const selected = !customStatusKey && s === status;
        return (
          <PickerItem
            key={`base:${s}`}
            selected={selected}
            hoverClassName={c.hoverBg}
            onClick={() => {
              // No explicit pin: auto-resolver picks the first matching
              // custom status server-side (or clears the sidecar when no
              // custom maps to this base / no model on project).
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
