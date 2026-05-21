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
 * Empty string is the "no override" sentinel: the backend omits the
 * effort flag entirely and the upstream CLI's own config / built-in
 * default decides what the model runs at. We render that state as
 * "Follow CLI config" rather than singling out one level as the
 * factory default, because the actual default at runtime is owned by
 * the user's local CLI install, not by Multica's catalog.
 */
export function ThinkingPicker({
  value,
  levels,
  canEdit = true,
  onChange,
}: {
  /** Persisted thinking_level — "" means "follow local CLI config". */
  value: string;
  /** Supported levels for the current (runtime, model) pair. Usually
   *  non-empty when the row is shown, but the stale-orphan clear path
   *  in ThinkingPropRow mounts the picker with an empty list plus a
   *  persisted value so the user can see and clear the dangling token. */
  levels: RuntimeModelThinkingLevel[];
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
        >
          {/* PickerItem wraps children in a flex `<span>`. Putting a
              `<div>` inside that <span> is block-in-inline (invalid HTML5)
              and triggers browser quirks that shift descendant x-position.
              Use a `<span>` with explicit `block` + `text-left` so layout
              is deterministic across rows regardless of whether the label
              row has the `default` badge sibling. */}
          {/* No model-factory-default badge here on purpose: when the
              picker is "Follow CLI config" (value === ""), Multica omits
              `--effort` and the local CLI config decides — the model's
              factory default is irrelevant to what actually fires, so
              flagging one option as "default" was misleading. */}
          <span className="block min-w-0 flex-1 text-left">
            <span className="truncate text-[13px] font-medium">{l.label}</span>
            {l.description && (
              <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
                {l.description}
              </span>
            )}
          </span>
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
