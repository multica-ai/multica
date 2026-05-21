"use client";

import { useState } from "react";
import type { RuntimeModelThinkingLevel } from "@multica/core/types";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { CHIP_CLASS } from "./chip";
import { useT } from "../../../i18n";

/**
 * Per-agent reasoning/effort picker (MUL-2339). Renders only when the
 * current model exposes a non-empty `supported_levels` set — Claude and
 * Codex today; every other provider gets nothing. The catalog is daemon-
 * discovered, so the value/label pairs match each CLI's own UI (`Low`,
 * `Extra high`, …) verbatim; never normalised across providers.
 *
 * The empty string is the "use model default" sentinel and renders as
 * "Default" in the chip, with the discovered `default_level` (when
 * present) badged inside the popover so the user can see what they'll
 * get if they clear.
 */
export function ThinkingPicker({
  value,
  levels,
  defaultLevel,
  canEdit = true,
  onChange,
}: {
  /** Persisted thinking_level — "" means "use model default". */
  value: string;
  /** Supported levels for the current (runtime, model) pair. Caller has
   *  already verified the list is non-empty before mounting this picker. */
  levels: RuntimeModelThinkingLevel[];
  /** Level the runtime uses when no override is sent. Surfaced as a badge
   *  in the popover. */
  defaultLevel?: string;
  /** When false, render a static read-only display and skip the popover. */
  canEdit?: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  const selected = value ? levels.find((l) => l.value === value) : undefined;
  // Unknown-but-set value (model swap that dropped the option, CLI upgrade
  // that trimmed the catalog): show the raw token so the user can see what
  // is actually persisted and clear it, rather than silently labelling it
  // "Default" when the backend would still send the stale value.
  const triggerLabel = selected
    ? selected.label
    : value || t(($) => $.pickers.thinking_default);
  const triggerTitle = t(($) => $.pickers.thinking_tooltip, {
    value: triggerLabel,
  });

  const select = async (next: string) => {
    setOpen(false);
    if (next !== value) await onChange(next);
  };

  if (!canEdit) {
    return (
      <span
        className="min-w-0 truncate px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
        title={triggerTitle}
      >
        {triggerLabel}
      </span>
    );
  }

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[14rem] max-w-md"
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className={CHIP_CLASS}
          aria-label={triggerTitle}
        />
      }
      trigger={
        <span className="min-w-0 truncate font-mono text-[11px]">
          {triggerLabel}
        </span>
      }
    >
      {levels.map((l) => (
        <PickerItem
          key={l.value}
          selected={l.value === value}
          onClick={() => void select(l.value)}
          tooltip={l.description || (l.label !== l.value ? `${l.label} · ${l.value}` : l.value)}
        >
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate font-medium">{l.label}</span>
              {l.value === defaultLevel && (
                <span className="shrink-0 rounded bg-primary/10 px-1 text-[10px] font-medium text-primary">
                  {t(($) => $.pickers.thinking_default_badge)}
                </span>
              )}
            </div>
            {l.description && (
              <div className="truncate text-[10px] text-muted-foreground">
                {l.description}
              </div>
            )}
          </div>
        </PickerItem>
      ))}

      {value && (
        <button
          type="button"
          onClick={() => void select("")}
          className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50"
          title={t(($) => $.pickers.thinking_clear_title)}
        >
          {t(($) => $.pickers.thinking_clear)}
        </button>
      )}
    </PropertyPicker>
  );
}
