"use client";

// CEREBRO-PATCH(privacy-toggle): cerebro modification of upstream file

import { Lock, Globe } from "lucide-react";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";

/**
 * PrivacyToggle — single toggle for the standalone-issue privacy flag.
 * Hidden when the issue belongs to a project (project access controls
 * take over there).
 *
 *   Off → "Visible to everyone in this workspace"
 *   On  → "Only you and workspace admins can see this issue"
 *
 * The help text always names the audience so the consequence is obvious.
 */
export function PrivacyToggle({
  isPrivate,
  onUpdate,
}: {
  isPrivate: boolean;
  onUpdate: (value: boolean) => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            data-testid="issue-privacy-toggle"
            aria-pressed={isPrivate}
            onClick={() => onUpdate(!isPrivate)}
            className="inline-flex items-center gap-1.5 rounded px-1 -mx-1 py-0.5 hover:bg-accent/40 transition-colors text-xs"
          >
            {isPrivate ? (
              <>
                <Lock className="size-3" />
                <span>Private</span>
              </>
            ) : (
              <>
                <Globe className="size-3 text-muted-foreground" />
                <span className="text-muted-foreground">Workspace</span>
              </>
            )}
          </button>
        }
      />
      <TooltipContent side="left">
        {isPrivate
          ? "Only you and workspace admins can see this issue."
          : "Visible to everyone in this workspace."}
      </TooltipContent>
    </Tooltip>
  );
}
