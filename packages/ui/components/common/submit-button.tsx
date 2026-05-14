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
  tooltip,
  stopTooltip,
}: SubmitButtonProps) {
  if (running) {
    const stopButton = (
      <Button size="icon-sm" onClick={onStop}>
        <Square className="fill-current" />
      </Button>
    );
    if (!stopTooltip) return stopButton;
    return (
      <Tooltip>
        <TooltipTrigger render={stopButton} />
        <TooltipContent side="top">{stopTooltip}</TooltipContent>
      </Tooltip>
    );
  }

  const submitButton = (
    <Button size="icon-sm" disabled={disabled || loading} onClick={onClick}>
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
