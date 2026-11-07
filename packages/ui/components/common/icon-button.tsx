"use client";

import { useId, type ComponentProps, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";

type IconButtonProps = Omit<ComponentProps<typeof Button>, "aria-label"> & {
  /** Names the action: the accessible name and the tooltip text. */
  label: string;
  /**
   * Keycaps shown after the label in the tooltip. Pass decorative keycaps:
   * the accessible name stays `label`.
   */
  shortcut?: ReactNode;
  /**
   * Why the button is disabled. Only read while `disabled` is true; the
   * tooltip shows it in place of the label. A natively disabled button takes
   * no hover or focus, so with a reason the button switches to
   * `aria-disabled` and Base UI blocks activation instead.
   */
  disabledReason?: ReactNode;
  tooltipSide?: ComponentProps<typeof TooltipContent>["side"];
};

/**
 * Icon-only button that always carries a visible tooltip and an accessible
 * name. Works as the `render` target of menu and popover triggers.
 *
 * Mounts a full tooltip root, so prefer `DeferredTooltip` inside rows that
 * repeat hundreds of times.
 */
function IconButton({
  label,
  shortcut,
  disabledReason,
  tooltipSide = "top",
  disabled,
  size = "icon-sm",
  variant = "ghost",
  className,
  children,
  ...props
}: IconButtonProps) {
  const reasonId = useId();
  const showReason = !!disabled && disabledReason != null && disabledReason !== false;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            {...props}
            size={size}
            variant={variant}
            disabled={disabled}
            focusableWhenDisabled={showReason || props.focusableWhenDisabled}
            aria-label={label}
            aria-describedby={showReason ? reasonId : props["aria-describedby"]}
            className={cn(
              // The base only dims native `disabled`; a reasoned button is
              // aria-disabled so it can still hover its tooltip.
              "aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:active:translate-y-0",
              className,
            )}
          />
        }
      >
        {children}
        {showReason ? (
          <span id={reasonId} className="sr-only">
            {disabledReason}
          </span>
        ) : null}
      </TooltipTrigger>
      <TooltipContent side={tooltipSide}>
        {showReason ? disabledReason : (
          <>
            {label}
            {shortcut}
          </>
        )}
      </TooltipContent>
    </Tooltip>
  );
}

export { IconButton, type IconButtonProps };
