"use client";

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

/**
 * The primary cell: leading icon, name, inline badges, then the description
 * in muted text filling whatever width is left — one line, never two.
 */
export function CollectionNameCell({
  icon,
  title,
  badges,
  description,
  trailing,
  muted = false,
  className,
}: {
  icon?: ReactNode;
  title: ReactNode;
  badges?: ReactNode;
  description?: ReactNode;
  trailing?: ReactNode;
  muted?: boolean;
  className?: string;
}) {
  return (
    <span className={cn("flex min-w-0 flex-1 items-center gap-2 whitespace-nowrap", className)}>
      {icon}
      <span
        className={cn(
          "min-w-0 truncate font-medium",
          muted && "text-muted-foreground",
        )}
      >
        {title}
      </span>
      {badges}
      {description ? (
        <span className="min-w-0 flex-1 basis-0 truncate text-muted-foreground">
          {description}
        </span>
      ) : (
        <span className="flex-1" />
      )}
      {trailing}
    </span>
  );
}

/** Small inline tag after a name ("You", "Archived"). */
export function CollectionTag({ children }: { children: ReactNode }) {
  return (
    <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium leading-4 text-muted-foreground">
      {children}
    </span>
  );
}

/** Placeholder for an empty cell. */
export function CollectionEmptyValue() {
  return <span className="text-faint-foreground">—</span>;
}
