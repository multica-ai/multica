"use client";

import { useState } from "react";
import { Globe, Lock } from "lucide-react";
import type { AgentVisibility } from "@multica/core/types";
import { useT } from "@multica/i18n/react";
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
  const t = useT("agents");
  const Icon = value === "private" ? Lock : Globe;
  const label =
    value === "private"
      ? t("visibility_private_label")
      : t("visibility_workspace_label");
  const tooltip =
    value === "private"
      ? t("visibility_tooltip_private")
      : t("visibility_tooltip_workspace");

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
          <div className="font-medium">{t("visibility_workspace_label")}</div>
          <div className="text-xs text-muted-foreground">
            {t("visibility_workspace_desc")}
          </div>
        </div>
      </PickerItem>
      <PickerItem
        selected={value === "private"}
        onClick={() => select("private")}
      >
        <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{t("visibility_private_label")}</div>
          <div className="text-xs text-muted-foreground">
            {t("visibility_private_desc")}
          </div>
        </div>
      </PickerItem>
    </PropertyPicker>
  );
}
