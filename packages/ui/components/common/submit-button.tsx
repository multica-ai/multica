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
  running?: boolean;
  onStop?: () => void;
  allowSubmitWhileRunning?: boolean;
  "data-acceptance"?: string;
  /**
   * Tooltip shown over the send button when idle. Pass a string or a node
   * (e.g. `Send · ⌘↵`). Omit to render no tooltip.
   * Callers compose the shortcut hint themselves to keep this component
   * free of `@multica/core` (platform-detection) and i18n imports.
   */
  tooltip?: ReactNode;
  /** Tooltip shown over the stop button while a run is in progress. */
  stopTooltip?: ReactNode;
}

function SubmitButton({
  onClick,
  disabled,
  loading,
  running,
  onStop,
  allowSubmitWhileRunning,
  "data-acceptance": dataAcceptance,
  tooltip,
  stopTooltip,
}: SubmitButtonProps) {
  if (running) {
    const stopButton = (
      <Button size="icon-sm" onClick={onStop} data-acceptance={dataAcceptance ? `${dataAcceptance}-stop` : undefined}>
        <Square className="fill-current" />
      </Button>
    );
    const stopWithTooltip = stopTooltip ? (
      <Tooltip>
        <TooltipTrigger render={stopButton} />
        <TooltipContent side="top">{stopTooltip}</TooltipContent>
      </Tooltip>
    ) : stopButton;
    if (!allowSubmitWhileRunning) return stopWithTooltip;

    const submitButton = (
      <Button size="icon-sm" disabled={disabled || loading} onClick={onClick} data-acceptance={dataAcceptance}>
        {loading ? <Loader2 className="animate-spin" /> : <ArrowUp />}
      </Button>
    );
    const sendWithTooltip = tooltip ? (
      <Tooltip>
        <TooltipTrigger render={submitButton} />
        <TooltipContent side="top">{tooltip}</TooltipContent>
      </Tooltip>
    ) : submitButton;

    return (
      <>
        {stopWithTooltip}
        {sendWithTooltip}
      </>
    );
  }

  const submitButton = (
    <Button size="icon-sm" disabled={disabled || loading} onClick={onClick} data-acceptance={dataAcceptance}>
      {loading ? <Loader2 className="animate-spin" /> : <ArrowUp />}
    </Button>
  );
  if (!tooltip) return submitButton;
  return (
    <Tooltip>
      <TooltipTrigger render={submitButton} />
      <TooltipContent side="top">{tooltip}</TooltipContent>
    </Tooltip>
  );
}

export { SubmitButton, type SubmitButtonProps };
