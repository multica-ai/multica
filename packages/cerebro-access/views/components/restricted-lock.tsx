"use client";

// CEREBRO-PATCH(restricted-lock): cerebro modification of upstream file

import { Lock } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

/**
 * RestrictedLock — small red lock rendered before a project name to mark a
 * project with access = 'restricted'. Replaces the earlier muted dot — red
 * was an explicit user request: privacy is a state worth seeing at a glance.
 *
 * Usage: place inline before the title.
 *   <RestrictedLock /> {project.title}
 */
export function RestrictedLock({ className }: { className?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            data-testid="restricted-lock"
            aria-label="Restricted project"
            className={cn(
              "inline-flex shrink-0 items-center justify-center align-middle text-destructive",
              className,
            )}
          >
            <Lock className="size-3.5" strokeWidth={2.25} aria-hidden />
          </span>
        }
      />
      <TooltipContent side="top">
        Restricted — only selected members can see this project
      </TooltipContent>
    </Tooltip>
  );
}
