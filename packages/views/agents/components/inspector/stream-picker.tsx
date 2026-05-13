"use client";

import { useState } from "react";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";
import { useT } from "../../../i18n";

export type StreamMode = "on" | "off";

const STREAM_MODES: StreamMode[] = ["on", "off"];

export function StreamPicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: StreamMode;
  canEdit?: boolean;
  onChange: (next: StreamMode) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  const label = t(($) => $.inspector.stream_value[value]);
  const tooltip = t(($) => $.inspector.stream_tooltip, { value: label });

  if (!canEdit) {
    return (
      <span className="truncate px-1.5 py-0.5 text-xs text-muted-foreground">
        {label}
      </span>
    );
  }

  const select = async (next: StreamMode) => {
    setOpen(false);
    if (next !== value) await onChange(next);
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[14rem]"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={<span className="truncate">{label}</span>}
    >
      {STREAM_MODES.map((mode) => (
        <PickerItem
          key={mode}
          selected={mode === value}
          onClick={() => void select(mode)}
        >
          <div className="text-left">
            <div className="font-medium">
              {t(($) => $.inspector.stream_value[mode])}
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.inspector.stream_description[mode])}
            </div>
          </div>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
