"use client";

import { useEffect, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { PropertyPicker } from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";
import { useT } from "../../../i18n";

export function HermesProfilePicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: string | null;
  /** When false, render a static read-only display and skip the popover. */
  canEdit?: boolean;
  onChange: (next: string | null) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(value ?? "");

  useEffect(() => {
    if (open) setDraft(value ?? "");
  }, [open, value]);

  if (!canEdit) {
    if (!value) {
      return (
        <span className="text-xs italic text-muted-foreground">
          {t(($) => $.pickers.hermes_profile_default)}
        </span>
      );
    }
    return (
      <span className="font-mono text-xs text-muted-foreground">{value}</span>
    );
  }

  const commit = async () => {
    setOpen(false);
    const trimmed = draft.trim();
    if (trimmed === (value ?? "")) return;
    await onChange(trimmed || null);
  };

  const displayValue = value ?? t(($) => $.pickers.hermes_profile_default);
  const tooltip = t(($) => $.pickers.hermes_profile_tooltip, {
    value: displayValue,
  });

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[16rem]"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={<span className="truncate font-mono text-xs">{displayValue}</span>}
    >
      <div className="space-y-2 p-2">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.pickers.hermes_profile_description)}
        </p>
        <div className="flex items-center gap-2">
          <Input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void commit();
              }
            }}
            autoFocus
            placeholder={t(($) => $.pickers.hermes_profile_placeholder)}
            className="h-8 w-40 font-mono text-xs"
          />
          <Button size="sm" onClick={() => void commit()}>
            {t(($) => $.inspector.save)}
          </Button>
        </div>
      </div>
    </PropertyPicker>
  );
}
