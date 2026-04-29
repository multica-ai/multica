"use client";

import { useState } from "react";
import { Globe, Lock } from "lucide-react";
import type { AgentVisibility } from "@multica/core/types";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";

export function VisibilityPicker({
  value,
  onChange,
}: {
  value: AgentVisibility;
  onChange: (next: AgentVisibility) => Promise<void> | void;
}) {
  const [open, setOpen] = useState(false);
  const Icon = value === "private" ? Lock : Globe;
  const label = value === "private" ? "Private" : "Workspace";
  const tooltip =
    value === "private"
      ? "Visibility · Private — only you can assign"
      : "Visibility · Workspace — all members can assign";

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
          <div className="font-medium">Workspace</div>
          <div className="text-xs text-muted-foreground">
            All members can assign
          </div>
        </div>
      </PickerItem>
      <PickerItem
        selected={value === "private"}
        onClick={() => select("private")}
      >
        <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">Private</div>
          <div className="text-xs text-muted-foreground">
            Only you can assign
          </div>
        </div>
      </PickerItem>
    </PropertyPicker>
  );
}
