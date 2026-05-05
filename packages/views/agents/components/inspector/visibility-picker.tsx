"use client";

import { useState } from "react";
import { Globe, Lock } from "lucide-react";
import {
  VISIBILITY_DESCRIPTION,
  VISIBILITY_LABEL,
  VISIBILITY_TOOLTIP,
} from "@multica/core/agents";
import type { AgentVisibility } from "@multica/core/types";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { VisibilityBadge } from "../visibility-badge";
import { CHIP_CLASS } from "./chip";

export function VisibilityPicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: AgentVisibility;
  /** When false, render a read-only `<VisibilityBadge>` and skip the popover. */
  canEdit?: boolean;
  onChange: (next: AgentVisibility) => Promise<void> | void;
}) {
  const [open, setOpen] = useState(false);

  if (!canEdit) {
    return <VisibilityBadge value={value} />;
  }

  const Icon = value === "private" ? Lock : Globe;
  const label = VISIBILITY_LABEL[value];
  const tooltip = `Visibility · ${VISIBILITY_TOOLTIP[value]}`;

  const select = async (next: AgentVisibility) => {
    setOpen(false);
    if (next !== value) await onChange(next);
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[12rem]"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={
        <>
          <Icon className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{label}</span>
        </>
      }
    >
      <PickerItem
        selected={value === "workspace"}
        onClick={() => select("workspace")}
      >
        <Globe className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{VISIBILITY_LABEL.workspace}</div>
          <div className="text-xs text-muted-foreground">
            {VISIBILITY_DESCRIPTION.workspace}
          </div>
        </div>
      </PickerItem>
      <PickerItem
        selected={value === "private"}
        onClick={() => select("private")}
      >
        <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{VISIBILITY_LABEL.private}</div>
          <div className="text-xs text-muted-foreground">
            {VISIBILITY_DESCRIPTION.private}
          </div>
        </div>
      </PickerItem>
    </PropertyPicker>
  );
}
