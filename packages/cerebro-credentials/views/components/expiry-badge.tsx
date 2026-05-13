"use client";

import { AlertTriangle, Clock } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";

const SOON_THRESHOLD_DAYS = 14;

function daysUntil(iso: string): number {
  const target = new Date(iso).getTime();
  const now = Date.now();
  return Math.floor((target - now) / (1000 * 60 * 60 * 24));
}

export function ExpiryBadge({ expiresAt }: { expiresAt?: string }) {
  if (!expiresAt) {
    return (
      <span className="text-xs text-muted-foreground" data-testid="expiry-none">
        No expiry
      </span>
    );
  }
  const days = daysUntil(expiresAt);
  if (days < 0) {
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span data-testid="expiry-expired">
              <Badge variant="destructive" className="gap-1">
                <AlertTriangle className="size-3" />
                Expired
              </Badge>
            </span>
          }
        />
        <TooltipContent side="left">
          Expired {Math.abs(days)} day{Math.abs(days) === 1 ? "" : "s"} ago
        </TooltipContent>
      </Tooltip>
    );
  }
  if (days <= SOON_THRESHOLD_DAYS) {
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span data-testid="expiry-soon">
              <Badge
                className="gap-1 bg-amber-100 text-amber-900 dark:bg-amber-900/30 dark:text-amber-200"
              >
                <Clock className="size-3" />
                Expires in {days}d
              </Badge>
            </span>
          }
        />
        <TooltipContent side="left">
          {new Date(expiresAt).toLocaleDateString()}
        </TooltipContent>
      </Tooltip>
    );
  }
  return (
    <span className="text-xs text-muted-foreground" data-testid="expiry-ok">
      {new Date(expiresAt).toLocaleDateString()}
    </span>
  );
}
