"use client";

import { useState } from "react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
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
  /**
   * The currently-selected status, used to check the matching row. `null`
   * means "no single current value" (e.g. a batch selection spanning several
   * statuses) — no row is checked. Single-issue callers always pass a concrete
   * status.
   */
  status: IssueStatus | null;
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

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-44"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ??
        (status != null ? (
          <>
            <StatusIcon status={status} className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{t(($) => $.status[status])}</span>
          </>
        ) : null)
      }
    >
      {ALL_STATUSES.map((s) => {
        const c = STATUS_CONFIG[s];
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
            <StatusIcon status={s} className="h-3.5 w-3.5" />
            <span>{t(($) => $.status[s])}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
