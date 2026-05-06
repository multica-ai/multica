"use client";

// CEREBRO-PATCH(restricted-ref): cerebro modification of upstream file

import { Lock } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";

/**
 * RestrictedRef — placeholder rendered in place of an issue/project link
 * when the current user can't access the referenced item. Used by:
 *
 *   - issue tree (sub-issue in a restricted project the viewer isn't in)
 *   - parent breadcrumb (parent lives in a restricted project)
 *   - mention dropdown (search match the user can't link to)
 *
 * Renders no identifying information — no number, no title — only the
 * generic "Restricted" label and a tooltip explaining the model.
 */
export function RestrictedRef({
  variant = "inline",
  className,
}: {
  /** "inline" → small pill in a row; "block" → wider redaction stripe */
  variant?: "inline" | "block";
  className?: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            data-testid="restricted-ref"
            aria-label="Restricted item"
            className={cn(
              "inline-flex items-center gap-1.5 rounded text-xs",
              "bg-muted text-muted-foreground",
              variant === "inline" ? "px-1.5 py-0.5" : "px-2 py-1",
              "[background-image:repeating-linear-gradient(135deg,transparent_0_5px,rgba(0,0,0,0.04)_5px_10px)]",
              className,
            )}
          >
            <Lock className="size-3" />
            <span>Restricted</span>
          </span>
        }
      />
      <TooltipContent side="top">
        This item is in a project you don&rsquo;t have access to. Ask an admin
        to add you.
      </TooltipContent>
    </Tooltip>
  );
}
