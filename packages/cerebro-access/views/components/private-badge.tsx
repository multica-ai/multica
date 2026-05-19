"use client";

// CEREBRO-PATCH(private-badge): cerebro modification of upstream file — JEH-1750

import { Lock } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

/**
 * PrivateBadge — lock + "Private" label.
 *
 * Used on autopilot list rows, autopilot detail header, and issue header for
 * anything the backend marks `is_private`. The badge is purely informative —
 * the backend filters non-owners out before responses reach the client, so
 * seeing the badge means the viewer is the owner.
 *
 * `size="md"` matches autopilot row text; `size="sm"` matches breadcrumbs.
 */
export function PrivateBadge({
  size = "md",
  className,
}: {
  size?: "sm" | "md";
  className?: string;
}) {
  const isSmall = size === "sm";
  return (
    <span
      aria-label="Private"
      data-testid="private-badge"
      className={cn(
        "inline-flex items-center gap-1 rounded-sm border border-amber-500/30 bg-amber-500/10 px-1.5 font-medium text-amber-700 dark:text-amber-400",
        isSmall ? "h-4 text-[10px] py-0" : "h-5 text-[11px] py-0",
        className,
      )}
    >
      <Lock className={isSmall ? "h-2.5 w-2.5" : "h-3 w-3"} />
      <span>Private</span>
    </span>
  );
}
