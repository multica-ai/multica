"use client";

import { useCallback } from "react";
import { Play, Pause } from "lucide-react";
import { useTimerStore } from "@multica/core/time-entries/timer-store";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";

interface TimerPlayButtonProps {
  issueId: string;
  issueIdentifier: string;
  issueTitle: string;
  className?: string;
}

export function TimerPlayButton({
  issueId,
  issueIdentifier,
  issueTitle,
  className,
}: TimerPlayButtonProps) {
  const activeTimer = useTimerStore((s) => s.activeTimer);
  const startTimer = useTimerStore((s) => s.startTimer);
  const isActive = activeTimer?.issueId === issueId;
  const isOtherRunning = !!activeTimer && !isActive;

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (isActive) return;
      if (isOtherRunning) {
        toast.info(
          `Timer running on ${activeTimer!.issueIdentifier}. Stop it first.`,
        );
        return;
      }
      startTimer(issueId, issueIdentifier, issueTitle);
      toast.success(`Timer started for ${issueIdentifier}`);
    },
    [
      issueId,
      issueIdentifier,
      issueTitle,
      isActive,
      isOtherRunning,
      activeTimer,
      startTimer,
    ],
  );

  if (isActive) {
    return (
      <span
        className={cn(
          "inline-flex items-center justify-center rounded-full size-5 bg-red-500/10 text-red-500",
          className,
        )}
        title="Timer running"
      >
        <Pause className="size-2.5 fill-current" />
      </span>
    );
  }

  return (
    <button
      className={cn(
        "inline-flex items-center justify-center rounded-full size-5 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors",
        className,
      )}
      onClick={handleClick}
      title="Start timer"
    >
      <Play className="size-2.5 fill-current" />
    </button>
  );
}
