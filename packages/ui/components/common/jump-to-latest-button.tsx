"use client";

import { ChevronDown } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

interface JumpToLatestButtonProps {
  visible: boolean;
  onClick: () => void;
  /** Optional label — defaults to a chevron-only pill. */
  label?: string;
  className?: string;
}

/**
 * Floating pill that jumps a scroll container to its newest content. Render
 * inside a `position: relative` parent (the scroll container's wrapper).
 */
export function JumpToLatestButton({
  visible,
  onClick,
  label,
  className,
}: JumpToLatestButtonProps) {
  return (
    <Button
      type="button"
      variant="secondary"
      size={label ? "sm" : "icon-sm"}
      onClick={onClick}
      aria-label={label ?? "Jump to latest"}
      className={cn(
        "absolute bottom-3 right-3 z-10 rounded-full shadow-md border bg-background/95 backdrop-blur transition-all",
        visible
          ? "opacity-100 translate-y-0 pointer-events-auto"
          : "opacity-0 translate-y-2 pointer-events-none",
        className,
      )}
    >
      {label && <span className="text-xs">{label}</span>}
      <ChevronDown className="size-4" />
    </Button>
  );
}
