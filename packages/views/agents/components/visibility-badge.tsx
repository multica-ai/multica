"use client";

import { Globe, Lock } from "lucide-react";
import {
  VISIBILITY_LABEL,
  VISIBILITY_TOOLTIP,
} from "@multica/core/agents";
import type { AgentVisibility } from "@multica/core/types";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

/**
 * Read-only visibility badge — used wherever a user should *see* an agent's
 * visibility (Personal / Workspace) without being able to change it. Replaces
 * the interactive `<VisibilityPicker>` for non-managers on the detail page,
 * and is also the canonical badge for hover cards and list rows.
 *
 * `compact` drops the text label and shows just the icon — for tight spaces
 * like the agent table where the column header already labels the field.
 */
export function VisibilityBadge({
  value,
  compact = false,
  className = "",
}: {
  value: AgentVisibility;
  compact?: boolean;
  className?: string;
}) {
  const Icon = value === "private" ? Lock : Globe;
  const label = VISIBILITY_LABEL[value];
  const tooltip = VISIBILITY_TOOLTIP[value];

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className={`inline-flex items-center gap-1 text-xs text-muted-foreground ${className}`}
            aria-label={tooltip}
          >
            <Icon className="h-3 w-3 shrink-0" />
            {!compact && <span className="truncate">{label}</span>}
          </span>
        }
      />
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
