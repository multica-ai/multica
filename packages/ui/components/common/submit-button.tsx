"use client";

import type { ReactNode } from "react";
import { ArrowUp, Loader2, Square } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

interface SubmitButtonProps {
  onClick: () => void;
  disabled?: boolean;
  loading?: boolean;
  /**
   * Blocked on background work the user started (an attachment still
   * uploading) rather than on a missing precondition. Disables the button and
   * marks it `aria-busy`, so a screen reader reaching it with a virtual cursor
   * reads "busy" rather than an unexplained dead control. It is NOT tab
   * reachable while disabled, so callers should also carry the reason in
   * `ariaLabel` / `tooltip` and in the visible label where there is one.
   */
  busy?: boolean;
  running?: boolean;
  onStop?: () => void;
  /** Keep a send control beside Stop so callers can enqueue follow-up work. */
  allowSubmitWhileRunning?: boolean;
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
}

function SubmitButton({
  onClick,
  disabled,
  loading,
  busy,
  running,
  onStop,
  allowSubmitWhileRunning,
  tooltip,
  ariaLabel,
  stopTooltip,
  stopAriaLabel,
}: SubmitButtonProps) {
  let stopControl: ReactNode = null;
  if (running) {
    const stopButton = (
      <Button
        size="icon-sm"
        className="rounded-full"
        onClick={onStop}
        aria-label={stopAriaLabel}
      >
        <Square className="fill-current" aria-hidden="true" />
      </Button>
    );
    stopControl = stopTooltip ? (
      <Tooltip>
        <TooltipTrigger render={stopButton} />
        <TooltipContent side="top">{stopTooltip}</TooltipContent>
      </Tooltip>
    ) : stopButton;
    if (!allowSubmitWhileRunning) return stopControl;
  }

  const submitButton = (
    <Button
      size="icon-sm"
      className="rounded-full"
      disabled={disabled || loading || busy}
      aria-disabled={busy || undefined}
      aria-busy={loading || busy || undefined}
      onClick={onClick}
      aria-label={ariaLabel}
    >
      {loading || busy ? (
        // `busy` spins too, not just greys out: an upload can be running with
        // nothing else on screen to explain the disabled control (a reopened
        // composer is still rebuilding its placeholder, or the user deleted
        // it). A spinner says "waiting on something" without a hover; the
        // tooltip says what.
        <Loader2 className="animate-spin" aria-hidden="true" />
      ) : (
        <ArrowUp aria-hidden="true" />
      )}
    </Button>
  );
  const submitControl = !tooltip
    ? submitButton
    : (
        <Tooltip>
          <TooltipTrigger render={submitButton} />
          <TooltipContent side="top">{tooltip}</TooltipContent>
        </Tooltip>
      );
  if (!stopControl) return submitControl;
  return (
    <>
      {stopControl}
      {submitControl}
    </>
  );
}

export { SubmitButton, type SubmitButtonProps };
