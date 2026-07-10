"use client";

import { useEffect, useId, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { PropertyPicker } from "../../../issues/components/pickers";
import { useT } from "../../../i18n";
import { CHIP_CLASS } from "./chip";

function minutesFromSeconds(valueSeconds: number | null | undefined): number | null {
  if (typeof valueSeconds !== "number" || valueSeconds <= 0) return null;
  return Math.round(valueSeconds / 60);
}

export function WaitTimeoutPicker({
  valueSeconds,
  canEdit = true,
  onChange,
}: {
  valueSeconds: number | null | undefined;
  canEdit?: boolean;
  onChange: (nextSeconds: number) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const inputId = useId();
  const [open, setOpen] = useState(false);
  const currentMinutes = minutesFromSeconds(valueSeconds);
  const currentDraft = currentMinutes == null ? "" : String(currentMinutes);
  const [draft, setDraft] = useState(currentDraft);
  const [openedDraft, setOpenedDraft] = useState(currentDraft);

  useEffect(() => {
    if (open) {
      setDraft(currentDraft);
      setOpenedDraft(currentDraft);
    }
  }, [currentDraft, open]);

  const displayValue =
    currentMinutes == null
      ? t(($) => $.pickers.wait_timeout_global)
      : t(($) => $.pickers.wait_timeout_minutes_value, {
          value: currentMinutes,
        });

  const tooltip = t(($) => $.pickers.wait_timeout_tooltip, {
    value: displayValue,
  });

  if (!canEdit) {
    return (
      <span className="font-mono text-xs tabular-nums text-muted-foreground">
        {displayValue}
      </span>
    );
  }

  const commit = async () => {
    const trimmed = draft.trim();
    const minutes = trimmed === "" ? 0 : Number(trimmed);
    if (!Number.isFinite(minutes) || minutes < 0 || !Number.isInteger(minutes)) {
      return;
    }

    const nextSeconds = minutes === 0 ? 0 : minutes * 60;

    setOpen(false);
    if (trimmed === openedDraft.trim()) {
      return;
    }

    if (nextSeconds !== (valueSeconds && valueSeconds > 0 ? valueSeconds : 0)) {
      await onChange(nextSeconds);
    }
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-[22rem]"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={<span className="font-mono tabular-nums">{displayValue}</span>}
    >
      <div className="space-y-2 p-2">
        <p className="text-xs leading-relaxed text-muted-foreground">
          {t(($) => $.pickers.wait_timeout_helper)}
        </p>
        <label htmlFor={inputId} className="block text-xs font-medium text-foreground">
          {t(($) => $.pickers.wait_timeout_input_label)}
        </label>
        <div className="flex items-center gap-2">
          <Input
            id={inputId}
            type="number"
            min={0}
            step={1}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void commit();
              }
            }}
            autoFocus
            aria-label={t(($) => $.pickers.wait_timeout_input_label)}
            className="h-8 w-24 font-mono text-xs"
          />
          <Button size="sm" onClick={() => void commit()}>
            {t(($) => $.inspector.save)}
          </Button>
        </div>
      </div>
    </PropertyPicker>
  );
}
