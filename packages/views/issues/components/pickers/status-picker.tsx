"use client";

import { useState } from "react";
import type { IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { ALL_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import { StatusIcon } from "../status-icon";
import { PropertyPicker, PickerItem } from "./property-picker";
import { PollingSetupDialog } from "../polling-setup-dialog";
import { useT } from "../../../i18n";

export function StatusPicker({
  status,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  pollIntervalMinutes,
}: {
  status: IssueStatus;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  pollIntervalMinutes?: number | null;
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const [pollingDialogOpen, setPollingDialogOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const { t } = useT("issues");

  const handleStatusSelect = (s: IssueStatus) => {
    if (s === "polling") {
      // If already has poll config, re-enable directly.
      if (pollIntervalMinutes != null) {
        onUpdate({ status: "polling" });
        setOpen(false);
      } else {
        // No config yet — show the setup dialog.
        setOpen(false);
        setPollingDialogOpen(true);
      }
    } else {
      onUpdate({ status: s });
      setOpen(false);
    }
  };

  const handlePollingConfirm = (intervalMinutes: number) => {
    onUpdate({ status: "polling", poll_interval_minutes: intervalMinutes });
  };

  return (
    <>
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
              <span className="truncate">{t(($) => $.status[status])}</span>
            </>
          )
        }
      >
        {ALL_STATUSES.map((s) => {
          const c = STATUS_CONFIG[s];
          return (
            <PickerItem
              key={s}
              selected={s === status}
              hoverClassName={c.hoverBg}
              onClick={() => handleStatusSelect(s)}
            >
              <StatusIcon status={s} className="h-3.5 w-3.5" />
              <span>{t(($) => $.status[s])}</span>
            </PickerItem>
          );
        })}
      </PropertyPicker>

      <PollingSetupDialog
        open={pollingDialogOpen}
        onOpenChange={setPollingDialogOpen}
        onConfirm={handlePollingConfirm}
        defaultInterval={pollIntervalMinutes}
      />
    </>
  );
}
