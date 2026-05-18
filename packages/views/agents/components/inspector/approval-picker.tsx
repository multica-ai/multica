"use client";

import { useState } from "react";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";
import { useT } from "../../../i18n";

export type ApprovalPolicy = "auto" | "prompt" | "deny";

const APPROVAL_POLICIES: ApprovalPolicy[] = ["auto", "prompt", "deny"];

export function ApprovalPicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: ApprovalPolicy;
  canEdit?: boolean;
  onChange: (next: ApprovalPolicy) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  const label = t(($) => $.inspector.approval_value[value]);
  const tooltip = t(($) => $.inspector.approval_tooltip, { value: label });

  if (!canEdit) {
    return (
      <span className="truncate px-1.5 py-0.5 text-xs text-muted-foreground">
        {label}
      </span>
    );
  }

  const select = async (next: ApprovalPolicy) => {
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
      trigger={<span className="truncate">{label}</span>}
    >
      {APPROVAL_POLICIES.map((policy) => (
        <PickerItem
          key={policy}
          selected={policy === value}
          onClick={() => void select(policy)}
        >
          <div className="text-left">
            <div className="font-medium">
              {t(($) => $.inspector.approval_value[policy])}
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.inspector.approval_description[policy])}
            </div>
          </div>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
