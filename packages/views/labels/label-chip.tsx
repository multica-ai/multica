"use client";

import type { CSSProperties } from "react";
import type { Label } from "@multica/core/types";
import { labelTextColor, paletteColor, paletteSlot } from "@multica/core/appearance";
import { X } from "@multica/ui/icons";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useT } from "../i18n";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";

interface LabelChipProps {
  label: Label;
  onRemove?: () => void;
  className?: string;
  /**
   * When true, show the full label name without truncation. Use this in
   * management/edit surfaces where users need to see or verify the exact
   * name. The default (false) truncates at 12rem to keep chips compact in
   * the issue sidebar and future board/list card rows.
   */
  fullName?: boolean;
}

/**
 * Renders a single label as a colored pill. If `onRemove` is provided, shows
 * an × button that calls it. Used in the issue-detail sidebar, the picker,
 * and the management dialog.
 *
 * SECURITY INVARIANT: `LabelChip` sets the color as the `--label-color`
 * custom property (`paletteColor` passes anything that is not a hex through)
 * and paints with `var(--label-color)`, trusting the backend's color format.
 * The backend's `normalizeColor` regex pins the value to
 * `^#?[0-9a-fA-F]{6}$`. A custom property is substituted wherever a theme's
 * CSS reads it, not only in this chip's background, so if that regex ever
 * loosens (named colors, `url(...)`, etc.) this becomes an injection vector
 * into every rule that uses the variable.
 */
export function LabelChip({ label, onRemove, className, fullName }: LabelChipProps) {
  const { t } = useT("labels");
  const textColor = labelTextColor(label.color);
  const nameClass = fullName ? "break-all" : "truncate max-w-[12rem]";
  return (
    <span
      data-slot="label-chip"
      data-mc="label-chip"
      data-color={label.color}
      data-palette={paletteSlot(label.color) ?? undefined}
      // `min-w-0` lets the chip shrink when it is a flex item in a width-capped
      // container (the create toolbar's label pill). Without it the chip keeps
      // its content width and the parent clips it mid-shape; with it the inner
      // name truncates and the chip keeps its rounded ends.
      // Custom properties rather than inline colors, so a theme can restyle the
      // chip from `var(--label-color)` without `!important`.
      className={`inline-flex min-w-0 items-center gap-1 rounded-label bg-(--label-color) px-2 py-0.5 text-caption font-medium text-(--label-foreground) ${className ?? ""}`}
      style={
        {
          "--label-color": paletteColor(label.color),
          "--label-foreground": `var(--label-chip-color, ${textColor})`,
        } as CSSProperties
      }
      // aria-label exposes the full name to screen readers when the span
      // visually truncates; sighted users get it from the hover tooltip,
      // which only opens when the name is actually cut off.
      aria-label={label.name}
    >
      <TruncatedTooltip content={fullName ? null : label.name}>
        <span className={nameClass}>{label.name}</span>
      </TruncatedTooltip>
      {onRemove && (
        <TooltipHint label={t(($) => $.remove_label, { name: label.name })}>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
            // bg-current/20 uses the computed text color so the hover state is
            // visible on both light and dark chip backgrounds. hover:bg-black/10
            // was invisible on darker chips (anything requiring light text).
            // eslint-disable-next-line no-restricted-syntax -- clear button inside a round label tag
            className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full hover:bg-current/20 focus:outline-none focus:ring-1 focus:ring-current"
            aria-label={t(($) => $.remove_label, { name: label.name })}
          >
            <X className="h-2.5 w-2.5" strokeWidth={2.5} />
          </button>
        </TooltipHint>
      )}
    </span>
  );
}
