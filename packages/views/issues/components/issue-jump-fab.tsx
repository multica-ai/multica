"use client";

import { ArrowDown, ArrowUp } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

interface IssueJumpFabProps {
  direction: "down" | "up";
  position: "bottom" | "top";
  onClick: () => void;
  label: string;
  className?: string;
}

export function IssueJumpFab({
  direction,
  position,
  onClick,
  label,
  className,
}: IssueJumpFabProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className={cn(
        "absolute left-1/2 z-20 inline-flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-border/70 bg-card/95 px-3 py-1.5 text-sm font-medium text-foreground shadow-md backdrop-blur transition hover:bg-accent hover:text-accent-foreground active:scale-95",
        position === "bottom" ? "bottom-4" : "top-4",
        className,
      )}
    >
      {direction === "down" ? <ArrowDown className="size-4" /> : <ArrowUp className="size-4" />}
      <span>{label}</span>
    </button>
  );
}
