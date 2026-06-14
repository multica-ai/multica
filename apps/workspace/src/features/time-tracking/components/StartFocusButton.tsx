"use client";

import { Focus } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useRouter } from "@/shared/router";
import type { FocusMode, StartFocusRequest } from "@/shared/types";
import { useStartFocusMutation } from "../hooks/use-focus";

interface StartFocusButtonProps {
  issueId?: string | null;
  issueTitle?: string | null;
  description?: string;
  mode?: FocusMode;
  preset?: string;
  size?: "sm" | "icon-xs";
  variant?: "default" | "outline" | "ghost";
  showLabel?: boolean;
  navigateOnStart?: boolean;
  className?: string;
}

const defaultPresetByMode: Record<FocusMode, string> = {
  flowtime: "flowtime_default",
  pomodoro: "pomodoro_25_5",
  quick_start: "two_minute_start",
};

/** Starts a Focus session from task surfaces without duplicating start logic. */
export function StartFocusButton({
  issueId,
  issueTitle,
  description,
  mode = "flowtime",
  preset,
  size = "sm",
  variant = "outline",
  showLabel = true,
  navigateOnStart = true,
  className,
}: StartFocusButtonProps) {
  const router = useRouter();
  const startFocus = useStartFocusMutation();
  const label = showLabel ? "Start Focus" : "Focus";

  const handleStart = () => {
    const body: StartFocusRequest = {
      mode,
      preset: preset ?? defaultPresetByMode[mode],
      issue_id: issueId ?? null,
      description: description?.trim() || issueTitle?.trim() || undefined,
      timer_conflict_action: "stop_existing",
    };

    startFocus.mutate(body, {
      onSuccess: () => {
        toast.success("Focus started");
        if (navigateOnStart) {
          router.push("/focus");
        }
      },
      onError: (error) => {
        toast.error(error instanceof Error ? error.message : "Failed to start focus");
      },
    });
  };

  const button = (
    <Button
      type="button"
      variant={variant}
      size={size}
      className={className}
      disabled={startFocus.isPending}
      onClick={handleStart}
      aria-label={label}
    >
      <Focus className={showLabel ? "mr-1.5 size-3.5" : "size-3.5"} />
      {showLabel ? label : null}
    </Button>
  );

  if (showLabel) {
    return button;
  }

  return (
    <Tooltip>
      <TooltipTrigger render={button} />
      <TooltipContent>Start Focus</TooltipContent>
    </Tooltip>
  );
}
