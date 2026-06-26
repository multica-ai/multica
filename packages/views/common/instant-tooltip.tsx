import type { ReactNode } from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useFormatInstantTooltip } from "./use-format-date-time";

// Renders the tooltip text lazily. Base UI portals the popup only while open,
// so this child mounts — and thus `compute()` runs — only on hover, keeping the
// expensive Intl offset formatting out of the per-row render path of long lists.
function TooltipText({ compute }: { compute: () => string }) {
  return <>{compute()}</>;
}

interface HoverTooltipProps {
  /** The already-rendered visible content the tooltip hangs off. */
  children: ReactNode;
  /** Tooltip text; invoked only when the tooltip mounts, never every render. */
  content: () => string;
  /** Cheap gate: when false, render the trigger alone with no tooltip. */
  enabled?: boolean;
  className?: string;
  tooltipSide?: "top" | "right" | "bottom" | "left";
}

/**
 * Shared "visible span + hover tooltip" wrapper. Single source for the
 * trigger/Tooltip/TooltipContent structure used by both <DateTime> and
 * <InstantTooltip>, so hover behavior (delay, side, empty-skip, a11y) stays
 * consistent across every time display.
 */
export function HoverTooltip({
  children,
  content,
  enabled = true,
  className,
  tooltipSide = "top",
}: HoverTooltipProps) {
  const trigger = <span className={className}>{children}</span>;
  if (!enabled) return trigger;
  return (
    <Tooltip>
      <TooltipTrigger render={trigger} />
      <TooltipContent side={tooltipSide}>
        <TooltipText compute={content} />
      </TooltipContent>
    </Tooltip>
  );
}

interface InstantTooltipProps {
  /** The instant whose full time + GMT offset the tooltip reveals. */
  value: string | null | undefined;
  /** The already-rendered, localized phrase to show (e.g. "Updated 2d ago"). */
  children: ReactNode;
  className?: string;
  tooltipSide?: "top" | "right" | "bottom" | "left";
}

/**
 * Wraps an already-rendered, localized phrase in a tooltip carrying the full
 * timestamp + GMT offset of `value` (in the viewer's Viewing Timezone). Use
 * when the instant is interpolated INTO a translated sentence and can't be
 * swapped for a bare <DateTime> without breaking per-locale word order — the
 * visible text stays whatever the caller rendered; only the hover is added.
 * Renders the children untouched (no tooltip) when value is empty.
 */
export function InstantTooltip({
  value,
  children,
  className,
  tooltipSide = "top",
}: InstantTooltipProps) {
  const formatTooltip = useFormatInstantTooltip();
  return (
    <HoverTooltip
      className={className}
      tooltipSide={tooltipSide}
      enabled={value != null}
      content={() => formatTooltip(value)}
    >
      {children}
    </HoverTooltip>
  );
}
