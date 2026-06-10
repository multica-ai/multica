"use client";

import { useState } from "react";
import type { AgentServiceTier } from "@multica/core/types";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { useT } from "../../../i18n";
import { CHIP_CLASS } from "./chip";

const SERVICE_TIERS: AgentServiceTier[] = ["", "default", "fast"];

export function ServiceTierPicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: AgentServiceTier | string | undefined;
  canEdit?: boolean;
  onChange: (next: AgentServiceTier) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const current = normalizeServiceTier(value);
  const label = t(($) => $.inspector.service_tier_value[current]);
  const tooltip = t(($) => $.inspector.service_tier_tooltip, { value: label });

  const select = async (next: AgentServiceTier) => {
    setOpen(false);
    if (next !== current) await onChange(next);
  };

  if (!canEdit) {
    return (
      <span
        className="min-w-0 truncate px-1.5 py-0.5 text-xs text-muted-foreground"
        title={tooltip}
      >
        {label}
      </span>
    );
  }

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[15rem] max-w-md"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={<span className="min-w-0 truncate">{label}</span>}
    >
      {SERVICE_TIERS.map((tier) => (
        <PickerItem
          key={tier || "local"}
          selected={tier === current}
          onClick={() => void select(tier)}
        >
          <span className="block min-w-0 flex-1 text-left">
            <span className="block truncate text-[13px] font-medium">
              {t(($) => $.inspector.service_tier_value[tier])}
            </span>
            <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
              {t(($) => $.inspector.service_tier_description[tier])}
            </span>
          </span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}

function normalizeServiceTier(
  value: AgentServiceTier | string | undefined,
): AgentServiceTier {
  if (value === "default" || value === "fast") return value;
  return "";
}
