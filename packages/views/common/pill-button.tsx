"use client";

import { cn } from "@multica/ui/lib/utils";

export function PillButton({
  children,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        // Mobile: bigger min-height + slightly more horizontal padding so
        // pills are real tap targets, not 24px-high microbuttons. Desktop
        // keeps the slim footprint via md: overrides.
        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs min-h-9 md:min-h-0 md:px-2.5 md:py-1",
        "hover:bg-accent/60 transition-colors cursor-pointer",
        "data-popup-open:bg-accent data-popup-open:text-accent-foreground",
        "disabled:cursor-not-allowed disabled:hover:bg-transparent",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}
