"use client";

import { cn } from "@multica/ui/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

/**
 * RestrictedDot — 6px filled dot rendered before a project name to indicate
 * that the project has access = 'restricted'. Quiet by design: this is a
 * state attribute (like priority), never an alarm. Tooltipped for context.
 *
 * Usage: place inline before the title.
 *   <RestrictedDot /> {project.title}
 */
export function RestrictedDot({ className }: { className?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            data-testid="restricted-dot"
            aria-label="Restricted project"
            className={cn(
              "inline-block size-1.5 shrink-0 rounded-full bg-muted-foreground/70 align-middle",
              className,
            )}
          />
        }
      />
      <TooltipContent side="top">
        Restricted — only selected members can see this project
      </TooltipContent>
    </Tooltip>
  );
}
