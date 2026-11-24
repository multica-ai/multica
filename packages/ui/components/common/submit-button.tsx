"use client";

import type { ReactNode, SyntheticEvent } from "react";
import { ArrowUp, Loader2, Square } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";

interface SubmitButtonProps {
  onClick: () => void;
  disabled?: boolean;
  loading?: boolean;
  /**
   * Blocked on background work the user started (an attachment still
   * uploading) rather than on a missing precondition. Disables the button and
   * marks it `aria-busy`, so a screen reader reaching it with a virtual cursor
   * reads "busy" rather than an unexplained dead control. Pass the reason as
   * `disabledReason` so it stays hoverable and focusable.
   */
  busy?: boolean;
  /**
   * Why the button is disabled; shown as its tooltip instead of `tooltip`.
   * A natively disabled button takes no hover or focus, so with a reason the
   * button is `aria-disabled` and Base UI blocks activation instead. Ignored
   * while `loading`.
   */
  disabledReason?: ReactNode;
  running?: boolean;
  onStop?: () => void;
  /**
   * Tooltip shown over the send button when idle. Pass a string or a node
   * (e.g. `Send · ⌘↵`). Omit to render no tooltip.
   * Callers compose the shortcut hint themselves to keep this component
   * free of `@multica/core` (platform-detection) and i18n imports.
   */
  tooltip?: ReactNode;
  /** Accessible name for the icon-only submit button. */
  ariaLabel?: string;
  /** Tooltip shown over the stop button while a run is in progress. */
  stopTooltip?: ReactNode;
  /** Accessible name for the icon-only stop button. */
  stopAriaLabel?: string;
  /** Applied to the send button, after its own classes. */
  className?: string;
}

/** Keep the composer focused so Send and Stop do not dismiss Android's keyboard. */
function keepFocusInComposer(event: SyntheticEvent<HTMLButtonElement>) {
  event.preventDefault();
}

function SubmitButton({
  onClick,
  disabled,
  loading,
  busy,
  running,
  onStop,
  disabledReason,
  tooltip,
  ariaLabel,
  stopTooltip,
  stopAriaLabel,
  className,
}: SubmitButtonProps) {
  const mode = running ? "stop" : loading || busy ? "loading" : "send";
  const isDisabled = !running && !!(disabled || loading || busy);
  const showReason = !running && isDisabled && !loading && disabledReason != null && disabledReason !== false;
  const iconLayerClass =
    "col-start-1 row-start-1 flex items-center justify-center transition-[opacity,transform] duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none";

  const button = (
    <Button
      size="icon-sm"
      className={cn(
        // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
        "rounded-full",
        showReason && "aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:active:translate-y-0",
        !running && className,
      )}
      disabled={isDisabled}
      focusableWhenDisabled={showReason}
      {...(!running && busy && !showReason ? { "aria-disabled": true } : {})}
      aria-busy={!running && (loading || busy) ? true : undefined}
      onPointerDown={keepFocusInComposer}
      onMouseDown={keepFocusInComposer}
      onClick={running ? onStop : onClick}
      aria-label={running ? stopAriaLabel : ariaLabel}
    >
      <span className="grid place-items-center" aria-hidden="true">
        <span
          className={cn(
            iconLayerClass,
            mode === "stop" ? "scale-100 opacity-100" : "pointer-events-none scale-75 opacity-0",
          )}
        >
          <Square className="fill-current" />
        </span>
        <span
          className={cn(
            iconLayerClass,
            mode === "loading" ? "scale-100 opacity-100" : "pointer-events-none scale-75 opacity-0",
          )}
        >
          {/* `busy` spins too, not just greys out: an upload can be running with
              nothing else on screen to explain the disabled control (a reopened
              composer is still rebuilding its placeholder, or the user deleted
              it). A spinner says "waiting on something" without a hover; the
              tooltip says what. */}
          <Loader2 className={cn(mode === "loading" && "animate-spin motion-reduce:animate-none")} />
        </span>
        <span
          className={cn(
            iconLayerClass,
            mode === "send" ? "scale-100 opacity-100" : "pointer-events-none scale-75 opacity-0",
          )}
        >
          <ArrowUp />
        </span>
      </span>
    </Button>
  );

  const tooltipContent = running ? stopTooltip : showReason ? disabledReason : tooltip;
  if (!tooltipContent) return button;
  return (
    <Tooltip>
      <TooltipTrigger render={button} />
      <TooltipContent side="top">{tooltipContent}</TooltipContent>
    </Tooltip>
  );
}

export { SubmitButton, type SubmitButtonProps };
